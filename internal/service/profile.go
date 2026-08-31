package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tradelab/linkedin-profile-api/internal/profile"
	"golang.org/x/sync/singleflight"
)

var publicIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{1,99}$`)

var (
	ErrInvalidURL     = errors.New("invalid LinkedIn profile URL")
	ErrInvalidSession = errors.New("invalid LinkedIn session")
	ErrBusy           = errors.New("profile service is at capacity")
)

type Fetcher interface {
	FetchProfile(context.Context, string) (profile.Profile, []string, error)
}

type FetcherFactory interface {
	NewFetcher(Session) (Fetcher, error)
}

type FetcherFactoryFunc func(Session) (Fetcher, error)

func (f FetcherFactoryFunc) NewFetcher(session Session) (Fetcher, error) {
	return f(session)
}

type Session struct {
	LIAT       string
	JSESSIONID string
}

type ProfileService struct {
	factory   FetcherFactory
	cache     *ttlCache
	group     singleflight.Group
	semaphore chan struct{}
}

func NewProfileService(factory FetcherFactory, cacheTTL time.Duration, cacheMaxEntries, maxConcurrent int) *ProfileService {
	return &ProfileService{
		factory:   factory,
		cache:     newTTLCache(cacheTTL, cacheMaxEntries),
		semaphore: make(chan struct{}, maxConcurrent),
	}
}

func (s *ProfileService) Get(ctx context.Context, rawURL string, session Session) (profile.Result, error) {
	publicIdentifier, err := ParseProfileURL(rawURL)
	if err != nil {
		return profile.Result{}, err
	}
	if err := validateSession(session); err != nil {
		return profile.Result{}, err
	}
	cacheKey := sessionCacheKey(session, publicIdentifier)
	if cached, ok := s.cache.get(cacheKey); ok {
		cached.Meta.CacheHit = true
		return cached, nil
	}

	resultCh := s.group.DoChan(cacheKey, func() (any, error) {
		if cached, ok := s.cache.get(cacheKey); ok {
			cached.Meta.CacheHit = true
			return cached, nil
		}
		select {
		case s.semaphore <- struct{}{}:
			defer func() { <-s.semaphore }()
		default:
			return profile.Result{}, ErrBusy
		}

		fetcher, factoryErr := s.factory.NewFetcher(session)
		if factoryErr != nil {
			return profile.Result{}, fmt.Errorf("%w: could not initialize session", ErrInvalidSession)
		}
		fetched, warnings, fetchErr := fetcher.FetchProfile(ctx, publicIdentifier)
		if fetchErr != nil {
			return profile.Result{}, fetchErr
		}
		now := time.Now().UTC()
		result := profile.Result{
			SchemaVersion: profile.SchemaVersion,
			Profile:       fetched,
			Meta: profile.Meta{
				FetchedAt: now,
				CacheHit:  false,
				Partial:   len(warnings) > 0,
				Warnings:  warnings,
			},
		}
		s.cache.set(cacheKey, result)
		return result, nil
	})
	select {
	case result := <-resultCh:
		if result.Err != nil {
			return profile.Result{}, result.Err
		}
		return result.Val.(profile.Result), nil
	case <-ctx.Done():
		return profile.Result{}, ctx.Err()
	}
}

func validateSession(session Session) error {
	liAt := strings.TrimSpace(session.LIAT)
	jsessionID := strings.TrimSpace(session.JSESSIONID)
	if liAt == "" {
		return fmt.Errorf("%w: li_at is required", ErrInvalidSession)
	}
	if len(liAt) > 4096 || len(jsessionID) > 1024 {
		return fmt.Errorf("%w: cookie value is too long", ErrInvalidSession)
	}
	if strings.ContainsAny(liAt+jsessionID, "\r\n") {
		return fmt.Errorf("%w: cookie contains invalid characters", ErrInvalidSession)
	}
	return nil
}

func sessionCacheKey(session Session, publicIdentifier string) string {
	fingerprint := sha256.Sum256([]byte(strings.TrimSpace(session.LIAT)))
	return fmt.Sprintf("%x:%s", fingerprint, publicIdentifier)
}

func ParseProfileURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: URL is required", ErrInvalidURL)
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return "", fmt.Errorf("%w: malformed URL", ErrInvalidURL)
	}
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("%w: URL must use HTTPS", ErrInvalidURL)
	}
	if parsed.User != nil || parsed.Port() != "" {
		return "", fmt.Errorf("%w: user information and ports are not allowed", ErrInvalidURL)
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" || net.ParseIP(host) != nil || (host != "linkedin.com" && !strings.HasSuffix(host, ".linkedin.com")) {
		return "", fmt.Errorf("%w: host must be linkedin.com", ErrInvalidURL)
	}

	cleanPath := path.Clean(parsed.EscapedPath())
	parts := strings.Split(strings.Trim(cleanPath, "/"), "/")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "in" {
		return "", fmt.Errorf("%w: expected /in/{public-identifier}", ErrInvalidURL)
	}
	publicIdentifier, err := url.PathUnescape(parts[1])
	if err != nil || !publicIdentifierPattern.MatchString(publicIdentifier) {
		return "", fmt.Errorf("%w: invalid public identifier", ErrInvalidURL)
	}
	return strings.ToLower(publicIdentifier), nil
}

type cacheEntry struct {
	result    profile.Result
	expiresAt time.Time
	createdAt time.Time
}

type ttlCache struct {
	mu         sync.Mutex
	ttl        time.Duration
	maxEntries int
	entries    map[string]cacheEntry
}

func newTTLCache(ttl time.Duration, maxEntries int) *ttlCache {
	return &ttlCache{
		ttl:        ttl,
		maxEntries: maxEntries,
		entries:    make(map[string]cacheEntry),
	}
}

func (c *ttlCache) get(key string) (profile.Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return profile.Result{}, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return profile.Result{}, false
	}
	return entry.result, true
}

func (c *ttlCache) set(key string, result profile.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for existingKey, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, existingKey)
		}
	}
	if len(c.entries) >= c.maxEntries {
		var oldestKey string
		var oldestTime time.Time
		for existingKey, entry := range c.entries {
			if oldestKey == "" || entry.createdAt.Before(oldestTime) {
				oldestKey = existingKey
				oldestTime = entry.createdAt
			}
		}
		delete(c.entries, oldestKey)
	}
	c.entries[key] = cacheEntry{
		result:    result,
		expiresAt: now.Add(c.ttl),
		createdAt: now,
	}
}
