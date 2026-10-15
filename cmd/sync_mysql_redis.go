//go:build mysqlsync

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/kalaGN/gin-finapi/app/configdata"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
	appmysql "github.com/kalaGN/gin-finapi/pkg/mysql"
	appredis "github.com/kalaGN/gin-finapi/pkg/redis"
)

func main() {
	apply := flag.Bool("apply", false, "将 MySQL 快照发布到配置中的 Redis；默认仅检查 MySQL 并统计，不写 Redis")
	flag.Parse()
	if err := runMySQLRedisSync(*apply); err != nil {
		log.Fatal(err)
	}
}

func runMySQLRedisSync(apply bool) (err error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return errors.New("加载 .env 失败，请检查格式和权限")
	}
	if err := config.Load(); err != nil {
		return err
	}
	cfg := config.Config
	store, err := appmysql.NewStore(cfg.MySQL)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	var sink *appredis.ConfigPublisher
	if apply {
		sink, err = appredis.NewConfigPublisher(cfg.Redis)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, sink.Close()) }()
	}
	stats, err := executeMySQLSync(context.Background(), apply, store.Snapshot, sink, configdata.SyncOptions{Interval: cfg.MySQL.SyncInterval, Timeout: cfg.MySQL.SyncTimeout, ReleaseTimeout: cfg.Redis.Timeout})
	if err != nil {
		return err
	}
	mode := "dry-run（未连接或写入 Redis）"
	if apply {
		mode = "已发布 Redis"
	}
	if stats.Skipped {
		mode = "其他实例同步中，本轮跳过"
	}
	fmt.Printf("MySQL 同步 %s：凭据=%d，授权=%d，映射=%d，清理托管键=%d\n", mode, stats.Credentials, stats.Products, stats.Mappings, stats.Removed)
	return nil
}

func executeMySQLSync(ctx context.Context, apply bool, load func(context.Context) (services.ConfigSnapshot, error), sink configdata.SyncSink, options configdata.SyncOptions) (stats configdata.SyncStats, err error) {
	if load == nil {
		return stats, errors.New("MySQL snapshot source is required")
	}
	if !apply {
		ctx, cancel := context.WithTimeout(ctx, options.Timeout)
		defer cancel()
		snapshot, err := load(ctx)
		if err != nil {
			return stats, err
		}
		if err := snapshot.Validate(); err != nil {
			return stats, err
		}
		stats.Credentials, stats.Products, stats.Mappings = len(snapshot.Credentials), len(snapshot.Products), len(snapshot.Mappings)
		return stats, nil
	}
	syncer, err := configdata.NewSynchronizer(load, sink, options, nil)
	if err != nil {
		return stats, err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err = errors.Join(err, syncer.Close(closeCtx))
	}()
	return syncer.Run(ctx)
}
