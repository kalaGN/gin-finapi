//go:build integration && redisinit

package main

import (
	"context"
	"os"
	"testing"
	"time"

	appredis "github.com/kalaGN/gin-finapi/pkg/redis"
	"github.com/redis/go-redis/v9"
)

func TestRedisMappingMigrationIntegration(t *testing.T) {
	addr := os.Getenv("FINAPI_TEST_REDIS_ADDR")
	if addr == "" {
		t.Fatal("FINAPI_TEST_REDIS_ADDR must identify a disposable Redis")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	const pcode = 98764
	target := appredis.ProductMapRedisKey(pcode)
	// Reserve both names without overwriting existing data; no FLUSHDB/FLUSHALL.
	source := "mongo_map"
	if n, err := client.Exists(ctx, source, target).Result(); err != nil || n != 0 {
		t.Fatalf("fixture names already exist or Redis unavailable: count=%d err=%v", n, err)
	}
	original := `{"custom":2}`
	if err := client.HSet(ctx, source, pcode, original).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Del(c, source, target).Err(); err != nil {
			t.Error(err)
		}
	})
	added, err := migrateRedisMapping(ctx, client, pcode)
	if err != nil || !added {
		t.Fatalf("first migration: added=%v err=%v", added, err)
	}
	check := func(want string) {
		t.Helper()
		v, err := client.Get(ctx, target).Result()
		if err != nil || v != want {
			t.Fatalf("target=%q err=%v", v, err)
		}
	}
	check(original)
	// Mutate only the isolated source: retry must preserve the original target.
	changed := `{"new":0}`
	if err := client.HSet(ctx, source, pcode, changed).Err(); err != nil {
		t.Fatal(err)
	}
	added, err = migrateRedisMapping(ctx, client, pcode)
	if err != nil || added {
		t.Fatalf("retry: added=%v err=%v", added, err)
	}
	check(original)
	v, err := client.HGet(ctx, source, "98764").Result()
	if err != nil || v != changed {
		t.Fatal("migration removed or changed source")
	}
	if err := client.Del(ctx, target).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.HSet(ctx, source, pcode, `{"invalid":null}`).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := migrateRedisMapping(ctx, client, pcode); err == nil {
		t.Fatal("invalid mapping migrated")
	}
	if n, err := client.Exists(ctx, target).Result(); err != nil || n != 0 {
		t.Fatal("invalid migration created target")
	}
}
