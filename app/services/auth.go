package services

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

var (
	ErrAPIKeyInvalid   = errors.New("API key is invalid")
	ErrAuthUnavailable = errors.New("authentication service unavailable")
	ErrProductDenied   = errors.New("API key is not authorized for product")
	// ErrInvalidRecord 表示损坏配置；不同读取策略都必须清除旧快照，不能视为临时故障。
	ErrInvalidRecord = errors.New("invalid configuration record")
)

// ProductKey 是与存储和缓存实现无关的产品授权查询键。
type ProductKey struct {
	Digest string
	Pcode  int
}

// Digest 避免原始 API Key 成为长期缓存键或进入刷新日志。
func Digest(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// Validate 每次请求都检查状态与到期时间，不依赖缓存刷新周期。
func (c Credential) Validate(now time.Time) error {
	if c.Status != 1 || c.ExpiresAt != 0 && now.Unix() >= c.ExpiresAt {
		return ErrAPIKeyInvalid
	}
	return nil
}

func (p ProductAccess) Validate(now time.Time) error {
	if p.Status != 1 || p.ExpiresAt != 0 && now.Unix() >= p.ExpiresAt {
		return ErrProductDenied
	}
	return nil
}

// Credential 是一次读取的完整凭据快照；Secret 不得写日志。
type Credential struct {
	Version   int    `json:"version"`
	Secret    string `json:"secret"`
	Status    int    `json:"status"`
	ExpiresAt int64  `json:"expires_at"`
}

// ProductAccess 是 API Key 对单个产品的授权状态快照。
type ProductAccess struct {
	Version   int   `json:"version"`
	Status    int   `json:"status"`
	ExpiresAt int64 `json:"expires_at"`
}
