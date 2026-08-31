package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	clearConfigEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != "8080" || cfg.CacheTTL != 30*time.Minute || cfg.MaxConcurrent != 2 {
		t.Errorf("Load() defaults = %#v", cfg)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	clearConfigEnv(t)
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
