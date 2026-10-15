package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
	driver "github.com/redis/go-redis/v9"
)

var (
	ErrProductMapping     = errors.New("product mapping is missing or invalid")
	ErrMappingUnavailable = errors.New("product mapping service unavailable")
)

// ProductMaps 按产品读取变量映射，区分 Redis 故障和产品配置异常。
type ProductMaps struct {
	get     func(context.Context, string) (string, error)
	close   func() error
	timeout time.Duration
}

// NewProductMaps 创建可复用客户端，不在装配时连接 Redis。
func NewProductMaps(cfg config.RedisConfig) (*ProductMaps, error) {
	client, err := NewClientWithConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &ProductMaps{
		get: func(ctx context.Context, key string) (string, error) {
			return client.Get(ctx, key).Result()
		}, close: client.Close, timeout: cfg.Timeout,
	}, nil
}

// ProductMapRedisKey 为每个产品生成独立 String Key。
func ProductMapRedisKey(pcode int) string { return "gin-finapi:product-map:v1:" + strconv.Itoa(pcode) }

// Load 获取产品 JSON，不回退或回写默认映射。
func (maps *ProductMaps) Load(ctx context.Context, pcode int) (map[string]int, error) {
	readCtx, cancel := context.WithTimeout(ctx, maps.timeout)
	defer cancel()
	raw, err := maps.get(readCtx, ProductMapRedisKey(pcode))
	if errors.Is(err, driver.Nil) {
		return nil, ErrProductMapping
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMappingUnavailable, err)
	}
	return parseProductMap(raw)
}

// ReadProductMap 将明确缺失与无效记录分别传递给统一读取层。
func (maps *ProductMaps) ReadProductMap(ctx context.Context, pcode int) (map[string]int, bool, error) {
	readCtx, cancel := context.WithTimeout(ctx, maps.timeout)
	defer cancel()
	raw, err := maps.get(readCtx, ProductMapRedisKey(pcode))
	if errors.Is(err, driver.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrMappingUnavailable, err)
	}
	value, err := parseProductMap(raw)
	if err != nil {
		return nil, false, errors.Join(ErrProductMapping, services.ErrInvalidRecord)
	}
	return value, true, nil
}

// ValidateProductMap 供显式迁移工具复用线上格式校验。
func ValidateProductMap(raw string) error { _, err := parseProductMap(raw); return err }

// Close 释放该读取器持有的连接池，不操作 Redis 数据。
func (maps *ProductMaps) Close() error {
	return maps.close()
}

func parseProductMap(raw string) (map[string]int, error) {
	value, err := services.ParseProductMap(raw)
	if err != nil {
		return nil, ErrProductMapping
	}
	return value, nil
}
