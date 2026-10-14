package middleware

import (
	"context"
	"errors"
	"sync"
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
