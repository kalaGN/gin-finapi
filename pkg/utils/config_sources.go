package utils

import (
	"context"
	"maps"
	"sync"
)

type configSourcesKey struct{}
type configSources struct {
	mu     sync.Mutex
	values map[string]string
}

// StartConfigSources 创建独立来源记录器；已有记录器时保留，取消与截止时间仍由原Context管理。
func StartConfigSources(ctx context.Context) context.Context {
	if _, ok := ctx.Value(configSourcesKey{}).(*configSources); ok {
		return ctx
	}
	return context.WithValue(ctx, configSourcesKey{}, &configSources{values: make(map[string]string)})
}

// SetConfigSource 只允许固定的配置类型与真实层级，不接受请求输入作为标签。
func SetConfigSource(ctx context.Context, kind, source string) {
	switch kind {
	case "credentials", "product_access", "product_map":
	default:
		return
	}
	switch source {
	case "memory", "redis", "mysql":
	default:
		return
	}
	value, ok := ctx.Value(configSourcesKey{}).(*configSources)
	if !ok {
		return
	}
	value.mu.Lock()
	defer value.mu.Unlock()
	value.values[kind] = source
}

// ConfigSourcesSnapshot 返回独立快照；无记录时返回空对象，不把未执行/失败推断为命中。
func ConfigSourcesSnapshot(ctx context.Context) map[string]string {
	value, ok := ctx.Value(configSourcesKey{}).(*configSources)
	if !ok {
		return map[string]string{}
	}
	value.mu.Lock()
	defer value.mu.Unlock()
	return maps.Clone(value.values)
}
