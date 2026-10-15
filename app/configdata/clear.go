package configdata

import "context"

// ClearStats 仅返回各类已清理条数，不返回缓存键或凭据。
type ClearStats struct {
	Credentials   int `json:"credentials"`
	ProductAccess int `json:"product_access"`
	ProductMaps   int `json:"product_maps"`
}

// Clear 清理本进程快照并隔离在途回源，不修改存储或中断已接受的请求。
func (r *Reader) Clear(ctx context.Context) (ClearStats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return ClearStats{}, err
	}
	if r.closed {
		return ClearStats{}, context.Canceled
	}
	// 所有多缓存操作遵循相同锁顺序；关闭与清理由生命周期锁串行化。
	r.credentials.mu.Lock()
	defer r.credentials.mu.Unlock()
	r.products.mu.Lock()
	defer r.products.mu.Unlock()
	r.mappings.mu.Lock()
	defer r.mappings.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return ClearStats{}, err
	}
	return ClearStats{Credentials: r.credentials.clearLocked(), ProductAccess: r.products.clearLocked(), ProductMaps: r.mappings.clearLocked()}, nil
}
