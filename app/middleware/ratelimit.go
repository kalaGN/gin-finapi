package middleware

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimiter 按 IP 使用独立固定窗口计数。
type RateLimiter struct {
	visitors map[string]*Visitor
	mu       sync.RWMutex
	rate     int           // 每个时间窗口允许的请求数
	window   time.Duration // 时间窗口
	lifeMu   sync.Mutex    // 生命周期不等待访客清理持有的锁。
	started  bool
	closed   bool
	stop     chan struct{}
	done     chan struct{}
}

type Visitor struct {
	windowStart time.Time
	lastSeen    time.Time
	count       int
}

// NewRateLimiter 创建限流器
func NewRateLimiter(rate int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		visitors: make(map[string]*Visitor),
		rate:     rate,
		window:   window,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start 由装配层显式启动清理任务，重复启动及关闭后启动均无副作用。
func (rl *RateLimiter) Start() {
	rl.lifeMu.Lock()
	defer rl.lifeMu.Unlock()
	if rl.started || rl.closed {
		return
	}
	rl.started = true
	go rl.cleanupVisitors()
}

// Close 发出停止信号并在调用方预算内等待退出；未启动和重复关闭均安全。
func (rl *RateLimiter) Close(ctx context.Context) error {
	rl.lifeMu.Lock()
	if !rl.closed {
		rl.closed = true
		close(rl.stop)
		if !rl.started {
			close(rl.done)
		}
	}
	rl.lifeMu.Unlock()
	select {
	case <-rl.done:
		return nil
	default:
	}
	select {
	case <-rl.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RateLimit 限流中间件
func (rl *RateLimiter) RateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		if !rl.allow(ip) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"status": 429,
				"msg":    fmt.Sprintf("Rate limit exceeded. Max %d requests per %v", rl.rate, rl.window),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.allowLocked(ip, time.Now())
}

// allowLocked 的调用方必须持有访客锁；显式时间便于验证窗口边界。
func (rl *RateLimiter) allowLocked(ip string, now time.Time) bool {
	visitor, exists := rl.visitors[ip]

	if !exists || now.Sub(visitor.windowStart) >= rl.window {
		// 固定窗口从首次放行起计时，不随之后的请求向后滑动。
		rl.visitors[ip] = &Visitor{
			windowStart: now,
			lastSeen:    now,
			count:       1,
		}
		return true
	}

	if visitor.count >= rl.rate {
		return false
	}

	visitor.count++
	visitor.lastSeen = now
	return true
}

func (rl *RateLimiter) cleanupVisitors() {
	defer close(rl.done)
	ticker := time.NewTicker(rl.window)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stop:
			return
		case <-ticker.C:
			rl.cleanupExpired(time.Now())
		}
	}
}

func (rl *RateLimiter) cleanupExpired(now time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for ip, visitor := range rl.visitors {
		if now.Sub(visitor.lastSeen) > rl.window*2 {
			delete(rl.visitors, ip)
		}
	}
}
