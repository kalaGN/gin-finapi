// Package bootstrap 处理程序初始化逻辑
package bootstrap

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/middleware"
	"github.com/kalaGN/gin-finapi/routes"
)

// SetupRoute 路由初始化
func SetupRoute(router *gin.Engine) {

	// 注册全局中间件
	registerGlobalMiddleWare(router)

	//  注册 API 路由
	routes.RegisterAPIRoutes(router)
}

func registerGlobalMiddleWare(router *gin.Engine) {
	// 异常恢复中间件（使用 Gin 内置）
	router.Use(gin.Recovery())

	// CORS 跨域中间件
	router.Use(middleware.CORS())

	// 请求日志中间件
	router.Use(middleware.Logger())

	// 全局限流：每秒 5,000 个请求
	rateLimiter := middleware.NewRateLimiter(5000, time.Second)
	router.Use(rateLimiter.RateLimit())
}
