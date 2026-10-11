// Package auth 定义认证凭据和与存储实现无关的认证错误。
package auth

import "errors"

var (
	ErrInvalid     = errors.New("API key is invalid")
	ErrUnavailable = errors.New("authentication service unavailable")
)

// Credential 是一次读取的完整凭据快照；Secret 不得写日志。
type Credential struct {
	Version   int    `json:"version"`
	Secret    string `json:"secret"`
	Status    int    `json:"status"`
	ExpiresAt int64  `json:"expires_at"`
}
