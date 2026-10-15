//go:build mysqlinit

package main

import (
	"strings"
	"testing"
)

func TestMySQLInitRequiresExplicitEnable(t *testing.T) {
	t.Setenv("MYSQL_ENABLED", "false")
	if err := runMySQLInit(true); err == nil || !strings.Contains(err.Error(), "MYSQL_ENABLED") {
		t.Fatal("关闭 MySQL 时不应初始化")
	}
}
