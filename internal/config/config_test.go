package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("LINKEDIN_LI_AT", "session")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != "8080" || cfg.CacheTTL != 30*time.Minute || cfg.MaxConcurrent != 2 {
		t.Errorf("Load() defaults = %#v", cfg)
	}
}

func TestLoadRequiresProductionAPIKey(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("LINKEDIN_LI_AT", "session")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "APP_API_KEY") {
		t.Fatalf("Load() error = %v, want APP_API_KEY error", err)
	}
}

func TestLoadRejectsMissingLinkedInSession(t *testing.T) {
	clearConfigEnv(t)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "LINKEDIN_LI_AT") {
		t.Fatalf("Load() error = %v, want LINKEDIN_LI_AT error", err)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("LINKEDIN_LI_AT", "session")
	t.Setenv("CACHE_TTL", "tomorrow")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "CACHE_TTL") {
		t.Fatalf("Load() error = %v, want CACHE_TTL error", err)
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"APP_ENV",
		"PORT",
		"APP_API_KEY",
		"LINKEDIN_LI_AT",
		"LINKEDIN_JSESSIONID",
		"LINKEDIN_DECORATION_ID",
		"CACHE_TTL",
		"CACHE_MAX_ENTRIES",
		"MAX_CONCURRENT_SCRAPES",
		"RATE_LIMIT_RPM",
		"RATE_LIMIT_BURST",
		"LINKEDIN_REQUEST_INTERVAL",
		"REQUEST_TIMEOUT",
		"UPSTREAM_TIMEOUT",
		"LINKEDIN_COOLDOWN",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
}
