package mongo

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/kalaGN/gin-finapi/pkg/config"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type clientManager struct {
	mu           sync.Mutex
	closed       bool
	client       *mongo.Client
	connect      func(context.Context) (*mongo.Client, error)
	disconnect   func(context.Context, *mongo.Client) error
	initTimeout  time.Duration
	initializing *clientInitialization
	closing      chan struct{}
	closeDone    chan struct{}
	closeErr     error
}

type clientInitialization struct {
	// done 关闭后结果字段只读；Context 仅属于这一轮，不继承任一请求取消。
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	client     *mongo.Client
	err        error
	cleanupErr error
}

func (manager *clientManager) get(ctx context.Context) (*mongo.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return nil, fmt.Errorf("%w: store is closed", ErrUnavailable)
	}
	if manager.client != nil {
		client := manager.client
		manager.mu.Unlock()
		return client, nil
	}
	if manager.closing == nil {
		manager.closing = make(chan struct{})
	}
	flight := manager.initializing
	start := flight == nil
	if start {
		timeout := manager.initTimeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		initCtx, cancel := context.WithTimeout(context.Background(), timeout)
		flight = &clientInitialization{ctx: initCtx, cancel: cancel, done: make(chan struct{})}
		manager.initializing = flight
	}
	closing := manager.closing
	manager.mu.Unlock()
	if start {
		go manager.initialize(flight)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-closing:
		return nil, fmt.Errorf("%w: store is closed", ErrUnavailable)
	case <-flight.ctx.Done():
		manager.mu.Lock()
		closed := manager.closed
		manager.mu.Unlock()
		if closed {
			return nil, fmt.Errorf("%w: store is closed", ErrUnavailable)
		}
		// 成功完成后也会释放初始化 Context，优先读取已发布的结果。
		select {
		case <-flight.done:
		default:
			return nil, flight.ctx.Err()
		}
	case <-flight.done:
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	manager.mu.Lock()
	closed := manager.closed
	manager.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("%w: store is closed", ErrUnavailable)
	}
	return flight.client, flight.err
}

// initialize 的网络操作与候选客户端清理均不占用状态锁。
func (manager *clientManager) initialize(flight *clientInitialization) {
	defer flight.cancel()
	client, err := manager.connect(flight.ctx)
	manager.mu.Lock()
	if manager.closed {
		err = fmt.Errorf("%w: store is closed", ErrUnavailable)
	} else if flight.ctx.Err() != nil {
		err = flight.ctx.Err()
	}
	if err == nil && client == nil {
		err = ErrUnavailable
	}
	if err == nil {
		manager.client = client
		flight.client = client
		manager.initializing = nil
		close(flight.done)
		manager.mu.Unlock()
		return
	}
	manager.mu.Unlock()
	if client != nil {
		flight.cleanupErr = manager.disconnectClient(client)
	}
	manager.mu.Lock()
	flight.err = errors.Join(err, flight.cleanupErr)
	manager.initializing = nil
	close(flight.done)
	manager.mu.Unlock()
}

func (manager *clientManager) disconnectClient(client *mongo.Client) error {
	if client == nil || manager.disconnect == nil {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return manager.disconnect(cleanupCtx, client)
}

func (manager *clientManager) close(ctx context.Context) error {
	manager.mu.Lock()
	var client *mongo.Client
	var flight *clientInitialization
	start := manager.closeDone == nil
	if start {
		manager.closed = true
		if manager.closing == nil {
			manager.closing = make(chan struct{})
		}
		close(manager.closing)
		manager.closeDone = make(chan struct{})
		client, flight = manager.client, manager.initializing
		manager.client = nil
	}
	done := manager.closeDone
	manager.mu.Unlock()
	if start {
		if flight != nil {
			flight.cancel()
		}
		go manager.finishClose(client, flight)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return manager.closeErr
	}
}

// finishClose 独立于关闭调用者的预算；重复 Close 等待同一结果，不重复释放。
func (manager *clientManager) finishClose(client *mongo.Client, flight *clientInitialization) {
	var initCleanupErr error
	if flight != nil {
		<-flight.done
		initCleanupErr = flight.cleanupErr
	}
	err := errors.Join(initCleanupErr, manager.disconnectClient(client))
	manager.mu.Lock()
	manager.closeErr = err
	close(manager.closeDone)
	manager.mu.Unlock()
}

func connectMongoClientWithConfig(ctx context.Context, cfg config.MongoDBConfig) (*mongo.Client, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("%w: DSN is empty", ErrUnavailable)
	}

	clientOptions := options.Client().
		ApplyURI(cfg.DSN).
		SetMaxPoolSize(uint64(cfg.MaxPool)).
		SetMinPoolSize(uint64(cfg.MinPool)).
		SetMaxConnIdleTime(30 * time.Second).
		SetConnectTimeout(5 * time.Second).
		SetServerSelectionTimeout(5 * time.Second)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, classifyMongoError("connect", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupErr := client.Disconnect(cleanupCtx)
		return nil, errors.Join(classifyMongoError("ping", err), cleanupErr)
	}

	log.Println("MongoDB connected with connection pool")
	return client, nil
}
