package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	appredis "github.com/kalaGN/gin-finapi/pkg/redis"
)

func testLoanAuth(load func(context.Context, string) (appredis.APIKeyCredential, error)) gin.HandlerFunc {
	return LoanAuthWithProduct(load, nil)
}

func testKeyReader(key, secret string) func(context.Context, string) (appredis.APIKeyCredential, error) {
	return func(_ context.Context, actual string) (appredis.APIKeyCredential, error) {
		if actual != key || key == "" || secret == "" {
			return appredis.APIKeyCredential{}, appredis.ErrAPIKeyInvalid
		}
		return appredis.APIKeyCredential{Version: 1, Secret: secret, Status: 1}, nil
	}
}
