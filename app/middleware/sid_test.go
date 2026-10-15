package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/sirupsen/logrus"
)

func TestRequestSIDGeneratedOnceAndReused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := 0
	var output bytes.Buffer
	log := logrus.New()
	log.SetOutput(&output)
	log.SetFormatter(&logrus.JSONFormatter{})
	r := gin.New()
	r.Use(Logger(log))
	r.POST("/api", RequestSID(func(prefix string, length int) (string, error) {
		calls++
		if prefix != "100" || length != 29 {
			t.Fatal("SID 格式参数变化")
		}
		return "100synthetic-sid", nil
	}, nil), func(c *gin.Context) {
		if request.SID(c) != "100synthetic-sid" {
			t.Fatal("认证前 SID 不存在")
		}
		c.Next()
	}, func(c *gin.Context) {
		request.JSON(c, 200, request.Response{Status: 0, Sid: request.SID(c)})
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api", nil)
	req.Header.Set("X-SID", "client-controlled")
	r.ServeHTTP(w, req)
	var response request.Response
	var completion struct {
		SID string `json:"sid"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output.Bytes(), &completion); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || w.Code != 200 || response.Sid != "100synthetic-sid" || completion.SID != response.Sid {
		t.Fatalf("次数=%d 响应=%s 日志=%s", calls, w.Body, &output)
	}
}

func TestRequestSIDFailureStopsBeforeAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name     string
		generate func(string, int) (string, error)
	}{
		{"随机源异常", func(string, int) (string, error) { return "partial", errors.New("sensitive-random-source") }},
		{"空结果", func(string, int) (string, error) { return "", nil }},
		{"缺少依赖", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var diagnostics, completion bytes.Buffer
			newLog := func(out *bytes.Buffer) *logrus.Logger {
				l := logrus.New()
				l.SetOutput(out)
				l.SetFormatter(&logrus.JSONFormatter{})
				return l
			}
			r := gin.New()
			r.Use(Logger(newLog(&completion)))
			r.POST("/api", RequestSID(tc.generate, newLog(&diagnostics)), LoanAuthWithProduct(func(context.Context, string) (services.Credential, error) {
				t.Fatal("SID 失败后不得读取凭据")
				return services.Credential{}, nil
			}, nil), func(c *gin.Context) { t.Fatal("SID 失败后不得查询") })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("POST", "/api", strings.NewReader("invalid-json")))
			var response request.Response
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != 500 || response.Status != 9001 || response.Sid != "" {
				t.Fatalf("响应=%s", w.Body)
			}
			var diagnostic, completed map[string]any
			if err := json.Unmarshal(diagnostics.Bytes(), &diagnostic); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(completion.Bytes(), &completed); err != nil {
				t.Fatal(err)
			}
			if diagnostic["error_kind"] != "sid_generation" || diagnostic["sid"] != "" || completed["sid"] != "" || completed["status"] != float64(9001) {
				t.Fatal("SID 失败日志字段不正确")
			}
			for _, row := range []map[string]any{diagnostic, completed} {
				if _, exists := row["request_id"]; exists {
					t.Fatal("失败日志不应包含 request_id")
				}
			}
			if strings.Contains(diagnostics.String(), "sensitive-random-source") || strings.Contains(w.Body.String(), "partial") {
				t.Fatal("泄露随机源或部分 SID")
			}
		})
	}
}
