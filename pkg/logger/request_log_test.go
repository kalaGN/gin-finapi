package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kalaGN/gin-finapi/pkg/config"
	driver "github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

func TestAsyncWriter_RetriesAndDrops(t *testing.T) {
	for _, succeedAt := range []int{1, 2, 3, 0} {
		var calls, failures atomic.Int64
		w := newAsyncWriter(4, time.Second, 3, func(ctx context.Context, data []byte) error {
			if _, ok := ctx.Deadline(); !ok {
				t.Error("缺少写入超时")
			}
			n := calls.Add(1)
			if succeedAt > 0 && n == int64(succeedAt) {
				return nil
			}
			return errors.New("含敏感信息的模拟错误")
		}, func() error { return nil }, func(reason string, count int64) {
			if reason == "write_failed" {
				failures.Add(count)
			}
		})
		// 仅测试替换等待，避免依赖固定 sleep；发送逻辑仍执行真实尝试次数。
		w.wait = func(context.Context, time.Duration) error { return nil }
		if _, err := w.Write([]byte("record")); err != nil {
			t.Fatal(err)
		}
		if err := closeWithin(t, w); err != nil {
			t.Fatal(err)
		}
		want := succeedAt
		if want == 0 {
			want = 3
		}
		if calls.Load() != int64(want) || (succeedAt == 0) != (failures.Load() == 1) {
			t.Fatalf("尝试=%d 丢弃=%d", calls.Load(), failures.Load())
		}
		if w.queuedBytes.Load() != 0 {
			t.Fatal("处理结束未释放字节预算")
		}
	}
}

func TestAsyncWriter_ByteBudgetIncludesInflight(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var delivered, dropped atomic.Int64
	w := newAsyncWriter(4, time.Second, 1, func(context.Context, []byte) error {
		delivered.Add(1)
		close(entered)
		<-release
		return nil
	}, func() error { return nil }, func(_ string, n int64) { dropped.Add(n) })
	w.maxBytes = 4
	w.Write([]byte("first"))
	if w.queuedBytes.Load() != 0 {
		t.Fatal("过大记录未释放预算")
	}
	w.Write([]byte("one"))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker未启动")
	}
	w.Write([]byte("two"))
	if w.queuedBytes.Load() != 3 {
		t.Fatal("进行中记录的预算错误")
	}
	close(release)
	if err := closeWithin(t, w); err != nil {
		t.Fatal(err)
	}
	if delivered.Load() != 1 || dropped.Load() != 2 || w.queuedBytes.Load() != 0 {
		t.Fatal("字节预算未正确丢弃或释放")
	}
}

func TestAsyncWriter_FailureDoesNotStopNextRecord(t *testing.T) {
	var calls, delivered atomic.Int64
	w := newAsyncWriter(4, time.Second, 3, func(_ context.Context, data []byte) error {
		calls.Add(1)
		if string(data) == "bad" {
			return errors.New("failed")
		}
		delivered.Add(1)
		return nil
	}, func() error { return nil }, func(string, int64) {})
	w.wait = func(context.Context, time.Duration) error { return nil }
	w.Write([]byte("bad"))
	w.Write([]byte("good"))
	if err := closeWithin(t, w); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 4 || delivered.Load() != 1 {
		t.Fatal("失败阻塞后续记录")
	}
}

func TestAsyncWriter_NonBlockingFullQueueAndCopiesRecord(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls, dropped atomic.Int64
	w := newAsyncWriter(1, time.Second, 1, func(_ context.Context, data []byte) error {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		if string(data) != "one" && string(data) != "two" {
			t.Error("缓冲内容被修改")
		}
		return nil
	}, func() error { return nil }, func(_ string, n int64) { dropped.Add(n) })
	data := []byte("one")
	w.Write(data)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker未启动")
	}
	copy(data, "xxx")
	w.Write([]byte("two"))
	finished := make(chan struct{})
	go func() { w.Write([]byte("drop")); close(finished) }()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("满队列阻塞请求")
	}
	close(release)
	if err := closeWithin(t, w); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || dropped.Load() != 1 {
		t.Fatalf("写入=%d 丢弃=%d", calls.Load(), dropped.Load())
	}
}

func TestAsyncWriter_CloseCancellationAndConcurrentWrites(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	var closes atomic.Int64
	w := newAsyncWriter(8, time.Minute, 3, func(ctx context.Context, _ []byte) error {
		once.Do(func() { close(entered) })
		<-ctx.Done()
		return ctx.Err()
	}, func() error { closes.Add(1); return nil }, func(string, int64) {})
	w.Write([]byte("first"))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker未启动")
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); w.Write([]byte("record")) }()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("关闭错误=%v", err)
	}
	wg.Wait()
	if err := closeWithin(t, w); err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("closed"))
	if err := closeWithin(t, w); err != nil || closes.Load() != 1 {
		t.Fatal("关闭不幂等")
	}
	if w.queuedBytes.Load() != 0 {
		t.Fatal("关闭丢弃后未释放字节预算")
	}
}

func closeWithin(t *testing.T, w *asyncWriter) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return w.Close(ctx)
}

func TestRequestLogger_FileAppendsJSONAndKeepsErrorsSeparate(t *testing.T) {
	file := filepath.Join(t.TempDir(), "request.log")
	if err := os.WriteFile(file, []byte("previous\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var errorsOut bytes.Buffer
	base := logrus.New()
	base.SetOutput(&errorsOut)
	log, closeLog, err := NewRequestLogger(base, config.RequestLogConfig{Output: "file", File: file, QueueSize: 4}, config.RedisConfig{})
	if err != nil {
		t.Fatal(err)
	}
	log.WithFields(logrus.Fields{"success": 1, "charge": 0}).Info("Request completed")
	if err := closeLog(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != 2 || string(lines[0]) != "previous" || errorsOut.Len() != 0 {
		t.Fatal("文件覆盖或日志串流")
	}
	var result struct{ Success, Charge int }
	if err := json.Unmarshal(lines[1], &result); err != nil || result.Success != 1 || result.Charge != 0 {
		t.Fatal("JSON字段错误")
	}
}

func TestRequestLogger_InvalidBackendAndLegacyConfiguration(t *testing.T) {
	if _, _, err := NewRequestLogger(nil, config.RequestLogConfig{Output: "bad", QueueSize: 1}, config.RedisConfig{}); err == nil {
		t.Fatal("无效模式通过")
	}
	if _, _, err := NewRequestLogger(nil, config.RequestLogConfig{Output: "file", File: filepath.Join(t.TempDir(), "missing", "x"), QueueSize: 1}, config.RedisConfig{}); err == nil {
		t.Fatal("打开失败被忽略")
	}
	if _, _, err := NewRequestLogger(nil, config.RequestLogConfig{Output: "redis", QueueSize: 1}, config.RedisConfig{}); err == nil {
		t.Fatal("无效Redis配置通过")
	}
	base := logrus.New()
	got, closeLog, err := NewRequestLogger(base, config.RequestLogConfig{}, config.RedisConfig{})
	if err != nil || got != base || closeLog(context.Background()) != nil {
		t.Fatal("直接构造配置兼容失败")
	}
}

type listRecorder struct {
	key    string
	values []interface{}
	ctx    context.Context
	err    error
}

func (r *listRecorder) RPush(ctx context.Context, key string, values ...interface{}) *driver.IntCmd {
	r.key, r.values, r.ctx = key, values, ctx
	return driver.NewIntResult(1, r.err)
}

func TestPushRequestLog_OneJSONRecordAndFixedKey(t *testing.T) {
	ctx := context.Background()
	client := &listRecorder{}
	data := []byte("{\"success\":1,\"charge\":0}\n")
	if err := pushRequestLog(ctx, client, data); err != nil {
		t.Fatal(err)
	}
	if client.key != "request_log" || client.ctx != ctx || len(client.values) != 1 || client.values[0] != string(data[:len(data)-1]) {
		t.Fatal("Redis请求日志契约错误")
	}
	client.err = errors.New("模拟故障")
	if err := pushRequestLog(ctx, client, data); !errors.Is(err, client.err) {
		t.Fatal("写入错误被忽略")
	}
}
