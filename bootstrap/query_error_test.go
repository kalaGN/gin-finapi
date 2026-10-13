package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/mongo"
	appredis "github.com/kalaGN/gin-finapi/pkg/redis"
)

func TestQueryError_PreservesContractAndOriginalCause(t *testing.T) {
	for _, tc := range []struct{ source, want error }{
		{nil, nil},
		{appredis.ErrProductMapping, services.ErrProductMapping},
		{errors.Join(appredis.ErrMappingUnavailable, context.DeadlineExceeded), services.ErrUnavailable},
		{mongo.ErrNotFound, services.ErrNotFound},
		{mongo.ErrUnavailable, services.ErrUnavailable},
		{mongo.ErrInvalidData, services.ErrInvalidData},
	} {
		got := queryError(tc.source)
		if !errors.Is(got, tc.want) || !errors.Is(got, tc.source) {
			t.Fatalf("got=%v want=%v", got, tc.want)
		}
	}
	// 旧 Handler 对 Mongo 超时的分类优先于 Mongo 不可用，Redis 超时则优先为服务不可用。
	err := queryError(errors.Join(mongo.ErrUnavailable, context.DeadlineExceeded))
	if errors.Is(err, services.ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Mongo timeout precedence changed: %v", err)
	}
}
