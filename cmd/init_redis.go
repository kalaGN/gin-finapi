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
	migrateMap := flag.Bool("migrate-map", false, "将指定产品的旧 mongo_map 字段迁移到 String KV，不覆盖目标或删除旧值")
	flag.Parse()
	if err := runRedisInit(*pcode, *authSamples, *migrateMap); err != nil {
		log.Fatal(err)
	}
}

func runRedisInit(pcode int, authSamples, migrateMap bool) (err error) {
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
	client, err := appredis.NewClientWithConfig(cfg)
	if err != nil {
		return err
	}

	defer func() { err = errors.Join(err, client.Close()) }()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	var added bool
	if migrateMap {
		added, err = migrateRedisMapping(ctx, client, pcode)
	} else {
		added, err = seedRedisMapping(ctx, client, pcode)
	}
	if err != nil {
		return fmt.Errorf("准备产品映射 %d 失败，请检查源数据、目标类型和 Redis 连接", pcode)
	}
	if added {
		log.Printf("已写入产品 KV 映射 %d", pcode)
	} else {
		log.Printf("产品 KV 映射 %d 已存在，保留原值", pcode)
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
	SetNX(context.Context, string, interface{}, time.Duration) *redis.BoolCmd
}

// seedRedisMapping 只添加缺失键，不覆盖已有产品或删除其他映射。
func seedRedisMapping(ctx context.Context, client redisInitializer, pcode int) (bool, error) {
	if pcode < 10001 || pcode > 99999 {
		return false, fmt.Errorf("pcode 必须在 10001–99999 之间")
	}
	const mapping = `{"var100001":0,"var100002":1,"var100003":2,"var100004":3,"var100005":4,"var100006":5}`
	return client.SetNX(ctx, appredis.ProductMapRedisKey(pcode), mapping, 0).Result()
}

type redisMappingMigrator interface {
	redisInitializer
	HGet(context.Context, string, string) *redis.StringCmd
}

// migrateRedisMapping 显式迁移一个产品；保留旧 Hash，以便旧版本回滚。
func migrateRedisMapping(ctx context.Context, client redisMappingMigrator, pcode int) (bool, error) {
	if pcode < 10001 || pcode > 99999 {
		return false, errors.New("pcode 必须在 10001–99999 之间")
	}
	raw, err := client.HGet(ctx, "mongo_map", strconv.Itoa(pcode)).Result()
	if err != nil {
		return false, errors.New("旧产品映射读取失败或缺失")
	}
	if err := appredis.ValidateProductMap(raw); err != nil {
		return false, errors.New("旧产品映射 JSON 无效")
	}
	return client.SetNX(ctx, appredis.ProductMapRedisKey(pcode), raw, 0).Result()
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
	productKey := appredis.APIKeyProductRedisKey(apiKey, pcode)
	productAdded, err := client.SetNX(ctx, productKey, `{"version":1,"status":1,"expires_at":0}`, 0).Result()
	if err != nil {
		return added, false, errors.New("写入产品授权样例失败")
	}
	return added, productAdded, nil
}
