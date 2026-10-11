// Package bootstrap 处理程序初始化逻辑
package bootstrap

import (
	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/middleware"
	"github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/kalaGN/gin-finapi/routes"
	"github.com/sirupsen/logrus"
)

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
