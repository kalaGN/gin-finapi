package redis

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/kalaGN/gin-finapi/app/configdata"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
	driver "github.com/redis/go-redis/v9"
)

var (
	ErrAPIKeyInvalid   = services.ErrAPIKeyInvalid
	ErrAuthUnavailable = services.ErrAuthUnavailable
)

// APIKeyCredential 保留旧名称，与认证边界的凭据快照类型一致；Secret 禁止写日志。
type APIKeyCredential = services.Credential

// APIKeys owns a read-only Redis connection pool. It does not cache credentials.
type APIKeys struct {
	get     func(context.Context, string) (string, error)
	close   func() error
	timeout time.Duration
	now     func() time.Time
}

// APIKeyRedisKey derives the storage key without exposing the original credential.
func APIKeyRedisKey(apiKey string) string {
	return "gin-finapi:auth:apikey:v1:" + configdata.Digest(apiKey)
}

// NewAPIKeys 使用统一客户端选项创建独立、懒连接的认证读取器。
func NewAPIKeys(cfg config.RedisConfig) (*APIKeys, error) {
	client, err := NewClientWithConfig(cfg)
	if err != nil {
		return nil, err
	}

	keys := &APIKeys{get: func(ctx context.Context, key string) (string, error) {
		return client.Get(ctx, key).Result()
	}, close: client.Close, timeout: cfg.Timeout, now: time.Now}
	return keys, nil
}

// Load distinguishes denied credentials from unavailable or corrupt storage.
func (keys *APIKeys) Load(ctx context.Context, apiKey string) (APIKeyCredential, error) {
	if apiKey == "" {
		return APIKeyCredential{}, ErrAPIKeyInvalid
	}
	readCtx, cancel := context.WithTimeout(ctx, keys.timeout)
	defer cancel()
	raw, err := keys.get(readCtx, APIKeyRedisKey(apiKey))
	if errors.Is(err, driver.Nil) {
		return APIKeyCredential{}, ErrAPIKeyInvalid
	}
	// Do not propagate driver errors: they can contain credentials or record content.
	if err != nil {
		return APIKeyCredential{}, ErrAuthUnavailable
	}
	credential, err := parseAPIKeyCredential(raw)
	if err != nil {
		return APIKeyCredential{}, err
	}
	if credential.Status != 1 || credential.ExpiresAt != 0 && keys.now().Unix() >= credential.ExpiresAt {
		return APIKeyCredential{}, ErrAPIKeyInvalid
	}
	return credential, nil
}

// Close releases the reader's pool without modifying Redis records.
func (keys *APIKeys) Close() error {
	return keys.close()
}

// ReadCredential 读取已摘要化的键，保留禁用/到期状态供读取层逐次判断。
func (keys *APIKeys) ReadCredential(ctx context.Context, digest string) (services.Credential, bool, error) {
	readCtx, cancel := context.WithTimeout(ctx, keys.timeout)
	defer cancel()
	raw, err := keys.get(readCtx, "gin-finapi:auth:apikey:v1:"+digest)
	if errors.Is(err, driver.Nil) {
		return services.Credential{}, false, nil
	}
	if err != nil {
		return services.Credential{}, false, services.ErrAuthUnavailable
	}
	value, err := parseAPIKeyCredential(raw)
	if err != nil {
		return services.Credential{}, false, errors.Join(services.ErrAuthUnavailable, configdata.ErrInvalidRecord)
	}
	return value, true, nil
}

func parseAPIKeyCredential(raw string) (APIKeyCredential, error) {
	invalid := func() (APIKeyCredential, error) { return APIKeyCredential{}, ErrAuthUnavailable }
	if len(raw) > 4<<10 {
		return invalid()
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return invalid()
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return invalid()
		}
		name, ok := token.(string)
		if !ok {
			return invalid()
		}
		if _, exists := fields[name]; exists {
			return invalid()
		}
		switch name {
		case "version", "secret", "status", "expires_at":
		default:
			return invalid()
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || string(value) == "null" {
			return invalid()
		}
		fields[name] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || len(fields) != 4 {
		return invalid()
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return invalid()
	}
	var credential APIKeyCredential
	if json.Unmarshal(fields["version"], &credential.Version) != nil ||
		json.Unmarshal(fields["secret"], &credential.Secret) != nil ||
		json.Unmarshal(fields["status"], &credential.Status) != nil ||
		json.Unmarshal(fields["expires_at"], &credential.ExpiresAt) != nil {
		return invalid()
	}
	if credential.Version != 1 || credential.Secret == "" || len(credential.Secret) > 256 ||
		credential.Status < 0 || credential.Status > 1 || credential.ExpiresAt < 0 {
		return invalid()
	}
	return credential, nil
}
