package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRegisterAPIRoutes_Health(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterAPIRoutes(router, func(c *gin.Context) { c.Status(http.StatusOK) }, func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) })

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.String() != "ok" {
		t.Fatalf("body = %q, want %q", response.Body.String(), "ok")
	}
}

func TestRegisterAPIRoutes_LoanUsesAPIPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterAPIRoutes(router, func(c *gin.Context) { c.Status(http.StatusNoContent) }, func(c *gin.Context) { c.Next() })

	for _, tt := range []struct {
		path string
		want int
	}{
		{"/api", http.StatusNoContent},
		{"/loan", http.StatusNotFound},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, tt.path, nil))
		if response.Code != tt.want {
			t.Fatalf("POST %s status = %d, want %d", tt.path, response.Code, tt.want)
		}
	}
}
