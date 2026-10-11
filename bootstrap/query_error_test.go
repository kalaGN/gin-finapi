package bootstrap

import (
	"context"
	"errors"
	"testing"

	queryloan "github.com/kalaGN/gin-finapi/app/loan"
	"github.com/kalaGN/gin-finapi/pkg/mongo"
	appredis "github.com/kalaGN/gin-finapi/pkg/redis"
)

func TestQueryError_PreservesContractAndOriginalCause(t *testing.T) {
	for _, tc := range []struct{ source, want error }{
		{nil, nil},
		{appredis.ErrProductMapping, queryloan.ErrProductMapping},
		{errors.Join(appredis.ErrMappingUnavailable, context.DeadlineExceeded), queryloan.ErrUnavailable},
		{mongo.ErrNotFound, queryloan.ErrNotFound},
		{mongo.ErrUnavailable, queryloan.ErrUnavailable},
		{mongo.ErrInvalidData, queryloan.ErrInvalidData},
	} {
		got := queryError(tc.source)
		if !errors.Is(got, tc.want) || !errors.Is(got, tc.source) {
			t.Fatalf("got=%v want=%v", got, tc.want)
		}
	}
	// 旧 Handler 对 Mongo 超时的分类优先于 Mongo 不可用，Redis 超时则优先为服务不可用。
	err := queryError(errors.Join(mongo.ErrUnavailable, context.DeadlineExceeded))
	if errors.Is(err, queryloan.ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Mongo timeout precedence changed: %v", err)
	}
}
