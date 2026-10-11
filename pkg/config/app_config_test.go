package config

import (
	"strings"
	"testing"
	"time"
)

func TestAppConfigValidate_RequiredValues(t *testing.T) {
	valid := AppConfig{
		Server:    ServerConfig{Port: "8082"},
		MongoDB:   MongoDBConfig{DSN: "mongodb://example.invalid", Database: "airis"},
		Redis:     RedisConfig{Addr: "localhost:6379", PoolSize: 10, Timeout: time.Second},
		SecretKey: "secret",
		APIKey:    "test-key",
		RateLimit: RateLimitConfig{Requests: 5000, Window: time.Second},
	}

	tests := []struct {
		name    string
		mutate  func(*AppConfig)
		wantErr string
	}{
		{name: "valid"},
		{name: "missing Redis address", mutate: func(config *AppConfig) { config.Redis.Addr = "" }, wantErr: "Redis"},
		{name: "negative Redis database", mutate: func(config *AppConfig) { config.Redis.DB = -1 }, wantErr: "Redis"},
		{name: "zero Redis pool", mutate: func(config *AppConfig) { config.Redis.PoolSize = 0 }, wantErr: "Redis"},
		{name: "zero Redis timeout", mutate: func(config *AppConfig) { config.Redis.Timeout = 0 }, wantErr: "Redis"},
		{name: "zero rate", mutate: func(config *AppConfig) { config.RateLimit.Requests = 0 }, wantErr: "RATE_LIMIT_REQUESTS"},
		{name: "zero window", mutate: func(config *AppConfig) { config.RateLimit.Window = 0 }, wantErr: "RATE_LIMIT_WINDOW"},
		{name: "missing API key", mutate: func(config *AppConfig) { config.APIKey = "" }, wantErr: "API key"},
		{name: "missing port", mutate: func(config *AppConfig) { config.Server.Port = "" }, wantErr: "server port"},
		{name: "missing Mongo DSN", mutate: func(config *AppConfig) { config.MongoDB.DSN = "" }, wantErr: "MongoDB DSN"},
		{name: "missing Mongo database", mutate: func(config *AppConfig) { config.MongoDB.Database = "" }, wantErr: "MongoDB database"},
		{name: "missing secret", mutate: func(config *AppConfig) { config.SecretKey = "" }, wantErr: "secret key"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			if test.mutate != nil {
				test.mutate(&candidate)
			}
			err := candidate.Validate()
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error = %v, want text %q", err, test.wantErr)
			}
		})
	}
}

func TestLoad_ReadsEnvironmentAndDefaults(t *testing.T) {
	previous := Config
	t.Cleanup(func() { Config = previous })

	t.Setenv("SERVER_PORT", "")
	t.Setenv("RATE_LIMIT_REQUESTS", "")
	t.Setenv("RATE_LIMIT_WINDOW", "")
	t.Setenv("ENV", "test")
	t.Setenv("REDIS_ADDR", "localhost:6379")
	t.Setenv("REDIS_DB", "0")
	t.Setenv("REDIS_POOL_SIZE", "10")
	t.Setenv("REDIS_TIMEOUT", "")
	t.Setenv("MONGODB_DSN", "mongodb://example.invalid")
	t.Setenv("MONGODB_DATABASE", "airis_test")
	t.Setenv("MONGODB_COLLECTION", "")
	t.Setenv("MONGODB_MAX_POOL", "invalid")
	t.Setenv("MONGODB_MIN_POOL", "3")
	t.Setenv("SECRET_KEY", "test-secret")
	t.Setenv("API_KEY", "test-key")
	t.Setenv("PHONE_AES_KEY", "") // Legacy AES configuration is no longer required.

	if err := Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if Config.Server.Port != "8082" || Config.Server.Env != "test" {
		t.Fatalf("server config = %#v", Config.Server)
	}
	if Config.RateLimit.Requests != 5000 || Config.RateLimit.Window != time.Second {
		t.Fatalf("rate limit = %#v", Config.RateLimit)
	}
	if Config.MongoDB.Collection != "data_20251101_0" {
		t.Fatalf("collection = %q", Config.MongoDB.Collection)
	}
	if Config.MongoDB.MaxPool != 100 || Config.MongoDB.MinPool != 3 {
		t.Fatalf("pool config = max %d, min %d", Config.MongoDB.MaxPool, Config.MongoDB.MinPool)
	}
	if err := Config.Validate(); err != nil {
		t.Fatalf("loaded config Validate() error = %v", err)
	}
}

func TestLoad_RateLimitConfiguration(t *testing.T) {
	previous := Config
	t.Cleanup(func() { Config = previous })
	for _, tt := range []struct {
		name, requests, window, wantError string
	}{
		{"custom", "12", "250ms", ""},
		{"malformed requests", "oops", "1s", "RATE_LIMIT_REQUESTS"},
		{"fractional requests", "1.5", "1s", "RATE_LIMIT_REQUESTS"},
		{"overflow requests", "999999999999999999999999", "1s", "RATE_LIMIT_REQUESTS"},
		{"zero requests", "0", "1s", "RATE_LIMIT_REQUESTS"},
		{"negative requests", "-1", "1s", "RATE_LIMIT_REQUESTS"},
		{"malformed window", "12", "oops", "RATE_LIMIT_WINDOW"},
		{"missing window unit", "12", "5", "RATE_LIMIT_WINDOW"},
		{"zero window", "12", "0s", "RATE_LIMIT_WINDOW"},
		{"negative window", "12", "-1s", "RATE_LIMIT_WINDOW"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("RATE_LIMIT_REQUESTS", tt.requests)
			t.Setenv("RATE_LIMIT_WINDOW", tt.window)
			before := Config
			err := Load()
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("Load error = %v", err)
				}
				if Config != before {
					t.Fatal("invalid load replaced configuration")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if Config.RateLimit.Requests != 12 || Config.RateLimit.Window != 250*time.Millisecond {
				t.Fatalf("rate limit = %#v", Config.RateLimit)
			}
		})
	}
}

func TestLoad_RedisTimeout(t *testing.T) {
	previous := Config
	t.Cleanup(func() { Config = previous })
	for _, raw := range []string{"", "250ms", "0s", "-1s", "bad", "1"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("REDIS_TIMEOUT", raw)
			before := Config
			err := Load()
			if raw == "" || raw == "250ms" {
				if err != nil {
					t.Fatal(err)
				}
				want := time.Second
				if raw == "250ms" {
					want = 250 * time.Millisecond
				}
				if Config.Redis.Timeout != want {
					t.Fatalf("timeout=%v", Config.Redis.Timeout)
				}
			} else if err == nil || !strings.Contains(err.Error(), "REDIS_TIMEOUT") || Config != before {
				t.Fatalf("err=%v; config should be unchanged", err)
			}
		})
	}
}
