package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/services"
	applogger "github.com/kalaGN/gin-finapi/pkg/logger"
	"github.com/sirupsen/logrus"
)

func TestLogger_InvalidBodiesStillRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{"", "{bad", strings.Repeat(" ", maxLoggedRequestBody+1) + "{}"} {
		t.Run(strings.TrimSpace(body), func(t *testing.T) {
			var output bytes.Buffer
			log := logrus.New()
			log.SetOutput(&output)
			log.SetFormatter(applogger.RequestFormatter{})
			r := gin.New()
			r.Use(Logger(log))
			r.POST("/api", LoanAuthWithProduct(func(context.Context, string) (services.Credential, error) {
				t.Fatal("无效请求不应读取凭据")
				return services.Credential{}, nil
			}, func(context.Context, string, int) error { t.Fatal("无效请求不应读取授权"); return nil }), func(*gin.Context) { t.Fatal("无效请求到达业务") })
			req := httptest.NewRequest("POST", "/api", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			r.ServeHTTP(response, req)
			var row struct {
				Input     string `json:"input"`
				Status    int    `json:"status"`
				Truncated bool   `json:"input_truncated"`
			}
			if err := json.Unmarshal(output.Bytes(), &row); err != nil {
				t.Fatal(err)
			}
			if response.Code != 400 || row.Status != 1004 {
				t.Fatalf("无效请求行为变化: %d/%d", response.Code, row.Status)
			}
			want := body
			if len(want) > maxLoggedRequestBody {
				want = want[:maxLoggedRequestBody]
			}
			if row.Input != want || row.Truncated != (len(body) > maxLoggedRequestBody) {
				t.Fatal("输入记录或超限标记错误")
			}
		})
	}
}

type failedBody struct {
	closed int
	err    error
}

func (b *failedBody) Read(p []byte) (int, error) { return copy(p, "partial"), b.err }
func (b *failedBody) Close() error               { b.closed++; return nil }

func TestCaptureInput_ReplaysBytesErrorAndClose(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/api", nil)
	body := &failedBody{err: errors.New("read failed")}
	c.Request.Body = body
	input, truncated := captureInput(c)
	if input != "partial" || truncated {
		t.Fatal("部分输入记录错误")
	}
	data, err := io.ReadAll(c.Request.Body)
	if string(data) != "partial" || !errors.Is(err, body.err) {
		t.Fatal("原读取错误或字节丢失")
	}
	if err := c.Request.Body.Close(); err != nil || body.closed != 1 {
		t.Fatal("原 Body 未正确关闭")
	}
}
