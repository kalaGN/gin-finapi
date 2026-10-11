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
	"github.com/kalaGN/gin-finapi/app/Http/controllers/loan"
	"github.com/kalaGN/gin-finapi/app/middleware"
	"github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/kalaGN/gin-finapi/pkg/mongo"
	appredis "github.com/kalaGN/gin-finapi/pkg/redis"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/kalaGN/gin-finapi/pkg/utils"
	"github.com/kalaGN/gin-finapi/routes"
)

func TestLoanQuery_UsesProductMapThroughAuthenticatedRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		pcode      int
		mappingErr error
		http, code int
	}{
		{"first product", 10001, nil, 200, 0},
		{"second product", 10002, nil, 200, 0},
		{"product invalid", 10001, appredis.ErrProductMapping, 200, 6001},
		{"Redis unavailable", 10001, appredis.ErrMappingUnavailable, 503, 9002},
		{"Redis deadline", 10001, errors.Join(appredis.ErrMappingUnavailable, context.DeadlineExceeded), 503, 9002},
	} {
		t.Run(tc.name, func(t *testing.T) {
			load := func(ctx context.Context, pcode int) (map[string]int, error) {
				if pcode != tc.pcode {
					t.Fatalf("pcode=%d", pcode)
				}
				if tc.mappingErr != nil {
					return nil, tc.mappingErr
				}
				if pcode == 10001 {
					return map[string]int{"first": 0, "last": 5}, nil
				}
				return map[string]int{"other": 1, "last": 5}, nil
			}
			query := newLoanQuery(config.MongoDBConfig{DSN: "test-dsn", Database: "test-db", Collection: "test-collection"}, load, func(ctx context.Context, cfg mongo.Config) (map[string]int, error) {
				if tc.mappingErr != nil {
					t.Fatal("Mongo queried for invalid mapping")
				}
				if cfg.Query != "0123456789abcdef0123" || cfg.DB != "test-db" || cfg.Collection != "test-collection" || cfg.DSN != "test-dsn" {
					t.Fatal("query config changed")
				}
				return mongo.ProcessUserData(cfg.Mapping, []string{"10", "20", "30", "40", "50", "60"})
			})
			handler, err := loan.NewLoanHandler(query, func(string, int) (string, error) { return "test-sid", nil }, nil)
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			routes.RegisterAPIRoutes(router, handler.Create, middleware.LoanAuth("test-key", "test-secret"))
			body := map[string]interface{}{"phone": "0123456789abcdef0123456789abcdef", "pcode": tc.pcode, "apikey": "test-key", "timestamp": time.Now().UnixMilli()}
			body["sign"] = utils.GenerateSign(body, "test-secret")
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/loan", strings.NewReader(string(data)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			var result request.Response
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.http || result.Status != tc.code {
				t.Fatalf("HTTP=%d body=%s", w.Code, w.Body)
			}
			if tc.code == 0 {
				if len(result.Data) != 2 || result.Data["last"] != 60 {
					t.Fatalf("data=%v", result.Data)
				}
				if tc.pcode == 10001 && result.Data["first"] != 10 {
					t.Fatal("wrong first product mapping")
				}
				if tc.pcode == 10002 && result.Data["other"] != 20 {
					t.Fatal("wrong second product mapping")
				}
			} else if result.Data != nil {
				t.Fatal("error response contains data")
			}
		})
	}
}
