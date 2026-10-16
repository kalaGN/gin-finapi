package utils

import (
	"context"
	"sync"
	"time"
)

type timingKey struct{}

type timing struct {
	mu     sync.Mutex
	start  time.Time
	now    func() time.Time
	points map[string]int64
}

// StartTiming 为当前操作建立累计毫秒计时器；已有计时器时保留原起点。
// 派生 Context 保留取消、截止时间和原有值，不保存请求内容。
func StartTiming(ctx context.Context) context.Context {
	if _, ok := ctx.Value(timingKey{}).(*timing); ok {
		return ctx
	}
	return context.WithValue(ctx, timingKey{}, &timing{
		start: time.Now(), now: time.Now, points: map[string]int64{"entry": 0},
	})
}

// MarkTiming 记录距入口的累计毫秒数；同名节点仅记录首次，空名和entry保留名不覆盖。
// name应来自固定代码常量，不应使用手机号、凭据等请求输入。未启动计时时为空操作。
func MarkTiming(ctx context.Context, name string) {
	timer, ok := ctx.Value(timingKey{}).(*timing)
	if !ok || name == "" || name == "entry" {
		return
	}
	timer.mu.Lock()
	defer timer.mu.Unlock()
	if _, exists := timer.points[name]; !exists {
		timer.points[name] = timer.now().Sub(timer.start).Milliseconds()
	}
}

// TimingSnapshot 返回独立快照，调用方修改或后续标记均不会影响对方；未启动时返回nil。
func TimingSnapshot(ctx context.Context) map[string]int64 {
	timer, ok := ctx.Value(timingKey{}).(*timing)
	if !ok {
		return nil
	}
	timer.mu.Lock()
	defer timer.mu.Unlock()
	snapshot := make(map[string]int64, len(timer.points))
	for name, value := range timer.points {
		snapshot[name] = value
	}
	return snapshot
}
