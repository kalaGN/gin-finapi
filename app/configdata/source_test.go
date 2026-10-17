package configdata

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/kalaGN/gin-finapi/pkg/utils"
)

func TestReaderReportsActualSources(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		for _, tc := range []struct {
			name       string
			found      bool
			err        error
			status     int
			expires    int64
			wantSource string
			wantError  error
		}{
			{"Redis命中", true, nil, 1, 0, "redis", nil},
			{"Redis缺失回源", false, nil, 1, 0, "mysql", nil},
			{"Redis故障回源", false, services.ErrAuthUnavailable, 1, 0, "mysql", nil},
			{"Redis坏记录回源", false, services.ErrInvalidRecord, 1, 0, "mysql", nil},
			{"禁用不绕过", true, nil, 0, 0, "redis", services.ErrAPIKeyInvalid},
			{"到期不绕过", true, nil, 1, 1, "redis", services.ErrAPIKeyInvalid},
		} {
			t.Run(tc.name+map[bool]string{true: "直读", false: "缓存"}[disabled], func(t *testing.T) {
				primary := NamedSource("credentials", "redis", Source[string, services.Credential](func(context.Context, string) (services.Credential, bool, error) {
					return services.Credential{Status: tc.status, ExpiresAt: tc.expires, Secret: "synthetic"}, tc.found, tc.err
				}))
				secondary := NamedSource("credentials", "mysql", Source[string, services.Credential](func(context.Context, string) (services.Credential, bool, error) {
					if tc.found && tc.err == nil {
						t.Error("绕过有效Redis记录")
					}
					return services.Credential{Status: 1, Secret: "synthetic"}, true, nil
				}))
				r := testReader(t, Sources{
					Credentials: []Source[string, services.Credential]{Fallback(primary, secondary, nil)},
					Products: []Source[services.ProductKey, services.ProductAccess]{NamedSource("product_access", "redis", Source[services.ProductKey, services.ProductAccess](func(context.Context, services.ProductKey) (services.ProductAccess, bool, error) {
						return services.ProductAccess{Status: 1}, true, nil
					}))},
					Maps: []Source[int, map[string]int]{NamedSource("product_map", "mysql", Source[int, map[string]int](func(context.Context, int) (map[string]int, bool, error) { return map[string]int{"a": 0}, true, nil }))},
				}, config.CacheConfig{Disabled: disabled})
				for round := 0; round < 2; round++ {
					ctx := utils.StartConfigSources(context.Background())
					if _, err := r.LoadCredential(ctx, "synthetic-key"); !errors.Is(err, tc.wantError) {
						t.Fatalf("凭据结果=%v", err)
					}
					if err := r.CheckProductAccess(ctx, "synthetic-key", 10001); err != nil {
						t.Fatal(err)
					}
					if _, err := r.LoadProductMap(ctx, 10001); err != nil {
						t.Fatal(err)
					}
					want := map[string]string{"credentials": tc.wantSource, "product_access": "redis", "product_map": "mysql"}
					if round == 1 && !disabled {
						for kind := range want {
							want[kind] = "memory"
						}
					}
					if got := utils.ConfigSourcesSnapshot(ctx); !reflect.DeepEqual(got, want) {
						t.Fatalf("来源=%v want=%v", got, want)
					}
					before := utils.ConfigSourcesSnapshot(ctx)
					r.Refresh(context.Background())
					if !reflect.DeepEqual(utils.ConfigSourcesSnapshot(ctx), before) {
						t.Fatal("后台刷新污染请求来源")
					}
				}
			})
		}
	}
}

type sourceWaitContext struct {
	context.Context
	once     sync.Once
	observed chan struct{}
}

func (c *sourceWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func TestSharedSourcePropagatesToEveryWaiter(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	var calls atomic.Int32
	r := testReader(t, Sources{Maps: []Source[int, map[string]int]{NamedSource("product_map", "redis", Source[int, map[string]int](func(ctx context.Context, _ int) (map[string]int, bool, error) {
		if len(utils.ConfigSourcesSnapshot(ctx)) != 0 {
			t.Error("共享读取继承了请求记录器")
		}
		calls.Add(1)
		close(started)
		<-release
		return map[string]int{"a": 0}, true, nil
	}))}}, config.CacheConfig{})
	first := utils.StartConfigSources(context.Background())
	utils.SetConfigSource(first, "credentials", "mysql")
	secondBase := utils.StartConfigSources(context.Background())
	second := &sourceWaitContext{Context: secondBase, observed: make(chan struct{})}
	done := make(chan error, 2)
	go func() { _, err := r.LoadProductMap(first, 10001); done <- err }()
	<-started
	go func() { _, err := r.LoadProductMap(second, 10001); done <- err }()
	<-second.observed
	canceledBase := utils.StartConfigSources(context.Background())
	canceledCtx, cancel := context.WithCancel(canceledBase)
	defer cancel()
	canceledWaiter := &sourceWaitContext{Context: canceledCtx, observed: make(chan struct{})}
	canceledDone := make(chan error, 1)
	go func() { _, err := r.LoadProductMap(canceledWaiter, 10001); canceledDone <- err }()
	<-canceledWaiter.observed
	cancel()
	if err := <-canceledDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误=%v", err)
	}
	if len(utils.ConfigSourcesSnapshot(canceledWaiter)) != 0 {
		t.Fatal("未拿到共享结果的请求声明命中")
	}
	unblock()
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("未合并回源")
	}
	for _, ctx := range []context.Context{first, second} {
		if utils.ConfigSourcesSnapshot(ctx)["product_map"] != "redis" {
			t.Fatal("等待者丢失真实来源")
		}
	}
	if _, ok := utils.ConfigSourcesSnapshot(second)["credentials"]; ok {
		t.Fatal("请求来源串数据")
	}
}

func TestSourceFailureMissingAndCancellationDoNotClaimHits(t *testing.T) {
	for _, failure := range []error{nil, services.ErrAuthUnavailable} {
		r := testReader(t, Sources{Maps: []Source[int, map[string]int]{NamedSource("product_map", "redis", Source[int, map[string]int](func(context.Context, int) (map[string]int, bool, error) { return nil, false, failure }))}}, config.CacheConfig{})
		ctx := utils.StartConfigSources(context.Background())
		_, _ = r.LoadProductMap(ctx, 10001)
		if len(utils.ConfigSourcesSnapshot(ctx)) != 0 {
			t.Fatal("缺失或故障错误标记为命中")
		}
	}
	if NamedSource[int, int]("product_map", "redis", nil) != nil {
		t.Fatal("隐藏了nil适配器")
	}
	ctx, cancel := context.WithCancel(utils.StartConfigSources(context.Background()))
	cancel()
	r := testReader(t, Sources{}, config.CacheConfig{})
	_, _ = r.LoadProductMap(ctx, 10001)
	if len(utils.ConfigSourcesSnapshot(ctx)) != 0 {
		t.Fatal("取消请求不应记录命中")
	}
}
