package logger

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kalaGN/gin-finapi/pkg/config"
	appredis "github.com/kalaGN/gin-finapi/pkg/redis"
	driver "github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

const requestLogKey = "request_log"

// NewRequestLogger 只为请求完成日志选择异步后端，应用内部日志不受影响。
func NewRequestLogger(base *logrus.Logger, cfg config.RequestLogConfig, redisCfg config.RedisConfig) (*logrus.Logger, func(context.Context) error, error) {
	if cfg.Output == "" {
		return base, func(context.Context) error { return nil }, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	var send func(context.Context, []byte) error
	var closeSink func() error
	attempts, timeout := 1, time.Second
	switch cfg.Output {
	case "file":
		f, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return nil, nil, errors.New("无法打开请求日志文件，请检查路径和权限")
		}
		send = func(_ context.Context, data []byte) error { _, err := f.Write(data); return err }
		closeSink = f.Close
	case "redis":
		client, err := appredis.NewClientWithConfig(redisCfg)
		if err != nil {
			return nil, nil, errors.New("无效的 charge Redis 配置")
		}
		send = func(ctx context.Context, data []byte) error {
			return pushRequestLog(ctx, client, data)
		}
		closeSink, attempts, timeout = client.Close, 3, redisCfg.Timeout
	}
	if base == nil {
		base = logrus.New()
		base.SetOutput(os.Stderr)
	}
	report := func(reason string, count int64) {
		// 不记录原始错误、Redis 地址或待写入的日志内容。
		base.WithFields(logrus.Fields{"reason": reason, "count": count}).Error("请求日志写入失败或丢弃")
	}
	w := newAsyncWriter(cfg.QueueSize, timeout, attempts, send, closeSink, report)
	if cfg.MaxBytes > 0 {
		w.maxBytes = cfg.MaxBytes
	}
	log := logrus.New()
	log.SetFormatter(RequestFormatter{})
	log.SetOutput(w)
	return log, w.Close, nil
}

type redisListWriter interface {
	RPush(context.Context, string, ...interface{}) *driver.IntCmd
}

func pushRequestLog(ctx context.Context, client redisListWriter, data []byte) error {
	return client.RPush(ctx, requestLogKey, strings.TrimSuffix(string(data), "\n")).Err()
}

type asyncWriter struct {
	queue       chan []byte
	mu          sync.RWMutex
	closed      bool
	closeOnce   sync.Once
	done        chan struct{}
	root        context.Context
	cancel      context.CancelFunc
	timeout     time.Duration
	attempts    int
	send        func(context.Context, []byte) error
	closeSink   func() error
	report      func(string, int64)
	wait        func(context.Context, time.Duration) error
	dropped     atomic.Int64
	queuedBytes atomic.Int64
	maxBytes    int64
	closeErr    error
}

func newAsyncWriter(size int, timeout time.Duration, attempts int, send func(context.Context, []byte) error, closeSink func() error, report func(string, int64)) *asyncWriter {
	root, cancel := context.WithCancel(context.Background())
	w := &asyncWriter{queue: make(chan []byte, size), done: make(chan struct{}), root: root, cancel: cancel,
		timeout: timeout, attempts: attempts, send: send, closeSink: closeSink, report: report, wait: waitRetry, maxBytes: 64 << 20}
	go w.run()
	return w
}

// Write 只复制并非阻塞入队，满队列和关闭后的调用不会影响 HTTP 响应。
func (w *asyncWriter) Write(data []byte) (int, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		w.dropped.Add(1)
		return len(data), nil
	}
	n := int64(len(data))
	if w.queuedBytes.Add(n) > w.maxBytes {
		w.queuedBytes.Add(-n)
		w.dropped.Add(1)
		return len(data), nil
	}
	record := append([]byte(nil), data...)
	select {
	case w.queue <- record:
	default:
		w.queuedBytes.Add(-n)
		w.dropped.Add(1)
	}
	return len(data), nil
}

func (w *asyncWriter) run() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer close(w.done)
	defer w.cancel()
	defer func() {
		w.reportDropped()
		if err := w.closeSink(); err != nil {
			w.closeErr = errors.New("请求日志后端关闭失败")
		}
	}()
	for {
		if w.root.Err() != nil {
			w.discardPending()
			return
		}
		select {
		case <-w.root.Done():
			w.discardPending()
			return
		case <-ticker.C:
			w.reportDropped()
		case record, ok := <-w.queue:
			if !ok {
				return
			}
			if !w.writeRecord(record) {
				w.report("write_failed", 1)
			}
			w.queuedBytes.Add(-int64(len(record)))
		}
	}
}

func (w *asyncWriter) writeRecord(record []byte) bool {
	for attempt := 0; attempt < w.attempts; attempt++ {
		if w.root.Err() != nil {
			return false
		}
		ctx, cancel := context.WithTimeout(w.root, w.timeout)
		err := w.send(ctx, record)
		cancel()
		if err == nil {
			return true
		}
		if attempt+1 < w.attempts {
			if err := w.wait(w.root, time.Duration(attempt+1)*100*time.Millisecond); err != nil {
				return false
			}
		}
	}
	return false
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (w *asyncWriter) reportDropped() {
	if n := w.dropped.Swap(0); n > 0 {
		w.report("queue_full_closed_or_byte_limit", n)
	}
}

func (w *asyncWriter) discardPending() {
	for {
		select {
		case record, ok := <-w.queue:
			if !ok {
				return
			}
			w.dropped.Add(1)
			w.queuedBytes.Add(-int64(len(record)))
		default:
			return
		}
	}
}

// Close 在调用方预算内排空，超时后取消发送；重复关闭安全。
func (w *asyncWriter) Close(ctx context.Context) error {
	w.closeOnce.Do(func() { w.mu.Lock(); w.closed = true; close(w.queue); w.mu.Unlock() })
	select {
	case <-w.done:
		return w.closeErr
	case <-ctx.Done():
		w.cancel()
		return ctx.Err()
	}
}
