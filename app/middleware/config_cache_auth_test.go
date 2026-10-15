package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestConfigCacheAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		token   string
		headers []string
		status  int
	}{
		{"disabled", "", []string{"Bearer synthetic-admin"}, 404},
		{"missing", "synthetic-admin", nil, 401},
		{"wrong", "synthetic-admin", []string{"Bearer customer-api-key"}, 401},
		{"basic", "synthetic-admin", []string{"Basic synthetic-admin"}, 401},
		{"empty", "synthetic-admin", []string{"Bearer "}, 401},
		{"extra space", "synthetic-admin", []string{"Bearer  synthetic-admin"}, 401},
		{"extra value", "synthetic-admin", []string{"Bearer synthetic-admin extra"}, 401},
		{"duplicate", "synthetic-admin", []string{"Bearer synthetic-admin", "Bearer synthetic-admin"}, 401},
		{"valid", "synthetic-admin", []string{"Bearer synthetic-admin"}, 204},
		{"case", "synthetic-admin", []string{"bearer synthetic-admin"}, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			router := gin.New()
			router.DELETE("/config/cache", ConfigCacheAuth(tc.token), func(c *gin.Context) { called = true; c.Status(204) })
			req := httptest.NewRequest("DELETE", "/config/cache?token=synthetic-admin", strings.NewReader(`{"token":"synthetic-admin"}`))
			for _, value := range tc.headers {
				req.Header.Add("Authorization", value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status || called != (tc.status == 204) {
				t.Fatal("认证或短路行为错误")
			}
			if tc.status == 401 && response.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatal("缺少认证提示")
			}
			if strings.Contains(response.Body.String(), "synthetic-admin") {
				t.Fatal("响应泄露Token")
			}
		})
	}
}

func TestCacheManagementLogsDoNotExposeTokenOrCharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, authorized := range []bool{false, true} {
		var output bytes.Buffer
		log := logrus.New()
		log.SetOutput(&output)
		log.SetFormatter(&logrus.JSONFormatter{})
		router := gin.New()
		router.Use(Logger(log))
		router.DELETE("/config/cache", ConfigCacheAuth("synthetic-admin"), func(c *gin.Context) { c.JSON(200, gin.H{"cleared": gin.H{"credentials": 1}}) })
		req := httptest.NewRequest(http.MethodDelete, "/config/cache?token=synthetic-admin", strings.NewReader(`{"token":"synthetic-admin"}`))
		if authorized {
			req.Header.Set("Authorization", "Bearer synthetic-admin")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		line := output.String()
		if strings.Contains(line, "synthetic-admin") || !strings.Contains(line, `"charge":0`) || !strings.Contains(line, `"input":null`) {
			t.Fatal("管理日志泄露凭据或产生计费标记")
		}
	}
}
