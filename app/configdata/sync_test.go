package configdata

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kalaGN/gin-finapi/app/services"
)

type fakeSyncSink struct {
	acquired                           bool
	acquireErr, publishErr, releaseErr error
	acquires, publishes, releases      atomic.Int64
}

func (f *fakeSyncSink) Acquire(ctx context.Context, lease time.Duration) (string, bool, error) {
	f.acquires.Add(1)
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	return "test-token", f.acquired, f.acquireErr
}
func (f *fakeSyncSink) Publish(ctx context.Context, token string, snapshot services.ConfigSnapshot) (int, error) {
	f.publishes.Add(1)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return 2, f.publishErr
}
func (f *fakeSyncSink) Release(context.Context, string) error { f.releases.Add(1); return f.releaseErr }
func syncSnapshot() services.ConfigSnapshot {
	digest := services.Digest("key")
	return services.ConfigSnapshot{Credentials: map[string]services.Credential{digest: {Version: 1, Secret: "secret", Status: 1}}, Products: map[services.ProductKey]services.ProductAccess{{Digest: digest, Pcode: 10001}: {Version: 1, Status: 1}}, Mappings: map[int]map[string]int{10001: {"a": 0}}}
}

func TestSynchronizerLeaseAndValidation(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		acquired, invalid              bool
		loadErr, publishErr            error
		loadCalls, publishes, releases int64
	}{
		{name: "正常", acquired: true, loadCalls: 1, publishes: 1, releases: 1},
		{name: "锁竞争"},
		{name: "源失败", acquired: true, loadErr: errors.New("load failed"), loadCalls: 1, releases: 1},
		{name: "无效快照", acquired: true, invalid: true, loadCalls: 1, releases: 1},
		{name: "发布失败", acquired: true, publishErr: errors.New("publish failed"), loadCalls: 1, publishes: 1, releases: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var loaded atomic.Int64
			var refreshed atomic.Int64
			sink := &fakeSyncSink{acquired: tc.acquired, publishErr: tc.publishErr}
			s, err := NewSynchronizer(func(context.Context) (services.ConfigSnapshot, error) {
				if sink.acquires.Load() != 1 {
					t.Error("先读取后加锁")
				}
				loaded.Add(1)
				if tc.invalid {
					return services.ConfigSnapshot{}, nil
				}
				return syncSnapshot(), tc.loadErr
			}, sink, SyncOptions{Interval: time.Minute, Timeout: time.Second, ReleaseTimeout: time.Second}, func(context.Context) { refreshed.Add(1) })
			if err != nil {
				t.Fatal(err)
			}
			stats, err := s.Run(context.Background())
			wantError := tc.invalid || tc.loadErr != nil || tc.publishErr != nil
			if (err != nil) != wantError || loaded.Load() != tc.loadCalls || sink.publishes.Load() != tc.publishes || sink.releases.Load() != tc.releases {
				t.Fatalf("load=%d publish=%d release=%d err=%v", loaded.Load(), sink.publishes.Load(), sink.releases.Load(), err)
			}
			if !tc.acquired && !stats.Skipped {
				t.Fatal("锁竞争未标记跳过")
			}
			if !wantError && tc.acquired {
				if stats.Credentials != 1 || stats.Products != 1 || stats.Mappings != 1 || stats.Removed != 2 || refreshed.Load() != 1 {
					t.Fatal("同步统计或刷新错误")
				}
			} else if refreshed.Load() != 0 {
				t.Fatal("失败后错误刷新")
			}
			if err := s.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSynchronizerStartCloseAndCancel(t *testing.T) {
	sink := &fakeSyncSink{acquired: true}
	reports := make(chan struct{}, 20)
	s, err := NewSynchronizer(func(ctx context.Context) (services.ConfigSnapshot, error) { return syncSnapshot(), nil }, sink, SyncOptions{Interval: 10 * time.Millisecond, Timeout: time.Second, ReleaseTimeout: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Start(func(SyncStats, error) {
		select {
		case reports <- struct{}{}:
		default:
		}
	})
	s.Start(nil)
	for range 2 {
		select {
		case <-reports:
		case <-time.After(time.Second):
			t.Fatal("未执行首次或周期同步")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	before := sink.acquires.Load()
	s.Start(nil)
	if _, err := s.Run(context.Background()); !errors.Is(err, context.Canceled) || sink.acquires.Load() != before {
		t.Fatal("关闭后仍执行同步")
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	blocked, err := NewSynchronizer(func(ctx context.Context) (services.ConfigSnapshot, error) {
		close(started)
		<-ctx.Done()
		return services.ConfigSnapshot{}, ctx.Err()
	}, sink, SyncOptions{Interval: time.Minute, Timeout: time.Minute, ReleaseTimeout: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	blocked.Start(nil)
	<-started
	if err := blocked.Close(ctx); err != nil {
		t.Fatal("关闭未取消 SQL 读取")
	}
}
