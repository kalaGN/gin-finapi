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
	"github.com/kalaGN/gin-finapi/app/configdata"
	"github.com/kalaGN/gin-finapi/app/middleware"
	"github.com/kalaGN/gin-finapi/app/services"
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
			credential := services.Credential{Version: 1, Secret: "test-secret", Status: 1}
			productFound := true
			var storageErr, mappingErr error
			reads := 0
			reader, err := configdata.New(configdata.Sources{
				Credentials: []configdata.Source[string, services.Credential]{func(context.Context, string) (services.Credential, bool, error) {
					reads++
					return credential, true, storageErr
				}},
				Products: []configdata.Source[services.ProductKey, services.ProductAccess]{func(context.Context, services.ProductKey) (services.ProductAccess, bool, error) {
					reads++
					return services.ProductAccess{Version: 1, Status: 1}, productFound, storageErr
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
			query, err := services.NewQuery(reader.LoadProductMap, func(context.Context, string, map[string]int) (map[string]int, error) {
				queries++
				return map[string]int{"a": 5}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			handler, err := handlerloan.NewLoanHandler(query, nil)
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			routes.RegisterAPIRoutes(router, handler.Create, middleware.LoanAuthWithProduct(reader.LoadCredential, reader.CheckProductAccess), middleware.RequestSID(func(string, int) (string, error) { return "test-sid", nil }, nil))
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
				mappingErr = services.ErrInvalidRecord
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

// 组合真实读取层、认证、查询用例和HTTP出口；只有存储边界使用替身。
func TestLoanBusinessScenarioMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, stage, redis, mysql, phone string
		queryErr                         error
		http, code, sqlReads             int
	}{
		{name: "正常查询", http: 200},
		{name: "凭据Redis缺失回源", stage: "credential", redis: "missing", http: 200, sqlReads: 1},
		{name: "凭据Redis故障回源", stage: "credential", redis: "failure", http: 200, sqlReads: 1},
		{name: "凭据Redis损坏回源", stage: "credential", redis: "invalid", http: 200, sqlReads: 1},
		{name: "Redis禁用Key不可绕过", stage: "credential", redis: "disabled", http: 400, code: 2001},
		{name: "Redis到期Key不可绕过", stage: "credential", redis: "expired", http: 400, code: 2001},
		{name: "MySQLKey缺失", stage: "credential", redis: "failure", mysql: "missing", http: 400, code: 2001, sqlReads: 1},
		{name: "MySQLKey禁用", stage: "credential", redis: "missing", mysql: "disabled", http: 400, code: 2001, sqlReads: 1},
		{name: "MySQLKey到期", stage: "credential", redis: "missing", mysql: "expired", http: 400, code: 2001, sqlReads: 1},
		{name: "MySQL凭据损坏", stage: "credential", redis: "missing", mysql: "invalid", http: 503, code: 9002, sqlReads: 1},
		{name: "凭据双源故障", stage: "credential", redis: "failure", mysql: "failure", http: 503, code: 9002, sqlReads: 1},
		{name: "授权Redis缺失回源", stage: "access", redis: "missing", http: 200, sqlReads: 1},
		{name: "授权Redis故障回源", stage: "access", redis: "failure", http: 200, sqlReads: 1},
		{name: "授权Redis损坏回源", stage: "access", redis: "invalid", http: 200, sqlReads: 1},
		{name: "Redis禁用授权不可绕过", stage: "access", redis: "disabled", http: 403, code: 2004},
		{name: "Redis到期授权不可绕过", stage: "access", redis: "expired", http: 403, code: 2004},
		{name: "MySQL授权缺失", stage: "access", redis: "missing", mysql: "missing", http: 403, code: 2004, sqlReads: 1},
		{name: "MySQL授权禁用", stage: "access", redis: "missing", mysql: "disabled", http: 403, code: 2004, sqlReads: 1},
		{name: "MySQL授权到期", stage: "access", redis: "missing", mysql: "expired", http: 403, code: 2004, sqlReads: 1},
		{name: "MySQL授权损坏", stage: "access", redis: "missing", mysql: "invalid", http: 503, code: 9002, sqlReads: 1},
		{name: "授权双源故障", stage: "access", redis: "failure", mysql: "failure", http: 503, code: 9002, sqlReads: 1},
		{name: "映射Redis缺失回源", stage: "mapping", redis: "missing", http: 200, sqlReads: 1},
		{name: "映射Redis故障回源", stage: "mapping", redis: "failure", http: 200, sqlReads: 1},
		{name: "映射Redis损坏回源", stage: "mapping", redis: "invalid", http: 200, sqlReads: 1},
		{name: "MySQL映射缺失", stage: "mapping", redis: "missing", mysql: "missing", http: 200, code: 6001, sqlReads: 1},
		{name: "MySQL映射损坏", stage: "mapping", redis: "missing", mysql: "invalid", http: 200, code: 6001, sqlReads: 1},
		{name: "映射双源故障", stage: "mapping", redis: "failure", mysql: "failure", http: 503, code: 9002, sqlReads: 1},
		{name: "Mongo未查得", queryErr: services.ErrNotFound, http: 200, code: 3001},
		{name: "Mongo不可用", queryErr: services.ErrUnavailable, http: 503, code: 9002},
		{name: "Mongo坏数据", queryErr: services.ErrInvalidData, http: 500, code: 9001},
		{name: "Mongo超时", queryErr: context.DeadlineExceeded, http: 504, code: 9003},
		{name: "Mongo取消", queryErr: context.Canceled, http: 504, code: 9003},
		{name: "未知查询错误", queryErr: errors.New("synthetic-private-detail"), http: 500, code: 9001},
		{name: "SHA256转换尚未接入", phone: strings.Repeat("a", 64), http: 503, code: 9002},
		{name: "64位非十六进制原值查询", phone: strings.Repeat("z", 64), http: 200},
	} {
		for _, disabled := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "缓存", true: "直读"}[disabled], func(t *testing.T) {
				sqlReads, finds := 0, 0
				outcome := func(stage string, lower bool) string {
					if stage != tc.stage {
						return ""
					}
					if lower {
						sqlReads++
						return tc.mysql
					}
					return tc.redis
				}
				state := func(mode string) (int, int64, bool, error) {
					switch mode {
					case "missing":
						return 0, 0, false, nil
					case "invalid":
						return 0, 0, false, services.ErrInvalidRecord
					case "failure":
						return 0, 0, false, services.ErrUnavailable
					case "disabled":
						return 0, 0, true, nil
					case "expired":
						return 1, 1, true, nil
					}
					return 1, 0, true, nil
				}
				credential := func(lower bool) configdata.Source[string, services.Credential] {
					return func(context.Context, string) (services.Credential, bool, error) {
						status, expiry, found, err := state(outcome("credential", lower))
						return services.Credential{Version: 1, Secret: "matrix-secret", Status: status, ExpiresAt: expiry}, found, err
					}
				}
				access := func(lower bool) configdata.Source[services.ProductKey, services.ProductAccess] {
					return func(context.Context, services.ProductKey) (services.ProductAccess, bool, error) {
						status, expiry, found, err := state(outcome("access", lower))
						return services.ProductAccess{Version: 1, Status: status, ExpiresAt: expiry}, found, err
					}
				}
				mapping := func(lower bool) configdata.Source[int, map[string]int] {
					return func(context.Context, int) (map[string]int, bool, error) {
						_, _, found, err := state(outcome("mapping", lower))
						return map[string]int{"a": 0}, found, err
					}
				}
				reader, err := configdata.New(configdata.Sources{
					Credentials: []configdata.Source[string, services.Credential]{configdata.Fallback(credential(false), credential(true), nil)},
					Products:    []configdata.Source[services.ProductKey, services.ProductAccess]{configdata.Fallback(access(false), access(true), nil)},
					Maps:        []configdata.Source[int, map[string]int]{configdata.Fallback(mapping(false), mapping(true), nil)},
				}, config.CacheConfig{Disabled: disabled, Concurrency: 1}, time.Second)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := reader.Close(context.Background()); err != nil {
						t.Error(err)
					}
				})
				phone := tc.phone
				if phone == "" {
					phone = "ABCDEFGHIJKLMNOPQRST012345678901"
				}
				query, err := services.NewQuery(reader.LoadProductMap, func(_ context.Context, lookup string, mapping map[string]int) (map[string]int, error) {
					finds++
					want := phone
					if len(want) == 32 {
						want = want[:20]
					}
					if lookup != want || mapping["a"] != 0 {
						t.Error("查询标识或映射改变")
					}
					return map[string]int{"a": 7}, tc.queryErr
				})
				if err != nil {
					t.Fatal(err)
				}
				handler, err := handlerloan.NewLoanHandler(query, nil)
				if err != nil {
					t.Fatal(err)
				}
				router := gin.New()
				routes.RegisterAPIRoutes(router, handler.Create, middleware.LoanAuthWithProduct(reader.LoadCredential, reader.CheckProductAccess), middleware.RequestSID(func(string, int) (string, error) { return "matrix-sid", nil }, nil))
				body := map[string]any{"phone": phone, "pcode": 10001, "apikey": "matrix-key", "nonce": "0007", "timestamp": time.Now().UnixMilli()}
				body["sign"] = utils.GenerateSign(map[string]any{"apikey": body["apikey"], "nonce": body["nonce"], "timestamp": body["timestamp"]}, "matrix-secret")
				encoded, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				for attempt := 0; attempt < 2; attempt++ {
					before := sqlReads
					req := httptest.NewRequest("POST", "/api", strings.NewReader(string(encoded)))
					req.Header.Set("Content-Type", "application/json")
					response := httptest.NewRecorder()
					router.ServeHTTP(response, req)
					var result request.Response
					if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					if response.Code != tc.http || result.Status != tc.code || strings.Contains(response.Body.String(), "private-detail") {
						t.Fatalf("HTTP=%d status=%d", response.Code, result.Status)
					}
					if attempt == 0 && sqlReads != tc.sqlReads {
						t.Fatalf("回源次数=%d，期望%d", sqlReads, tc.sqlReads)
					}
					if disabled && sqlReads-before != tc.sqlReads {
						t.Fatal("关闭缓存时未按请求回源")
					}
					if attempt == 1 && !disabled {
						wantReads := 0
						if tc.mysql == "missing" || tc.mysql == "invalid" || tc.mysql == "failure" {
							wantReads = tc.sqlReads
						}
						if sqlReads-before != wantReads {
							t.Fatal("有效快照未复用，或缺失/失败结果被缓存")
						}
					}
					if tc.code == 0 {
						if result.Sid != "matrix-sid" || result.Data["a"] != 7 {
							t.Fatal("成功响应不符合合同")
						}
					} else if result.Sid != "" || result.Data != nil {
						t.Fatal("失败响应包含成功数据或SID")
					}
				}
				wantFinds := 0
				if tc.code == 0 || tc.queryErr != nil {
					wantFinds = 2
				}
				if finds != wantFinds {
					t.Fatal("被拒绝请求触及数据查询或重放行为改变")
				}
			})
		}
	}
}
