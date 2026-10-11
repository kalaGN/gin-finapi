package redis

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/kalaGN/gin-finapi/app/auth"
	"github.com/kalaGN/gin-finapi/app/configdata"
	driver "github.com/redis/go-redis/v9"
	"io"
	"strconv"
	"strings"
	"time"
)

func APIKeyProductRedisKey(apiKey string, pcode int) string {
	return "gin-finapi:auth:apikey-product:v1:" + configdata.Digest(apiKey) + ":" + strconv.Itoa(pcode)
}

// ReadProductAccess 只读取和解析，不启动缓存或定时任务。
func (keys *APIKeys) ReadProductAccess(ctx context.Context, key configdata.ProductKey) (auth.ProductAccess, bool, error) {
	readCtx, cancel := context.WithTimeout(ctx, keys.timeout)
	defer cancel()
	raw, err := keys.get(readCtx, "gin-finapi:auth:apikey-product:v1:"+key.Digest+":"+strconv.Itoa(key.Pcode))
	if errors.Is(err, driver.Nil) {
		return auth.ProductAccess{}, false, nil
	}
	if err != nil {
		return auth.ProductAccess{}, false, auth.ErrUnavailable
	}
	value, err := parseProductAccess(raw)
	if err != nil {
		return auth.ProductAccess{}, false, errors.Join(auth.ErrUnavailable, configdata.ErrInvalidRecord)
	}
	return value, true, nil
}

// LoadProduct 保留直读适配方法，生产路由使用统一读取层。
func (keys *APIKeys) LoadProduct(ctx context.Context, key string, pcode int) error {
	if key == "" || pcode < 10001 || pcode > 99999 {
		return auth.ErrProductDenied
	}
	value, found, err := keys.ReadProductAccess(ctx, configdata.ProductKey{Digest: configdata.Digest(key), Pcode: pcode})
	if err != nil {
		return err
	}
	if !found {
		return auth.ErrProductDenied
	}
	return validateProductAccess(value, keys.now())
}

func parseProductAccess(raw string) (auth.ProductAccess, error) {
	invalid := func() (auth.ProductAccess, error) {
		return auth.ProductAccess{}, errors.New("invalid product authorization record")
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
	var access auth.ProductAccess
	if json.Unmarshal(fields["version"], &access.Version) != nil || json.Unmarshal(fields["status"], &access.Status) != nil || json.Unmarshal(fields["expires_at"], &access.ExpiresAt) != nil || access.Version != 1 || access.Status < 0 || access.Status > 1 || access.ExpiresAt < 0 {
		return invalid()
	}
	return access, nil
}

func validateProductAccess(access auth.ProductAccess, now time.Time) error {
	if access.Status != 1 || access.ExpiresAt != 0 && now.Unix() >= access.ExpiresAt {
		return auth.ErrProductDenied
	}
	return nil
}
