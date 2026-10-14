package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/sirupsen/logrus"
)

func TestNewRouter_RequestLogFileBackend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	file := filepath.Join(t.TempDir(), "request.log")
	var appLog bytes.Buffer
	base := logrus.New()
	base.SetOutput(&appLog)
	cfg := config.AppConfig{
		MongoDB:    config.MongoDBConfig{DSN: "invalid-test-dsn", Database: "test", MaxPool: 1},
		Redis:      config.RedisConfig{Addr: "127.0.0.1:1", PoolSize: 1, Timeout: time.Second},
		RateLimit:  config.RateLimitConfig{Requests: 100, Window: time.Second},
		RequestLog: config.RequestLogConfig{Output: "file", File: file, QueueSize: 4},
	}
	router, closeDependencies, err := NewRouter(cfg, base)
	if err != nil {
		t.Fatal(err)
	}
	var closeErr error
	closeResources := sync.OnceFunc(func() { closeErr = closeDependencies(context.Background()) })
	t.Cleanup(func() {
		closeResources()
		if closeErr != nil {
			t.Error(closeErr)
		}
	})
	req := httptest.NewRequest("POST", "/api", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("HTTP=%d", w.Code)
	}
	closeResources()
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var row struct {
		Success, Charge int
		Method, Path    string
	}
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	if row.Success != 0 || row.Charge != 0 || row.Method != "POST" || row.Path != "/api" {
		t.Fatal("日志记录错误")
	}
	var fields map[string]interface{}
	var actual interface{}
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fields["response_data"], actual) || !reflect.DeepEqual(fields["input"], map[string]interface{}{}) || fields["status"] != float64(1001) {
		t.Fatal("完整请求/响应未进入文件后端")
	}
	if _, ok := fields["msg"]; ok {
		t.Fatal("日志顶层 msg 应删除")
	}
	if timestamp, ok := fields["time"].(float64); !ok || timestamp < 1000000000000 {
		t.Fatal("日志时间应为毫秒整数")
	}
	if appLog.Len() != 0 {
		t.Fatal("请求日志写入了内部日志后端")
	}
}
