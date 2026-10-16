//go:build mysqlinit

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMySQLInitRequiresExplicitEnable(t *testing.T) {
	t.Setenv("MYSQL_ENABLED", "false")
	if err := runMySQLInit(true); err == nil || !strings.Contains(err.Error(), "MYSQL_ENABLED") {
		t.Fatal("关闭 MySQL 时不应初始化")
	}
}

func TestMySQLInitRejectsUnsafeConfigurationBeforeConnecting(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(filepath.Join(".", ".env"), []byte("VALUE=\"synthetic-private-value"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runMySQLInit(false); err == nil || strings.Contains(err.Error(), "private-value") {
		t.Fatal("损坏环境文件未安全拒绝")
	}
	if err := os.WriteFile(".env", nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MYSQL_ENABLED", "invalid")
	if err := runMySQLInit(false); err == nil || !strings.Contains(err.Error(), "MYSQL_ENABLED") {
		t.Fatal("非法开关仍执行初始化")
	}
	t.Setenv("MYSQL_ENABLED", "true")
	t.Setenv("MYSQL_HOST", "")
	if err := runMySQLInit(false); err == nil {
		t.Fatal("缺失连接配置仍执行初始化")
	}
}
