package redis

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/kalaGN/gin-finapi/app/services"
	driver "github.com/redis/go-redis/v9"
	"io"
	"strconv"
	"strings"
)

func APIKeyProductRedisKey(apiKey string, pcode int) string {
	return ProductAccessRedisKey(services.ProductKey{Digest: services.Digest(apiKey), Pcode: pcode})
}

func ProductAccessRedisKey(key services.ProductKey) string {
	return "gin-finapi:auth:apikey-product:v1:" + key.Digest + ":" + strconv.Itoa(key.Pcode)
}

// ReadProductAccess 只读取和解析，不启动缓存或定时任务。
func (keys *APIKeys) ReadProductAccess(ctx context.Context, key services.ProductKey) (services.ProductAccess, bool, error) {
	readCtx, cancel := context.WithTimeout(ctx, keys.timeout)
	defer cancel()
	raw, err := keys.get(readCtx, ProductAccessRedisKey(key))
	if errors.Is(err, driver.Nil) {
		return services.ProductAccess{}, false, nil
	}
	if err != nil {
		return services.ProductAccess{}, false, services.ErrAuthUnavailable
	}
	value, err := parseProductAccess(raw)
	if err != nil {
		return services.ProductAccess{}, false, errors.Join(services.ErrAuthUnavailable, services.ErrInvalidRecord)
	}
	return value, true, nil
}

// LoadProduct 保留直读适配方法，生产路由使用统一读取层。
func (keys *APIKeys) LoadProduct(ctx context.Context, key string, pcode int) error {
	if key == "" || pcode < 10001 || pcode > 99999 {
		return services.ErrProductDenied
	}
	value, found, err := keys.ReadProductAccess(ctx, services.ProductKey{Digest: services.Digest(key), Pcode: pcode})
	if err != nil {
		return err
	}
	if !found {
		return services.ErrProductDenied
	}
	return value.Validate(keys.now())
}

func parseProductAccess(raw string) (services.ProductAccess, error) {
	invalid := func() (services.ProductAccess, error) {
		return services.ProductAccess{}, errors.New("invalid product authorization record")
	}
	if len(raw) > 4<<10 {
		return invalid()
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return invalid()
	}
	fields := make(map[string]json.RawMessage, 3)
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return invalid()
		}
		name, ok := token.(string)
		if !ok || name != "version" && name != "status" && name != "expires_at" {
			return invalid()
		}
		if _, exists := fields[name]; exists {
			return invalid()
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || string(value) == "null" {
			return invalid()
		}
		fields[name] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || len(fields) != 3 {
		return invalid()
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return invalid()
	}
	var access services.ProductAccess
	if json.Unmarshal(fields["version"], &access.Version) != nil || json.Unmarshal(fields["status"], &access.Status) != nil || json.Unmarshal(fields["expires_at"], &access.ExpiresAt) != nil || access.Version != 1 || access.Status < 0 || access.Status > 1 || access.ExpiresAt < 0 {
		return invalid()
	}
	return access, nil
}
