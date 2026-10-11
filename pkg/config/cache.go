package config

import (
	"fmt"
	"strconv"
	"time"
)

// CacheConfig 约束每个进程的配置缓存容量和回源并发。
type CacheConfig struct {
	RefreshInterval    time.Duration
	APIKeyEntries      int
	ProductAuthEntries int
	ProductMapEntries  int
	Concurrency        int
	// Disabled 使用反向字段，让显式构造的零值配置继续默认启用缓存。
	Disabled bool
}

// WithDefaults 供显式构造配置的测试和调用方使用；环境变量仍须严格校验。
func (c CacheConfig) WithDefaults() CacheConfig {
	if c.RefreshInterval == 0 {
		c.RefreshInterval = 5 * time.Minute
	}
	if c.APIKeyEntries == 0 {
		c.APIKeyEntries = 1000
	}
	if c.ProductAuthEntries == 0 {
		c.ProductAuthEntries = 10000
	}
	if c.ProductMapEntries == 0 {
		c.ProductMapEntries = 1000
	}
	if c.Concurrency == 0 {
		c.Concurrency = 4
	}
	return c
}

func (c CacheConfig) Validate() error {
	if c.RefreshInterval <= 0 || c.APIKeyEntries <= 0 || c.ProductAuthEntries <= 0 || c.ProductMapEntries <= 0 || c.Concurrency <= 0 {
		return fmt.Errorf("invalid Redis cache configuration: durations and limits must be positive")
	}
	return nil
}

func loadCacheConfig() (CacheConfig, error) {
	c := CacheConfig{}.WithDefaults()
	enabled, err := strconv.ParseBool(getEnvWithDefault("REDIS_CACHE_ENABLED", "true"))
	if err != nil {
		return c, fmt.Errorf("REDIS_CACHE_ENABLED must be a boolean")
	}
	c.Disabled = !enabled
	interval, err := time.ParseDuration(getEnvWithDefault("REDIS_CACHE_REFRESH_INTERVAL", "5m"))
	if err != nil || interval <= 0 {
		return c, fmt.Errorf("REDIS_CACHE_REFRESH_INTERVAL must be a positive duration")
	}
	c.RefreshInterval = interval
	for _, item := range []struct {
		name   string
		target *int
	}{
		{"REDIS_APIKEY_CACHE_MAX_ENTRIES", &c.APIKeyEntries},
		{"REDIS_PRODUCT_AUTH_CACHE_MAX_ENTRIES", &c.ProductAuthEntries},
		{"REDIS_PRODUCT_MAP_CACHE_MAX_ENTRIES", &c.ProductMapEntries},
		{"REDIS_CACHE_SYNC_CONCURRENCY", &c.Concurrency},
	} {
		value, err := strconv.Atoi(getEnvWithDefault(item.name, strconv.Itoa(*item.target)))
		if err != nil || value <= 0 {
			return c, fmt.Errorf("%s must be a positive integer", item.name)
		}
		*item.target = value
	}
	return c, nil
}
