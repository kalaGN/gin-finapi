package bench

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/Http/controllers/loan"
	"github.com/kalaGN/gin-finapi/app/configdata"
	"github.com/kalaGN/gin-finapi/app/middleware"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/bootstrap"
	"github.com/kalaGN/gin-finapi/pkg/config"
	applog "github.com/kalaGN/gin-finapi/pkg/logger"
	"github.com/kalaGN/gin-finapi/pkg/utils"
	"github.com/sirupsen/logrus"
)

// BenchmarkAPI 测量进程内 HTTP 链路；不加载 .env、不监听端口、不连接 Redis/Mongo。
func BenchmarkAPI(b *testing.B) {
	gin.SetMode(gin.TestMode)
	for _, disabled := range []bool{false, true} {
		for _, concurrency := range []int{1, 32} {
			b.Run(fmt.Sprintf("cache_%t/c%d/file", !disabled, concurrency), func(b *testing.B) {
				benchmarkAPI(b, disabled, concurrency)
			})
		}
	}
}

func benchmarkAPI(b *testing.B, disabled bool, concurrency int) {
	b.StopTimer()
	if b.N > 1000000 {
		b.Fatal("请使用有界 -benchtime=5000x，避免生成过量临时日志")
	}
	var reads atomic.Int64
	reader, err := configdata.New(configdata.Sources{
		Credentials: []configdata.Source[string, services.Credential]{func(context.Context, string) (services.Credential, bool, error) {
			reads.Add(1)
			return services.Credential{Version: 1, Secret: "synthetic-secret", Status: 1}, true, nil
		}},
		Products: []configdata.Source[services.ProductKey, services.ProductAccess]{func(context.Context, services.ProductKey) (services.ProductAccess, bool, error) {
			reads.Add(1)
			return services.ProductAccess{Version: 1, Status: 1}, true, nil
		}},
		Maps: []configdata.Source[int, map[string]int]{func(context.Context, int) (map[string]int, bool, error) {
			reads.Add(1)
			return map[string]int{"var100001": 0, "var100002": 1, "var100003": 2, "var100004": 3, "var100005": 4, "var100006": 5}, true, nil
		}},
	}, config.CacheConfig{Disabled: disabled}, time.Second)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { closeBenchmark(b, reader.Close) })
	base := logrus.New()
	base.SetOutput(io.Discard)
	logFile := filepath.Join(b.TempDir(), "request.log")
	log, closeLog, err := applog.NewRequestLogger(base, config.RequestLogConfig{Output: "file", File: logFile, QueueSize: 10000, MaxBytes: 64 << 20}, config.RedisConfig{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { closeBenchmark(b, closeLog) })
	query, err := services.NewQuery(reader.LoadProductMap, func(_ context.Context, key string, mapping map[string]int) (map[string]int, error) {
		if key != "0123456789abcdef0123" {
			return nil, services.ErrInvalidData
		}
		result := make(map[string]int, len(mapping))
		for name, index := range mapping {
			result[name] = index + 1
		}
		return result, nil
	})
	if err != nil {
		b.Fatal(err)
	}
	h, err := loan.NewLoanHandler(query, utils.GenerateSID, base)
	if err != nil {
		b.Fatal(err)
	}
	r := gin.New()
	closeRoute := bootstrap.SetupRoute(r, h.Create, middleware.LoanAuthWithProduct(reader.LoadCredential, reader.CheckProductAccess), base, config.RateLimitConfig{Requests: int(^uint(0) >> 1), Window: time.Minute}, log)
	b.Cleanup(func() { closeBenchmark(b, closeRoute) })
	params := map[string]interface{}{"apikey": "synthetic-key", "nonce": "0001", "timestamp": time.Now().UnixMilli()}
	sign := utils.GenerateSign(params, "synthetic-secret")
	params["phone"], params["pcode"], params["sign"] = "0123456789abcdef0123456789abcdef", 10001, sign
	body, err := json.Marshal(params)
	if err != nil {
		b.Fatal(err)
	}
	perform := func() bool {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		var result struct {
			Status int
			Data   map[string]int
			Sid    string
		}
		return w.Code == 200 && json.Unmarshal(w.Body.Bytes(), &result) == nil && result.Status == 0 && len(result.Data) == 6 && result.Data["var100006"] == 6 && result.Sid != ""
	}
	const warmup = 100
	for range warmup {
		if !perform() {
			b.Fatal("预热请求失败")
		}
	}
	initialReads := reads.Load()
	durations := make([]time.Duration, b.N)
	var next, failures atomic.Int64
	var workers sync.WaitGroup
	b.ReportAllocs()
	b.ResetTimer()
	b.StartTimer()
	start := time.Now()
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				i := int(next.Add(1) - 1)
				if i >= b.N {
					return
				}
				tick := time.Now()
				if !perform() {
					failures.Add(1)
				}
				durations[i] = time.Since(tick)
			}
		}()
	}
	workers.Wait()
	elapsed := time.Since(start)
	b.StopTimer()
	closeBenchmark(b, closeLog)
	f, err := os.Open(logFile)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	var records int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		records++
	}
	if err := scanner.Err(); err != nil {
		b.Fatal(err)
	}
	if records != b.N+warmup {
		b.Fatalf("日志未完整落盘: %d/%d", records, b.N+warmup)
	}
	if failures.Load() != 0 {
		b.Fatalf("请求失败: %d/%d", failures.Load(), b.N)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	var total time.Duration
	for _, d := range durations {
		total += d
	}
	percentile := func(p float64) float64 {
		return float64(durations[int(math.Ceil(p*float64(b.N)))-1].Nanoseconds()) / 1000
	}
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "req/s")
	b.ReportMetric(float64(total.Nanoseconds())/float64(b.N)/1000, "avg_us")
	b.ReportMetric(percentile(.95), "p95_us")
	b.ReportMetric(percentile(.99), "p99_us")
	b.ReportMetric(0, "failure_pct")
	b.ReportMetric(0, "log_missing")
	b.ReportMetric(float64(reads.Load()-initialReads)/float64(b.N), "source_reads/op")
}

func closeBenchmark(b *testing.B, close func(context.Context) error) {
	b.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := close(ctx); err != nil {
		b.Fatal(err)
	}
}
