package bootstrap

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kalaGN/gin-finapi/app/configdata"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
	redisdriver "github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

func TestFallbackReporterSafeStructuredEvents(t *testing.T) {
	for _, reason := range []string{"missing", "invalid_record", "unavailable"} {
		t.Run(reason, func(t *testing.T) {
			var output bytes.Buffer
			log := logrus.New()
			log.SetOutput(&output)
			log.SetFormatter(&logrus.JSONFormatter{})
			log.SetLevel(logrus.DebugLevel)
			fallbackReporter(log, "credential")(reason)
			var event map[string]any
			if err := json.Unmarshal(output.Bytes(), &event); err != nil {
				t.Fatal(err)
			}
			level := "warning"
			if reason == "missing" {
				level = "debug"
			}
			if event["event"] != "config_source_fallback" || event["reason"] != reason || event["data_type"] != "credential" || event["level"] != level || len(event) != 6 {
				t.Fatal("回源日志缺失固定分类或存在额外字段")
			}
		})
	}
	fallbackReporter(nil, "credential")("unavailable")
}

func TestConfigurationModesConstructAndCloseWithoutConnecting(t *testing.T) {
	for _, mode := range []string{"redis-only", "mysql-no-sync", "mysql-sync", "bad-redis", "bad-mysql", "bad-cache"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.AppConfig{Redis: config.RedisConfig{Addr: "synthetic.invalid:6379", PoolSize: 1, Timeout: time.Second}, MySQL: config.MySQLConfig{Enabled: mode != "redis-only", Host: "synthetic.invalid", Port: 3306, Database: "synthetic", User: "synthetic", TLSMode: "disabled", ConnectTimeout: time.Second, QueryTimeout: time.Second, WriteTimeout: time.Second, MaxOpenConns: 1, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, SecretKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), SecretKeyID: "test-v1", SyncEnabled: mode == "mysql-sync", SyncInterval: time.Minute, SyncTimeout: time.Second}}
			switch mode {
			case "bad-redis":
				cfg.Redis.PoolSize = 0
			case "bad-mysql":
				cfg.MySQL.User = ""
			case "bad-cache":
				cfg.Cache.Concurrency = -1
			}
			runtime, err := newConfiguration(cfg, nil)
			if strings.HasPrefix(mode, "bad-") {
				if err == nil || runtime != nil {
					t.Fatal("无效装配配置未拒绝")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if (runtime.mysql != nil) != cfg.MySQL.Enabled || (runtime.syncer != nil) != cfg.MySQL.SyncEnabled || (runtime.publisher != nil) != cfg.MySQL.SyncEnabled {
				t.Fatal("配置开关未控制依赖装配")
			}
			if err := runtime.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			// Reader/worker可重复关闭，但现有Redis池重复关闭会显式返回ErrClosed。
			// 记录现有边界，不把未经确认的整体关闭幂等性当成需求。
			if err := runtime.close(context.Background()); !errors.Is(err, redisdriver.ErrClosed) {
				t.Fatalf("重复关闭应保留Redis已关闭错误，实际=%v", err)
			}
		})
	}
}

type configurationTestSink struct {
	mode   string
	called chan struct{}
}

func (s *configurationTestSink) Acquire(context.Context, time.Duration) (string, bool, error) {
	close(s.called)
	switch s.mode {
	case "failure":
		return "", false, errors.New("synthetic-private-storage-detail")
	case "canceled":
		return "", false, context.Canceled
	case "skipped":
		return "", false, nil
	}
	return "synthetic-token", true, nil
}
func (*configurationTestSink) Publish(context.Context, string, services.ConfigSnapshot) (int, error) {
	return 0, nil
}
func (*configurationTestSink) Release(context.Context, string) error { return nil }

type configurationLogWriter struct{ lines chan []byte }

func (w configurationLogWriter) Write(p []byte) (int, error) {
	select {
	case w.lines <- bytes.Clone(p):
	default:
	}
	return len(p), nil
}

func newConfigurationReaderForTest(t *testing.T, interval time.Duration, failure *atomic.Bool) *configdata.Reader {
	t.Helper()
	reader, err := configdata.New(configdata.Sources{
		Credentials: []configdata.Source[string, services.Credential]{func(context.Context, string) (services.Credential, bool, error) {
			if failure != nil && failure.Load() {
				return services.Credential{}, false, services.ErrAuthUnavailable
			}
			return services.Credential{Version: 1, Secret: "synthetic-secret", Status: 1}, true, nil
		}},
		Products: []configdata.Source[services.ProductKey, services.ProductAccess]{func(context.Context, services.ProductKey) (services.ProductAccess, bool, error) {
			return services.ProductAccess{Version: 1, Status: 1}, true, nil
		}},
		Maps: []configdata.Source[int, map[string]int]{func(context.Context, int) (map[string]int, bool, error) { return map[string]int{"a": 0}, true, nil }},
	}, config.CacheConfig{RefreshInterval: interval, Concurrency: 1}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reader.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return reader
}

func TestConfigurationWorkerLogsSafeSyncOutcomes(t *testing.T) {
	for _, mode := range []string{"success", "skipped", "failure", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			writer := configurationLogWriter{lines: make(chan []byte, 4)}
			log := logrus.New()
			log.SetOutput(writer)
			log.SetFormatter(&logrus.JSONFormatter{})
			log.SetLevel(logrus.DebugLevel)
			sink := &configurationTestSink{mode: mode, called: make(chan struct{})}
			syncer, err := configdata.NewSynchronizer(func(context.Context) (services.ConfigSnapshot, error) {
				return services.ConfigSnapshot{Credentials: map[string]services.Credential{}, Products: map[services.ProductKey]services.ProductAccess{}, Mappings: map[int]map[string]int{10001: {"a": 0}}}, nil
			}, sink, configdata.SyncOptions{Interval: time.Hour, Timeout: time.Second, ReleaseTimeout: time.Second}, nil)
			if err != nil {
				t.Fatal(err)
			}
			runtime := &configurationRuntime{reader: newConfigurationReaderForTest(t, time.Hour, nil), syncer: syncer}
			runtime.start(log)
			select {
			case <-sink.called:
			case <-time.After(time.Second):
				t.Fatal("首次同步未启动")
			}
			if mode == "canceled" {
				if err := runtime.stop(context.Background()); err != nil {
					t.Fatal(err)
				}
				select {
				case <-writer.lines:
					t.Fatal("正常取消被记录为异常")
				default:
				}
				return
			}
			select {
			case line := <-writer.lines:
				var row map[string]any
				if err := json.Unmarshal(line, &row); err != nil {
					t.Fatal(err)
				}
				want := map[string]string{"success": "info", "skipped": "debug", "failure": "error"}[mode]
				if row["event"] != "mysql_redis_sync" || row["level"] != want || strings.Contains(string(line), "private-storage") {
					t.Fatal("同步结果日志错误或泄露敏感信息")
				}
			case <-time.After(time.Second):
				t.Fatal("同步结果未记录")
			}
			if err := runtime.stop(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConfigurationRefreshReportsFailureWithoutLosingSnapshot(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "刷新成功", true: "临时失败保留快照"}[failed], func(t *testing.T) {
			var unavailable atomic.Bool
			reader := newConfigurationReaderForTest(t, 10*time.Millisecond, &unavailable)
			if _, err := reader.LoadCredential(context.Background(), "key"); err != nil {
				t.Fatal(err)
			}
			unavailable.Store(failed)
			writer := configurationLogWriter{lines: make(chan []byte, 4)}
			log := logrus.New()
			log.SetOutput(writer)
			log.SetLevel(logrus.DebugLevel)
			log.SetFormatter(&logrus.JSONFormatter{})
			runtime := &configurationRuntime{reader: reader}
			runtime.start(log)
			select {
			case line := <-writer.lines:
				var row map[string]any
				if err := json.Unmarshal(line, &row); err != nil {
					t.Fatal(err)
				}
				level := "debug"
				wantFailures := float64(0)
				if failed {
					level = "warning"
					wantFailures = 1
				}
				if row["level"] != level || row["checked"] != float64(1) || row["failed"] != wantFailures {
					t.Fatal("刷新日志分类或数量错误")
				}
			case <-time.After(time.Second):
				t.Fatal("缓存刷新任务未执行")
			}
			if _, err := reader.LoadCredential(context.Background(), "key"); err != nil {
				t.Fatal("刷新临时故障丢失快照")
			}
			if err := runtime.stop(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
