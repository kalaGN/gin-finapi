package config

import (
	"encoding/base64"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// MySQLConfig 只用于配置回源和同步，不承载 Mongo 业务数据。
type MySQLConfig struct {
	Enabled         bool
	Host            string
	Port            int
	Database        string
	User            string
	Password        string
	TLSMode         string
	TLSCAFile       string
	TLSServerName   string
	ConnectTimeout  time.Duration
	QueryTimeout    time.Duration
	WriteTimeout    time.Duration
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	SecretKey       string
	SecretKeyID     string
	SyncEnabled     bool
	SyncInterval    time.Duration
	SyncTimeout     time.Duration
}

func (c MySQLConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Host == "" || c.Database == "" || c.User == "" || c.Port < 1 || c.Port > 65535 {
		return errors.New("invalid MySQL host, port, database or user")
	}
	if c.TLSMode != "disabled" && c.TLSMode != "verify_identity" {
		return errors.New("MYSQL_TLS_MODE must be disabled or verify_identity")
	}
	if c.ConnectTimeout <= 0 || c.QueryTimeout <= 0 || c.WriteTimeout <= 0 || c.ConnMaxLifetime <= 0 || c.ConnMaxIdleTime <= 0 {
		return errors.New("MySQL durations must be positive")
	}
	if c.MaxOpenConns <= 0 || c.MaxIdleConns < 0 || c.MaxIdleConns > c.MaxOpenConns {
		return errors.New("invalid MySQL connection pool configuration")
	}
	key, err := base64.StdEncoding.DecodeString(c.SecretKey)
	if err != nil || len(key) != 32 || strings.TrimSpace(c.SecretKeyID) == "" {
		return errors.New("MYSQL_SECRET_KEY must be Base64 of 32 bytes and MYSQL_SECRET_KEY_ID is required")
	}
	if c.SyncInterval <= 0 || c.SyncTimeout <= 0 {
		return errors.New("MYSQL_SYNC_INTERVAL and MYSQL_SYNC_TIMEOUT must be positive")
	}
	return nil
}

func loadMySQLConfig() (MySQLConfig, error) {
	var c MySQLConfig
	var err error
	c.Enabled, err = strconv.ParseBool(getEnvWithDefault("MYSQL_ENABLED", "false"))
	if err != nil {
		return c, errors.New("MYSQL_ENABLED must be a boolean")
	}
	// 关闭时不解析历史预留项，不影响原有 Redis-only 部署。
	if !c.Enabled {
		return c, nil
	}
	c.Host, c.Database, c.User, c.Password = os.Getenv("MYSQL_HOST"), os.Getenv("MYSQL_DATABASE"), os.Getenv("MYSQL_USER"), os.Getenv("MYSQL_PASSWORD")
	c.TLSMode, c.TLSCAFile, c.TLSServerName = getEnvWithDefault("MYSQL_TLS_MODE", "verify_identity"), os.Getenv("MYSQL_TLS_CA_FILE"), os.Getenv("MYSQL_TLS_SERVER_NAME")
	c.SecretKey, c.SecretKeyID = os.Getenv("MYSQL_SECRET_KEY"), os.Getenv("MYSQL_SECRET_KEY_ID")
	for _, item := range []struct {
		name, fallback string
		target         *int
	}{
		{"MYSQL_PORT", "3306", &c.Port}, {"MYSQL_MAX_OPEN_CONNS", "20", &c.MaxOpenConns}, {"MYSQL_MAX_IDLE_CONNS", "10", &c.MaxIdleConns},
	} {
		*item.target, err = strconv.Atoi(getEnvWithDefault(item.name, item.fallback))
		if err != nil {
			return c, errors.New(item.name + " must be an integer")
		}
	}
	for _, item := range []struct {
		name, fallback string
		target         *time.Duration
	}{
		{"MYSQL_CONNECT_TIMEOUT", "1s", &c.ConnectTimeout}, {"MYSQL_QUERY_TIMEOUT", "1s", &c.QueryTimeout},
		{"MYSQL_WRITE_TIMEOUT", "5s", &c.WriteTimeout}, {"MYSQL_CONN_MAX_LIFETIME", "30m", &c.ConnMaxLifetime},
		{"MYSQL_CONN_MAX_IDLE_TIME", "5m", &c.ConnMaxIdleTime}, {"MYSQL_SYNC_INTERVAL", "5m", &c.SyncInterval}, {"MYSQL_SYNC_TIMEOUT", "30s", &c.SyncTimeout},
	} {
		*item.target, err = time.ParseDuration(getEnvWithDefault(item.name, item.fallback))
		if err != nil || *item.target <= 0 {
			return c, errors.New(item.name + " must be a positive duration")
		}
	}
	c.SyncEnabled, err = strconv.ParseBool(getEnvWithDefault("MYSQL_SYNC_ENABLED", "true"))
	if err != nil {
		return c, errors.New("MYSQL_SYNC_ENABLED must be a boolean")
	}
	return c, c.Validate()
}
