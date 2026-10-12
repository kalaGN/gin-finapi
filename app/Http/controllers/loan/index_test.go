package loan

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	queryloan "github.com/kalaGN/gin-finapi/app/loan"
	"github.com/kalaGN/gin-finapi/app/middleware"
	"github.com/kalaGN/gin-finapi/pkg/rescode"
	"github.com/kalaGN/gin-finapi/pkg/utils"
	"github.com/kalaGN/gin-finapi/routes"
)

const testSecret = "test-secret"

func TestLoanHandler_UsesPhoneLookupKey(t *testing.T) {
	for _, tt := range []struct{ phone, query string }{
		{"r707qyr0k2xmucjp7lz0", "r707qyr0k2xmucjp7lz0"},
		{"0123456789abcdef0123456789abcdef", "0123456789abcdef0123"},
		{"0123456789ABCDEF0123456789ABCDEF", "0123456789ABCDEF0123"},
	} {
		phone := tt.phone
		t.Run(phone, func(t *testing.T) {
			body := map[string]interface{}{"phone": phone, "pcode": 10001, "apikey": "key", "nonce": "3093", "timestamp": time.Now().UnixMilli()}
			body["sign"] = utils.GenerateSign(map[string]interface{}{"apikey": body["apikey"], "nonce": body["nonce"], "timestamp": body["timestamp"]}, testSecret)
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			called := false
			response := performCreate(t, string(data), func(ctx context.Context, got string, pcode int) (map[string]int, error) {
				called = true
				if pcode != 10001 {
					t.Fatalf("pcode = %d", pcode)
				}
				if got != tt.query {
					t.Fatalf("query value = %q, want %q", got, tt.query)
				}
				return map[string]int{"var100001": 10}, nil
			}, successfulSID, testSecret)
			if !called || response.Code != http.StatusOK || decodeResponse(t, response).Status != rescode.SuccessCode {
				t.Fatalf("called=%v, HTTP=%d, response=%s", called, response.Code, response.Body)
			}
		})
	}
}

func TestLoanHandler_SHA256WaitsForConversionService(t *testing.T) {
	body := map[string]interface{}{"phone": strings.Repeat("b", 64), "pcode": 10001, "apikey": "key", "nonce": "3093", "timestamp": time.Now().UnixMilli()}
	body["sign"] = utils.GenerateSign(map[string]interface{}{"apikey": body["apikey"], "nonce": body["nonce"], "timestamp": body["timestamp"]}, testSecret)
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	response := performCreate(t, string(data), func(context.Context, string, int) (map[string]int, error) {
		t.Fatal("query called before conversion service is available")
		return nil, nil
	}, func(string, int) (string, error) { t.Fatal("SID called before conversion"); return "", nil }, testSecret)
	if response.Code != http.StatusServiceUnavailable || decodeResponse(t, response).Status != rescode.ErrServiceUnavailable {
		t.Fatalf("HTTP=%d, response=%s", response.Code, response.Body)
	}
}

func TestCreate_RejectsNonIntegerNumbers(t *testing.T) {
	timestamp := time.Now().UnixMilli()
	queryCalled := false
	query := func(context.Context, string, int) (map[string]int, error) {
		queryCalled = true
		return nil, nil
	}

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"decimal pcode", `{"phone":"phone","pcode":10001.5,"apikey":"key","nonce": "3093", "timestamp":` + jsonNumber(timestamp) + `,"sign":"x"}`, rescode.ErrInvalidPcode},
		{"string pcode", `{"phone":"phone","pcode":"10001","apikey":"key","nonce": "3093", "timestamp":` + jsonNumber(timestamp) + `,"sign":"x"}`, rescode.ErrInvalidPcode},
		{"exponent pcode", `{"phone":"phone","pcode":1e4,"apikey":"key","nonce": "3093", "timestamp":` + jsonNumber(timestamp) + `,"sign":"x"}`, rescode.ErrInvalidPcode},
		{"overflow pcode", `{"phone":"phone","pcode":9223372036854775808,"apikey":"key","nonce": "3093", "timestamp":` + jsonNumber(timestamp) + `,"sign":"x"}`, rescode.ErrInvalidPcode},
		{"out of range pcode", `{"phone":"phone","pcode":100000,"apikey":"key","nonce": "3093", "timestamp":` + jsonNumber(timestamp) + `,"sign":"x"}`, rescode.ErrInvalidPcode},
		{"decimal timestamp", `{"phone":"phone","pcode":10001,"apikey":"key","nonce": "3093", "timestamp":1.5,"sign":"x"}`, rescode.ErrInvalidTimestamp},
		{"string timestamp", `{"phone":"phone","pcode":10001,"apikey":"key","nonce": "3093", "timestamp":"1","sign":"x"}`, rescode.ErrInvalidTimestamp},
		{"overflow timestamp", `{"phone":"phone","pcode":10001,"apikey":"key","nonce": "3093", "timestamp":9223372036854775808,"sign":"x"}`, rescode.ErrInvalidTimestamp},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := performCreate(t, tt.body, query, successfulSID, testSecret)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("HTTP status = %d, want %d", response.Code, http.StatusBadRequest)
			}
			if got := decodeResponse(t, response).Status; got != tt.wantStatus {
				t.Fatalf("business status = %d, want %d", got, tt.wantStatus)
			}
		})
	}
	if queryCalled {
		t.Fatal("query called for invalid request")
	}
}

func TestCreate_RejectsNonStringPhoneWithFieldError(t *testing.T) {
	timestamp := time.Now().UnixMilli()
	body := `{"phone":13800138000,"pcode":10001,"apikey":"key","nonce": "3093", "timestamp":` + jsonNumber(timestamp) + `,"sign":"x"}`
	query := func(context.Context, string, int) (map[string]int, error) {
		t.Fatal("query called for invalid phone")
		return nil, nil
	}

	response := performCreate(t, body, query, successfulSID, testSecret)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("HTTP status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if got := decodeResponse(t, response).Status; got != rescode.ErrInvalidPhone {
		t.Fatalf("business status = %d, want %d", got, rescode.ErrInvalidPhone)
	}
}

func TestCreate_MapsDataErrorsWithoutLeakingDetails(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantHTTP   int
		wantStatus int
	}{
		{"not found", queryloan.ErrNotFound, http.StatusOK, rescode.ErrDataNotFound},
		{"product mapping", queryloan.ErrProductMapping, http.StatusOK, rescode.ErrProductException},
		{"Redis unavailable", queryloan.ErrUnavailable, http.StatusServiceUnavailable, rescode.ErrServiceUnavailable},
		{"Redis timeout", errors.Join(queryloan.ErrUnavailable, context.DeadlineExceeded), http.StatusServiceUnavailable, rescode.ErrServiceUnavailable},
		{"timeout", context.DeadlineExceeded, http.StatusGatewayTimeout, rescode.ErrTimeout},
		{"canceled", context.Canceled, http.StatusGatewayTimeout, rescode.ErrTimeout},
		{"unavailable", queryloan.ErrUnavailable, http.StatusServiceUnavailable, rescode.ErrServiceUnavailable},
		{"invalid data", queryloan.ErrInvalidData, http.StatusInternalServerError, rescode.ErrInternalError},
		{"unknown", errors.New("driver secret details"), http.StatusInternalServerError, rescode.ErrInternalError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := func(context.Context, string, int) (map[string]int, error) {
				return nil, tt.err
			}
			response := performCreate(t, validRequestBody(t), query, successfulSID, testSecret)
			if response.Code != tt.wantHTTP {
				t.Fatalf("HTTP status = %d, want %d", response.Code, tt.wantHTTP)
			}
			body := decodeResponse(t, response)
			if body.Status != tt.wantStatus {
				t.Fatalf("business status = %d, want %d", body.Status, tt.wantStatus)
			}
			if strings.Contains(body.Msg, "driver secret details") {
				t.Fatalf("response leaked internal error: %q", body.Msg)
			}
		})
	}
}

func TestCreate_ReturnsSuccess(t *testing.T) {
	wantData := map[string]int{"var100001": 10}
	query := func(context.Context, string, int) (map[string]int, error) {
		return wantData, nil
	}

	response := performCreate(t, validRequestBody(t), query, successfulSID, testSecret)
	if response.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d", response.Code, http.StatusOK)
	}
	body := decodeResponse(t, response)
	if body.Status != rescode.SuccessCode || body.Sid != "100testsid" || body.Data["var100001"] != 10 {
		t.Fatalf("response = %#v", body)
	}
}

func TestCreate_HandlesSIDGenerationFailure(t *testing.T) {
	queryCalled := false
	query := func(context.Context, string, int) (map[string]int, error) {
		queryCalled = true
		return nil, nil
	}
	sid := func(string, int) (string, error) {
		return "", errors.New("random source details")
	}

	response := performCreate(t, validRequestBody(t), query, sid, testSecret)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("HTTP status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	body := decodeResponse(t, response)
	if body.Status != rescode.ErrInternalError || strings.Contains(body.Msg, "random source details") {
		t.Fatalf("response = %#v", body)
	}
	if queryCalled {
		t.Fatal("query called after SID generation failed")
	}
}

func performCreate(t *testing.T, body string, query QueryFunc, sid SIDFunc, secret string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/loan", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler, err := NewLoanHandler(query, sid, nil)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	routes.RegisterAPIRoutes(router, handler.Create, middleware.LoanAuth(testKeyReader("key", secret)))
	router.ServeHTTP(response, request)
	return response
}

func validRequestBody(t *testing.T) string {
	t.Helper()
	timestamp := time.Now().UnixMilli()
	params := map[string]interface{}{
		"phone":  "phone",
		"pcode":  10001,
		"apikey": "key",
		"nonce":  "3093", "timestamp": timestamp,
	}
	body := map[string]interface{}{
		"phone":  params["phone"],
		"pcode":  params["pcode"],
		"apikey": params["apikey"],
		"nonce":  "3093", "timestamp": timestamp,
		"sign": utils.GenerateSign(map[string]interface{}{"apikey": params["apikey"], "nonce": params["nonce"], "timestamp": params["timestamp"]}, testSecret),
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func successfulSID(string, int) (string, error) {
	return "100testsid", nil
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder) CommonRes {
	t.Helper()
	var body CommonRes
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}

func jsonNumber(value int64) string {
	return strconv.FormatInt(value, 10)
}

func TestLoanHandler_UsesOriginalPhoneAndRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	called := false
	handler, err := NewLoanHandler(func(ctx context.Context, phone string, pcode int) (map[string]int, error) {
		called = true
		if phone != "phone" || !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("query did not receive original phone and canceled context")
		}
		return nil, ctx.Err()
	}, successfulSID, nil)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	routes.RegisterAPIRoutes(router, handler.Create, middleware.LoanAuth(testKeyReader("key", testSecret)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/loan", strings.NewReader(validRequestBody(t))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if !called || w.Code != 504 {
		t.Fatalf("called=%v, HTTP=%d", called, w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 200 || w.Body.String() != "ok" {
		t.Fatal("health should not require authentication")
	}
}

func TestLoanHandler_FailsClosedWithoutMiddleware(t *testing.T) {
	handler, err := NewLoanHandler(func(context.Context, string, int) (map[string]int, error) { t.Fatal("query called"); return nil, nil }, successfulSID, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.POST("/loan", handler.Create)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/loan", strings.NewReader(validRequestBody(t))))
	if w.Code != 401 {
		t.Fatalf("HTTP = %d", w.Code)
	}
	if _, err := NewLoanHandler(nil, successfulSID, nil); err == nil {
		t.Fatal("missing dependency accepted")
	}
}
