package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/pkg/config"
	applogger "github.com/kalaGN/gin-finapi/pkg/logger"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/sirupsen/logrus"
)

func TestSetupRoute_PanicStillProducesOneCompleteRequestLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = io.Discard
	t.Cleanup(func() { gin.DefaultErrorWriter = original })
	for _, tc := range []struct {
		name                  string
		handler               gin.HandlerFunc
		http, success, charge int
		status                *int
	}{
		{name: "响应前异常", handler: func(*gin.Context) { panic("synthetic failure") }, http: 500},
		{name: "部分文本已写出", handler: func(c *gin.Context) { c.String(202, "partial"); panic("synthetic failure") }, http: 202},
		{name: "成功JSON已写出", handler: func(c *gin.Context) {
			request.JSON(c, 200, request.Response{Status: 0, Msg: "success", Sid: "test-sid", Data: map[string]int{"x": 1}})
			panic("synthetic failure")
		}, http: 200, success: 1, charge: 1, status: intPointer(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "request.log")
			base := logrus.New()
			base.SetOutput(io.Discard)
			log, closeLog, err := applogger.NewRequestLogger(base, config.RequestLogConfig{Output: "file", File: file, QueueSize: 4}, config.RedisConfig{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := closeLog(context.Background()); err != nil {
					t.Error(err)
				}
			})
			r := gin.New()
			closeMiddleware := SetupRoute(r, tc.handler, func(c *gin.Context) { c.Next() }, base, config.RateLimitConfig{Requests: 10, Window: time.Hour}, log)
			t.Cleanup(func() {
				if err := closeMiddleware(context.Background()); err != nil {
					t.Error(err)
				}
			})
			body := `{"extra":{"nested":true},"nonce":"0001"}`
			req := httptest.NewRequest("POST", "/api", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			r.ServeHTTP(response, req)
			if response.Code != tc.http || tc.http == 500 && response.Body.Len() != 0 {
				t.Fatalf("响应行为变化: %d %s", response.Code, response.Body.String())
			}
			if err := closeLog(context.Background()); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var row struct {
				HTTP         int             `json:"http_status"`
				Status       *int            `json:"status"`
				Success      int             `json:"success"`
				Charge       int             `json:"charge"`
				Time         int64           `json:"time"`
				Input        json.RawMessage `json:"input"`
				ResponseData json.RawMessage `json:"response_data"`
			}
			decoder := json.NewDecoder(bytes.NewReader(data))
			if err := decoder.Decode(&row); err != nil {
				t.Fatal("异常完成日志缺失", err)
			}
			if err := decoder.Decode(new(interface{})); err != io.EOF {
				t.Fatal("同请求日志多于一条或JSON无效", err)
			}
			if row.HTTP != tc.http || row.Success != tc.success || row.Charge != tc.charge || !reflect.DeepEqual(row.Status, tc.status) || row.Time < 1000000000000 {
				t.Fatalf("日志状态错误: %+v", row)
			}
			var input, expected interface{}
			if err := json.Unmarshal(row.Input, &input); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(body), &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(input, expected) {
				t.Fatal("完整请求体变化")
			}
			var logged, actual interface{}
			if err := json.Unmarshal(row.ResponseData, &logged); err != nil {
				t.Fatal(err)
			}
			if json.Unmarshal(response.Body.Bytes(), &actual) != nil {
				actual = response.Body.String()
			}
			if !reflect.DeepEqual(logged, actual) {
				t.Fatal("记录响应与实际响应不同")
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if _, ok := fields["msg"]; ok {
				t.Fatal("顶层msg不应恢复")
			}
		})
	}
}

func intPointer(value int) *int { return &value }

type panicBodyReader struct{}

func (panicBodyReader) Read([]byte) (int, error) { panic("synthetic input reader failure") }

func TestSetupRoute_OuterRecoveryStillProtectsLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = io.Discard
	t.Cleanup(func() { gin.DefaultErrorWriter = original })
	log := logrus.New()
	log.SetOutput(io.Discard)
	router := gin.New()
	closeMiddleware := SetupRoute(router, func(*gin.Context) { t.Fatal("读取异常后不应执行业务") }, func(c *gin.Context) { c.Next() }, log, config.RateLimitConfig{Requests: 10, Window: time.Hour})
	t.Cleanup(func() {
		if err := closeMiddleware(context.Background()); err != nil {
			t.Error(err)
		}
	})
	req := httptest.NewRequest("POST", "/api", nil)
	req.Body = io.NopCloser(panicBodyReader{})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != 500 || response.Body.Len() != 0 {
		t.Fatal("外层 Recovery 的兜底响应发生变化")
	}
}
