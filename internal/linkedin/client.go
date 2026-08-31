package linkedin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/tradelab/linkedin-profile-api/internal/profile"
	"golang.org/x/time/rate"
)

const (
	defaultBaseURL    = "https://www.linkedin.com"
	defaultDecoration = "com.linkedin.voyager.dash.deco.identity.profile.FullProfileWithEntities-93"
	maxResponseSize   = 8 << 20
	userAgent         = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
)

type ErrorKind string

const (
	ErrAuthentication ErrorKind = "authentication"
	ErrForbidden      ErrorKind = "forbidden"
	ErrNotFound       ErrorKind = "not_found"
	ErrRateLimited    ErrorKind = "rate_limited"
	ErrChallenge      ErrorKind = "challenge"
	ErrUpstream       ErrorKind = "upstream"
	ErrSchema         ErrorKind = "schema"
)

type Error struct {
	Kind       ErrorKind
	StatusCode int
	Message    string
	Cause      error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func KindOf(err error) ErrorKind {
	var linkedInErr *Error
	if errors.As(err, &linkedInErr) {
		return linkedInErr.Kind
	}
	return ErrUpstream
}

type Options struct {
	LIAT       string
	JSESSIONID string
	Decoration string
	BaseURL    string
	HTTPClient *http.Client
	Interval   time.Duration
	Cooldown   time.Duration
}

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	limiter    *rate.Limiter
	cooldown   time.Duration
	decoration string

	sessionMu sync.Mutex
	csrfToken string

	cooldownMu    sync.RWMutex
	cooldownUntil time.Time
}

func NewClient(opts Options) (*Client, error) {
	if strings.TrimSpace(opts.LIAT) == "" {
		return nil, errors.New("LinkedIn li_at cookie is required")
	}
	base := opts.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("parse LinkedIn base URL: %w", err)
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		jar, jarErr := cookiejar.New(nil)
		if jarErr != nil {
			return nil, fmt.Errorf("create cookie jar: %w", jarErr)
		}
		httpClient = &http.Client{
			Jar:     jar,
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConns:          10,
				MaxIdleConnsPerHost:   4,
				IdleConnTimeout:       60 * time.Second,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: 10 * time.Second,
			},
		}
	} else if httpClient.Jar == nil {
		jar, jarErr := cookiejar.New(nil)
		if jarErr != nil {
			return nil, fmt.Errorf("create cookie jar: %w", jarErr)
		}
		httpClient.Jar = jar
	}

	sessionURL := *baseURL
	sessionURL.Path = "/"
	cookies := []*http.Cookie{{
		Name:     "li_at",
		Value:    strings.Trim(strings.TrimSpace(opts.LIAT), `"`),
		Path:     "/",
		Secure:   sessionURL.Scheme == "https",
		HttpOnly: true,
	}}
	csrf := normalizeJSESSIONID(opts.JSESSIONID)
	if csrf != "" {
		cookies = append(cookies, &http.Cookie{
			Name:     "JSESSIONID",
			Value:    csrf,
			Path:     "/",
			Secure:   sessionURL.Scheme == "https",
			HttpOnly: true,
		})
	}
	httpClient.Jar.SetCookies(&sessionURL, cookies)

	interval := opts.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	cooldown := opts.Cooldown
	if cooldown <= 0 {
		cooldown = 15 * time.Minute
	}
	decoration := strings.TrimSpace(opts.Decoration)
	if decoration == "" {
		decoration = defaultDecoration
	}

	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
		limiter:    rate.NewLimiter(rate.Every(interval), 1),
		cooldown:   cooldown,
		decoration: decoration,
		csrfToken:  csrf,
	}, nil
}

func (c *Client) FetchProfile(ctx context.Context, publicIdentifier string) (profile.Profile, []string, error) {
	if err := c.checkCooldown(); err != nil {
		return profile.Profile{}, nil, err
	}
	if err := c.ensureCSRF(ctx); err != nil {
		return profile.Profile{}, nil, err
	}

	endpoint := c.resolve("/voyager/api/identity/dash/profiles")
	query := endpoint.Query()
	query.Set("q", "memberIdentity")
	query.Set("memberIdentity", publicIdentifier)
	query.Set("decorationId", c.decoration)
	endpoint.RawQuery = query.Encode()

	body, err := c.getJSON(ctx, endpoint)
	if err != nil {
		return profile.Profile{}, nil, err
	}
	parsed, warnings, err := ParseProfileView(publicIdentifier, body)
	if err != nil {
		return profile.Profile{}, nil, &Error{
			Kind:    ErrSchema,
			Message: "LinkedIn returned an unsupported profile response",
			Cause:   err,
		}
	}
	return parsed, warnings, nil
}

func (c *Client) ensureCSRF(ctx context.Context) error {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if c.csrfToken != "" {
		return nil
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("wait for LinkedIn rate limiter: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resolve("/feed/").String(), nil)
	if err != nil {
		return fmt.Errorf("create LinkedIn session request: %w", err)
	}
	c.setCommonHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &Error{Kind: ErrUpstream, Message: "could not initialize LinkedIn session", Cause: err}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	if isChallengeURL(resp.Request.URL) {
		c.startCooldown()
		return &Error{Kind: ErrChallenge, StatusCode: resp.StatusCode, Message: "LinkedIn requested an account challenge"}
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &Error{Kind: ErrAuthentication, StatusCode: resp.StatusCode, Message: "LinkedIn session is invalid or expired"}
	}

	for _, cookie := range c.httpClient.Jar.Cookies(c.baseURL) {
		if cookie.Name == "JSESSIONID" {
			c.csrfToken = normalizeJSESSIONID(cookie.Value)
			break
		}
	}
	if c.csrfToken == "" {
		return &Error{Kind: ErrAuthentication, Message: "LinkedIn did not establish a CSRF session; provide LINKEDIN_JSESSIONID"}
	}
	return nil
}

func (c *Client) getJSON(ctx context.Context, endpoint *url.URL) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("wait for LinkedIn rate limiter: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("create LinkedIn request: %w", err)
		}
		c.setCommonHeaders(req)
		req.Header.Set("csrf-token", c.csrfToken)
		req.Header.Set("x-restli-protocol-version", "2.0.0")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if attempt == 0 && ctx.Err() == nil {
				select {
				case <-time.After(250 * time.Millisecond):
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return nil, &Error{Kind: ErrUpstream, Message: "LinkedIn request failed", Cause: err}
		}

		body, readErr := readLimited(resp.Body, maxResponseSize)
		resp.Body.Close()
		if readErr != nil {
			return nil, &Error{Kind: ErrUpstream, StatusCode: resp.StatusCode, Message: "could not read LinkedIn response", Cause: readErr}
		}

		if resp.StatusCode >= 500 && attempt == 0 {
			lastErr = fmt.Errorf("LinkedIn returned %s", resp.Status)
			select {
			case <-time.After(250 * time.Millisecond):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if err := c.classifyResponse(resp, body); err != nil {
			return nil, err
		}
		return body, nil
	}
	return nil, &Error{Kind: ErrUpstream, Message: "LinkedIn request failed after retry", Cause: lastErr}
}

func (c *Client) classifyResponse(resp *http.Response, body []byte) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return &Error{Kind: ErrAuthentication, StatusCode: resp.StatusCode, Message: "LinkedIn session is invalid or expired"}
	case http.StatusForbidden:
		return &Error{Kind: ErrForbidden, StatusCode: resp.StatusCode, Message: "LinkedIn denied access to this profile"}
	case http.StatusNotFound:
		return &Error{Kind: ErrNotFound, StatusCode: resp.StatusCode, Message: "LinkedIn profile was not found"}
	case http.StatusTooManyRequests, 999:
		c.startCooldown()
		return &Error{Kind: ErrRateLimited, StatusCode: resp.StatusCode, Message: "LinkedIn rate limit or anti-automation protection was triggered"}
	case http.StatusBadRequest, http.StatusGone, http.StatusUnprocessableEntity:
		return &Error{Kind: ErrSchema, StatusCode: resp.StatusCode, Message: "LinkedIn rejected the configured profile query"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Error{Kind: ErrUpstream, StatusCode: resp.StatusCode, Message: fmt.Sprintf("LinkedIn returned HTTP %d", resp.StatusCode)}
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "json") || json.Valid(body) == false {
		text := strings.ToLower(string(body))
		if strings.Contains(text, "checkpoint") || strings.Contains(text, "challenge") || strings.Contains(text, "sign in") {
			c.startCooldown()
			return &Error{Kind: ErrChallenge, StatusCode: resp.StatusCode, Message: "LinkedIn requested an account challenge or login"}
		}
		return &Error{Kind: ErrSchema, StatusCode: resp.StatusCode, Message: "LinkedIn returned a non-JSON response"}
	}
	return nil
}

func (c *Client) setCommonHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.linkedin.normalized+json+2.1")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Li-Lang", "en_US")
}

func (c *Client) resolve(path string) *url.URL {
	resolved := *c.baseURL
	resolved.Path = path
	resolved.RawQuery = ""
	return &resolved
}

func (c *Client) checkCooldown() error {
	c.cooldownMu.RLock()
	until := c.cooldownUntil
	c.cooldownMu.RUnlock()
	if time.Now().Before(until) {
		return &Error{Kind: ErrRateLimited, Message: fmt.Sprintf("LinkedIn client is cooling down until %s", until.UTC().Format(time.RFC3339))}
	}
	return nil
}

func (c *Client) startCooldown() {
	c.cooldownMu.Lock()
	c.cooldownUntil = time.Now().Add(c.cooldown)
	c.cooldownMu.Unlock()
}

func normalizeJSESSIONID(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"`)
}

func isChallengeURL(value *url.URL) bool {
	if value == nil {
		return false
	}
	path := strings.ToLower(value.Path)
	return strings.Contains(path, "checkpoint") || strings.Contains(path, "challenge") || strings.Contains(path, "login")
}

func readLimited(reader io.ReadCloser, max int64) ([]byte, error) {
	limited := io.LimitReader(reader, max+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("response exceeds %d bytes", max)
	}
	return body, nil
}
