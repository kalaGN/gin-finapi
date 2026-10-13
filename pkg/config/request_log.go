package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type RequestLogConfig struct {
	Output    string
	File      string
	QueueSize int
}

func (c RequestLogConfig) Validate() error {
	if c.Output == "" {
		return nil
	}
	if c.Output != "file" && c.Output != "redis" {
		return fmt.Errorf("REQUEST_LOG_OUTPUT must be file or redis")
	}
	if c.QueueSize <= 0 {
		return fmt.Errorf("REQUEST_LOG_QUEUE_SIZE must be positive")
	}
	if c.Output == "file" && c.File == "" {
		return fmt.Errorf("REQUEST_LOG_FILE is required")
	}
	return nil
}

func loadRequestLog() (RequestLogConfig, RedisConfig, error) {
	c := RequestLogConfig{Output: getEnvWithDefault("REQUEST_LOG_OUTPUT", "file"), File: getEnvWithDefault("REQUEST_LOG_FILE", "request.log")}
	var err error
	c.QueueSize, err = strconv.Atoi(getEnvWithDefault("REQUEST_LOG_QUEUE_SIZE", "10000"))
	if err != nil {
		return c, RedisConfig{}, fmt.Errorf("REQUEST_LOG_QUEUE_SIZE must be positive")
	}
	if err = c.Validate(); err != nil {
		return c, RedisConfig{}, err
	}
	if c.Output != "redis" {
		return c, RedisConfig{}, nil
	}
	r := RedisConfig{Addr: os.Getenv("CHARGE_REDIS_ADDR"), Password: os.Getenv("CHARGE_REDIS_PASSWORD")}
	r.DB, err = strconv.Atoi(getEnvWithDefault("CHARGE_REDIS_DB", "0"))
	if err != nil || r.DB < 0 {
		return c, r, fmt.Errorf("CHARGE_REDIS_DB must be non-negative")
	}
	r.PoolSize, err = strconv.Atoi(getEnvWithDefault("CHARGE_REDIS_POOL_SIZE", "10"))
	if err != nil || r.PoolSize <= 0 {
		return c, r, fmt.Errorf("CHARGE_REDIS_POOL_SIZE must be positive")
	}
	r.Timeout, err = time.ParseDuration(getEnvWithDefault("CHARGE_REDIS_TIMEOUT", "1s"))
	if err != nil || r.Timeout <= 0 {
		return c, r, fmt.Errorf("CHARGE_REDIS_TIMEOUT must be a positive duration")
	}
	if r.Addr == "" {
		return c, r, fmt.Errorf("CHARGE_REDIS_ADDR is required")
	}
	return c, r, nil
}
