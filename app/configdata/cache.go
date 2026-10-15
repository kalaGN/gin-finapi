package configdata

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/kalaGN/gin-finapi/app/services"
)

// Source 只有明确缺失才能继续下一个数据源，错误和禁用记录都不能降级。
type Source[K comparable, V any] func(context.Context, K) (V, bool, error)

type entry[K comparable, V any] struct {
	key   K
	value V
}
type flight[V any] struct {
	done  chan struct{}
	value V
	found bool
	err   error
}

// cache 不保存缓存时间或 TTL；只按容量淘汰，并由显式刷新更新快照。
type cache[K comparable, V any] struct {
	mu       sync.Mutex
	entries  map[K]*list.Element
	lru      *list.List
	flights  map[K]*flight[V]
	sources  []Source[K, V]
	capacity int
	clone    func(V) V
	root     context.Context
	timeout  time.Duration
	gate     chan struct{}
	closed   bool
	disabled bool
	wg       sync.WaitGroup
}

func newCache[K comparable, V any](root context.Context, timeout time.Duration, gate chan struct{}, capacity int, sources []Source[K, V], clone func(V) V) *cache[K, V] {
	return &cache[K, V]{entries: make(map[K]*list.Element), lru: list.New(), flights: make(map[K]*flight[V]), sources: append([]Source[K, V](nil), sources...), capacity: capacity, clone: clone, root: root, timeout: timeout, gate: gate}
}

func (c *cache[K, V]) lookupLocked(key K, refresh bool) (*flight[V], bool) {
	if !refresh {
		if e, ok := c.entries[key]; ok {
			c.lru.MoveToFront(e)
			return &flight[V]{value: e.Value.(entry[K, V]).value, found: true}, true
		}
	}
	f, ok := c.flights[key]
	return f, ok
}

func waitFlight[V any](ctx context.Context, f *flight[V]) (V, bool, error) {
	var zero V
	if f.done == nil {
		return f.value, f.found, f.err
	}
	select {
	case <-ctx.Done():
		return zero, false, ctx.Err()
	case <-f.done:
		return f.value, f.found, f.err
	}
}

func (c *cache[K, V]) load(ctx context.Context, key K, refresh bool) (V, bool, error) {
	if c.disabled {
		return c.direct(ctx, key)
	}
	var zero V
	if err := ctx.Err(); err != nil {
		return zero, false, err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return zero, false, context.Canceled
	}
	f, ok := c.lookupLocked(key, refresh)
	c.mu.Unlock()
	if ok && f.done == nil {
		return c.clone(f.value), f.found, f.err
	}
	// 冷读取的等待预算同时覆盖排队和合并等待；热缓存不创建计时器。
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if ok {
		v, found, err := waitFlight(ctx, f)
		return c.clone(v), found, err
	}
	// 限制正在进行的回源，不为任意数量的未知键启动 goroutine。
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return zero, false, ctx.Err()
	case <-c.root.Done():
		return zero, false, c.root.Err()
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		<-c.gate
		return zero, false, context.Canceled
	}
	f, ok = c.lookupLocked(key, refresh)
	if ok {
		c.mu.Unlock()
		<-c.gate
		v, found, err := waitFlight(ctx, f)
		return c.clone(v), found, err
	}
	f = &flight[V]{done: make(chan struct{})}
	c.flights[key] = f
	c.wg.Add(1)
	c.mu.Unlock()
	// 合并读取不使用第一个请求的 Context，单个等待者取消不影响其他请求。
	go c.fetch(key, refresh, f)
	v, found, err := waitFlight(ctx, f)
	return c.clone(v), found, err
}

func (c *cache[K, V]) fetch(key K, refresh bool, f *flight[V]) {
	defer c.wg.Done()
	defer func() { <-c.gate }()
	ctx, cancel := context.WithTimeout(c.root, c.timeout)
	defer cancel()
	f.value, f.found, f.err = c.readSources(ctx, key)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		if f.err == nil && f.found {
			if e, ok := c.entries[key]; ok {
				e.Value = entry[K, V]{key, c.clone(f.value)}
				if !refresh {
					c.lru.MoveToFront(e)
				}
			} else if !refresh {
				c.entries[key] = c.lru.PushFront(entry[K, V]{key, c.clone(f.value)})
				if c.lru.Len() > c.capacity {
					e := c.lru.Back()
					delete(c.entries, e.Value.(entry[K, V]).key)
					c.lru.Remove(e)
				}
			}
		} else if f.err == nil || errors.Is(f.err, services.ErrInvalidRecord) {
			if e, ok := c.entries[key]; ok {
				delete(c.entries, key)
				c.lru.Remove(e)
			}
		}
	}
	delete(c.flights, key)
	close(f.done)
}

func (c *cache[K, V]) readSources(ctx context.Context, key K) (V, bool, error) {
	var zero V
	for _, source := range c.sources {
		if err := ctx.Err(); err != nil {
			return zero, false, err
		}
		value, found, err := source(ctx, key)
		if err != nil || found {
			return value, found, err
		}
	}
	return zero, false, nil
}

// direct 完全绕过快照和合并读取；每次请求读数据源，仍限制并发、等待时间和关闭。
func (c *cache[K, V]) direct(ctx context.Context, key K) (V, bool, error) {
	var zero V
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	stop := context.AfterFunc(c.root, cancel)
	defer stop()
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return zero, false, ctx.Err()
	}
	defer func() { <-c.gate }()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return zero, false, context.Canceled
	}
	c.wg.Add(1)
	c.mu.Unlock()
	defer c.wg.Done()
	value, found, err := c.readSources(ctx, key)
	return c.clone(value), found, err
}

func (c *cache[K, V]) keys() []K {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]K, 0, len(c.entries))
	for key := range c.entries {
		keys = append(keys, key)
	}
	return keys
}

func (c *cache[K, V]) stop() {
	c.mu.Lock()
	c.closed = true
	c.entries = make(map[K]*list.Element)
	c.lru.Init()
	c.mu.Unlock()
}
