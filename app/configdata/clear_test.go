package configdata

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
)

func TestReaderClearAllCachesWithoutReadingSources(t *testing.T) {
	var reads atomic.Int32
	r := testReader(t, Sources{Maps: []Source[int, map[string]int]{func(context.Context, int) (map[string]int, bool, error) {
		reads.Add(1)
		return map[string]int{"a": 0}, true, nil
	}}}, config.CacheConfig{})
	ctx := context.Background()
	if _, err := r.LoadCredential(ctx, "key"); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckProductAccess(ctx, "key", 10001); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LoadProductMap(ctx, 10001); err != nil {
		t.Fatal(err)
	}
	stats, err := r.Clear(ctx)
	if err != nil || stats != (ClearStats{Credentials: 1, ProductAccess: 1, ProductMaps: 1}) || reads.Load() != 1 {
		t.Fatal("清理数量不正确或触发存储读取")
	}
	if len(r.credentials.keys())+len(r.products.keys())+len(r.mappings.keys()) != 0 {
		t.Fatal("仍有缓存残留")
	}
	if stats, err := r.Clear(ctx); err != nil || stats != (ClearStats{}) {
		t.Fatal("重复清理不幂等")
	}
	if _, err := r.LoadProductMap(ctx, 10001); err != nil || reads.Load() != 2 {
		t.Fatal("清理后不能重新回源")
	}
}

func TestReaderClearCanceledClosedAndDisabled(t *testing.T) {
	r := testReader(t, Sources{}, config.CacheConfig{})
	if _, err := r.LoadCredential(context.Background(), "key"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Clear(ctx); !errors.Is(err, context.Canceled) || len(r.credentials.keys()) != 1 {
		t.Fatal("取消仍修改了缓存")
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Clear(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatal("已关闭读取器不应清理成功")
	}
	disabled := testReader(t, Sources{}, config.CacheConfig{Disabled: true})
	if _, err := disabled.LoadCredential(context.Background(), "key"); err != nil {
		t.Fatal(err)
	}
	if stats, err := disabled.Clear(context.Background()); err != nil || stats != (ClearStats{}) {
		t.Fatal("关闭内存时应零数量成功")
	}
}

func TestReaderClearFencesOldFlights(t *testing.T) {
	for _, oldFirst := range []bool{true, false} {
		for _, invalid := range []bool{true, false} {
			name := map[bool]string{true: "旧先完成", false: "新先完成"}[oldFirst] + map[bool]string{true: "旧失败", false: "旧成功"}[invalid]
			t.Run(name, func(t *testing.T) {
				oldStarted, newStarted := make(chan struct{}), make(chan struct{})
				releaseOld, releaseNew := make(chan struct{}), make(chan struct{})
				var calls atomic.Int32
				r := testReader(t, Sources{Credentials: []Source[string, services.Credential]{func(ctx context.Context, _ string) (services.Credential, bool, error) {
					n := calls.Add(1)
					var release chan struct{}
					secret := "new"
					if n == 1 {
						close(oldStarted)
						release, secret = releaseOld, "old"
					} else {
						close(newStarted)
						release = releaseNew
					}
					select {
					case <-release:
					case <-ctx.Done():
						return services.Credential{}, false, ctx.Err()
					}
					if n == 1 && invalid {
						return services.Credential{}, false, services.ErrInvalidRecord
					}
					return services.Credential{Version: 1, Secret: secret, Status: 1}, true, nil
				}}}, config.CacheConfig{Concurrency: 2})
				oldDone, newDone := make(chan struct{}), make(chan struct{})
				go func() { defer close(oldDone); _, _ = r.LoadCredential(context.Background(), "key") }()
				awaitClearTest(t, oldStarted)
				if _, err := r.Clear(context.Background()); err != nil {
					t.Fatal(err)
				}
				go func() {
					defer close(newDone)
					value, err := r.LoadCredential(context.Background(), "key")
					if err != nil || value.Secret != "new" {
						t.Error("清理后复用了旧回源")
					}
				}()
				awaitClearTest(t, newStarted)
				if oldFirst {
					close(releaseOld)
					<-oldDone
					r.credentials.mu.Lock()
					current := r.credentials.flights[services.Digest("key")]
					r.credentials.mu.Unlock()
					if current == nil {
						t.Error("旧读取删除了新的合并读取")
					}
					close(releaseNew)
					<-newDone
				} else {
					close(releaseNew)
					<-newDone
					close(releaseOld)
					<-oldDone
				}
				value, err := r.LoadCredential(context.Background(), "key")
				if err != nil || value.Secret != "new" || calls.Load() != 2 {
					t.Fatal("旧读取回填或删除了新缓存")
				}
			})
		}
	}
}

func awaitClearTest(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("等待并发读取信号超时")
	}
}

func TestReaderClearConcurrentWithRefreshAndClose(t *testing.T) {
	r := testReader(t, Sources{}, config.CacheConfig{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				_, _ = r.LoadCredential(context.Background(), "key")
				r.Refresh(context.Background())
				_, _ = r.Clear(context.Background())
			}
		}()
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}
