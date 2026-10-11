// Package bootstrap 处理程序初始化逻辑
package bootstrap

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/Http/controllers/loan"
	"github.com/kalaGN/gin-finapi/app/middleware"
	"github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/kalaGN/gin-finapi/pkg/mongo"
	appredis "github.com/kalaGN/gin-finapi/pkg/redis"
	"github.com/kalaGN/gin-finapi/pkg/utils"
	"github.com/kalaGN/gin-finapi/routes"
	"github.com/sirupsen/logrus"
)

// NewRouter 根据启动配置和依赖装配 HTTP 处理器与中间件。
// Redis/MongoDB 均按需连接；调用方负责执行返回的 Redis 关闭函数。
func NewRouter(cfg config.AppConfig, log *logrus.Logger) (*gin.Engine, func() error, error) {
	maps, err := appredis.NewProductMaps(cfg.Redis)
	if err != nil {
		return nil, nil, err
	}
	handler, err := loan.NewLoanHandler(
		newLoanQuery(cfg.MongoDB, maps.Load, mongo.GetMongo), utils.GenerateSID, log,
	)
	if err != nil {
		return nil, nil, errors.Join(err, maps.Close())
	}
	router := gin.New()
	SetupRoute(router, handler.Create, middleware.LoanAuth(cfg.APIKey, cfg.SecretKey), log, cfg.RateLimit)
	return router, maps.Close, nil
}

// newLoanQuery 先读取产品映射，再查询 Mongo；配置异常不访问 Mongo。
func newLoanQuery(cfg config.MongoDBConfig, load func(context.Context, int) (map[string]int, error), query func(context.Context, mongo.Config) (map[string]int, error)) loan.QueryFunc {
	return func(ctx context.Context, phone string, pcode int) (map[string]int, error) {
		mapping, err := load(ctx, pcode)
		if err != nil {
			return nil, err
		}
		return query(ctx, mongo.Config{DSN: cfg.DSN, DB: cfg.Database, Collection: cfg.Collection, Query: phone, Mapping: mapping})
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
