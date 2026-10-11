//go:build redisinit

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/redis/go-redis/v9"
)

// 单文件运行，避免与 init_mongo.go 的 main 同时进入默认构建。
func main() {
	pcode := flag.Int("pcode", 10001, "需要初始化的产品编码（10001–99999）")
	flag.Parse()
	if err := runRedisInit(*pcode); err != nil {
		log.Fatal(err)
	}
}

func runRedisInit(pcode int) (err error) {
	if pcode < 10001 || pcode > 99999 {
		return fmt.Errorf("pcode 必须在 10001–99999 之间")
	}
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		// 解析错误可能携带配置原文，不将其写入日志。
		return errors.New("加载 .env 失败，请检查文件格式和读取权限")
	}
	// 复用配置加载，不要求与该工具无关的 API Key 或 Mongo 必填配置。
	if err := config.Load(); err != nil {
		return err
	}
	cfg := config.Config.Redis
	if cfg.Addr == "" || cfg.DB < 0 || cfg.PoolSize <= 0 || cfg.Timeout <= 0 {
		return errors.New("Redis 初始化配置无效")
	}
	client := redis.NewClient(&redis.Options{
		Addr: cfg.Addr, Password: cfg.Password, DB: cfg.DB, PoolSize: cfg.PoolSize,
		DialTimeout: cfg.Timeout, ReadTimeout: cfg.Timeout, WriteTimeout: cfg.Timeout,
		ContextTimeoutEnabled: true, MaxRetries: -1,
	})
	defer func() { err = errors.Join(err, client.Close()) }()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	added, err := seedRedisMapping(ctx, client, pcode)
	if err != nil {
		return fmt.Errorf("初始化 mongo_map 产品 %d 失败: %w", pcode, err)
	}
	if added {
		log.Printf("已初始化 mongo_map 产品 %d", pcode)
	} else {
		log.Printf("mongo_map 产品 %d 已存在，保留原值", pcode)
	}
	return nil
}

// redisInitializer 隔离写操作，离线测试不连接实际 Redis。
type redisInitializer interface {
	HSetNX(context.Context, string, string, interface{}) *redis.BoolCmd
}

// seedRedisMapping 只添加缺失字段，不覆盖已有产品或删除其他映射。
func seedRedisMapping(ctx context.Context, client redisInitializer, pcode int) (bool, error) {
	if pcode < 10001 || pcode > 99999 {
		return false, fmt.Errorf("pcode 必须在 10001–99999 之间")
	}
	const mapping = `{"var100001":0,"var100002":1,"var100003":2,"var100004":3,"var100005":4,"var100006":5}`
	return client.HSetNX(ctx, "mongo_map", strconv.Itoa(pcode), mapping).Result()
}
