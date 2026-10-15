package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
	driver "github.com/redis/go-redis/v9"
)

const syncLockKey = "gin-finapi:mysql-sync:v1:lock"
const syncManifestKey = "gin-finapi:mysql-sync:v1:managed"

var ErrSyncUnavailable = errors.New("configuration publication unavailable")
var ErrSyncLeaseLost = errors.New("configuration publication lease lost")

type syncRedis interface {
	SetNX(context.Context, string, interface{}, time.Duration) *driver.BoolCmd
	Eval(context.Context, string, []string, ...interface{}) *driver.Cmd
	Close() error
}

// ConfigPublisher 独立持有发布连接池，只更新三类配置，不操作请求日志。
type ConfigPublisher struct{ client syncRedis }

func NewConfigPublisher(cfg config.RedisConfig) (*ConfigPublisher, error) {
	client, err := NewClientWithConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &ConfigPublisher{client: client}, nil
}

func (p *ConfigPublisher) Acquire(ctx context.Context, lease time.Duration) (string, bool, error) {
	if lease <= 0 {
		return "", false, ErrSyncUnavailable
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", false, ErrSyncUnavailable
	}
	token := hex.EncodeToString(random[:])
	ok, err := p.client.SetNX(ctx, syncLockKey, token, lease).Result()
	if err != nil {
		return "", false, ErrSyncUnavailable
	}
	if !ok {
		return "", false, nil
	}
	return token, true, nil
}

const releaseSyncLua = `
if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) end
return 0`

func (p *ConfigPublisher) Release(ctx context.Context, token string) error {
	if token == "" {
		return ErrSyncLeaseLost
	}
	if err := p.client.Eval(ctx, releaseSyncLua, []string{syncLockKey}, token).Err(); err != nil {
		return ErrSyncUnavailable
	}
	return nil
}

// 清单也是外部输入：先检查全部旧键的范围，再允许任何业务写入或删除。
const publishConfigLua = `
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return {0, 0} end
local function allowed(k)
  local digest = string.match(k, '^gin%-finapi:auth:apikey:v1:([0-9a-f]+)$')
  if digest and #digest == 64 then return true end
  local d, p = string.match(k, '^gin%-finapi:auth:apikey%-product:v1:([0-9a-f]+):(%d+)$')
  if d and #d == 64 and #p == 5 and tonumber(p) >= 10001 and tonumber(p) <= 99999 then return true end
  local product = string.match(k, '^gin%-finapi:product%-map:v1:(%d+)$')
  return product and #product == 5 and tonumber(product) >= 10001 and tonumber(product) <= 99999
end
local old = redis.call('SMEMBERS', KEYS[2])
for _, key in ipairs(old) do if not allowed(key) then return redis.error_reply('invalid managed configuration manifest') end end
local count = tonumber(ARGV[2])
if not count or count < 0 or #ARGV ~= 2 + count * 2 then return redis.error_reply('invalid configuration payload') end
local current = {}
for i = 1, count do
  local key = ARGV[1 + i * 2]
  if not allowed(key) or current[key] then return redis.error_reply('invalid configuration key') end
  current[key] = true
end
local removed = 0
for _, key in ipairs(old) do if not current[key] then removed = removed + redis.call('DEL', key) end end
redis.call('DEL', KEYS[2])
for i = 1, count do
  local key, value = ARGV[1 + i * 2], ARGV[2 + i * 2]
  redis.call('SET', key, value)
  redis.call('SADD', KEYS[2], key)
end
return {1, removed}`

// Publish 先验证完整快照，再用单实例 Lua 隔离发布；租约失效时不写入。
// Lua 运行时错误不回滚已执行命令，网络错误也不能证明发布未执行。
func (p *ConfigPublisher) Publish(ctx context.Context, token string, snapshot services.ConfigSnapshot) (int, error) {
	if token == "" {
		return 0, ErrSyncLeaseLost
	}
	if err := snapshot.Validate(); err != nil {
		return 0, err
	}
	records := make(map[string]string, len(snapshot.Credentials)+len(snapshot.Products)+len(snapshot.Mappings))
	for digest, value := range snapshot.Credentials {
		data, err := json.Marshal(value)
		if err != nil {
			return 0, services.ErrInvalidRecord
		}
		records[CredentialRedisKey(digest)] = string(data)
	}
	for key, value := range snapshot.Products {
		data, err := json.Marshal(value)
		if err != nil {
			return 0, services.ErrInvalidRecord
		}
		records[ProductAccessRedisKey(key)] = string(data)
	}
	for pcode, mapping := range snapshot.Mappings {
		data, err := json.Marshal(mapping)
		if err != nil {
			return 0, services.ErrInvalidRecord
		}
		records[ProductMapRedisKey(pcode)] = string(data)
	}
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := []interface{}{token, len(keys)}
	for _, key := range keys {
		args = append(args, key, records[key])
	}
	result, err := p.client.Eval(ctx, publishConfigLua, []string{syncLockKey, syncManifestKey}, args...).Slice()
	if err != nil || len(result) != 2 {
		return 0, ErrSyncUnavailable
	}
	valid, ok := result[0].(int64)
	if !ok {
		return 0, ErrSyncUnavailable
	}
	if valid != 1 {
		return 0, ErrSyncLeaseLost
	}
	removed, ok := result[1].(int64)
	if !ok || removed < 0 {
		return 0, ErrSyncUnavailable
	}
	return int(removed), nil
}

func (p *ConfigPublisher) Close() error {
	if err := p.client.Close(); err != nil {
		return ErrSyncUnavailable
	}
	return nil
}
