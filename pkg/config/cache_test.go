package config

import (
	"testing"
	"time"
)

func TestCacheConfigDefaultsAndEnvironment(t *testing.T) {
	t.Setenv("REDIS_CACHE_ENABLED", "")
	names := []string{"REDIS_CACHE_REFRESH_INTERVAL", "REDIS_APIKEY_CACHE_MAX_ENTRIES", "REDIS_PRODUCT_AUTH_CACHE_MAX_ENTRIES", "REDIS_PRODUCT_MAP_CACHE_MAX_ENTRIES", "REDIS_CACHE_SYNC_CONCURRENCY"}
	for _, name := range names {
		t.Setenv(name, "")
	}
	cfg, err := loadCacheConfig()
	if err != nil || cfg != (CacheConfig{5 * time.Minute, 1000, 10000, 1000, 4, false}) {
		t.Fatalf("cfg=%v err=%v", cfg, err)
	}
	for i, name := range names {
		value := "2"
		if i == 0 {
			value = "2m"
		}
		t.Setenv(name, value)
	}
	cfg, err = loadCacheConfig()
	if err != nil || cfg != (CacheConfig{2 * time.Minute, 2, 2, 2, 2, false}) {
		t.Fatalf("cfg=%v err=%v", cfg, err)
	}
}

func TestCacheConfigRejectsInvalidEnvironment(t *testing.T) {
	for _, name := range []string{"REDIS_CACHE_REFRESH_INTERVAL", "REDIS_APIKEY_CACHE_MAX_ENTRIES", "REDIS_PRODUCT_AUTH_CACHE_MAX_ENTRIES", "REDIS_PRODUCT_MAP_CACHE_MAX_ENTRIES", "REDIS_CACHE_SYNC_CONCURRENCY"} {
		for _, value := range []string{"0", "-1", "invalid"} {
			t.Run(name+value, func(t *testing.T) {
				t.Setenv(name, value)
				previous := Config
				if err := Load(); err == nil {
					t.Fatal("invalid limit accepted")
				}
				if Config != previous {
					t.Fatal("invalid load published config")
				}
			})
		}
	}
	if err := (CacheConfig{Concurrency: -1}).WithDefaults().Validate(); err == nil {
		t.Fatal("negative configuration accepted")
	}
}

func TestCacheEnabledSwitch(t *testing.T) {
	for _, tc := range []struct {
		value             string
		disabled, invalid bool
	}{
		{"", false, false}, {"true", false, false}, {"false", true, false}, {"invalid", false, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("REDIS_CACHE_ENABLED", tc.value)
			cfg, err := loadCacheConfig()
			if (err != nil) != tc.invalid || err == nil && cfg.Disabled != tc.disabled {
				t.Fatalf("disabled=%v err=%v", cfg.Disabled, err)
			}
			if tc.invalid {
				previous := Config
				if err := Load(); err == nil || Config != previous {
					t.Fatal("invalid switch published configuration")
				}
			}
		})
	}
	if cfg := (CacheConfig{Disabled: true}).WithDefaults(); !cfg.Disabled {
		t.Fatal("defaults overrode disabled switch")
	}
}
