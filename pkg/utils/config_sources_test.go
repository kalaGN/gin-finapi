package utils

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

func TestConfigSourcesIsolationAndSnapshot(t *testing.T) {
	ctx := StartConfigSources(context.Background())
	if StartConfigSources(ctx) != ctx {
		t.Fatal("重置了来源记录器")
	}
	SetConfigSource(ctx, "credentials", "redis")
	want := map[string]string{"credentials": "redis"}
	snapshot := ConfigSourcesSnapshot(ctx)
	snapshot["credentials"] = "mysql"
	SetConfigSource(ctx, "bad-kind", "redis")
	SetConfigSource(ctx, "product_map", "bad-source")
	SetConfigSource(context.Background(), "credentials", "mysql")
	if !reflect.DeepEqual(ConfigSourcesSnapshot(ctx), want) {
		t.Fatal("来源记录不隔离")
	}
	if len(ConfigSourcesSnapshot(StartConfigSources(context.Background()))) != 0 {
		t.Fatal("请求串数据")
	}
	SetConfigSource(ctx, "product_access", "memory")
	if _, ok := snapshot["product_access"]; ok {
		t.Fatal("快照被后续更新")
	}
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() { defer wg.Done(); SetConfigSource(ctx, "product_map", "mysql"); _ = ConfigSourcesSnapshot(ctx) }()
	}
	wg.Wait()
	if ConfigSourcesSnapshot(ctx)["product_map"] != "mysql" {
		t.Fatal("并发记录错误")
	}
}
