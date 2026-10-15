//go:build mysqlsync

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kalaGN/gin-finapi/app/configdata"
	"github.com/kalaGN/gin-finapi/app/services"
)

type testSyncSink struct{ calls int }

func (s *testSyncSink) Acquire(context.Context, time.Duration) (string, bool, error) {
	s.calls++
	return "token", true, nil
}
func (s *testSyncSink) Publish(context.Context, string, services.ConfigSnapshot) (int, error) {
	s.calls++
	return 0, nil
}
func (s *testSyncSink) Release(context.Context, string) error { s.calls++; return nil }
func TestMySQLSyncDryRunDoesNotUseRedis(t *testing.T) {
	for _, apply := range []bool{false, true} {
		sink := &testSyncSink{}
		load := func(context.Context) (services.ConfigSnapshot, error) {
			return services.ConfigSnapshot{Credentials: map[string]services.Credential{}, Products: map[services.ProductKey]services.ProductAccess{}, Mappings: map[int]map[string]int{10001: {"a": 0}}}, nil
		}
		stats, err := executeMySQLSync(context.Background(), apply, load, sink, configdata.SyncOptions{Interval: time.Minute, Timeout: time.Second, ReleaseTimeout: time.Second})
		if err != nil || stats.Mappings != 1 {
			t.Fatal("同步执行失败")
		}
		want := 0
		if apply {
			want = 3
		}
		if sink.calls != want {
			t.Fatal("dry-run 发生 Redis 操作或 apply 未发布")
		}
	}
}
func TestMySQLSyncSourceFailureDoesNotPublish(t *testing.T) {
	sink := &testSyncSink{}
	failure := errors.New("source failed")
	_, err := executeMySQLSync(context.Background(), true, func(context.Context) (services.ConfigSnapshot, error) { return services.ConfigSnapshot{}, failure }, sink, configdata.SyncOptions{Interval: time.Minute, Timeout: time.Second, ReleaseTimeout: time.Second})
	if !errors.Is(err, failure) || sink.calls != 2 {
		t.Fatal("读取失败后错误发布或未释放锁")
	}
}
