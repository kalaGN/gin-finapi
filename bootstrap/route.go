// Package bootstrap 处理程序初始化逻辑
package bootstrap

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/Http/controllers"
	"github.com/kalaGN/gin-finapi/app/Http/controllers/loan"
	"github.com/kalaGN/gin-finapi/app/middleware"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
	applogger "github.com/kalaGN/gin-finapi/pkg/logger"
	"github.com/kalaGN/gin-finapi/pkg/mongo"
	appredis "github.com/kalaGN/gin-finapi/pkg/redis"
	"github.com/kalaGN/gin-finapi/pkg/utils"
	"github.com/kalaGN/gin-finapi/routes"
	"github.com/sirupsen/logrus"
)

// NewRouter 根据启动配置和依赖装配 HTTP 处理器与中间件。
// 存储按需连接；已启用的配置同步在装配完成后启动，调用方负责关闭依赖。
func NewRouter(cfg config.AppConfig, log *logrus.Logger) (*gin.Engine, func(context.Context) error, error) {
	configuration, err := newConfiguration(cfg, log)
	if err != nil {
		return nil, nil, err
	}
	store, err := mongo.NewStore(cfg.MongoDB)
	if err != nil {
		return nil, nil, errors.Join(err, configuration.close(context.Background()))
	}
	reader := configuration.reader
	requestLog, closeRequestLog, err := applogger.NewRequestLogger(log, cfg.RequestLog, cfg.ChargeRedis)
	if err != nil {
		return nil, nil, errors.Join(err, configuration.close(context.Background()), store.Close(context.Background()))
	}
	closeDependencies := func(ctx context.Context) error {
		return errors.Join(configuration.stop(ctx), configuration.closeMySQL(), store.Close(ctx), configuration.closeRedis(), closeRequestLog(ctx))
	}
	query, err := services.NewQuery(func(ctx context.Context, pcode int) (map[string]int, error) {
		mapping, err := reader.LoadProductMap(ctx, pcode)
		return mapping, queryError(err)
	}, func(ctx context.Context, phone string, mapping map[string]int) (map[string]int, error) {
		data, err := store.Query(ctx, phone, mapping)
		return data, queryError(err)
	})
	if err != nil {
		return nil, nil, errors.Join(err, closeDependencies(context.Background()))
	}
	handler, err := loan.NewLoanHandler(query, log)
	if err != nil {
		return nil, nil, errors.Join(err, closeDependencies(context.Background()))
	}
	var cacheHandler *controllers.CacheHandler
	if cfg.ConfigCacheToken != "" {
		cacheHandler, err = controllers.NewCacheHandler(reader.Clear, log)
		if err != nil {
			return nil, nil, errors.Join(err, closeDependencies(context.Background()))
		}
	}
	router := gin.New()
	closeMiddleware := SetupRoute(router, handler.Create, middleware.LoanAuthWithProduct(reader.LoadCredential, reader.CheckProductAccess), log, cfg.RateLimit, requestLog)
	if cacheHandler != nil {
		routes.RegisterConfigCacheRoute(router, cacheHandler.Clear, middleware.ConfigCacheAuth(cfg.ConfigCacheToken), middleware.RequestSID(utils.GenerateSID, log))
	}
	configuration.start(log)
	return router, func(ctx context.Context) error {
		return errors.Join(closeMiddleware(ctx), closeDependencies(ctx))
	}, nil
}

// queryError 在装配适配边界转换存储错误，HTTP 和查询用例不依赖存储包。
func queryError(err error) error {
	switch {
	case errors.Is(err, appredis.ErrProductMapping):
		return errors.Join(services.ErrProductMapping, err)
	case errors.Is(err, appredis.ErrMappingUnavailable):
		return errors.Join(services.ErrUnavailable, err)
	case errors.Is(err, mongo.ErrNotFound):
		return errors.Join(services.ErrNotFound, err)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, mongo.ErrUnavailable):
		return errors.Join(services.ErrUnavailable, err)
	case errors.Is(err, mongo.ErrInvalidData):
		return errors.Join(services.ErrInvalidData, err)
	default:
		return err
	}
}

// SetupRoute 装配路由和中间件，调用方必须执行返回的关闭函数。
func SetupRoute(router *gin.Engine, loanHandler, loanAuth gin.HandlerFunc, log *logrus.Logger, limit config.RateLimitConfig, requestLogs ...*logrus.Logger) func(context.Context) error {

	// 注册全局中间件
	requestLog := log
	if len(requestLogs) > 0 {
		requestLog = requestLogs[0]
	}
	closeMiddleware := registerGlobalMiddleWare(router, requestLog, limit)

	//  注册 API 路由
	routes.RegisterAPIRoutes(router, loanHandler, loanAuth, middleware.RequestSID(utils.GenerateSID, log))
	return closeMiddleware
}

func registerGlobalMiddleWare(router *gin.Engine, log *logrus.Logger, limit config.RateLimitConfig) func(context.Context) error {
	// 异常恢复中间件（使用 Gin 内置）
	router.Use(gin.Recovery())

	// CORS 跨域中间件
	router.Use(middleware.CORS())

	// 请求日志中间件
	router.Use(middleware.Logger(log))
	// 业务异常先恢复再记录完成日志；外层 Recovery 仍保护日志和 CORS 自身。
	router.Use(gin.Recovery())

	// 全局按 IP 限流，参数由启动配置提供。
	rateLimiter := middleware.NewRateLimiter(limit.Requests, limit.Window)
	router.Use(rateLimiter.RateLimit())
	rateLimiter.Start()
	return rateLimiter.Close
}
