//go:build integration

package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/kalaGN/gin-finapi/app/configdata"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
	appredis "github.com/kalaGN/gin-finapi/pkg/redis"
	driver "github.com/redis/go-redis/v9"
)

// 测试只读授权的 MySQL，所有 Redis 写入都在本用例创建的临时进程中。
func TestLocalMySQLRedisFlow(t *testing.T) {
	if os.Getenv("FINAPI_LOCAL_MYSQL_TEST") != "1" {
		t.Skip("需显式启用本地集成验证")
	}
	if err := godotenv.Load("../../.env"); err != nil {
		t.Fatal("无法加载本地配置")
	}
	if err := config.Load(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config.MySQL
	if cfg.Host != "127.0.0.1" || cfg.Database != "fin_api" {
		t.Fatal("本用例只读本地 fin_api")
	}
	store, err := NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snapshot, err := store.Snapshot(ctx)
	if err != nil || snapshot.Validate() != nil {
		t.Fatal("本地 MySQL 快照读取失败")
	}
	redisCfg, client, stop := isolatedRedis(t)
	publisher, err := appredis.NewConfigPublisher(redisCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = publisher.Close() })
	keys, err := appredis.NewAPIKeys(redisCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = keys.Close() })
	mappings, err := appredis.NewProductMaps(redisCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mappings.Close() })
	newReader := func(disabled bool) *configdata.Reader {
		t.Helper()
		r, err := configdata.New(configdata.Sources{
			Credentials: []configdata.Source[string, services.Credential]{configdata.Fallback(keys.ReadCredential, store.ReadCredential, nil)},
			Products:    []configdata.Source[services.ProductKey, services.ProductAccess]{configdata.Fallback(keys.ReadProductAccess, store.ReadProductAccess, nil)},
			Maps:        []configdata.Source[int, map[string]int]{configdata.Fallback(mappings.ReadProductMap, store.ReadProductMap, nil)},
		}, config.CacheConfig{Disabled: disabled}, redisCfg.Timeout+cfg.QueryTimeout)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = r.Close(context.Background()) })
		return r
	}
	assertReads := func(r *configdata.Reader) {
		t.Helper()
		v, err := r.LoadCredential(ctx, "example-test-key")
		if err != nil || v.Secret != "example-test-secret" {
			t.Fatal("凭据回源失败")
		}
		if err := r.CheckProductAccess(ctx, "example-test-key", 10001); err != nil {
			t.Fatal("授权回源失败")
		}
		if err := r.CheckProductAccess(ctx, "example-test-key-2", 10002); !errors.Is(err, services.ErrProductDenied) {
			t.Fatal("不同 API Key 的授权未隔离")
		}
		m, err := r.LoadProductMap(ctx, 10001)
		if err != nil || len(m) != 6 || m["var100006"] != 5 {
			t.Fatal("映射回源失败")
		}
	}
	// Redis 为空时，开启和关闭内存缓存均应正常回源。
	assertReads(newReader(false))
	assertReads(newReader(true))
	token, ok, err := publisher.Acquire(ctx, 10*time.Second)
	if err != nil || !ok {
		t.Fatal("无法获得临时 Redis 租约")
	}
	if _, ok, err := publisher.Acquire(ctx, time.Second); err != nil || ok {
		t.Fatal("锁竞争没有跳过")
	}
	if _, err := publisher.Publish(ctx, "wrong-token", snapshot); !errors.Is(err, appredis.ErrSyncLeaseLost) {
		t.Fatal("失效租约允许发布")
	}
	if _, err := publisher.Publish(ctx, token, services.ConfigSnapshot{}); err == nil {
		t.Fatal("非法快照允许发布")
	}
	if n, err := publisher.Publish(ctx, token, snapshot); err != nil || n != 0 {
		t.Fatal("完整快照首次发布失败")
	}
	if n, err := publisher.Publish(ctx, token, snapshot); err != nil || n != 0 {
		t.Fatal("重复发布不幂等")
	}
	digest := services.Digest("example-test-key")
	credential := snapshot.Credentials[digest]
	credential.Status = 0
	snapshot.Credentials[digest] = credential
	if _, err := publisher.Publish(ctx, token, snapshot); err != nil {
		t.Fatal("禁用快照发布失败")
	}
	if _, err := newReader(true).LoadCredential(ctx, "example-test-key"); !errors.Is(err, services.ErrAPIKeyInvalid) {
		t.Fatal("Redis 禁用凭据被 MySQL 启用状态绕过")
	}
	if err := client.Set(ctx, appredis.CredentialRedisKey(digest), "broken-json", 0).Err(); err != nil {
		t.Fatal("临时数据写入失败")
	}
	assertReads(newReader(true))
	// 清理只针对清单托管的删除项，不得触及其他键或请求日志。
	if err := client.Set(ctx, "unmanaged-marker", "keep", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.RPush(ctx, "request_log", "keep").Err(); err != nil {
		t.Fatal(err)
	}
	delete(snapshot.Credentials, digest)
	for key := range snapshot.Products {
		if key.Digest == digest {
			delete(snapshot.Products, key)
		}
	}
	if n, err := publisher.Publish(ctx, token, snapshot); err != nil || n != 3 {
		t.Fatal("托管删除数量错误")
	}
	if client.Exists(ctx, appredis.CredentialRedisKey(digest)).Val() != 0 || client.Get(ctx, "unmanaged-marker").Val() != "keep" || client.LLen(ctx, "request_log").Val() != 1 {
		t.Fatal("发布影响了非托管数据")
	}
	if err := client.SAdd(ctx, "gin-finapi:mysql-sync:v1:managed", "unmanaged-marker").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(ctx, token, snapshot); err == nil || client.Get(ctx, "unmanaged-marker").Val() != "keep" {
		t.Fatal("污染清单未被拒绝")
	}
	if err := client.SRem(ctx, "gin-finapi:mysql-sync:v1:managed", "unmanaged-marker").Err(); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Release(ctx, token); err != nil {
		t.Fatal(err)
	}
	expired, ok, err := publisher.Acquire(ctx, time.Millisecond)
	if err != nil || !ok {
		t.Fatal("短租约获取失败")
	}
	for client.Exists(ctx, "gin-finapi:mysql-sync:v1:lock").Val() != 0 {
		select {
		case <-ctx.Done():
			t.Fatal("租约未到期")
		case <-time.After(time.Millisecond):
		}
	}
	if _, err := publisher.Publish(ctx, expired, snapshot); !errors.Is(err, appredis.ErrSyncLeaseLost) {
		t.Fatal("到期租约仍可发布")
	}
	current, ok, err := publisher.Acquire(ctx, time.Second)
	if err != nil || !ok {
		t.Fatal("到期后无法获得新租约")
	}
	if err := publisher.Release(ctx, expired); err != nil || client.Get(ctx, "gin-finapi:mysql-sync:v1:lock").Val() != current {
		t.Fatal("旧实例释放了新实例的租约")
	}
	if err := publisher.Release(ctx, current); err != nil {
		t.Fatal(err)
	}
	// 同步器首次启动立即发布，并刷新本进程已缓存的禁用记录。
	reader := newReader(false)
	raw, _ := json.Marshal(credential)
	if err := client.Set(ctx, appredis.CredentialRedisKey(digest), raw, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.LoadCredential(ctx, "example-test-key"); !errors.Is(err, services.ErrAPIKeyInvalid) {
		t.Fatal("未建立禁用缓存")
	}
	syncer, err := configdata.NewSynchronizer(store.Snapshot, publisher, configdata.SyncOptions{Interval: time.Minute, Timeout: 5 * time.Second, ReleaseTimeout: time.Second}, func(ctx context.Context) { reader.Refresh(ctx) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syncer.Close(context.Background()) })
	done := make(chan error, 1)
	syncer.Start(func(_ configdata.SyncStats, err error) { done <- err })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("首次异步同步失败")
		}
	case <-ctx.Done():
		t.Fatal("首次同步未启动")
	}
	assertReads(reader)
	if err := syncer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	token, ok, err = publisher.Acquire(ctx, time.Second)
	if err != nil || !ok {
		t.Fatal("空快照验证租约失败")
	}
	empty := services.ConfigSnapshot{Credentials: map[string]services.Credential{}, Products: map[services.ProductKey]services.ProductAccess{}, Mappings: map[int]map[string]int{}}
	if removed, err := publisher.Publish(ctx, token, empty); err != nil || removed != 7 || client.Exists(ctx, "gin-finapi:mysql-sync:v1:managed").Val() != 0 {
		t.Fatal("合法完整空快照未清理托管键")
	}
	if err := publisher.Release(ctx, token); err != nil {
		t.Fatal(err)
	}
	stop()
	assertReads(newReader(true))
	assertReads(newReader(false))
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := newReader(true).LoadCredential(ctx, "example-test-key"); !errors.Is(err, services.ErrAuthUnavailable) {
		t.Fatal("双源故障未失败关闭")
	}
}

func isolatedRedis(t *testing.T) (config.RedisConfig, *driver.Client, func()) {
	t.Helper()
	path, err := exec.LookPath("redis-server")
	if err != nil {
		t.Fatal("需要本地 redis-server 才能执行显式集成测试")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	process := exec.Command(path, "--bind", "127.0.0.1", "--port", strconv.Itoa(port), "--save", "", "--appendonly", "no", "--dir", t.TempDir())
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	stop := func() {
		if !stopped {
			stopped = true
			_ = process.Process.Kill()
			_ = process.Wait()
		}
	}
	t.Cleanup(stop)
	cfg := config.RedisConfig{Addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), PoolSize: 2, Timeout: 200 * time.Millisecond}
	client, err := appredis.NewClientWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for client.Ping(ctx).Err() != nil {
		select {
		case <-ctx.Done():
			t.Fatal("临时 Redis 启动失败")
		case <-time.After(10 * time.Millisecond):
		}
	}
	return cfg, client, stop
}
