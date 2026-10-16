//go:build mysqlsync

package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kalaGN/gin-finapi/app/configdata"
	"github.com/kalaGN/gin-finapi/app/services"
)

type testSyncSink struct {
	calls                              int
	skip                               bool
	acquireErr, publishErr, releaseErr error
}

func (s *testSyncSink) Acquire(context.Context, time.Duration) (string, bool, error) {
	s.calls++
	return "token", !s.skip, s.acquireErr
}
func (s *testSyncSink) Publish(context.Context, string, services.ConfigSnapshot) (int, error) {
	s.calls++
	return 0, s.publishErr
}
func (s *testSyncSink) Release(context.Context, string) error { s.calls++; return s.releaseErr }
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

func TestMySQLSyncFailureAndLeaseMatrix(t *testing.T) {
	options := configdata.SyncOptions{Interval: time.Minute, Timeout: time.Second, ReleaseTimeout: time.Second}
	failure := errors.New("synthetic-failure")
	for _, tc := range []struct {
		name                                          string
		apply, invalid, skip                          bool
		sourceErr, acquireErr, publishErr, releaseErr error
		calls                                         int
		failed                                        bool
	}{
		{name: "dry-run源失败", sourceErr: failure, failed: true},
		{name: "dry-run坏快照", invalid: true, failed: true},
		{name: "apply坏快照", apply: true, invalid: true, calls: 2, failed: true},
		{name: "获取租约失败", apply: true, acquireErr: failure, calls: 1, failed: true},
		{name: "锁竞争跳过", apply: true, skip: true, calls: 1},
		{name: "发布失败仍释放", apply: true, publishErr: failure, calls: 3, failed: true},
		{name: "释放失败上报", apply: true, releaseErr: failure, calls: 3, failed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := &testSyncSink{skip: tc.skip, acquireErr: tc.acquireErr, publishErr: tc.publishErr, releaseErr: tc.releaseErr}
			loaded := 0
			stats, err := executeMySQLSync(context.Background(), tc.apply, func(context.Context) (services.ConfigSnapshot, error) {
				loaded++
				if tc.invalid || tc.sourceErr != nil {
					return services.ConfigSnapshot{}, tc.sourceErr
				}
				return services.ConfigSnapshot{Credentials: map[string]services.Credential{}, Products: map[services.ProductKey]services.ProductAccess{}, Mappings: map[int]map[string]int{}}, nil
			}, sink, options)
			if (err != nil) != tc.failed || sink.calls != tc.calls || stats.Skipped != tc.skip {
				t.Fatal("dry-run或apply失败路径不符合合同")
			}
			if (tc.skip || tc.acquireErr != nil) && loaded != 0 {
				t.Fatal("未获租约读取了快照")
			}
		})
	}
	if _, err := executeMySQLSync(context.Background(), false, nil, nil, options); err == nil {
		t.Fatal("缺失源未拒绝")
	}
	if _, err := executeMySQLSync(context.Background(), true, func(context.Context) (services.ConfigSnapshot, error) { return services.ConfigSnapshot{}, nil }, nil, options); err == nil {
		t.Fatal("缺失发布器未拒绝")
	}
}

func TestMySQLSyncCommandRejectsConfigBeforeConnecting(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".env", []byte("VALUE=\"synthetic-private-value"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runMySQLRedisSync(true); err == nil || strings.Contains(err.Error(), "private-value") {
		t.Fatal("损坏环境文件未安全拒绝")
	}
	if err := os.WriteFile(".env", nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MYSQL_ENABLED", "invalid")
	if err := runMySQLRedisSync(false); err == nil {
		t.Fatal("非法配置未拒绝")
	}
	t.Setenv("MYSQL_ENABLED", "false")
	if err := runMySQLRedisSync(true); err == nil {
		t.Fatal("关闭MySQL时不应同步")
	}
}
