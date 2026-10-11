//go:build redisinit

package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

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
