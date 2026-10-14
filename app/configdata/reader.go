// Package configdata 封装内存优先的数据读取；数据源由启动层注入。
package configdata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"sync"
	"time"

	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
)

type ProductKey struct {
	Digest string
	Pcode  int
}
type Sources struct {
	Credentials []Source[string, services.Credential]
	Products    []Source[ProductKey, services.ProductAccess]
	Maps        []Source[int, map[string]int]
}

// Reader 缓存凭据、授权和映射，但不缓存 Mongo 查询结果。
type Reader struct {
	credentials     *cache[string, services.Credential]
	products        *cache[ProductKey, services.ProductAccess]
	mappings        *cache[int, map[string]int]
	root            context.Context
	cancel          context.CancelFunc
	interval        time.Duration
	concurrency     int
	disabled        bool
	now             func() time.Time
	mu              sync.Mutex
	started, closed bool
	workers         sync.WaitGroup
	closeOnce       sync.Once
	done            chan struct{}
}

func New(s Sources, cfg config.CacheConfig, timeout time.Duration) (*Reader, error) {
	cfg = cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if timeout <= 0 || len(s.Credentials) == 0 || len(s.Products) == 0 || len(s.Maps) == 0 {
		return nil, errors.New("configuration reader requires sources and a positive timeout")
	}
	for _, source := range s.Credentials {
		if source == nil {
			return nil, errors.New("nil credential source")
		}
	}
	for _, source := range s.Products {
		if source == nil {
			return nil, errors.New("nil authorization source")
		}
	}
	for _, source := range s.Maps {
		if source == nil {
			return nil, errors.New("nil mapping source")
		}
	}
	root, cancel := context.WithCancel(context.Background())
	gate := make(chan struct{}, cfg.Concurrency)
	r := &Reader{root: root, cancel: cancel, interval: cfg.RefreshInterval, concurrency: cfg.Concurrency, disabled: cfg.Disabled, now: time.Now, done: make(chan struct{})}
	r.credentials = newCache(root, timeout, gate, cfg.APIKeyEntries, s.Credentials, func(v services.Credential) services.Credential { return v })
	r.products = newCache(root, timeout, gate, cfg.ProductAuthEntries, s.Products, func(v services.ProductAccess) services.ProductAccess { return v })
	r.mappings = newCache(root, timeout, gate, cfg.ProductMapEntries, s.Maps, maps.Clone[map[string]int])
	r.credentials.disabled, r.products.disabled, r.mappings.disabled = cfg.Disabled, cfg.Disabled, cfg.Disabled
	return r, nil
}

// Digest 避免原始 API Key 成为长期缓存键或进入刷新日志。
func Digest(key string) string { sum := sha256.Sum256([]byte(key)); return hex.EncodeToString(sum[:]) }

func (r *Reader) LoadCredential(ctx context.Context, key string) (services.Credential, error) {
	if key == "" {
		return services.Credential{}, services.ErrAPIKeyInvalid
	}
	value, found, err := r.credentials.load(ctx, Digest(key), false)
	if err != nil {
		return services.Credential{}, services.ErrAuthUnavailable
	}
	if !found || value.Status != 1 || value.ExpiresAt != 0 && r.now().Unix() >= value.ExpiresAt {
		return services.Credential{}, services.ErrAPIKeyInvalid
	}
	return value, nil
}

func (r *Reader) LoadProductAccess(ctx context.Context, key string, pcode int) (services.ProductAccess, error) {
	if key == "" || pcode < 10001 || pcode > 99999 {
		return services.ProductAccess{}, services.ErrProductDenied
	}
	value, found, err := r.products.load(ctx, ProductKey{Digest(key), pcode}, false)
	if err != nil {
		return services.ProductAccess{}, services.ErrAuthUnavailable
	}
	if !found || value.Status != 1 || value.ExpiresAt != 0 && r.now().Unix() >= value.ExpiresAt {
		return services.ProductAccess{}, services.ErrProductDenied
	}
	return value, nil
}

func (r *Reader) CheckProductAccess(ctx context.Context, key string, pcode int) error {
	_, err := r.LoadProductAccess(ctx, key, pcode)
	return err
}

func (r *Reader) LoadProductMap(ctx context.Context, pcode int) (map[string]int, error) {
	value, found, err := r.mappings.load(ctx, pcode, false)
	if errors.Is(err, ErrInvalidRecord) {
		return nil, services.ErrProductMapping
	}
	if err != nil {
		return nil, errors.Join(services.ErrUnavailable, err)
	}
	if !found {
		return nil, services.ErrProductMapping
	}
	return value, nil
}

// Close 先取消刷新及合并读取，再等待完成；调用方随后关闭存储连接池。
func (r *Reader) Close(ctx context.Context) error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		r.cancel()
		r.credentials.stop()
		r.products.stop()
		r.mappings.stop()
		r.mu.Unlock()
		go func() {
			r.workers.Wait()
			r.credentials.wg.Wait()
			r.products.wg.Wait()
			r.mappings.wg.Wait()
			close(r.done)
		}()
	})
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
