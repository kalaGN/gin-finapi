// Package services 定义业务流程和与存储实现无关的错误语义。
package services

import (
	"context"
	"errors"
)

var (
	ErrProductMapping = errors.New("product mapping is invalid")
	ErrUnavailable    = errors.New("query dependency unavailable")
	ErrNotFound       = errors.New("query data not found")
	ErrInvalidData    = errors.New("query data invalid")
)

type QueryFunc func(context.Context, string, int) (map[string]int, error)

// NewQuery 按产品加载映射后查询数据，不缓存、不访问全局配置。
func NewQuery(load func(context.Context, int) (map[string]int, error), find func(context.Context, string, map[string]int) (map[string]int, error)) (QueryFunc, error) {
	if load == nil || find == nil {
		return nil, errors.New("query requires mapping and data dependencies")
	}
	return func(ctx context.Context, phone string, pcode int) (map[string]int, error) {
		mapping, err := load(ctx, pcode)
		if err != nil {
			return nil, err
		}
		return find(ctx, phone, mapping)
	}, nil
}
