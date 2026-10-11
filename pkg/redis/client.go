package redis

import (
	"errors"

	"github.com/kalaGN/gin-finapi/pkg/config"
	driver "github.com/redis/go-redis/v9"
)

// NewClientWithConfig 统一客户端选项；每次调用拥有独立连接池，懒连接且无自动重试。
func NewClientWithConfig(cfg config.RedisConfig) (*driver.Client, error) {
	if cfg.Addr == "" || cfg.PoolSize <= 0 || cfg.DB < 0 || cfg.Timeout <= 0 {
		return nil, errors.New("invalid Redis configuration")
	}
	return driver.NewClient(&driver.Options{
		Addr: cfg.Addr, Password: cfg.Password, DB: cfg.DB, PoolSize: cfg.PoolSize,
		DialTimeout: cfg.Timeout, ReadTimeout: cfg.Timeout, WriteTimeout: cfg.Timeout,
		ContextTimeoutEnabled: true, MaxRetries: -1,
	}), nil
}
