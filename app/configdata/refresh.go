package configdata

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// RefreshStats 只暴露固定计数，不暴露缓存键、密钥或驱动错误。
type RefreshStats struct {
	Checked, Failed int
	Duration        time.Duration
}

// Refresh 跳过内存，逐条发布快照；故障保留旧值，缺失或损坏移除旧值。
func (r *Reader) Refresh(ctx context.Context) RefreshStats {
	if r.disabled {
		return RefreshStats{}
	}
	start := time.Now()
	var checked, failed atomic.Int64
	jobs := make(chan func() error)
	var wg sync.WaitGroup
	for i := 0; i < r.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil || r.root.Err() != nil {
					continue
				}
				err := job()
				checked.Add(1)
				if err != nil {
					failed.Add(1)
				}
			}
		}()
	}
	send := func(job func() error) bool {
		select {
		case jobs <- job:
			return true
		case <-ctx.Done():
			return false
		case <-r.root.Done():
			return false
		}
	}
	for _, key := range r.credentials.keys() {
		if !send(func() error { _, _, err := r.credentials.load(ctx, key, true); return err }) {
			break
		}
	}
	for _, key := range r.products.keys() {
		if !send(func() error { _, _, err := r.products.load(ctx, key, true); return err }) {
			break
		}
	}
	for _, key := range r.mappings.keys() {
		if !send(func() error { _, _, err := r.mappings.load(ctx, key, true); return err }) {
			break
		}
	}
	close(jobs)
	wg.Wait()
	return RefreshStats{int(checked.Load()), int(failed.Load()), time.Since(start)}
}

// Start 由 bootstrap 显式调用；构造函数不启动定时任务，重复调用无副作用。
func (r *Reader) Start(report func(RefreshStats)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabled || r.started || r.closed {
		return
	}
	r.started = true
	r.workers.Add(1)
	go func() {
		defer r.workers.Done()
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-r.root.Done():
				return
			case <-ticker.C:
				stats := r.Refresh(r.root)
				if report != nil {
					report(stats)
				}
			}
		}
	}()
}
