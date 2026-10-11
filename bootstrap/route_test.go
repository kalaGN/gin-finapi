package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/pkg/config"
)

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
