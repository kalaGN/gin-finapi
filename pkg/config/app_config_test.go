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
