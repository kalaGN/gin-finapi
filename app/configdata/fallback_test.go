package configdata

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
)

func TestFallbackOnRedisMissingOrFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		found   bool
		status  int
		expires int64
		err     error
		lower   int
	}{
		{"缺失", false, 0, 0, nil, 1}, {"连接异常", false, 0, 0, services.ErrAuthUnavailable, 1},
		{"源超时", false, 0, 0, context.DeadlineExceeded, 1}, {"损坏", false, 0, 0, services.ErrInvalidRecord, 1},
		{"启用", true, 1, 0, nil, 0}, {"禁用", true, 0, 0, nil, 0}, {"到期", true, 1, 1, nil, 0},
	} {
		for _, disabled := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "缓存", true: "直读"}[disabled], func(t *testing.T) {
				calls, reports := 0, 0
				primary := func(context.Context, string) (services.Credential, bool, error) {
					return services.Credential{Version: 1, Secret: "redis", Status: tc.status, ExpiresAt: tc.expires}, tc.found, tc.err
				}
				secondary := func(context.Context, string) (services.Credential, bool, error) {
					calls++
					return services.Credential{Version: 1, Secret: "mysql", Status: 1}, true, nil
				}
				source := Fallback[string, services.Credential](primary, secondary, func(string) { reports++ })
				r := testReader(t, Sources{Credentials: []Source[string, services.Credential]{source}}, config.CacheConfig{Disabled: disabled})
				value, err := r.LoadCredential(context.Background(), "key")
				if calls != tc.lower || reports != tc.lower {
					t.Fatal("回源次数错误")
				}
				if tc.lower == 1 {
					if err != nil || value.Secret != "mysql" {
						t.Fatal("MySQL 回源失败")
					}
				} else if tc.status == 1 && tc.expires == 0 {
					if err != nil || value.Secret != "redis" {
						t.Fatal("有效 Redis 命中不应回源")
					}
				} else if !errors.Is(err, services.ErrAPIKeyInvalid) {
					t.Fatal("禁用或到期不应回源放行")
				}
			})
		}
	}
}

func TestFallbackCanceledAndMissingAuthority(t *testing.T) {
	called := false
	source := Fallback[int, int](func(ctx context.Context, _ int) (int, bool, error) { return 0, false, errors.New("redis failure") }, func(context.Context, int) (int, bool, error) { called = true; return 0, false, nil }, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := source(ctx, 1); !errors.Is(err, context.Canceled) || called {
		t.Fatal("取消后仍执行回源")
	}
	value, found, err := source(context.Background(), 1)
	if err != nil || found || value != 0 || !called {
		t.Fatal("MySQL 明确缺失应为拒绝，而非服务错误")
	}
	deadline, done := context.WithTimeout(context.Background(), time.Nanosecond)
	defer done()
	<-deadline.Done()
	called = false
	if _, _, err := source(deadline, 1); !errors.Is(err, context.DeadlineExceeded) || called {
		t.Fatal("总预算耗尽后仍回源")
	}
}
