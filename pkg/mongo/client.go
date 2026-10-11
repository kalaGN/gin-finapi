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
	mu         sync.Mutex
	closed     bool
	client     *mongo.Client
	connect    func(context.Context) (*mongo.Client, error)
	disconnect func(context.Context, *mongo.Client) error
}

func (manager *clientManager) get(ctx context.Context) (*mongo.Client, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed {
		return nil, fmt.Errorf("%w: store is closed", ErrUnavailable)
	}

	if manager.client != nil {
		return manager.client, nil
	}
	client, err := manager.connect(ctx)
	if err != nil {
		return nil, err
	}
	manager.client = client
	return client, nil
}

func (manager *clientManager) close(ctx context.Context) error {
	manager.mu.Lock()
	manager.closed = true
	client := manager.client
	manager.client = nil
	manager.mu.Unlock()

	if client == nil || manager.disconnect == nil {
		return nil
	}
	return manager.disconnect(ctx, client)
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
