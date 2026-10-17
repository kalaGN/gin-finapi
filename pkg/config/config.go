package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// AppConfig 应用配置结构
type AppConfig struct {
	Server           ServerConfig
	MongoDB          MongoDBConfig
	Redis            RedisConfig
	RateLimit        RateLimitConfig
	Cache            CacheConfig
	RequestLog       RequestLogConfig
	ChargeRedis      RedisConfig
	MySQL            MySQLConfig
	ConfigCacheToken string
}

// RateLimitConfig configures the existing per-process, per-IP limiter.
type RateLimitConfig struct {
	Requests int
	Window   time.Duration
}

func (c RateLimitConfig) Validate() error {
	if c.Requests <= 0 {
		return fmt.Errorf("RATE_LIMIT_REQUESTS must be a positive integer")
	}
	if c.Window <= 0 {
		return fmt.Errorf("RATE_LIMIT_WINDOW must be a positive duration")
	}
	return nil
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port       string
	Env        string
	InstanceID string
	Version    string
}

// MongoDBConfig MongoDB 配置
type MongoDBConfig struct {
	DSN        string
	Database   string
	Collection string
	Timeout    string
	MaxPool    int
	MinPool    int
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
	PoolSize int
	Timeout  time.Duration
}

var Config *AppConfig

// Load 从环境变量加载配置
func Load() error {
	token := os.Getenv("CONFIG_CACHE_TOKEN")
	if err := validateConfigCacheToken(token); err != nil {
		return err
	}
	mysql, err := loadMySQLConfig()
	if err != nil {
		return err
	}
	requestLog, chargeRedis, err := loadRequestLog()
	if err != nil {
		return err
	}
	cache, err := loadCacheConfig()
	if err != nil {
		return err
	}
	redisTimeout, err := time.ParseDuration(getEnvWithDefault("REDIS_TIMEOUT", "1s"))
	if err != nil || redisTimeout <= 0 {
		return fmt.Errorf("REDIS_TIMEOUT must be a positive duration")
	}
	requests, err := strconv.Atoi(getEnvWithDefault("RATE_LIMIT_REQUESTS", "5000"))
	if err != nil {
		return fmt.Errorf("RATE_LIMIT_REQUESTS must be a positive integer")
	}
	window, err := time.ParseDuration(getEnvWithDefault("RATE_LIMIT_WINDOW", "1s"))
	if err != nil {
		return fmt.Errorf("RATE_LIMIT_WINDOW must be a positive duration")
	}
	rateLimit := RateLimitConfig{Requests: requests, Window: window}
	if err := rateLimit.Validate(); err != nil {
		return err
	}
	Config = &AppConfig{
		Server: ServerConfig{
			Port:       getEnvWithDefault("SERVER_PORT", "8082"),
			Env:        getEnvWithDefault("ENV", "production"),
			InstanceID: os.Getenv("INSTANCE_ID"),
			Version:    os.Getenv("APP_VERSION"),
		}.WithLogIdentityDefaults(),
		MongoDB: MongoDBConfig{
			DSN:        os.Getenv("MONGODB_DSN"),
			Database:   os.Getenv("MONGODB_DATABASE"),
			Collection: getEnvWithDefault("MONGODB_COLLECTION", "data_20251101_0"),
			Timeout:    getEnvWithDefault("MONGODB_TIMEOUT", "5s"),
			MaxPool:    getEnvIntWithDefault("MONGODB_MAX_POOL", 100),
			MinPool:    getEnvIntWithDefault("MONGODB_MIN_POOL", 10),
		},
		Redis: RedisConfig{
			Addr:     getEnvWithDefault("REDIS_ADDR", "localhost:6379"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       getEnvIntWithDefault("REDIS_DB", 0),
			PoolSize: getEnvIntWithDefault("REDIS_POOL_SIZE", 10),
			Timeout:  redisTimeout,
		},
		RateLimit:        rateLimit,
		Cache:            cache,
		RequestLog:       requestLog,
		ChargeRedis:      chargeRedis,
		MySQL:            mysql,
		ConfigCacheToken: token,
	}

	return nil
}

// getEnvWithDefault 获取环境变量，如果不存在则返回默认值
func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvIntWithDefault 获取整数类型的环境变量，如果不存在或解析失败则返回默认值
func getEnvIntWithDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// Validate 验证配置
func (c *AppConfig) Validate() error {
	if err := validateConfigCacheToken(c.ConfigCacheToken); err != nil {
		return err
	}
	if err := c.MySQL.Validate(); err != nil {
		return err
	}
	if err := c.RequestLog.Validate(); err != nil {
		return err
	}
	if c.RequestLog.Output == "redis" && (c.ChargeRedis.Addr == "" || c.ChargeRedis.DB < 0 || c.ChargeRedis.PoolSize <= 0 || c.ChargeRedis.Timeout <= 0) {
		return fmt.Errorf("invalid charge Redis configuration")
	}
	if err := c.Cache.WithDefaults().Validate(); err != nil {
		return err
	}
	if c.Server.Port == "" {
		return fmt.Errorf("server port is required")
	}
	if c.MongoDB.DSN == "" {
		return fmt.Errorf("MongoDB DSN is required")
	}
	if c.MongoDB.Database == "" {
		return fmt.Errorf("MongoDB database is required")
	}
	if c.Redis.Addr == "" || c.Redis.DB < 0 || c.Redis.PoolSize <= 0 || c.Redis.Timeout <= 0 {
		return fmt.Errorf("invalid Redis configuration")
	}
	if err := c.RateLimit.Validate(); err != nil {
		return err
	}
	return nil
}

func validateConfigCacheToken(token string) error {
	if token != "" && (len(strings.Fields(token)) != 1 || strings.TrimSpace(token) != token) {
		return fmt.Errorf("CONFIG_CACHE_TOKEN must not contain whitespace")
	}
	return nil
}
