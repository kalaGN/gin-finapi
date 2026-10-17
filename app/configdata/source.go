package configdata

import (
	"context"

	"github.com/kalaGN/gin-finapi/pkg/utils"
)

// NamedSource 在适配器实际取得记录时标记来源；缺失或错误不宣称命中，不改变回源结果。
func NamedSource[K comparable, V any](kind, name string, source Source[K, V]) Source[K, V] {
	if source == nil {
		return nil
	}
	return func(ctx context.Context, key K) (V, bool, error) {
		value, found, err := source(ctx, key)
		if found && err == nil {
			utils.SetConfigSource(ctx, kind, name)
		}
		return value, found, err
	}
}
