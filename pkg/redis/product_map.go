package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/kalaGN/gin-finapi/pkg/config"
	driver "github.com/redis/go-redis/v9"
)

var (
	ErrProductMapping     = errors.New("product mapping is missing or invalid")
	ErrMappingUnavailable = errors.New("product mapping service unavailable")
)

// ProductMaps 按产品读取变量映射，区分 Redis 故障和产品配置异常。
type ProductMaps struct {
	hget    func(context.Context, string, string) (string, error)
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
		hget: func(ctx context.Context, key, field string) (string, error) {
			return client.HGet(ctx, key, field).Result()
		}, close: client.Close, timeout: cfg.Timeout,
	}, nil
}

// Load 获取 mongo_map 中 pcode 对应的 JSON，不回退或回写默认映射。
func (maps *ProductMaps) Load(ctx context.Context, pcode int) (map[string]int, error) {
	readCtx, cancel := context.WithTimeout(ctx, maps.timeout)
	defer cancel()
	raw, err := maps.hget(readCtx, "mongo_map", strconv.Itoa(pcode))
	if errors.Is(err, driver.Nil) {
		return nil, ErrProductMapping
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMappingUnavailable, err)
	}
	return parseProductMap(raw)
}

// Close 释放该读取器持有的连接池，不操作 Redis 数据。
func (maps *ProductMaps) Close() error {
	return maps.close()
}

func parseProductMap(raw string) (map[string]int, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrProductMapping
	}
	result := make(map[string]int)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, ErrProductMapping
		}
		name, ok := token.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return nil, ErrProductMapping
		}
		if _, exists := result[name]; exists {
			return nil, ErrProductMapping
		}
		// 解析原始整数文本，拒绝 null、字符串、小数和指数表示。
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, ErrProductMapping
		}
		index, err := strconv.Atoi(string(value))
		if err != nil || index < 0 || index == int(^uint(0)>>1) {
			return nil, ErrProductMapping
		}
		result[name] = index
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || len(result) == 0 {
		return nil, ErrProductMapping
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return nil, ErrProductMapping
	}
	return result, nil
}
