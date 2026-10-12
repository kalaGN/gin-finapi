package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
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
			body := map[string]interface{}{"phone": "test-identifier", "pcode": 10001, "apikey": "key", "nonce": "0001", "timestamp": time.Now().UnixMilli()}
			body["sign"] = utils.GenerateSign(body, "secret")
			tc.mutate(body)
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.POST("/loan", LoanAuthWithProduct(testKeyReader("key", "secret"), func(context.Context, string, int) error {
				t.Error("rejected authentication reached product authorization")
				return nil
			}), func(*gin.Context) { t.Error("rejected authentication reached handler") })
			req := httptest.NewRequest("POST", "/loan", bytes.NewReader(data))
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
