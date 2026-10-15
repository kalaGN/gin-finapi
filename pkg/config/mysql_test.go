package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func mysqlEnvironment(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"MYSQL_ENABLED": "true", "MYSQL_HOST": "127.0.0.1", "MYSQL_PORT": "3306", "MYSQL_DATABASE": "fin_api", "MYSQL_USER": "test-user", "MYSQL_PASSWORD": "test-password",
		"MYSQL_TLS_MODE": "disabled", "MYSQL_SECRET_KEY": base64.StdEncoding.EncodeToString(make([]byte, 32)), "MYSQL_SECRET_KEY_ID": "test-v1",
		"MYSQL_CONNECT_TIMEOUT": "", "MYSQL_QUERY_TIMEOUT": "", "MYSQL_WRITE_TIMEOUT": "", "MYSQL_MAX_OPEN_CONNS": "", "MYSQL_MAX_IDLE_CONNS": "", "MYSQL_CONN_MAX_LIFETIME": "", "MYSQL_CONN_MAX_IDLE_TIME": "", "MYSQL_SYNC_ENABLED": "", "MYSQL_SYNC_INTERVAL": "", "MYSQL_SYNC_TIMEOUT": "",
	} {
		t.Setenv(key, value)
	}
}

func TestMySQLConfigDefaultsAndDisable(t *testing.T) {
	mysqlEnvironment(t)
	c, err := loadMySQLConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !c.Enabled || !c.SyncEnabled || c.SyncInterval != 5*time.Minute || c.Port != 3306 || c.QueryTimeout != time.Second {
		t.Fatal("MySQL 默认配置不正确")
	}
	t.Setenv("MYSQL_ENABLED", "false")
	t.Setenv("MYSQL_PORT", "invalid")
	if _, err := loadMySQLConfig(); err != nil {
		t.Fatal("关闭时不应解析预留配置")
	}
}

func TestMySQLConfigRejectsInvalidWithoutSecrets(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"MYSQL_ENABLED", "invalid"}, {"MYSQL_HOST", ""}, {"MYSQL_DATABASE", ""}, {"MYSQL_USER", ""},
		{"MYSQL_PORT", "0"}, {"MYSQL_PORT", "65536"}, {"MYSQL_PORT", "invalid"}, {"MYSQL_QUERY_TIMEOUT", "0s"}, {"MYSQL_WRITE_TIMEOUT", "wrong"},
		{"MYSQL_MAX_OPEN_CONNS", "0"}, {"MYSQL_MAX_IDLE_CONNS", "30"}, {"MYSQL_TLS_MODE", "insecure"},
		{"MYSQL_SECRET_KEY", "sensitive-invalid-key"}, {"MYSQL_SECRET_KEY_ID", ""}, {"MYSQL_SYNC_ENABLED", "yes"}, {"MYSQL_SYNC_INTERVAL", "0s"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			mysqlEnvironment(t)
			t.Setenv(tc.key, tc.value)
			_, err := loadMySQLConfig()
			if err == nil {
				t.Fatal("无效配置未被拒绝")
			}
			if strings.Contains(err.Error(), "test-password") || strings.Contains(err.Error(), "sensitive-invalid-key") {
				t.Fatal("配置错误泄露敏感值")
			}
		})
	}
}
