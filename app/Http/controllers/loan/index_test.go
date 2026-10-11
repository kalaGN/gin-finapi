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
	appmongo "github.com/kalaGN/gin-finapi/pkg/mongo"
	"github.com/kalaGN/gin-finapi/pkg/rescode"
	"github.com/kalaGN/gin-finapi/pkg/utils"
)

const testSecret = "test-secret"

func TestCreate_RejectsNonIntegerNumbers(t *testing.T) {
	timestamp := time.Now().UnixMilli()
	queryCalled := false
	query := func(context.Context, appmongo.Config) (map[string]int, error) {
		queryCalled = true
		return nil, nil
	}

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"decimal pcode", `{"phone":"phone","pcode":10001.5,"apikey":"key","timestamp":` + jsonNumber(timestamp) + `,"sign":"x"}`, rescode.ErrInvalidPcode},
		{"string pcode", `{"phone":"phone","pcode":"10001","apikey":"key","timestamp":` + jsonNumber(timestamp) + `,"sign":"x"}`, rescode.ErrInvalidPcode},
		{"exponent pcode", `{"phone":"phone","pcode":1e4,"apikey":"key","timestamp":` + jsonNumber(timestamp) + `,"sign":"x"}`, rescode.ErrInvalidPcode},
		{"overflow pcode", `{"phone":"phone","pcode":9223372036854775808,"apikey":"key","timestamp":` + jsonNumber(timestamp) + `,"sign":"x"}`, rescode.ErrInvalidPcode},
		{"out of range pcode", `{"phone":"phone","pcode":100000,"apikey":"key","timestamp":` + jsonNumber(timestamp) + `,"sign":"x"}`, rescode.ErrInvalidPcode},
		{"decimal timestamp", `{"phone":"phone","pcode":10001,"apikey":"key","timestamp":1.5,"sign":"x"}`, rescode.ErrInvalidTimestamp},
		{"string timestamp", `{"phone":"phone","pcode":10001,"apikey":"key","timestamp":"1","sign":"x"}`, rescode.ErrInvalidTimestamp},
		{"overflow timestamp", `{"phone":"phone","pcode":10001,"apikey":"key","timestamp":9223372036854775808,"sign":"x"}`, rescode.ErrInvalidTimestamp},
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
	body := `{"phone":13800138000,"pcode":10001,"apikey":"key","timestamp":` + jsonNumber(timestamp) + `,"sign":"x"}`
	query := func(context.Context, appmongo.Config) (map[string]int, error) {
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
		{"not found", appmongo.ErrNotFound, http.StatusOK, rescode.ErrDataNotFound},
		{"timeout", context.DeadlineExceeded, http.StatusGatewayTimeout, rescode.ErrTimeout},
		{"canceled", context.Canceled, http.StatusGatewayTimeout, rescode.ErrTimeout},
		{"unavailable", appmongo.ErrUnavailable, http.StatusServiceUnavailable, rescode.ErrServiceUnavailable},
		{"invalid data", appmongo.ErrInvalidData, http.StatusInternalServerError, rescode.ErrInternalError},
		{"unknown", errors.New("driver secret details"), http.StatusInternalServerError, rescode.ErrInternalError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := func(context.Context, appmongo.Config) (map[string]int, error) {
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
	query := func(context.Context, appmongo.Config) (map[string]int, error) {
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
	query := func(context.Context, appmongo.Config) (map[string]int, error) {
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

func performCreate(t *testing.T, body string, query queryFunc, sid sidFunc, secret string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/loan", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	create(ctx, query, sid, secret)
	return response
}

func validRequestBody(t *testing.T) string {
	t.Helper()
	timestamp := time.Now().UnixMilli()
	params := map[string]interface{}{
		"phone":     "phone",
		"pcode":     10001,
		"apikey":    "key",
		"timestamp": timestamp,
	}
	body := map[string]interface{}{
		"phone":     params["phone"],
		"pcode":     params["pcode"],
		"apikey":    params["apikey"],
		"timestamp": timestamp,
		"sign":      utils.GenerateSign(params, testSecret),
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
