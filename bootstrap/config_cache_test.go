package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	handlerloan "github.com/kalaGN/gin-finapi/app/Http/controllers/loan"
	"github.com/kalaGN/gin-finapi/app/auth"
	"github.com/kalaGN/gin-finapi/app/configdata"
	queryloan "github.com/kalaGN/gin-finapi/app/loan"
	"github.com/kalaGN/gin-finapi/app/middleware"
	"github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/kalaGN/gin-finapi/pkg/utils"
	"github.com/kalaGN/gin-finapi/routes"
)

// 缓存回归走真实中间件、查询用例及 Handler，不连接外部 Redis/Mongo。
func TestLoanWithConfigCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		name       string
		http, code int
	}{
		{"warm cache during outage", 200, 0}, {"disabled credential", 400, 2001}, {"deleted product access", 403, 2004}, {"invalid mapping", 200, 6001},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := context.Background()
			credential := auth.Credential{Version: 1, Secret: "test-secret", Status: 1}
			productFound := true
			var storageErr, mappingErr error
			reads := 0
			reader, err := configdata.New(configdata.Sources{
				Credentials: []configdata.Source[string, auth.Credential]{func(context.Context, string) (auth.Credential, bool, error) {
					reads++
					return credential, true, storageErr
				}},
				Products: []configdata.Source[configdata.ProductKey, auth.ProductAccess]{func(context.Context, configdata.ProductKey) (auth.ProductAccess, bool, error) {
					reads++
					return auth.ProductAccess{Version: 1, Status: 1}, productFound, storageErr
				}},
				Maps: []configdata.Source[int, map[string]int]{func(context.Context, int) (map[string]int, bool, error) {
					reads++
					if storageErr != nil {
						return nil, false, storageErr
					}
					return map[string]int{"a": 0}, true, mappingErr
				}},
			}, config.CacheConfig{Concurrency: 1}, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close(ctx)
			queries := 0
			query, err := queryloan.NewQuery(reader.LoadProductMap, func(context.Context, string, map[string]int) (map[string]int, error) {
				queries++
				return map[string]int{"a": 5}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			handler, err := handlerloan.NewLoanHandler(query, func(string, int) (string, error) { return "test-sid", nil }, nil)
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			routes.RegisterAPIRoutes(router, handler.Create, middleware.LoanAuthWithProduct(reader.LoadCredential, reader.CheckProductAccess))
			send := func() (*httptest.ResponseRecorder, request.Response) {
				t.Helper()
				body := map[string]interface{}{"phone": "0123456789abcdef0123456789abcdef", "pcode": 10001, "apikey": "test-key", "nonce": "3093", "timestamp": time.Now().UnixMilli()}
				body["sign"] = utils.GenerateSign(map[string]interface{}{"apikey": body["apikey"], "nonce": body["nonce"], "timestamp": body["timestamp"]}, "test-secret")
				data, _ := json.Marshal(body)
				req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(string(data)))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				var result request.Response
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				return w, result
			}
			w, result := send()
			if w.Code != 200 || result.Status != 0 {
				t.Fatalf("warm HTTP=%d status=%d", w.Code, result.Status)
			}
			switch scenario.name {
			case "warm cache during outage":
				storageErr = errors.New("network")
			case "disabled credential":
				credential.Status = 0
			case "deleted product access":
				productFound = false
			case "invalid mapping":
				mappingErr = configdata.ErrInvalidRecord
			}
			reader.Refresh(ctx)
			before := reads
			w, result = send()
			if w.Code != scenario.http || result.Status != scenario.code {
				t.Fatalf("HTTP=%d status=%d", w.Code, result.Status)
			}
			if scenario.code != 0 && queries != 1 {
				t.Fatal("rejected request reached Mongo boundary")
			}
			if scenario.code == 0 && reads != before {
				t.Fatal("warm cache contacted failing source")
			}
		})
	}
}
