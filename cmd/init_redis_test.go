//go:build redisinit

package main

import (
	"context"
	"encoding/json"
	"errors"
	appredis "github.com/kalaGN/gin-finapi/pkg/redis"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type fakeRedisInitializer struct {
	calls      int
	added      bool
	err        error
	key, field string
	value      interface{}
}

func (fake *fakeRedisInitializer) HSetNX(ctx context.Context, key, field string, value interface{}) *redis.BoolCmd {
	fake.calls++
	fake.key, fake.field, fake.value = key, field, value
	return redis.NewBoolResult(fake.added, fake.err)
}

func TestSeedRedisMapping(t *testing.T) {
	failure := errors.New("write failed")
	for _, tc := range []struct {
		name  string
		pcode int
		added bool
		err   error
	}{
		{"new product", 10001, true, nil},
		{"another product", 10002, true, nil},
		{"existing product preserved", 10001, false, nil},
		{"Redis failure", 10001, false, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeRedisInitializer{added: tc.added, err: tc.err}
			added, err := seedRedisMapping(context.Background(), fake, tc.pcode)
			if added != tc.added || !errors.Is(err, tc.err) || fake.calls != 1 || fake.key != "mongo_map" {
				t.Fatalf("added=%v error=%v calls=%d key=%s", added, err, fake.calls, fake.key)
			}
			if tc.pcode == 10001 && fake.field != "10001" || tc.pcode == 10002 && fake.field != "10002" {
				t.Fatalf("field=%s", fake.field)
			}
			var mapping map[string]int
			if err := json.Unmarshal([]byte(fake.value.(string)), &mapping); err != nil {
				t.Fatal(err)
			}
			want := map[string]int{"var100001": 0, "var100002": 1, "var100003": 2, "var100004": 3, "var100005": 4, "var100006": 5}
			if !reflect.DeepEqual(mapping, want) {
				t.Fatalf("mapping=%v", mapping)
			}
		})
	}
}

func TestSeedRedisMappingRejectsInvalidPcode(t *testing.T) {
	for _, pcode := range []int{0, 10000, 100000} {
		fake := &fakeRedisInitializer{}
		if _, err := seedRedisMapping(context.Background(), fake, pcode); err == nil || fake.calls != 0 {
			t.Fatalf("pcode=%d err=%v calls=%d", pcode, err, fake.calls)
		}
	}
}

type fakeRedisAuthInitializer struct {
	keys     []string
	values   []string
	existing map[string]bool
	failAt   int
}

func (fake *fakeRedisAuthInitializer) SetNX(ctx context.Context, key string, value interface{}, ttl time.Duration) *redis.BoolCmd {
	fake.keys = append(fake.keys, key)
	fake.values = append(fake.values, value.(string))
	if ttl != 0 {
		panic("unexpected TTL")
	}
	if len(fake.keys) == fake.failAt {
		return redis.NewBoolResult(false, errors.New("sensitive-error"))
	}
	return redis.NewBoolResult(!fake.existing[key], nil)
}
func TestSeedRedisAuthSamples(t *testing.T) {
	key := appredis.APIKeyRedisKey("test-key")
	productKey := strings.Replace(key, "apikey:v1:", "apikey-product:v1:", 1) + ":10002"
	for _, existing := range []bool{false, true} {
		fake := &fakeRedisAuthInitializer{existing: map[string]bool{key: existing, productKey: existing}}
		added, productAdded, err := seedRedisAuthSamples(context.Background(), fake, 10002, "test-key", "secret\"with-escape")
		if err != nil || added == existing || productAdded == existing || !reflect.DeepEqual(fake.keys, []string{key, productKey}) {
			t.Fatal("incorrect sample writes")
		}
		var credential appredis.APIKeyCredential
		if err := json.Unmarshal([]byte(fake.values[0]), &credential); err != nil || credential.Secret != "secret\"with-escape" || credential.Status != 1 || credential.ExpiresAt != 0 {
			t.Fatal("invalid credential sample")
		}
		if fake.values[1] != `{"version":1,"status":1,"expires_at":0}` {
			t.Fatal("invalid product sample")
		}
	}
}
func TestSeedRedisAuthSamples_Failures(t *testing.T) {
	for _, tc := range []struct {
		pcode             int
		key, secret       string
		failAt, wantCalls int
	}{
		{10000, "key", "secret", 0, 0}, {10001, "", "secret", 0, 0}, {10001, "key", "", 0, 0}, {10001, "key", strings.Repeat("s", 257), 0, 0},
		{10001, "key", "secret", 1, 1}, {10001, "key", "secret", 2, 2},
	} {
		fake := &fakeRedisAuthInitializer{failAt: tc.failAt}
		_, _, err := seedRedisAuthSamples(context.Background(), fake, tc.pcode, tc.key, tc.secret)
		if err == nil || len(fake.keys) != tc.wantCalls || strings.Contains(err.Error(), "sensitive-error") {
			t.Fatal("invalid failure handling")
		}
	}
}
