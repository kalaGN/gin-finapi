//go:build integration

package mysql

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
)

// 仅显式启用后读取授权本地库，不创建、删除或修改数据。
func TestLocalMySQLReadOnly(t *testing.T) {
	if os.Getenv("FINAPI_LOCAL_MYSQL_TEST") != "1" {
		t.Skip("需显式授权本地 MySQL 读取")
	}
	if err := godotenv.Load("../../.env"); err != nil {
		t.Fatal("无法加载本地配置")
	}
	if err := config.Load(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config.MySQL
	if cfg.Host != "127.0.0.1" || cfg.Database != "fin_api" {
		t.Fatal("本用例只读授权本地 fin_api")
	}
	s, err := NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		var databaseError *driver.MySQLError
		if errors.As(err, &databaseError) {
			t.Fatalf("MySQL Ping失败，驱动错误码=%d", databaseError.Number)
		}
		t.Fatalf("MySQL Ping失败，错误类型=%T，取消=%t，超时=%t", err, errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded))
	}
	for _, check := range []struct {
		query string
		want  int
	}{
		{"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='fin_api' AND table_name IN ('api_customers','api_keys','api_products','api_key_products') AND engine='InnoDB'", 4},
		{"SELECT COUNT(*) FROM information_schema.table_constraints WHERE constraint_schema='fin_api' AND constraint_type='FOREIGN KEY' AND table_name IN ('api_keys','api_key_products')", 3},
		{"SELECT COUNT(*) FROM information_schema.table_constraints WHERE constraint_schema='fin_api' AND constraint_type='CHECK' AND table_name IN ('api_customers','api_keys','api_products','api_key_products')", 4},
		{"SELECT COUNT(*) FROM api_customers", 1},
		{"SELECT COUNT(*) FROM api_keys", 2},
		{"SELECT COUNT(*) FROM api_products", 2},
		{"SELECT COUNT(*) FROM api_key_products", 3},
	} {
		var count int
		if err := s.db.QueryRowContext(ctx, check.query).Scan(&count); err != nil || count != check.want {
			t.Fatal("本地四表结构、约束或样例数量不符合预期")
		}
	}
	var encrypted []byte
	if err := s.db.QueryRowContext(ctx, "SELECT secret_ciphertext FROM api_keys WHERE id=1").Scan(&encrypted); err != nil || len(encrypted) <= 28 || bytes.Contains(encrypted, []byte("example-test-secret")) {
		t.Fatal("样例 secret 未以密文存储")
	}
	for _, apiKey := range []string{"example-test-key", "example-test-key-2"} {
		v, found, err := s.ReadCredential(ctx, services.Digest(apiKey))
		if err != nil || !found || v.Status != 1 || v.Secret == "" {
			t.Fatal("本地样例凭据读取失败")
		}
	}
	snapshot, err := s.Snapshot(ctx)
	if err != nil || snapshot.Validate() != nil || len(snapshot.Credentials) != 2 || len(snapshot.Products) != 3 || len(snapshot.Mappings) != 2 {
		t.Fatal("本地配置快照不完整")
	}
}
