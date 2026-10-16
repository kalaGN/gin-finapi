package redis

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kalaGN/gin-finapi/app/services"
	driver "github.com/redis/go-redis/v9"
)

type fakeSyncRedis struct {
	acquired bool
	err      error
	evals    int
	sets     int
	result   []interface{}
	check    func(string, []string, []interface{})
}

func (f *fakeSyncRedis) SetNX(ctx context.Context, key string, value interface{}, ttl time.Duration) *driver.BoolCmd {
	f.sets++
	return driver.NewBoolResult(f.acquired, f.err)
}

func TestConfigPublisherLeaseReleaseAndFailureBoundaries(t *testing.T) {
	fake := &fakeSyncRedis{}
	p := &ConfigPublisher{client: fake}
	if _, _, err := p.Acquire(context.Background(), 0); !errors.Is(err, ErrSyncUnavailable) || fake.sets != 0 {
		t.Fatal("无效租约触及Redis")
	}
	if token, acquired, err := p.Acquire(context.Background(), time.Second); err != nil || acquired || token != "" {
		t.Fatal("租约竞争不应报成功")
	}
	if err := p.Release(context.Background(), ""); !errors.Is(err, ErrSyncLeaseLost) || fake.evals != 0 {
		t.Fatal("空令牌发生释放")
	}
	fake.check = func(script string, keys []string, args []interface{}) {
		if script != releaseSyncLua || len(keys) != 1 || keys[0] != syncLockKey || len(args) != 1 || args[0] != "synthetic-token" {
			t.Fatal("释放未绑定令牌")
		}
	}
	if err := p.Release(context.Background(), "synthetic-token"); err != nil {
		t.Fatal(err)
	}
	fake.check = nil
	for _, result := range [][]interface{}{nil, {int64(1)}, {"wrong", int64(0)}, {int64(1), "wrong"}, {int64(1), int64(-1)}} {
		fake.result = result
		if _, err := p.Publish(context.Background(), "synthetic-token", publicationSnapshot()); !errors.Is(err, ErrSyncUnavailable) {
			t.Fatal("不合法发布响应未拒绝")
		}
	}
	if _, err := p.Publish(context.Background(), "", publicationSnapshot()); !errors.Is(err, ErrSyncLeaseLost) {
		t.Fatal("空令牌允许发布")
	}
	fake.err = errors.New("synthetic-private-redis-detail")
	for _, err := range []error{p.Release(context.Background(), "synthetic-token"), p.Close()} {
		if !errors.Is(err, ErrSyncUnavailable) || strings.Contains(err.Error(), "private-redis") {
			t.Fatal("释放或关闭错误未脱敏")
		}
	}
	fake.err = nil
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}
func (f *fakeSyncRedis) Eval(ctx context.Context, script string, keys []string, args ...interface{}) *driver.Cmd {
	f.evals++
	if f.check != nil {
		f.check(script, keys, args)
	}
	return driver.NewCmdResult(f.result, f.err)
}
func (f *fakeSyncRedis) Close() error { return f.err }
func publicationSnapshot() services.ConfigSnapshot {
	d := services.Digest("synthetic-key")
	return services.ConfigSnapshot{Credentials: map[string]services.Credential{d: {Version: 1, Secret: "synthetic-secret", Status: 0}}, Products: map[services.ProductKey]services.ProductAccess{{Digest: d, Pcode: 10001}: {Version: 1, Status: 0, ExpiresAt: 1}}, Mappings: map[int]map[string]int{10001: {"a": 0}}}
}

func TestConfigPublisherValidationAndContract(t *testing.T) {
	fake := &fakeSyncRedis{acquired: true, result: []interface{}{int64(1), int64(2)}}
	p := &ConfigPublisher{client: fake}
	token, ok, err := p.Acquire(context.Background(), time.Second)
	if err != nil || !ok || len(token) != 64 {
		t.Fatal("租约获取失败")
	}
	if _, err := p.Publish(context.Background(), token, services.ConfigSnapshot{}); !errors.Is(err, services.ErrInvalidRecord) || fake.evals != 0 {
		t.Fatal("无效快照发生写入")
	}
	fake.check = func(script string, keys []string, args []interface{}) {
		if script != publishConfigLua || len(keys) != 2 || keys[0] != syncLockKey || keys[1] != syncManifestKey || args[0] != token || args[1] != 3 {
			t.Fatal("发布参数不正确")
		}
		for i := 2; i < len(args); i += 2 {
			key := args[i].(string)
			raw := args[i+1].(string)
			if strings.Contains(key, "synthetic-key") {
				t.Fatal("Key 未摘要化")
			}
			if strings.HasPrefix(key, "gin-finapi:auth:apikey:v1:") {
				var v services.Credential
				if json.Unmarshal([]byte(raw), &v) != nil || v.Status != 0 || v.Version != 1 || v.Secret != "synthetic-secret" {
					t.Fatal("凭据契约变化或禁用被丢弃")
				}
			}
		}
	}
	removed, err := p.Publish(context.Background(), token, publicationSnapshot())
	if err != nil || removed != 2 || fake.evals != 1 {
		t.Fatal("发布失败")
	}
	fake.check = nil
	fake.result = []interface{}{int64(0), int64(0)}
	if _, err := p.Publish(context.Background(), token, publicationSnapshot()); !errors.Is(err, ErrSyncLeaseLost) {
		t.Fatal("租约失效仍成功")
	}
	fake.err = errors.New("raw sensitive redis details")
	if _, _, err := p.Acquire(context.Background(), time.Second); !errors.Is(err, ErrSyncUnavailable) || strings.Contains(err.Error(), "sensitive") {
		t.Fatal("错误未脱敏")
	}
}
