package mongo

import "time"

// lookupFilter 固定查询字段名，保持既有 t 字符串契约。
type lookupFilter struct {
	T string `bson:"t"`
}

// storedDocument 对应既有 Mongo 文档，不改变 t/v 字段及二进制载荷格式。
type storedDocument struct {
	T string `bson:"t"`
	V []byte `bson:"v"`
}

// mongoTimeout 解析 MONGODB_TIMEOUT，缺失或非法时回退到 5s。
func mongoTimeout(raw string) time.Duration {
	if duration, err := time.ParseDuration(raw); err == nil && duration > 0 {
		return duration
	}
	return 5 * time.Second
}
