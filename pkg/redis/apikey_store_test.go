package redis

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
	driver "github.com/redis/go-redis/v9"
)

func TestAPIKeys_Load(t *testing.T) {
	for _, tc := range []struct {
		name, raw     string
		readErr, want error
	}{
		{name: "enabled", raw: `{"version":1,"secret":"secret","status":1,"expires_at":101}`},
		{name: "never expires", raw: `{"version":1,"secret":"secret","status":1,"expires_at":0}`},
		{name: "disabled", raw: `{"version":1,"secret":"secret","status":0,"expires_at":0}`, want: ErrAPIKeyInvalid},
		{name: "expiration boundary", raw: `{"version":1,"secret":"secret","status":1,"expires_at":100}`, want: ErrAPIKeyInvalid},
		{name: "missing", readErr: driver.Nil, want: ErrAPIKeyInvalid},
		{name: "network", readErr: errors.New("sensitive driver error"), want: ErrAuthUnavailable},
		{name: "corrupt", raw: `{`, want: ErrAuthUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			keys := &APIKeys{timeout: time.Second, now: func() time.Time { return time.Unix(100, 0) }, get: func(ctx context.Context, key string) (string, error) {
				calls++
				if key != APIKeyRedisKey("key") || strings.Contains(key, "secret") {
					t.Fatal("incorrect lookup key")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("missing deadline")
				}
				return tc.raw, tc.readErr
			}}
			for i := 0; i < 2; i++ {
				credential, err := keys.Load(context.Background(), "key")
				if !errors.Is(err, tc.want) {
					t.Fatalf("err=%v want=%v", err, tc.want)
				}
				if err == nil && credential.Secret != "secret" {
					t.Fatal("wrong secret")
				}
			}
			if calls != 2 {
				t.Fatal("credentials were cached")
			}
		})
	}
}

func TestAPIKeys_LoadProductReadsSourceAndDeniesDisabled(t *testing.T) {
	raw := `{"version":1,"status":1,"expires_at":0}`
	calls := 0
	now := time.Unix(100, 0)
	keys := &APIKeys{
		timeout: time.Second,
		now:     func() time.Time { return now },
		get: func(_ context.Context, key string) (string, error) {
			calls++
			if key != APIKeyProductRedisKey("client-key", 10001) {
				t.Fatalf("unexpected Redis key: %s", key)
			}
			return raw, nil
		},
	}
	for i := 0; i < 2; i++ {
		if err := keys.LoadProduct(context.Background(), "client-key", 10001); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("Redis reads = %d, want 2", calls)
	}
	now = now.Add(5 * time.Minute)
	raw = `{"version":1,"status":0,"expires_at":0}`
	if err := keys.LoadProduct(context.Background(), "client-key", 10001); !errors.Is(err, services.ErrProductDenied) {
		t.Fatalf("LoadProduct() error = %v, want product denial", err)
	}
	if calls != 3 {
		t.Fatalf("Redis reads = %d, want 3", calls)
	}
}

func TestParseProductAccessRejectsInvalidRecords(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`, `{}`, `{"version":1,"status":1,"expires_at":0}{}`,
		`{"version":1,"status":null,"expires_at":0}`,
		`{"version":1,"status":2,"expires_at":0}`,
		`{"version":1,"version":1,"status":1,"expires_at":0}`,
		`{"version":1,"status":1,"expires_at":-1}`,
		`{"version":1,"status":1,"expires_at":0,"extra":true}`,
	} {
		if _, err := parseProductAccess(raw); err == nil {
			t.Fatalf("accepted invalid product authorization record: %s", raw)
		}
	}
}

func TestReadSourcesPreserveStatusAndClassifyInvalid(t *testing.T) {
	for _, product := range []bool{false, true} {
		for _, tc := range []struct {
			raw            string
			driverErr      error
			found, invalid bool
		}{
			{`{"version":1,"secret":"secret","status":0,"expires_at":1}`, nil, true, false},
			{"", driver.Nil, false, false},
			{"{", nil, false, true},
			{"", errors.New("sensitive driver error"), false, false},
		} {
			raw := tc.raw
			if product && tc.found {
				raw = `{"version":1,"status":0,"expires_at":1}`
			}
			keys := &APIKeys{timeout: time.Second, get: func(context.Context, string) (string, error) { return raw, tc.driverErr }}
			var found bool
			var err error
			if product {
				var v services.ProductAccess
				v, found, err = keys.ReadProductAccess(context.Background(), services.ProductKey{Digest: services.Digest("key"), Pcode: 10001})
				if found && (v.Status != 0 || v.ExpiresAt != 1) {
					t.Fatal("source erased refusal")
				}
			} else {
				var v services.Credential
				v, found, err = keys.ReadCredential(context.Background(), services.Digest("key"))
				if found && (v.Status != 0 || v.ExpiresAt != 1) {
					t.Fatal("source erased refusal")
				}
			}
			if found != tc.found || errors.Is(err, services.ErrInvalidRecord) != tc.invalid {
				t.Fatalf("found=%v err=%v", found, err)
			}
			if err != nil && !errors.Is(err, services.ErrAuthUnavailable) {
				t.Fatal("wrong source error")
			}
			if tc.driverErr != nil && tc.driverErr != driver.Nil && err == nil {
				t.Fatal("source swallowed failure")
			}
		}
	}
}

func TestParseAPIKeyCredential_RejectsInvalidRecords(t *testing.T) {
	valid := `{"version":1,"secret":"secret","status":1,"expires_at":0}`
	for _, raw := range []string{
		`null`, `[]`, valid + `{}`, strings.Replace(valid, `"version":1`, `"version":2`, 1),
		strings.Replace(valid, `"status":1`, `"status":null`, 1), strings.Replace(valid, `"status":1`, `"status":3`, 1),
		strings.Replace(valid, `"status":1`, `"status":"1"`, 1), strings.Replace(valid, `"version":1`, `"version":1.0`, 1),
		strings.Replace(valid, `"expires_at":0`, `"expires_at":-1`, 1), strings.Replace(valid, `"secret":"secret"`, `"secret":""`, 1),
		strings.Replace(valid, `"secret":"secret"`, `"secret":"`+strings.Repeat("x", 257)+`"`, 1),
		`{"version":1,"version":1,"secret":"s","status":1,"expires_at":0}`,
		`{"version":1,"secret":"s","status":1}`, strings.Replace(valid, `{`, `{"unknown":1,`, 1), strings.Repeat(" ", 4097),
	} {
		if _, err := parseAPIKeyCredential(raw); !errors.Is(err, ErrAuthUnavailable) {
			t.Fatalf("accepted invalid record")
		}
	}
}

func TestAPIKeys_ContextAndConstruction(t *testing.T) {
	if _, err := NewAPIKeys(config.RedisConfig{}); err == nil {
		t.Fatal("accepted invalid configuration")
	}
	keys, err := NewAPIKeys(config.RedisConfig{Addr: "127.0.0.1:1", PoolSize: 1, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := keys.Close(); err != nil {
		t.Fatal(err)
	}
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		defer cancel()
		keys := &APIKeys{timeout: time.Millisecond, now: time.Now, get: func(ctx context.Context, _ string) (string, error) { <-ctx.Done(); return "", ctx.Err() }}
		if _, err := keys.Load(ctx, "key"); !errors.Is(err, ErrAuthUnavailable) {
			t.Fatal("cancellation did not fail closed")
		}
	}
	if APIKeyRedisKey("key") == APIKeyRedisKey("KEY") || APIKeyRedisKey("key") == APIKeyRedisKey(" key") {
		t.Fatal("key normalized")
	}
}
