package redis

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/kalaGN/gin-finapi/pkg/config"
	driver "github.com/redis/go-redis/v9"
)

func TestParseProductMap(t *testing.T) {
	got, err := parseProductMap(`{"varA":0,"varB":5,"alias":0}`)
	if err != nil || !reflect.DeepEqual(got, map[string]int{"varA": 0, "varB": 5, "alias": 0}) {
		t.Fatalf("mapping=%v, err=%v", got, err)
	}
	for _, raw := range []string{
		"", "null", "[]", "{}", "{", `{"":0}`, `{" ":0}`,
		`{"a":null}`, `{"a":"0"}`, `{"a":-1}`, `{"a":1.5}`, `{"a":1e1}`,
		`{"a":9223372036854775807}`, `{"a":9223372036854775808}`,
		`{"a":0,"a":1}`, `{"a":0} {}`, `{"a":true}`,
	} {
		t.Run(raw, func(t *testing.T) {
			got, err := parseProductMap(raw)
			if !errors.Is(err, ErrProductMapping) || got != nil {
				t.Fatalf("mapping=%v, err=%v", got, err)
			}
		})
	}
}

func TestProductMaps_LoadClassifiesFailures(t *testing.T) {
	failure := errors.New("driver sensitive details")
	for _, tc := range []struct {
		name, raw       string
		driverErr, want error
	}{
		{"valid", `{"a":0}`, nil, nil},
		{"missing", "", driver.Nil, ErrProductMapping},
		{"invalid", "null", nil, ErrProductMapping},
		{"unavailable", "", failure, ErrMappingUnavailable},
		{"timeout", "", context.DeadlineExceeded, ErrMappingUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			maps := &ProductMaps{timeout: time.Second, hget: func(ctx context.Context, key, field string) (string, error) {
				if key != "mongo_map" || field != "10001" {
					t.Fatalf("HGET %s %s", key, field)
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("missing deadline")
				}
				return tc.raw, tc.driverErr
			}}
			_, err := maps.Load(context.Background(), 10001)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
			if tc.want == ErrMappingUnavailable && !errors.Is(err, tc.driverErr) {
				t.Fatal("driver cause lost")
			}
		})
	}
}

func TestProductMaps_CancellationAndFreshReads(t *testing.T) {
	calls := 0
	maps := &ProductMaps{timeout: time.Second, hget: func(ctx context.Context, _ string, field string) (string, error) {
		calls++
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if field == "10001" {
			return `{"a":0}`, nil
		}
		return `{"b":1}`, nil
	}}
	for _, pcode := range []int{10001, 10002, 10001} {
		if _, err := maps.Load(context.Background(), pcode); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 3 {
		t.Fatal("mapping reads were cached")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := maps.Load(ctx, 10001); !errors.Is(err, ErrMappingUnavailable) || !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	maps.timeout = time.Nanosecond
	maps.hget = func(ctx context.Context, _ string, _ string) (string, error) { <-ctx.Done(); return "", ctx.Err() }
	if _, err := maps.Load(context.Background(), 10001); !errors.Is(err, ErrMappingUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
}

func TestProductMaps_ClosesOwnedClient(t *testing.T) {
	if _, err := NewProductMaps(config.RedisConfig{}); err == nil {
		t.Fatal("invalid config accepted")
	}
	maps, err := NewProductMaps(config.RedisConfig{Addr: "127.0.0.1:1", PoolSize: 1, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := maps.Close(); err != nil {
		t.Fatal(err)
	}
	want := errors.New("close failed")
	maps.close = func() error { return want }
	if !errors.Is(maps.Close(), want) {
		t.Fatal("close error lost")
	}
}
