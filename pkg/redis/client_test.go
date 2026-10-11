package redis

import (
	"testing"
	"time"

	"github.com/kalaGN/gin-finapi/pkg/config"
	driver "github.com/redis/go-redis/v9"
)

func TestNewClientWithConfig_IndependentPoolsAndSharedOptions(t *testing.T) {
	cfg := config.RedisConfig{Addr: "127.0.0.1:1", DB: 2, PoolSize: 3, Timeout: 2 * time.Second}
	a, err := NewClientWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewClientWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Error(err)
		}
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	})
	if a == b {
		t.Fatal("unexpected shared ownership")
	}
	for _, client := range []interface{ Options() *driver.Options }{a, b} {
		opts := client.Options()
		if opts.DB != 2 || opts.PoolSize != 3 || opts.DialTimeout != cfg.Timeout || opts.ReadTimeout != cfg.Timeout || opts.WriteTimeout != cfg.Timeout || !opts.ContextTimeoutEnabled || opts.MaxRetries != 0 {
			t.Fatal("Redis options changed")
		}
	}
	if _, err := NewClientWithConfig(config.RedisConfig{}); err == nil {
		t.Fatal("invalid config accepted")
	}
}
