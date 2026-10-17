package mongo

import (
	"context"
	"errors"
	"fmt"

	"github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/kalaGN/gin-finapi/pkg/utils"
	driver "go.mongodb.org/mongo-driver/mongo"
)

// Store 持有独立配置快照与懒连接，不读取全局配置，不改变文档格式。
type Store struct {
	cfg     config.MongoDBConfig
	clients clientManager
	find    func(context.Context, *driver.Client, string, string, string) ([]byte, error)
}

func NewStore(cfg config.MongoDBConfig) (*Store, error) {
	if cfg.DSN == "" || cfg.Database == "" || cfg.MaxPool <= 0 || cfg.MinPool < 0 || cfg.MinPool > cfg.MaxPool {
		return nil, fmt.Errorf("%w: invalid Mongo configuration", ErrUnavailable)
	}
	if cfg.Collection == "" {
		cfg.Collection = "data_20251101_0"
	}
	return &Store{cfg: cfg, clients: clientManager{
		initTimeout: mongoTimeout(cfg.Timeout),
		connect:     func(ctx context.Context) (*driver.Client, error) { return connectMongoClientWithConfig(ctx, cfg) },
		disconnect:  func(ctx context.Context, client *driver.Client) error { return client.Disconnect(ctx) },
	}, find: findStoredPayload}, nil
}

// Query 仅接收查询条件，连接、集合和超时来自该实例。
func (store *Store) Query(ctx context.Context, phone string, mapping map[string]int) (map[string]int, error) {
	if len(mapping) == 0 {
		return nil, fmt.Errorf("%w: mapping is required", ErrInvalidData)
	}
	queryCtx, cancel := context.WithTimeout(ctx, mongoTimeout(store.cfg.Timeout))
	defer cancel()
	utils.MarkTiming(ctx, "mongo_start")
	client, err := store.clients.get(queryCtx)
	utils.MarkTiming(ctx, "mongo_client")
	if err != nil {
		return nil, err
	}
	data, err := store.find(queryCtx, client, store.cfg.Database, store.cfg.Collection, phone)
	utils.MarkTiming(ctx, "mongo_find")
	if err != nil {
		return nil, err
	}
	return decodeUserData(queryCtx, data, mapping)
}

// Close 终止实例，未初始化时不建连接，关闭后不允许再次发布客户端。
func (store *Store) Close(ctx context.Context) error {
	return store.clients.close(ctx)
}

func findStoredPayload(ctx context.Context, client *driver.Client, database, collection, phone string) ([]byte, error) {
	var doc storedDocument
	err := client.Database(database).Collection(collection).FindOne(ctx, lookupFilter{T: phone}).Decode(&doc)
	if errors.Is(err, driver.ErrNoDocuments) {
		return nil, fmt.Errorf("%w: query returned no document", ErrNotFound)
	}
	if err != nil {
		return nil, classifyMongoError("find document", err)
	}
	return doc.V, nil
}
