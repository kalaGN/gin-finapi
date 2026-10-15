package loan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/middleware"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/kalaGN/gin-finapi/pkg/utils"
	"github.com/sirupsen/logrus"
)

func TestPreQueryDiagnosticDoesNotLeakCause(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, phone, kind string
		http              int
	}{
		{"SID", strings.Repeat("r", 32), "sid_generation", 500},
		{"转换未接入", strings.Repeat("a", 64), "phone_conversion", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			log := logrus.New()
			log.SetOutput(&output)
			log.SetFormatter(&logrus.JSONFormatter{})
			h, err := NewLoanHandler(func(context.Context, string, int) (map[string]int, error) { t.Fatal("不应查询"); return nil, nil }, log)
			if err != nil {
				t.Fatal(err)
			}
			r := gin.New()
			generate := successfulSID
			if tc.kind == "sid_generation" {
				generate = func(string, int) (string, error) { return "", errors.New("sensitive-entropy-error") }
			}
			r.POST("/api", middleware.RequestSID(generate, log), func(c *gin.Context) {
				request.SetAuthenticated(c, request.Request{Phone: request.StrictString{Value: tc.phone}})
				h.Create(c)
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("POST", "/api", nil))
			var entry map[string]any
			if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.http || entry["error_kind"] != tc.kind {
				t.Fatalf("HTTP=%d log=%v", w.Code, entry)
			}
			if _, exists := entry["request_id"]; exists {
				t.Fatal("诊断日志不应包含 request_id")
			}
			if tc.kind == "phone_conversion" && entry["sid"] != "100testsid" {
				t.Fatal("转换失败日志应保留 SID")
			}
			if tc.kind == "sid_generation" && entry["sid"] != "" {
				t.Fatal("SID 失败不应有标识")
			}
			if strings.Contains(output.String(), "sensitive-entropy-error") {
				t.Fatal("错误原因泄漏")
			}
		})
	}
}

func TestQueryDiagnosticClassificationAndCorrelation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, kind, level string
		err               error
	}{
		{"未查得", "", "", services.ErrNotFound},
		{"映射", "product_mapping", "warning", services.ErrProductMapping},
		{"依赖", "unavailable", "error", errors.Join(services.ErrUnavailable, context.DeadlineExceeded)},
		{"超时", "timeout", "error", context.DeadlineExceeded},
		{"取消", "canceled", "error", context.Canceled},
		{"数据", "invalid_data", "error", services.ErrInvalidData},
		{"未知", "internal", "error", errors.New("sensitive-driver-details")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var internal, completed bytes.Buffer
			newLog := func(out *bytes.Buffer) *logrus.Logger {
				log := logrus.New()
				log.SetOutput(out)
				log.SetFormatter(&logrus.JSONFormatter{})
				return log
			}
			h, err := NewLoanHandler(func(context.Context, string, int) (map[string]int, error) { return nil, tc.err }, newLog(&internal))
			if err != nil {
				t.Fatal(err)
			}
			r := gin.New()
			r.Use(middleware.Logger(newLog(&completed)))
			r.POST("/api", middleware.RequestSID(utils.GenerateSID, newLog(&internal)), middleware.LoanAuthWithProduct(testKeyReader("key", testSecret), nil), h.Create)
			var previous string
			for range 2 {
				internal.Reset()
				completed.Reset()
				w := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/api", strings.NewReader(validRequestBody(t)))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Request-ID", "untrusted-client-value")
				r.ServeHTTP(w, req)
				var completion map[string]any
				if err := json.Unmarshal(completed.Bytes(), &completion); err != nil {
					t.Fatal(err)
				}
				id, _ := completion["sid"].(string)
				if id == "" || id == previous || id == "untrusted-client-value" {
					t.Fatalf("invalid correlation ID %q", id)
				}
				previous = id
				if _, exists := completion["request_id"]; exists {
					t.Fatal("完成日志不应包含 request_id")
				}
				if w.Header().Get("X-Request-ID") != "" || strings.Contains(w.Body.String(), "request_id") {
					t.Fatal("response contract changed")
				}
				if tc.kind == "" {
					if internal.Len() != 0 {
						t.Fatal("normal not-found logged as failure")
					}
					continue
				}
				var diagnostic map[string]any
				if err := json.Unmarshal(internal.Bytes(), &diagnostic); err != nil {
					t.Fatal(err)
				}
				if diagnostic["error_kind"] != tc.kind || diagnostic["level"] != tc.level || diagnostic["sid"] != id {
					t.Fatalf("diagnostic=%v", diagnostic)
				}
				if _, exists := diagnostic["request_id"]; exists {
					t.Fatal("诊断日志不应包含 request_id")
				}
				if strings.Contains(internal.String(), "sensitive-driver-details") || strings.Contains(internal.String(), testSecret) {
					t.Fatal("diagnostic leaked sensitive data")
				}
			}
		})
	}
}
