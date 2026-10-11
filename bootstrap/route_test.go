package bootstrap

import (
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
		APIKey: "test-key", SecretKey: "test-secret",
		MongoDB:   config.MongoDBConfig{DSN: "invalid-test-dsn"},
		RateLimit: config.RateLimitConfig{Requests: 100, Window: time.Second},
	}
	router, err := NewRouter(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK || health.Body.String() != "ok" {
		t.Fatalf("health: HTTP=%d, body=%s", health.Code, health.Body)
	}
	for _, tc := range []struct {
		name, key  string
		http, code int
	}{
		{"reject wrong key", "wrong-key", http.StatusBadRequest, 2001},
		{"authenticated SHA256 reaches handler", cfg.APIKey, http.StatusServiceUnavailable, 9002},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]interface{}{
				"phone": strings.Repeat("b", 64), "pcode": 10001,
				"apikey": tc.key, "timestamp": time.Now().UnixMilli(),
			}
			body["sign"] = utils.GenerateSign(body, cfg.SecretKey)
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/loan", strings.NewReader(string(data)))
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
