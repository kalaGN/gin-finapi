//go:build redisinit

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	appredis "github.com/kalaGN/gin-finapi/pkg/redis"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	"github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/redis/go-redis/v9"
)

// 单文件运行，避免与 init_mongo.go 的 main 同时进入默认构建。
func main() {
	pcode := flag.Int("pcode", 10001, "需要初始化的产品编码（10001–99999）")
	authSamples := flag.Bool("auth-samples", false, "增加 API Key 凭据和产品授权样例（从 INIT_API_KEY/INIT_API_SECRET 读取）")
	flag.Parse()
	if err := runRedisInit(*pcode, *authSamples); err != nil {
		log.Fatal(err)
	}
}

func runRedisInit(pcode int, authSamples bool) (err error) {
	if pcode < 10001 || pcode > 99999 {
		return fmt.Errorf("pcode 必须在 10001–99999 之间")
	}
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		// 解析错误可能携带配置原文，不将其写入日志。
		return errors.New("加载 .env 失败，请检查文件格式和读取权限")
	}
	apiKey, secret := os.Getenv("INIT_API_KEY"), os.Getenv("INIT_API_SECRET")
	if authSamples {
		if err := validateAuthSample(apiKey, secret); err != nil {
			return err
		}
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
	if authSamples {
		credentialAdded, productAdded, err := seedRedisAuthSamples(ctx, client, pcode, apiKey, secret)
		if err != nil {
			return errors.New("初始化认证样例失败，可能已部分写入；重试保留已有值")
		}
		log.Printf("认证样例处理完成：凭据新增=%t，产品授权新增=%t（已有值保留）", credentialAdded, productAdded)
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

// redisAuthInitializer only permits non-overwriting String writes.
type redisAuthInitializer interface {
	SetNX(context.Context, string, interface{}, time.Duration) *redis.BoolCmd
}

func validateAuthSample(apiKey, secret string) error {
	if apiKey == "" || secret == "" || len(secret) > 256 {
		return errors.New("认证样例需要非空 INIT_API_KEY 和 1–256 字节 INIT_API_SECRET")
	}
	return nil
}

// seedRedisAuthSamples preserves existing records, including invalid or disabled ones.
// Two writes are not atomic; a retry can complete a partially written sample.
func seedRedisAuthSamples(ctx context.Context, client redisAuthInitializer, pcode int, apiKey, secret string) (bool, bool, error) {
	if pcode < 10001 || pcode > 99999 {
		return false, false, errors.New("pcode 必须在 10001–99999 之间")
	}
	if err := validateAuthSample(apiKey, secret); err != nil {
		return false, false, err
	}
	value, err := json.Marshal(appredis.APIKeyCredential{Version: 1, Secret: secret, Status: 1, ExpiresAt: 0})
	if err != nil {
		return false, false, errors.New("无法生成凭据样例")
	}
	key := appredis.APIKeyRedisKey(apiKey)
	added, err := client.SetNX(ctx, key, string(value), 0).Result()
	if err != nil {
		return false, false, errors.New("写入凭据样例失败")
	}
	productKey := "gin-finapi:auth:apikey-product:v1:" + key[len("gin-finapi:auth:apikey:v1:"):] + ":" + strconv.Itoa(pcode)
	productAdded, err := client.SetNX(ctx, productKey, `{"version":1,"status":1,"expires_at":0}`, 0).Result()
	if err != nil {
		return added, false, errors.New("写入产品授权样例失败")
	}
	return added, productAdded, nil
}
