package configdata

import (
	"context"
	"errors"

	"github.com/kalaGN/gin-finapi/app/services"
)

// Fallback 显式组合 Redis 与权威存储；有效命中（包括禁用/到期）不能被下层覆盖。
// 通用 Source 链仍保持错误即停止，只有装配此适配器才启用错误回源。
func Fallback[K comparable, V any](primary, secondary Source[K, V], report func(string)) Source[K, V] {
	if primary == nil || secondary == nil {
		return nil
	}
	return func(ctx context.Context, key K) (V, bool, error) {
		var zero V
		if err := ctx.Err(); err != nil {
			return zero, false, err
		}
		value, found, err := primary(ctx, key)
		if err == nil && found {
			return value, true, nil
		}
		if canceled := ctx.Err(); canceled != nil {
			return zero, false, canceled
		}
		if report != nil {
			reason := "missing"
			if errors.Is(err, services.ErrInvalidRecord) {
				reason = "invalid_record"
			} else if err != nil {
				reason = "unavailable"
			}
			report(reason)
		}
		return secondary(ctx, key)
	}
}
