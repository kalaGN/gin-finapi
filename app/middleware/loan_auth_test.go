package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/pkg/loanrequest"
	"github.com/kalaGN/gin-finapi/pkg/rescode"
	"github.com/kalaGN/gin-finapi/pkg/utils"
	"github.com/sirupsen/logrus"
)

func TestLoanAuth_RejectsBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name   string
		mutate func(map[string]interface{})
		code   int
	}{
		{"missing phone", func(b map[string]interface{}) { delete(b, "phone") }, rescode.ErrMissingParam},
		{"null phone", func(b map[string]interface{}) { b["phone"] = nil }, rescode.ErrInvalidPhone},
		{"missing pcode", func(b map[string]interface{}) { delete(b, "pcode") }, rescode.ErrInvalidPcode},
		{"null pcode", func(b map[string]interface{}) { b["pcode"] = nil }, rescode.ErrInvalidPcode},
		{"missing key", func(b map[string]interface{}) { delete(b, "apikey") }, rescode.ErrInvalidApikey},
		{"wrong key valid signature", func(b map[string]interface{}) {
			b["apikey"] = "unknown-key"
			b["sign"] = utils.GenerateSign(b, "secret")
		}, rescode.ErrInvalidApikey},
		{"missing timestamp", func(b map[string]interface{}) { delete(b, "timestamp") }, rescode.ErrInvalidTimestamp},
		{"expired", func(b map[string]interface{}) { b["timestamp"] = time.Now().Add(-10 * time.Minute).UnixMilli() }, rescode.ErrInvalidTimestamp},
		{"future", func(b map[string]interface{}) { b["timestamp"] = time.Now().Add(time.Hour).UnixMilli() }, rescode.ErrInvalidTimestamp},
		{"missing sign", func(b map[string]interface{}) { delete(b, "sign") }, rescode.ErrInvalidSign},
		{"wrong sign", func(b map[string]interface{}) { b["sign"] = "wrong" }, rescode.ErrInvalidSign},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := map[string]interface{}{"phone": "ciphertext", "pcode": 10001, "apikey": "key", "timestamp": time.Now().UnixMilli()}
			b["sign"] = utils.GenerateSign(b, "secret")
			tt.mutate(b)
			data, _ := json.Marshal(b)
			r := gin.New()
			r.POST("/loan", LoanAuth("key", "secret"), func(c *gin.Context) { t.Fatal("unauthenticated handler called") })
			req := httptest.NewRequest(http.MethodPost, "/loan", bytes.NewReader(data))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			var response loanrequest.Response
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != 400 || response.Status != tt.code {
				t.Fatalf("got HTTP %d, body %s", w.Code, w.Body)
			}
		})
	}
}

func TestLoanAuth_ValidatesJSONAndConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	valid := map[string]interface{}{"phone": "ciphertext", "pcode": 99999, "apikey": "key", "timestamp": time.Now().UnixMilli()}
	valid["sign"] = utils.GenerateSign(valid, "secret")
	data, _ := json.Marshal(valid)
	for _, tt := range []struct {
		name, body, contentType, key, secret string
		want                                 int
	}{
		{"valid", string(data), "application/json", "key", "secret", 204},
		{"missing config", string(data), "application/json", "", "", 400},
		{"malformed", "{", "application/json", "key", "secret", 400},
		{"empty", "", "application/json", "key", "secret", 400},
		{"wrong content type", string(data), "text/plain", "key", "secret", 400},
		{"trailing JSON", string(data) + "{}", "application/json", "key", "secret", 400},
		{"oversized", string(data) + strings.Repeat(" ", 64<<10), "application/json", "key", "secret", 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.POST("/loan", LoanAuth(tt.key, tt.secret), func(c *gin.Context) {
				body, ok := loanrequest.Authenticated(c)
				if !ok || body.Phone.Value != "ciphertext" {
					t.Fatal("authenticated request not available")
				}
				c.Status(204)
			})
			req := httptest.NewRequest("POST", "/loan", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Fatalf("HTTP = %d, want %d", w.Code, tt.want)
			}
		})
	}
}

func TestLogger_DoesNotCaptureSensitivePayloads(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	log := logrus.New()
	log.SetOutput(&output)
	r := gin.New()
	r.Use(Logger(log))
	r.POST("/loan", func(c *gin.Context) {
		var b map[string]string
		if err := c.ShouldBindJSON(&b); err != nil {
			t.Fatal(err)
		}
		c.JSON(200, b)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/loan?apikey=query-secret", strings.NewReader(`{"phone":"sensitive-phone","apikey":"sensitive-key","sign":"sensitive-sign"}`)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "sensitive-phone") {
		t.Fatal("logger altered request or response")
	}
	for _, value := range []string{"sensitive-phone", "sensitive-key", "sensitive-sign", "query-secret"} {
		if strings.Contains(output.String(), value) {
			t.Fatal("sensitive value leaked to logs")
		}
	}
	if !strings.Contains(output.String(), "http_status") {
		t.Fatal("missing request metadata")
	}
}
