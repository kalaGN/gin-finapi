package mongo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kalaGN/gin-finapi/pkg/config"
	"go.mongodb.org/mongo-driver/mongo"
)

// Config 包含 MongoDB 连接信息和查询条件
type Config struct {
	DSN        string
	DB         string
	Collection string // 集合名称
	Query      string
	Mapping    map[string]int // 已按产品校验的字段索引映射
}

// lookupFilter 固定查询字段名，保持既有 t 字符串契约。
type lookupFilter struct {
	T string `bson:"t"`
}

// storedDocument 对应既有 Mongo 文档，不改变 t/v 字段及二进制载荷格式。
type storedDocument struct {
	T string `bson:"t"`
	V []byte `bson:"v"`
}

func GetMongo(ctx context.Context, queryConfig Config) (map[string]int, error) {
	if len(queryConfig.Mapping) == 0 {
		return nil, fmt.Errorf("%w: mapping is required", ErrInvalidData)
	}
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

	query := lookupFilter{
		T: queryConfig.Query,
	}

	var foundDoc storedDocument

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

	return decodeUserData(foundDoc.V, queryConfig.Mapping)
}

// mongoTimeout 解析 MONGODB_TIMEOUT，缺失或非法时回退到 5s。
func mongoTimeout(raw string) time.Duration {
	if duration, err := time.ParseDuration(raw); err == nil && duration > 0 {
		return duration
	}
	return 5 * time.Second
}
