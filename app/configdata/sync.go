package configdata

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/kalaGN/gin-finapi/app/services"
)

type SyncSink interface {
	Acquire(context.Context, time.Duration) (string, bool, error)
	Publish(context.Context, string, services.ConfigSnapshot) (int, error)
	Release(context.Context, string) error
}
type SyncOptions struct{ Interval, Timeout, ReleaseTimeout time.Duration }
type SyncStats struct {
	Credentials, Products, Mappings, Removed int
	Skipped                                  bool
	Duration                                 time.Duration
}

// Synchronizer 负责租约、完整读取、发布及生命周期，不持有存储连接池。
type Synchronizer struct {
	load            func(context.Context) (services.ConfigSnapshot, error)
	sink            SyncSink
	options         SyncOptions
	after           func(context.Context)
	root            context.Context
	cancel          context.CancelFunc
	mu              sync.Mutex
	started, closed bool
	wg              sync.WaitGroup
	once            sync.Once
	done            chan struct{}
}

func NewSynchronizer(load func(context.Context) (services.ConfigSnapshot, error), sink SyncSink, options SyncOptions, after func(context.Context)) (*Synchronizer, error) {
	if load == nil || sink == nil || options.Interval <= 0 || options.Timeout <= 0 || options.ReleaseTimeout <= 0 {
		return nil, errors.New("invalid configuration synchronizer dependencies or durations")
	}
	root, cancel := context.WithCancel(context.Background())
	return &Synchronizer{load: load, sink: sink, options: options, after: after, root: root, cancel: cancel, done: make(chan struct{})}, nil
}

func (s *Synchronizer) Run(parent context.Context) (stats SyncStats, err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return stats, context.Canceled
	}
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	start := time.Now()
	defer func() { stats.Duration = time.Since(start) }()
	ctx, cancel := context.WithTimeout(parent, s.options.Timeout)
	defer cancel()
	stop := context.AfterFunc(s.root, cancel)
	defer stop()
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	// 租约先于 SQL 快照读取获取，防止较旧的并发快照覆盖较新的发布。
	token, acquired, err := s.sink.Acquire(ctx, s.options.Timeout+time.Second)
	if err != nil {
		return stats, err
	}
	if !acquired {
		stats.Skipped = true
		return stats, nil
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), s.options.ReleaseTimeout)
		defer releaseCancel()
		err = errors.Join(err, s.sink.Release(releaseCtx, token))
	}()
	snapshot, err := s.load(ctx)
	if err != nil {
		return stats, err
	}
	if err := snapshot.Validate(); err != nil {
		return stats, err
	}
	removed, err := s.sink.Publish(ctx, token, snapshot)
	if err != nil {
		return stats, err
	}
	stats.Credentials, stats.Products, stats.Mappings, stats.Removed = len(snapshot.Credentials), len(snapshot.Products), len(snapshot.Mappings), removed
	if s.after != nil {
		s.after(ctx)
	}
	return stats, nil
}

// Start 启动后立即异步同步一次，再按周期执行；构造函数不会启动 worker。
func (s *Synchronizer) Start(report func(SyncStats, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.closed {
		return
	}
	s.started = true
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.options.Interval)
		defer ticker.Stop()
		for {
			if s.root.Err() != nil {
				return
			}
			stats, err := s.Run(s.root)
			if report != nil {
				report(stats, err)
			}
			select {
			case <-s.root.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Synchronizer) Close(ctx context.Context) error {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.cancel()
		s.mu.Unlock()
		go func() { s.wg.Wait(); close(s.done) }()
	})
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
