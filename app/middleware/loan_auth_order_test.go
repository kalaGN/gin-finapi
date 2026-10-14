package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/utils"
)

func TestLoanAuthWithProduct_RejectsBeforeAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		mutate func(map[string]interface{})
		code   int
	}{
		{"missing nonce", func(b map[string]interface{}) { delete(b, "nonce") }, 1001},
		{"tampered nonce", func(b map[string]interface{}) { b["nonce"] = "changed" }, 2002},
		{"unknown key", func(b map[string]interface{}) { b["apikey"] = "unknown" }, 2001},
		{"expired timestamp", func(b map[string]interface{}) { b["timestamp"] = time.Now().Add(-10 * time.Minute).UnixMilli() }, 1005},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]interface{}{"phone": "test-identifierrrrrrrrrrrrrrrrrr", "pcode": 10001, "apikey": "key", "nonce": "0001", "timestamp": time.Now().UnixMilli()}
			body["sign"] = utils.GenerateSign(map[string]interface{}{"apikey": body["apikey"], "nonce": body["nonce"], "timestamp": body["timestamp"]}, "secret")
			tc.mutate(body)
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.POST("/api", LoanAuthWithProduct(testKeyReader("key", "secret"), func(context.Context, string, int) error {
				t.Error("rejected authentication reached product authorization")
				return nil
			}), func(*gin.Context) { t.Error("rejected authentication reached handler") })
			req := httptest.NewRequest("POST", "/api", bytes.NewReader(data))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			var result struct {
				Status int `json:"status"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if w.Code != 400 || result.Status != tc.code {
				t.Fatalf("HTTP=%d code=%d", w.Code, result.Status)
			}
		})
	}
}

// 产品不参与签名，但修改后的产品仍必须通过授权检查。
func TestLoanAuthWithProduct_UnsignedProductStillRequiresAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := map[string]interface{}{"phone": "test-identifierrrrrrrrrrrrrrrrrr", "pcode": 10001, "apikey": "key", "nonce": "0001", "timestamp": time.Now().UnixMilli()}
	body["sign"] = utils.GenerateSign(map[string]interface{}{"apikey": body["apikey"], "nonce": body["nonce"], "timestamp": body["timestamp"]}, "secret")
	body["pcode"] = 10002
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	router := gin.New()
	router.POST("/api", LoanAuthWithProduct(testKeyReader("key", "secret"), func(_ context.Context, key string, pcode int) error {
		called = true
		if key != "key" || pcode != 10002 {
			t.Fatal("产品授权未使用当前请求参数")
		}
		return services.ErrProductDenied
	}), func(*gin.Context) { t.Error("未授权请求进入 Handler") })
	req := httptest.NewRequest("POST", "/api", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var result struct {
		Status int `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !called || w.Code != 403 || result.Status != 2004 {
		t.Fatalf("授权调用=%v HTTP=%d 业务码=%d", called, w.Code, result.Status)
	}
}
