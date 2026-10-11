package mongo

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kalaGN/gin-finapi/pkg/config"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Config 包含 MongoDB 连接信息和查询条件
type Config struct {
	DSN        string
	DB         string
	Collection string // 集合名称
	Query      string
}

var (
	ErrNotFound    = errors.New("MongoDB document not found")
	ErrInvalidData = errors.New("invalid MongoDB document data")
	ErrUnavailable = errors.New("MongoDB unavailable")
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

func GetMongo(ctx context.Context, queryConfig Config) (map[string]int, error) {
	appConfig := config.GetMongoConfig()
	if queryConfig.DSN == "" {
		queryConfig.DSN = appConfig.DSN
	}
	if queryConfig.DB == "" {
		queryConfig.DB = appConfig.Database
	}
	if queryConfig.Collection == "" {
		queryConfig.Collection = appConfig.Collection
	}
	if queryConfig.DSN == "" || queryConfig.DB == "" {
		return nil, fmt.Errorf("%w: invalid DSN or database configuration", ErrUnavailable)
	}

	// 使用连接池客户端
	client, err := GetClient(ctx)
	if err != nil {
		return nil, err
	}

	// 获取数据库实例
	database := client.Database(queryConfig.DB)

	// 使用配置的集合名称，如果未指定则使用默认值
	colName := queryConfig.Collection
	if colName == "" {
		colName = "data_20251101_0"
	}
	collection := database.Collection(colName)

	query := struct {
		T string `bson:"t"`
	}{
		T: queryConfig.Query,
	}

	var foundDoc struct {
		T string `bson:"t"`
		V []byte `bson:"v"`
	}

	// 查询必须带上截止时间，否则慢查询会无限占用连接池，而 HTTP 层不会取消它。
	queryCtx, cancel := context.WithTimeout(ctx, mongoTimeout(appConfig.Timeout))
	defer cancel()

	err = collection.FindOne(queryCtx, query).Decode(&foundDoc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, fmt.Errorf("%w: query returned no document", ErrNotFound)
		}
		return nil, classifyMongoError("find document", err)
	}

	return decodeUserData(foundDoc.V)
}

// mongoTimeout 解析 MONGODB_TIMEOUT，缺失或非法时回退到 5s。
func mongoTimeout(raw string) time.Duration {
	if duration, err := time.ParseDuration(raw); err == nil && duration > 0 {
		return duration
	}
	return 5 * time.Second
}

func classifyMongoError(action string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("MongoDB %s: %w", action, err)
	}
	return fmt.Errorf("%w during %s: %w", ErrUnavailable, action, err)
}

func decodeUserData(data []byte) (map[string]int, error) {
	decompressedData, err := gzipDecompress(data)
	if err != nil {
		return nil, fmt.Errorf("%w: decompress payload: %v", ErrInvalidData, err)
	}

	varList := map[string]int{
		"var100001": 0,
		"var100002": 1,
		"var100003": 2,
		"var100004": 3,
		"var100005": 4,
		"var100006": 5,
	}
	return ProcessUserData(varList, strings.Split(string(decompressedData), ","))
}

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

func gzipDecompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty input data")
	}

	buf := bytes.NewBuffer(data)
	gz, err := gzip.NewReader(buf)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	var out bytes.Buffer
	_, err = out.ReadFrom(gz)
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func ProcessUserData(varlist map[string]int, userDataArray []string) (map[string]int, error) {
	requiredFields := 0
	for _, index := range varlist {
		if index < 0 {
			return nil, fmt.Errorf("%w: negative field index %d", ErrInvalidData, index)
		}
		if index+1 > requiredFields {
			requiredFields = index + 1
		}
	}
	if len(userDataArray) != requiredFields {
		return nil, fmt.Errorf("%w: got %d fields, want %d", ErrInvalidData, len(userDataArray), requiredFields)
	}

	result := make(map[string]int)

	for key, index := range varlist {
		value, err := strconv.Atoi(userDataArray[index])
		if err != nil {
			return nil, fmt.Errorf("%w: field %s is not an integer", ErrInvalidData, key)
		}
		result[key] = value
	}

	return result, nil
}
