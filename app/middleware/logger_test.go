package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/Http/controllers/loan"
	"github.com/kalaGN/gin-finapi/app/services"
	applogger "github.com/kalaGN/gin-finapi/pkg/logger"
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
		{name: "无效凭据", keyErr: services.ErrAPIKeyInvalid, http: 400, code: 2001},
		{name: "认证依赖故障", keyErr: services.ErrAuthUnavailable, http: 503, code: 9002},
		{name: "无权限", grantErr: services.ErrProductDenied, http: 403, code: 2004},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			log := logrus.New()
			log.SetOutput(&output)
			log.SetFormatter(applogger.RequestFormatter{})
			query, err := services.NewQuery(func(ctx context.Context, _ int) (map[string]int, error) {
				if _, ok := utils.TimingSnapshot(ctx)["auth"]; !ok {
					t.Error("进入业务前未结束鉴权计时")
				}
				if errors.Is(tc.queryErr, services.ErrProductMapping) {
					return nil, tc.queryErr
				}
				return map[string]int{"var100001": 0}, nil
			}, func(ctx context.Context, _ string, _ map[string]int) (map[string]int, error) {
				if _, ok := utils.TimingSnapshot(ctx)["product_map"]; !ok {
					t.Error("进入查询前未记录映射计时")
				}
				return map[string]int{"var100001": 1}, tc.queryErr
			})
			if err != nil {
				t.Fatal(err)
			}
			handler, err := loan.NewLoanHandler(query, nil)
			if err != nil {
				t.Fatal(err)
			}
			r := gin.New()
			r.Use(Logger(log))
			r.POST("/api", RequestSID(func(string, int) (string, error) { return "test-sid", nil }, nil), LoanAuthWithProduct(func(context.Context, string) (services.Credential, error) {
				return services.Credential{Secret: "test-secret"}, tc.keyErr
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
			var row struct {
				SID          string           `json:"sid"`
				Input        json.RawMessage  `json:"input"`
				ResponseData json.RawMessage  `json:"response_data"`
				Status       int              `json:"status"`
				Timing       map[string]int64 `json:"duration_ms"`
			}
			if err := json.Unmarshal(output.Bytes(), &row); err != nil {
				t.Fatal(err)
			}
			if row.Status != tc.code || row.SID != "test-sid" {
				t.Fatalf("日志status=%d", row.Status)
			}
			if _, ok := row.Timing["auth"]; !ok {
				t.Fatal("缺少鉴权计时")
			}
			wantMap := tc.mutate == nil && tc.keyErr == nil && tc.grantErr == nil
			wantQuery := wantMap && !errors.Is(tc.queryErr, services.ErrProductMapping)
			_, hasMap := row.Timing["product_map"]
			_, hasQuery := row.Timing["query"]
			if hasMap != wantMap || hasQuery != wantQuery {
				t.Fatalf("短路节点错误=%v", row.Timing)
			}
			previous := int64(0)
			for _, stage := range []string{"auth", "product_map", "query", "finish"} {
				if value, ok := row.Timing[stage]; ok {
					if value < previous {
						t.Fatalf("时间点倒序=%v", row.Timing)
					}
					previous = value
				}
			}
			var input, response map[string]interface{}
			if err := json.Unmarshal(row.Input, &input); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(row.ResponseData, &response); err != nil {
				t.Fatal(err)
			}
			if input["apikey"] != "test-key" || response["status"] != float64(tc.code) {
				t.Fatal("完整输入/响应缺失")
			}
			var actual map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &actual); err != nil || !reflect.DeepEqual(actual, response) {
				t.Fatal("日志响应与实际响应不一致")
			}
		})
	}
}

func TestLogger_NoBusinessStatusDoesNotCharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, status := range []int{http.StatusOK, http.StatusTooManyRequests, http.StatusInternalServerError} {
		var output bytes.Buffer
		log := logrus.New()
		log.SetOutput(&output)
		log.SetFormatter(applogger.RequestFormatter{})
		r := gin.New()
		r.Use(Logger(log))
		r.GET("/health", func(c *gin.Context) { c.String(status, "ok") })
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/health", nil))
		assertResultLog(t, output.Bytes(), 0, 0)
		var row struct {
			Timing map[string]int64 `json:"duration_ms"`
		}
		if err := json.Unmarshal(output.Bytes(), &row); err != nil || len(row.Timing) != 2 {
			t.Fatal("health不应有业务节点")
		}
	}
}

func TestLoggerTimingShortCircuitAndPanic(t *testing.T) {
	for _, panicQuery := range []bool{false, true} {
		t.Run(map[bool]string{false: "限流短路", true: "查询Panic"}[panicQuery], func(t *testing.T) {
			var output bytes.Buffer
			log := logrus.New()
			log.SetOutput(&output)
			log.SetFormatter(applogger.RequestFormatter{})
			r := gin.New()
			r.Use(Logger(log), gin.RecoveryWithWriter(io.Discard))
			if !panicQuery {
				r.Use(func(c *gin.Context) { c.AbortWithStatus(http.StatusTooManyRequests) })
			}
			query, err := services.NewQuery(func(context.Context, int) (map[string]int, error) { return map[string]int{"a": 0}, nil }, func(context.Context, string, map[string]int) (map[string]int, error) { panic("synthetic-query-panic") })
			if err != nil {
				t.Fatal(err)
			}
			r.POST("/api", func(c *gin.Context) {
				utils.MarkTiming(c.Request.Context(), "auth")
				_, _ = query(c.Request.Context(), "synthetic", 10001)
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("POST", "/api", nil))
			assertResultLog(t, output.Bytes(), 0, 0)
			var row struct {
				Timing map[string]int64 `json:"duration_ms"`
			}
			if err := json.Unmarshal(output.Bytes(), &row); err != nil {
				t.Fatal(err)
			}
			_, hasMap := row.Timing["product_map"]
			_, hasAuth := row.Timing["auth"]
			_, hasQuery := row.Timing["query"]
			if hasQuery || hasAuth != panicQuery || hasMap != panicQuery {
				t.Fatalf("异常路径节点=%v", row.Timing)
			}
			wantStatus := http.StatusTooManyRequests
			if panicQuery {
				wantStatus = http.StatusInternalServerError
			}
			if w.Code != wantStatus {
				t.Fatalf("HTTP=%d", w.Code)
			}
		})
	}
}

func assertResultLog(t *testing.T, data []byte, success, charge int) {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["request_id"]; exists {
		t.Fatal("日志不应再包含 request_id")
	}
	if _, exists := fields["msg"]; exists {
		t.Fatal("日志不应包含顶层 msg")
	}
	var timing map[string]int64
	if err := json.Unmarshal(fields["duration_ms"], &timing); err != nil || timing == nil {
		t.Fatalf("duration_ms必须是JSON对象：%s", fields["duration_ms"])
	}
	if entry, ok := timing["entry"]; !ok || entry != 0 {
		t.Fatal("入口必须为0")
	}
	if finish, ok := timing["finish"]; !ok || finish < 0 {
		t.Fatal("缺少结束时间点")
	}
	var timestamp int64
	if err := json.Unmarshal(fields["time"], &timestamp); err != nil || timestamp < 1000000000000 {
		t.Fatal("time 应为毫秒时间戳")
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
