package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/kalaGN/gin-finapi/pkg/utils"
)

func TestNewRouter_AssemblesPublicHealthAndLoanAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.AppConfig{
		MongoDB:   config.MongoDBConfig{DSN: "invalid-test-dsn", Database: "test", MaxPool: 100},
		Redis:     config.RedisConfig{Addr: "127.0.0.1:1", PoolSize: 1, Timeout: time.Second},
		RateLimit: config.RateLimitConfig{Requests: 100, Window: time.Second},
	}
	router, closeRedis, err := NewRouter(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := closeRedis(context.Background()); err != nil {
			t.Error(err)
		}
	})
	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK || health.Body.String() != "ok" {
		t.Fatalf("health: HTTP=%d, body=%s", health.Code, health.Body)
	}
	for _, tc := range []struct {
		name, key  string
		http, code int
	}{
		{"Redis unavailable rejects unknown key", "wrong-key", http.StatusServiceUnavailable, 9002},
		{"Redis unavailable rejects configured key", "test-key", http.StatusServiceUnavailable, 9002},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]interface{}{
				"phone": strings.Repeat("b", 64), "pcode": 10001,
				"apikey": tc.key, "nonce": "3093", "timestamp": time.Now().UnixMilli(),
			}
			body["sign"] = utils.GenerateSign(map[string]interface{}{"apikey": body["apikey"], "nonce": body["nonce"], "timestamp": body["timestamp"]}, "test-secret")
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(string(data)))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			var result request.Response
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if response.Code != tc.http || result.Status != tc.code {
				t.Fatalf("HTTP=%d, body=%s", response.Code, response.Body)
			}
		})
	}
}

func TestSetupRoute_UsesConfiguredRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	SetupRoute(router, func(c *gin.Context) { c.Status(204) }, func(c *gin.Context) { c.Next() }, nil, config.RateLimitConfig{Requests: 1, Window: time.Hour})
	for _, tt := range []struct {
		ip     string
		status int
	}{
		{"192.0.2.1:1234", http.StatusOK},
		{"192.0.2.1:1234", http.StatusTooManyRequests},
		{"192.0.2.2:1234", http.StatusOK},
	} {
		request := httptest.NewRequest("GET", "/health", nil)
		request.RemoteAddr = tt.ip
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != tt.status {
			t.Fatalf("IP %s: HTTP=%d, want %d", tt.ip, response.Code, tt.status)
		}
	}
}
