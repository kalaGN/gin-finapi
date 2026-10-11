// Package bootstrap 处理程序初始化逻辑
package bootstrap

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/Http/controllers/loan"
	queryloan "github.com/kalaGN/gin-finapi/app/loan"
	"github.com/kalaGN/gin-finapi/app/middleware"
	"github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/kalaGN/gin-finapi/pkg/mongo"
	appredis "github.com/kalaGN/gin-finapi/pkg/redis"
	"github.com/kalaGN/gin-finapi/pkg/utils"
	"github.com/kalaGN/gin-finapi/routes"
	"github.com/sirupsen/logrus"
)

// NewRouter 根据启动配置和依赖装配 HTTP 处理器与中间件。
// Redis/MongoDB 均按需连接；调用方负责执行返回的依赖关闭函数。
func NewRouter(cfg config.AppConfig, log *logrus.Logger) (*gin.Engine, func(context.Context) error, error) {
	maps, err := appredis.NewProductMaps(cfg.Redis)
	if err != nil {
		return nil, nil, err
	}
	keys, err := appredis.NewAPIKeys(cfg.Redis)
	if err != nil {
		return nil, nil, errors.Join(err, maps.Close())
	}
	store, err := mongo.NewStore(cfg.MongoDB)
	if err != nil {
		return nil, nil, errors.Join(err, keys.Close(), maps.Close())
	}
	closeDependencies := func(ctx context.Context) error { return errors.Join(store.Close(ctx), keys.Close(), maps.Close()) }
	query, err := queryloan.NewQuery(func(ctx context.Context, pcode int) (map[string]int, error) {
		mapping, err := maps.Load(ctx, pcode)
		return mapping, queryError(err)
	}, func(ctx context.Context, phone string, mapping map[string]int) (map[string]int, error) {
		data, err := store.Query(ctx, phone, mapping)
		return data, queryError(err)
	})
	if err != nil {
		return nil, nil, errors.Join(err, closeDependencies(context.Background()))
	}
	handler, err := loan.NewLoanHandler(query, utils.GenerateSID, log)
	if err != nil {
		return nil, nil, errors.Join(err, closeDependencies(context.Background()))
	}
	router := gin.New()
	SetupRoute(router, handler.Create, middleware.LoanAuth(keys.Load), log, cfg.RateLimit)
	return router, closeDependencies, nil
}

// queryError 在装配适配边界转换存储错误，HTTP 和查询用例不依赖存储包。
func queryError(err error) error {
	switch {
	case errors.Is(err, appredis.ErrProductMapping):
		return errors.Join(queryloan.ErrProductMapping, err)
	case errors.Is(err, appredis.ErrMappingUnavailable):
		return errors.Join(queryloan.ErrUnavailable, err)
	case errors.Is(err, mongo.ErrNotFound):
		return errors.Join(queryloan.ErrNotFound, err)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, mongo.ErrUnavailable):
		return errors.Join(queryloan.ErrUnavailable, err)
	case errors.Is(err, mongo.ErrInvalidData):
		return errors.Join(queryloan.ErrInvalidData, err)
	default:
		return err
	}
}

// SetupRoute 路由初始化
func SetupRoute(router *gin.Engine, loanHandler, loanAuth gin.HandlerFunc, log *logrus.Logger, limit config.RateLimitConfig) {

	// 注册全局中间件
	registerGlobalMiddleWare(router, log, limit)

	//  注册 API 路由
	routes.RegisterAPIRoutes(router, loanHandler, loanAuth)
}

func registerGlobalMiddleWare(router *gin.Engine, log *logrus.Logger, limit config.RateLimitConfig) {
	// 异常恢复中间件（使用 Gin 内置）
	router.Use(gin.Recovery())

	// CORS 跨域中间件
	router.Use(middleware.CORS())

	// 请求日志中间件
	router.Use(middleware.Logger(log))

	// 全局按 IP 限流，参数由启动配置提供。
	rateLimiter := middleware.NewRateLimiter(limit.Requests, limit.Window)
	router.Use(rateLimiter.RateLimit())
}
