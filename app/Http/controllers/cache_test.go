package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/configdata"
	"github.com/kalaGN/gin-finapi/app/middleware"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/kalaGN/gin-finapi/routes"
	"github.com/sirupsen/logrus"
)

func TestCacheHandlerClearResponseAndSafeAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if _, err := NewCacheHandler(nil, nil); err == nil {
		t.Fatal("缺失依赖应拒绝")
	}
	for _, failed := range []bool{false, true} {
		var logs bytes.Buffer
		log := logrus.New()
		log.SetOutput(&logs)
		log.SetFormatter(&logrus.JSONFormatter{})
		handler, err := NewCacheHandler(func(ctx context.Context) (configdata.ClearStats, error) {
			if ctx == nil {
				t.Error("未传递请求Context")
			}
			if failed {
				return configdata.ClearStats{}, errors.New("private-storage-detail")
			}
			return configdata.ClearStats{Credentials: 1, ProductAccess: 2, ProductMaps: 3}, nil
		}, log)
		if err != nil {
			t.Fatal(err)
		}
		router := gin.New()
		router.DELETE("/config/cache", handler.Clear)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("DELETE", "/config/cache", nil))
		want := 200
		if failed {
			want = 503
		}
		if response.Code != want || strings.Contains(response.Body.String()+logs.String(), "private-storage-detail") {
			t.Fatal("响应状态错误或泄露内部错误")
		}
		var body struct {
			Cleared configdata.ClearStats `json:"cleared"`
			Status  *int                  `json:"status"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Status != nil {
			t.Fatal("管理响应不应携带业务status")
		}
		if !failed && body.Cleared != (configdata.ClearStats{Credentials: 1, ProductAccess: 2, ProductMaps: 3}) {
			t.Fatal("清理数量未返回")
		}
		var event map[string]any
		if err := json.Unmarshal(logs.Bytes(), &event); err != nil || event["event"] == "" {
			t.Fatal("缺少结构化审计")
		}
	}
}

func TestCacheHTTPFlowClearsThreeRealMemoryCaches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reads := 0
	reader, err := configdata.New(configdata.Sources{
		Credentials: []configdata.Source[string, services.Credential]{func(context.Context, string) (services.Credential, bool, error) {
			reads++
			return services.Credential{Version: 1, Secret: "synthetic-customer-secret", Status: 1}, true, nil
		}},
		Products: []configdata.Source[services.ProductKey, services.ProductAccess]{func(context.Context, services.ProductKey) (services.ProductAccess, bool, error) {
			reads++
			return services.ProductAccess{Version: 1, Status: 1}, true, nil
		}},
		Maps: []configdata.Source[int, map[string]int]{func(context.Context, int) (map[string]int, bool, error) {
			reads++
			return map[string]int{"a": 0}, true, nil
		}},
	}, config.CacheConfig{}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reader.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	warm := func() {
		t.Helper()
		if _, err := reader.LoadCredential(context.Background(), "synthetic-key"); err != nil {
			t.Fatal(err)
		}
		if err := reader.CheckProductAccess(context.Background(), "synthetic-key", 10001); err != nil {
			t.Fatal(err)
		}
		if _, err := reader.LoadProductMap(context.Background(), 10001); err != nil {
			t.Fatal(err)
		}
	}
	warm()
	var logs bytes.Buffer
	log := logrus.New()
	log.SetOutput(&logs)
	log.SetFormatter(&logrus.JSONFormatter{})
	handler, err := NewCacheHandler(reader.Clear, log)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(middleware.Logger(log))
	routes.RegisterConfigCacheRoute(router, handler.Clear, middleware.ConfigCacheAuth("synthetic-admin"), func(c *gin.Context) { request.SetSID(c, "audit-test-sid"); c.Next() })
	for _, authorized := range []bool{false, true} {
		req := httptest.NewRequest("DELETE", "/config/cache", nil)
		if authorized {
			req.Header.Set("Authorization", "Bearer synthetic-admin")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if !authorized {
			if response.Code != 401 {
				t.Fatal("未授权调用未拒绝")
			}
			warm()
			if reads != 3 {
				t.Fatal("未授权调用清理了缓存")
			}
			continue
		}
		var body struct {
			Cleared configdata.ClearStats `json:"cleared"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != 200 || body.Cleared != (configdata.ClearStats{Credentials: 1, ProductAccess: 1, ProductMaps: 1}) || reads != 3 {
			t.Fatal("HTTP清理未覆盖三类缓存或触发额外读取")
		}
	}
	warm()
	if reads != 6 {
		t.Fatal("清理后未重新读取三类来源")
	}
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil || event["sid"] != "audit-test-sid" {
			t.Fatal("管理日志未关联SID")
		}
		if strings.Contains(line, "synthetic-admin") || event["charge"] == float64(1) {
			t.Fatal("管理日志泄露Token或计费")
		}
	}
}
