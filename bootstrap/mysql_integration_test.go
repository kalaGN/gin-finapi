//go:build integration

package bootstrap

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
)

// 验证真实启动装配的回源链；关闭定时发布，不连接现有 Redis 或 Mongo。
func TestLocalMySQLConfigurationWiring(t *testing.T) {
	if os.Getenv("FINAPI_LOCAL_MYSQL_TEST") != "1" {
		t.Skip("需显式启用本地集成验证")
	}
	if err := godotenv.Load("../.env"); err != nil {
		t.Fatal("无法加载本地配置")
	}
	if err := config.Load(); err != nil {
		t.Fatal(err)
	}
	cfg := *config.Config
	if cfg.MySQL.Host != "127.0.0.1" || cfg.MySQL.Database != "fin_api" {
		t.Fatal("本用例只读本地 fin_api")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB = listener.Addr().String(), "", 0
	cfg.Redis.Timeout = 100 * time.Millisecond
	_ = listener.Close()
	for _, disabled := range []bool{false, true} {
		cfg.Cache.Disabled = disabled
		cfg.MySQL.SyncEnabled = false
		runtime, err := newConfiguration(cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if _, err := runtime.reader.LoadCredential(ctx, "example-test-key"); err != nil {
			t.Fatal("启动装配的凭据回源失败")
		}
		if err := runtime.reader.CheckProductAccess(ctx, "example-test-key", 10001); err != nil {
			t.Fatal("启动装配的授权回源失败")
		}
		if err := runtime.reader.CheckProductAccess(ctx, "example-test-key-2", 10002); !errors.Is(err, services.ErrProductDenied) {
			t.Fatal("启动装配的授权隔离失败")
		}
		if mapping, err := runtime.reader.LoadProductMap(ctx, 10001); err != nil || len(mapping) != 6 {
			t.Fatal("启动装配的映射回源失败")
		}
		if runtime.syncer != nil {
			t.Fatal("同步开关未关闭worker")
		}
		if err := runtime.close(ctx); err != nil {
			t.Fatal(err)
		}
		cancel()
	}
	// 构造同步器不产生外部写入；只有显式 start 才启动发布。
	cfg.MySQL.SyncEnabled = true
	runtime, err := newConfiguration(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.syncer == nil {
		t.Fatal("启用开关后未装配同步器")
	}
	if err := runtime.close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
