package config

import (
	"strings"
	"testing"
	"time"
)

func TestRequestLogConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, output, addr, db, pool, timeout, size, want string }{
		{"默认文件", "", "", "bad", "bad", "bad", "", ""},
		{"独立Redis", "redis", "charge.invalid:6380", "2", "3", "250ms", "8", ""},
		{"非法模式", "stdout", "", "0", "10", "1s", "10", "REQUEST_LOG_OUTPUT"},
		{"缺少地址", "redis", "", "0", "10", "1s", "10", "CHARGE_REDIS_ADDR"},
		{"非法DB", "redis", "charge.invalid:6380", "bad", "10", "1s", "10", "CHARGE_REDIS_DB"},
		{"负DB", "redis", "charge.invalid:6380", "-1", "10", "1s", "10", "CHARGE_REDIS_DB"},
		{"非法池", "redis", "charge.invalid:6380", "0", "0", "1s", "10", "CHARGE_REDIS_POOL_SIZE"},
		{"非法超时", "redis", "charge.invalid:6380", "0", "10", "0s", "10", "CHARGE_REDIS_TIMEOUT"},
		{"非法队列", "file", "", "0", "10", "1s", "0", "REQUEST_LOG_QUEUE_SIZE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("REQUEST_LOG_OUTPUT", tc.output)
			t.Setenv("REQUEST_LOG_MAX_BYTES", "")
			t.Setenv("REQUEST_LOG_FILE", "")
			t.Setenv("REQUEST_LOG_QUEUE_SIZE", tc.size)
			t.Setenv("CHARGE_REDIS_ADDR", tc.addr)
			t.Setenv("CHARGE_REDIS_DB", tc.db)
			t.Setenv("CHARGE_REDIS_POOL_SIZE", tc.pool)
			t.Setenv("CHARGE_REDIS_TIMEOUT", tc.timeout)
			c, r, err := loadRequestLog()
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("错误=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.output == "" {
				if c.Output != "file" || c.File != "request.log" || c.QueueSize != 10000 || r.Addr != "" {
					t.Fatal("默认或闲置配置错误")
				}
			} else if c.QueueSize != 8 || r.Addr != tc.addr || r.DB != 2 || r.PoolSize != 3 || r.Timeout != 250*time.Millisecond {
				t.Fatal("独立 Redis 配置错误")
			}
		})
	}
}

func TestRequestLogMaxBytes(t *testing.T) {
	for _, value := range []string{"", "1024", "0", "-1", "bad"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("REQUEST_LOG_OUTPUT", "file")
			t.Setenv("REQUEST_LOG_QUEUE_SIZE", "4")
			t.Setenv("REQUEST_LOG_MAX_BYTES", value)
			cfg, _, err := loadRequestLog()
			if value == "" || value == "1024" {
				want := int64(67108864)
				if value == "1024" {
					want = 1024
				}
				if err != nil || cfg.MaxBytes != want {
					t.Fatalf("配置=%+v 错误=%v", cfg, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "REQUEST_LOG_MAX_BYTES") {
				t.Fatal("无效字节预算被接受")
			}
		})
	}
}
