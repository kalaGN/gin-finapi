//go:build mysqlinit

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/kalaGN/gin-finapi/pkg/config"
	appmysql "github.com/kalaGN/gin-finapi/pkg/mysql"
)

func main() {
	schemaOnly := flag.Bool("schema-only", false, "只创建缺失表，不写合成样例")
	flag.Parse()
	if err := runMySQLInit(*schemaOnly); err != nil {
		log.Fatal(err)
	}
}

// 单文件运行；不运行 Mongo 初始化，不删除、清空或覆盖已有记录。
func runMySQLInit(schemaOnly bool) (err error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return errors.New("加载 .env 失败，请检查格式和权限")
	}
	if err := config.Load(); err != nil {
		return err
	}
	s, err := appmysql.NewStore(config.Config.MySQL)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	ctx := context.Background()
	if err = s.Ping(ctx); err != nil {
		return fmt.Errorf("MySQL 连接检查失败: %w", err)
	}
	if err = s.ApplySchema(ctx); err != nil {
		return err
	}
	if schemaOnly {
		fmt.Println("MySQL 四表已检查，已有表保留")
		return nil
	}
	stats, err := s.SeedSamples(ctx)
	if err != nil {
		return fmt.Errorf("MySQL 样例写入失败: %w", err)
	}
	fmt.Printf("MySQL 样例初始化完成：新增客户=%d，凭据=%d，产品=%d，授权=%d；已有记录保留\n", stats.CustomersAdded, stats.KeysAdded, stats.ProductsAdded, stats.GrantsAdded)
	return nil
}
