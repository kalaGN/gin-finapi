package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/sirupsen/logrus"
)

func TestSetupRouteSIDBeforeAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	log := logrus.New()
	log.SetOutput(&output)
	log.SetFormatter(&logrus.JSONFormatter{})
	var sid string
	authCalls := 0
	r := gin.New()
	close := SetupRoute(r, func(c *gin.Context) { t.Fatal("认证拒绝后不应执行 Handler") }, func(c *gin.Context) {
		authCalls++
		sid = request.SID(c)
		if len(sid) != 32 || !strings.HasPrefix(sid, "100") {
			t.Fatalf("认证前 SID=%q", sid)
		}
		request.AbortWithStatusJSON(c, 400, request.Response{Status: 2001, Msg: "invalid"})
	}, log, config.RateLimitConfig{Requests: 10, Window: time.Minute})
	t.Cleanup(func() {
		if err := close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 200 || w.Body.String() != "ok" || authCalls != 0 {
		t.Fatal("health 被认证影响")
	}
	output.Reset()
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api", strings.NewReader("invalid-json")))
	var response request.Response
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != 400 || response.Status != 2001 || response.Sid != "" || authCalls != 1 {
		t.Fatalf("响应契约变化: %s", w.Body)
	}
	var completion map[string]any
	if err := json.Unmarshal(output.Bytes(), &completion); err != nil {
		t.Fatal(err)
	}
	if completion["sid"] != sid {
		t.Fatalf("日志未关联 SID: %s", output.String())
	}
	if _, exists := completion["request_id"]; exists {
		t.Fatal("日志不应包含 request_id")
	}
}
