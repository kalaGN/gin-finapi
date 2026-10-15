package middleware

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRateLimiter_ExplicitLifecycle(t *testing.T) {
	limiter := NewRateLimiter(2, time.Hour)
	if limiter.started {
		t.Fatal("构造函数不应启动 worker")
	}
	limiter.Start()
	limiter.Start()
	if !limiter.started {
		t.Fatal("Start 未启动 worker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := limiter.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-limiter.done:
	default:
		t.Fatal("Close 未等待 worker 退出")
	}
	limiter.Start()
	if err := limiter.Close(ctx); err != nil {
		t.Fatal("重复关闭失败", err)
	}
}

func TestRateLimiter_CloseBeforeStart(t *testing.T) {
	limiter := NewRateLimiter(2, time.Hour)
	if err := limiter.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	limiter.Start()
	if limiter.started {
		t.Fatal("已关闭限流器不应启动 worker")
	}
	select {
	case <-limiter.done:
	default:
		t.Fatal("未启动的限流器关闭后仍等待 worker")
	}
}

func TestRateLimiter_CloseRespectsCallerBudget(t *testing.T) {
	limiter := NewRateLimiter(2, time.Hour)
	// 模拟 worker 尚未退出，确定性验证取消预算，不依赖 sleep。
	limiter.started = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// 清理持有访客锁也不能阻塞发送关闭信号或突破等待预算。
	limiter.mu.Lock()
	result := make(chan error, 1)
	go func() { result <- limiter.Close(ctx) }()
	var err error
	select {
	case err = <-result:
	case <-time.After(time.Second):
		err = errors.New("关闭等待访客锁，超出调用方预算")
	}
	limiter.mu.Unlock()
	close(limiter.done)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-limiter.stop:
	default:
		t.Fatal("超时关闭未发送停止信号")
	}
	if err := limiter.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRateLimiter_ConcurrentStartCloseAndRequests(t *testing.T) {
	limiter := NewRateLimiter(100, time.Hour)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limiter.Start()
			limiter.allow("192.0.2.1")
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := limiter.Close(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	select {
	case <-limiter.done:
	default:
		t.Fatal("并发关闭后 worker 仍存活")
	}
}

func TestRateLimiter_CleanupPreservesActiveVisitors(t *testing.T) {
	now := time.Now()
	limiter := NewRateLimiter(2, time.Minute)
	limiter.visitors["expired"] = &Visitor{lastSeen: now.Add(-3 * time.Minute)}
	limiter.visitors["boundary"] = &Visitor{lastSeen: now.Add(-2 * time.Minute)}
	limiter.visitors["active"] = &Visitor{lastSeen: now}
	limiter.cleanupExpired(now)
	if _, ok := limiter.visitors["expired"]; ok {
		t.Fatal("过期访客未删除")
	}
	if len(limiter.visitors) != 2 {
		t.Fatal("有效访客或边界访客被删除")
	}
	if err := limiter.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRateLimiter_FixedWindowDoesNotSlide(t *testing.T) {
	limiter := NewRateLimiter(2, 200*time.Millisecond)
	t.Cleanup(func() {
		if err := limiter.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	base := time.Unix(1000, 0)
	for _, tc := range []struct {
		offset time.Duration
		want   bool
	}{
		{0, true}, {120 * time.Millisecond, true}, {199 * time.Millisecond, false},
		{200 * time.Millisecond, true}, {201 * time.Millisecond, true}, {202 * time.Millisecond, false},
		{400 * time.Millisecond, true},
	} {
		if got := limiter.allowAt("192.0.2.1", base.Add(tc.offset)); got != tc.want {
			t.Errorf("时间=%v 放行=%v 期望=%v", tc.offset, got, tc.want)
		}
	}
}

func TestRateLimiter_FixedWindowIsIndependentPerIP(t *testing.T) {
	limiter := NewRateLimiter(1, time.Second)
	t.Cleanup(func() {
		if err := limiter.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	base := time.Unix(1000, 0)
	for _, tc := range []struct {
		ip     string
		offset time.Duration
		want   bool
	}{
		{"192.0.2.1", 0, true}, {"192.0.2.2", 500 * time.Millisecond, true},
		{"192.0.2.1", time.Second, true}, {"192.0.2.2", time.Second, false},
		{"192.0.2.2", 1500 * time.Millisecond, true},
	} {
		if got := limiter.allowAt(tc.ip, base.Add(tc.offset)); got != tc.want {
			t.Errorf("IP=%s 时间=%v 放行=%v", tc.ip, tc.offset, got)
		}
	}
}

func TestRateLimiter_ConcurrentWindowQuota(t *testing.T) {
	limiter := NewRateLimiter(10, time.Second)
	t.Cleanup(func() {
		if err := limiter.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	base := time.Unix(1000, 0)
	for _, now := range []time.Time{base, base.Add(time.Second)} {
		var allowed atomic.Int64
		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if limiter.allowAt("192.0.2.1", now) {
					allowed.Add(1)
				}
			}()
		}
		wg.Wait()
		if allowed.Load() != 10 {
			t.Fatalf("同窗口放行=%d", allowed.Load())
		}
	}
}

func (rl *RateLimiter) allowAt(ip string, now time.Time) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.allowLocked(ip, now)
}
