package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/Http/controllers/loan"
	"github.com/kalaGN/gin-finapi/app/auth"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/utils"
	"github.com/sirupsen/logrus"
)

func TestLogger_RequestResultFlags(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name                        string
		queryErr, keyErr, grantErr  error
		mutate                      func(map[string]interface{})
		http, code, success, charge int
	}{
		{name: "成功", http: 200, code: 0, success: 1, charge: 1},
		{name: "未查得", queryErr: services.ErrNotFound, http: 200, code: 3001, success: 1},
		{name: "产品异常", queryErr: services.ErrProductMapping, http: 200, code: 6001},
		{name: "依赖故障", queryErr: services.ErrUnavailable, http: 503, code: 9002},
		{name: "内部错误", queryErr: services.ErrInvalidData, http: 500, code: 9001},
		{name: "超时", queryErr: context.DeadlineExceeded, http: 504, code: 9003},
		{name: "缺少参数", mutate: func(b map[string]interface{}) { delete(b, "phone") }, http: 400, code: 1001},
		{name: "手机号长度错误", mutate: func(b map[string]interface{}) { b["phone"] = "short" }, http: 400, code: 1002},
		{name: "签名错误", mutate: func(b map[string]interface{}) { b["sign"] = "invalid" }, http: 400, code: 2002},
		{name: "无效凭据", keyErr: auth.ErrInvalid, http: 400, code: 2001},
		{name: "认证依赖故障", keyErr: auth.ErrUnavailable, http: 503, code: 9002},
		{name: "无权限", grantErr: auth.ErrProductDenied, http: 403, code: 2004},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			log := logrus.New()
			log.SetOutput(&output)
			log.SetFormatter(&logrus.JSONFormatter{})
			handler, err := loan.NewLoanHandler(func(context.Context, string, int) (map[string]int, error) {
				return map[string]int{"var100001": 1}, tc.queryErr
			}, func(string, int) (string, error) { return "test-sid", nil }, nil)
			if err != nil {
				t.Fatal(err)
			}
			r := gin.New()
			r.Use(Logger(log))
			r.POST("/api", LoanAuthWithProduct(func(context.Context, string) (auth.Credential, error) {
				return auth.Credential{Secret: "test-secret"}, tc.keyErr
			}, func(context.Context, string, int) error { return tc.grantErr }), handler.Create)
			body := map[string]interface{}{"phone": strings.Repeat("r", 32), "pcode": 10001, "apikey": "test-key", "nonce": "0001", "timestamp": time.Now().UnixMilli()}
			body["sign"] = utils.GenerateSign(map[string]interface{}{"apikey": body["apikey"], "nonce": body["nonce"], "timestamp": body["timestamp"]}, "test-secret")
			if tc.mutate != nil {
				tc.mutate(body)
			}
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/api", bytes.NewReader(data))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			var result struct {
				Status int `json:"status"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.http || result.Status != tc.code {
				t.Fatalf("HTTP=%d 业务码=%d", w.Code, result.Status)
			}
			assertResultLog(t, output.Bytes(), tc.success, tc.charge)
		})
	}
}

func TestLogger_NoBusinessStatusDoesNotCharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, status := range []int{http.StatusOK, http.StatusTooManyRequests, http.StatusInternalServerError} {
		var output bytes.Buffer
		log := logrus.New()
		log.SetOutput(&output)
		log.SetFormatter(&logrus.JSONFormatter{})
		r := gin.New()
		r.Use(Logger(log))
		r.GET("/health", func(c *gin.Context) { c.String(status, "ok") })
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/health", nil))
		assertResultLog(t, output.Bytes(), 0, 0)
	}
}

func assertResultLog(t *testing.T, data []byte, success, charge int) {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]int{"success": success, "charge": charge} {
		raw, exists := fields[key]
		if !exists {
			t.Fatalf("日志缺少 %s", key)
		}
		var got int
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s=%d 预期=%d", key, got, want)
		}
	}
}
