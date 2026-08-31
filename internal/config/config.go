package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment        string
	Port               string
	APIKey             string
	LinkedInLIAT       string
	LinkedInJSESSIONID string
	LinkedInDecoration string
	CacheTTL           time.Duration
	CacheMaxEntries    int
	MaxConcurrent      int
	RateLimitRPM       int
	RateLimitBurst     int
	LinkedInInterval   time.Duration
	RequestTimeout     time.Duration
	UpstreamTimeout    time.Duration
	Cooldown           time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		Environment:        env("APP_ENV", "development"),
		Port:               env("PORT", "8080"),
		APIKey:             strings.TrimSpace(os.Getenv("APP_API_KEY")),
		LinkedInLIAT:       strings.TrimSpace(os.Getenv("LINKEDIN_LI_AT")),
		LinkedInJSESSIONID: strings.TrimSpace(os.Getenv("LINKEDIN_JSESSIONID")),
		LinkedInDecoration: env("LINKEDIN_DECORATION_ID", "com.linkedin.voyager.dash.deco.identity.profile.FullProfileWithEntities-93"),
	}

	var err error
	if cfg.CacheTTL, err = durationEnv("CACHE_TTL", 30*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.CacheMaxEntries, err = intEnv("CACHE_MAX_ENTRIES", 200); err != nil {
		return Config{}, err
	}
	if cfg.MaxConcurrent, err = intEnv("MAX_CONCURRENT_SCRAPES", 2); err != nil {
		return Config{}, err
	}
	if cfg.RateLimitRPM, err = intEnv("RATE_LIMIT_RPM", 10); err != nil {
		return Config{}, err
	}
	if cfg.RateLimitBurst, err = intEnv("RATE_LIMIT_BURST", 3); err != nil {
		return Config{}, err
	}
	if cfg.LinkedInInterval, err = durationEnv("LINKEDIN_REQUEST_INTERVAL", 2*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.RequestTimeout, err = durationEnv("REQUEST_TIMEOUT", 25*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.UpstreamTimeout, err = durationEnv("UPSTREAM_TIMEOUT", 15*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.Cooldown, err = durationEnv("LINKEDIN_COOLDOWN", 15*time.Minute); err != nil {
		return Config{}, err
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.Port == "" {
		return errors.New("PORT cannot be empty")
	}
	if c.LinkedInLIAT == "" {
		return errors.New("LINKEDIN_LI_AT is required")
	}
	if strings.ContainsAny(c.LinkedInLIAT+c.LinkedInJSESSIONID+c.LinkedInDecoration, "\r\n") {
		return errors.New("LinkedIn session values contain invalid characters")
	}
	if c.Environment == "production" && c.APIKey == "" {
		return errors.New("APP_API_KEY is required when APP_ENV=production")
	}
	if c.CacheTTL <= 0 || c.CacheMaxEntries <= 0 {
		return errors.New("cache settings must be greater than zero")
	}
	if c.MaxConcurrent <= 0 || c.RateLimitRPM <= 0 || c.RateLimitBurst <= 0 {
		return errors.New("rate and concurrency settings must be greater than zero")
	}
	if c.LinkedInInterval < 0 || c.RequestTimeout <= 0 || c.UpstreamTimeout <= 0 || c.Cooldown <= 0 {
		return errors.New("timeout settings are invalid")
	}
	return nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func intEnv(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return value, nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration such as 15s or 5m: %w", key, err)
	}
	return value, nil
}
