//go:build integration

package bootstrap

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/pkg/config"
	appredis "github.com/kalaGN/gin-finapi/pkg/redis"
	"github.com/kalaGN/gin-finapi/pkg/utils"
	redis "github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	mongo "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Explicit opt-in endpoints must point to disposable test services, never app databases.
func TestLoanIntegration(t *testing.T) {
	addr, dsn := os.Getenv("FINAPI_TEST_REDIS_ADDR"), os.Getenv("FINAPI_TEST_MONGO_DSN")
	if addr == "" || dsn == "" {
		t.Fatal("integration requires FINAPI_TEST_REDIS_ADDR and FINAPI_TEST_MONGO_DSN for disposable services")
	}
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rc := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := rc.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	mc, err := mongo.Connect(ctx, options.Client().ApplyURI(dsn))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mc.Disconnect(c); err != nil {
			t.Error(err)
		}
	})
	if err := mc.Ping(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatal(err)
	}
	token := hex.EncodeToString(id[:])
	database := "finapi_integration_" + token
	db := mc.Database(database)
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Drop(c); err != nil {
			t.Error(err)
		}
	})
	var payload bytes.Buffer
	zw := gzip.NewWriter(&payload)
	if _, err := zw.Write([]byte("10,20,30,40,50,60")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	phone := "integration-fixture"
	if _, err := db.Collection("data_20251101_0").InsertOne(ctx, bson.M{"t": phone, "v": payload.Bytes()}); err != nil {
		t.Fatal(err)
	}
	const pcode = 98765
	mapKey := appredis.ProductMapRedisKey(pcode)
	mapping := `{"var100001":0,"var100002":1,"var100003":2,"var100004":3,"var100005":4,"var100006":5}`
	// NX refuses to replace any pre-existing product configuration, even on a misconfigured endpoint.
	added, err := rc.SetNX(ctx, mapKey, mapping, 0).Result()
	if err != nil || !added {
		t.Fatalf("test mapping not reserved: added=%v err=%v", added, err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := rc.Del(c, mapKey).Err(); err != nil {
			t.Error(err)
		}
	})
	expected := map[string]int{"var100001": 10, "var100002": 20, "var100003": 30, "var100004": 40, "var100005": 50, "var100006": 60}
	for _, tc := range []struct {
		name, access string
		cached       bool
		http, code   int
	}{
		{"success cached", `{"version":1,"status":1,"expires_at":0}`, true, 200, 0},
		{"success direct", `{"version":1,"status":1,"expires_at":0}`, false, 200, 0},
		{"missing access", "", false, 403, 2004},
		{"disabled access", `{"version":1,"status":0,"expires_at":0}`, false, 403, 2004},
		{"expired access", `{"version":1,"status":1,"expires_at":1}`, false, 403, 2004},
		{"corrupt access", `{"version":1,"status":1}`, false, 503, 9002},
		{"wrong Redis type", "list", false, 503, 9002},
	} {
		t.Run(tc.name, func(t *testing.T) {
			apiKey := "integration-" + token + tc.name
			key := appredis.APIKeyRedisKey(apiKey)
			accessKey := appredis.APIKeyProductRedisKey(apiKey, pcode)
			t.Cleanup(func() {
				c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := rc.Del(c, key, accessKey).Err(); err != nil {
					t.Error(err)
				}
			})
			if err := rc.Set(ctx, key, `{"version":1,"secret":"integration-test-secret","status":1,"expires_at":0}`, 0).Err(); err != nil {
				t.Fatal(err)
			}
			if tc.access == "list" {
				if err := rc.RPush(ctx, accessKey, "invalid").Err(); err != nil {
					t.Fatal(err)
				}
			} else if tc.access != "" {
				if err := rc.Set(ctx, accessKey, tc.access, 0).Err(); err != nil {
					t.Fatal(err)
				}
			}
			cfg := config.AppConfig{MongoDB: config.MongoDBConfig{DSN: dsn, Database: database, Collection: "data_20251101_0", Timeout: "2s", MaxPool: 4}, Redis: config.RedisConfig{Addr: addr, PoolSize: 4, Timeout: time.Second}, RateLimit: config.RateLimitConfig{Requests: 1000, Window: time.Second}, Cache: config.CacheConfig{Disabled: !tc.cached}}
			if tc.code != 0 {
				cfg.MongoDB.DSN = "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=100"
			}
			log := logrus.New()
			log.SetOutput(&bytes.Buffer{})
			router, closeDependencies, err := NewRouter(cfg, log)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := closeDependencies(c); err != nil {
					t.Error(err)
				}
			}()
			// Repeating the same nonce is allowed by the current contract; it is not replay protection.
			for attempt := 0; attempt < 2; attempt++ {
				body := map[string]interface{}{"phone": phone, "pcode": pcode, "apikey": apiKey, "nonce": "0001", "timestamp": time.Now().UnixMilli()}
				body["sign"] = utils.GenerateSign(map[string]interface{}{"apikey": body["apikey"], "nonce": body["nonce"], "timestamp": body["timestamp"]}, "integration-test-secret")
				raw, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest("POST", "/loan", bytes.NewReader(raw))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				var result struct {
					Status int            `json:"status"`
					Data   map[string]int `json:"data"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				wantHTTP, wantCode := tc.http, tc.code
				if tc.code == 0 && attempt == 1 && !tc.cached {
					wantHTTP, wantCode = 403, 2004
				}
				if w.Code != wantHTTP || result.Status != wantCode {
					t.Fatalf("HTTP=%d status=%d, want %d/%d", w.Code, result.Status, wantHTTP, wantCode)
				}
				if wantCode == 0 && !reflect.DeepEqual(result.Data, expected) {
					t.Fatalf("unexpected variable mapping: %v", result.Data)
				}
				if tc.code == 0 && attempt == 0 {
					// Cached mode retains its snapshot until refresh; direct mode observes revocation immediately.
					if err := rc.Set(ctx, accessKey, `{"version":1,"status":0,"expires_at":0}`, 0).Err(); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
