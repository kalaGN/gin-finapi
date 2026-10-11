package mongo

import (
	"context"
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
	client     *mongo.Client
	connect    func(context.Context) (*mongo.Client, error)
	disconnect func(context.Context, *mongo.Client) error
}

var clients = clientManager{
	connect: connectMongoClient,
	disconnect: func(ctx context.Context, client *mongo.Client) error {
		return client.Disconnect(ctx)
	},
}

// GetClient 获取全局 MongoDB 客户端（单例模式 + 连接池）
func GetClient(ctx context.Context) (*mongo.Client, error) {
	return clients.get(ctx)
}

// Close 关闭 MongoDB 连接
func Close(ctx context.Context) error {
	return clients.close(ctx)
}

func (manager *clientManager) get(ctx context.Context) (*mongo.Client, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

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
	client := manager.client
	manager.client = nil
	manager.mu.Unlock()

	if client == nil || manager.disconnect == nil {
		return nil
	}
	return manager.disconnect(ctx, client)
}

func connectMongoClient(ctx context.Context) (*mongo.Client, error) {
	cfg := config.GetMongoConfig()
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
		_ = client.Disconnect(cleanupCtx)
		return nil, classifyMongoError("ping", err)
	}

	log.Println("MongoDB connected with connection pool")
	return client, nil
}

// connectToMongoDB 是未接入主链路的旧连接入口，本次仅搬移，保留原行为。
func connectToMongoDB(ctx context.Context, dsn string) (*mongo.Client, error) {
	if dsn == "" {
		return nil, fmt.Errorf("invalid DSN")
	}

	// 设置连接选项
	clientOptions := options.Client().ApplyURI(dsn)

	// 设置连接超时时间
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// 连接到 MongoDB
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, err
	}

	// 检查连接是否成功
	err = client.Ping(ctx, nil)
	if err != nil {
		return nil, err
	}

	log.Println("Connected to MongoDB!")
	return client, nil
}
