/*
 * @Author: afei
 * @Date: 2022-07-14 09:22:45
 * @LastEditors: afei
 * @LastEditTime: 2022-07-18 10:55:38
 * @Description:
 *
 * Copyright (c) 2022 , All Rights Reserved.
 */
package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RegisterAPIRoutes 注册健康检查及 SID → 认证 → 业务处理链。
func RegisterAPIRoutes(router *gin.Engine, loanHandler, loanAuth, loanSID gin.HandlerFunc) {
	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// Loan 业务接口统一挂在 /api 入口。
	router.POST("/api", loanSID, loanAuth, loanHandler)
}

// RegisterConfigCacheRoute 仅由启用管理Token的启动层注册，不复用业务认证。
func RegisterConfigCacheRoute(router *gin.Engine, handler, auth, sid gin.HandlerFunc) {
	router.DELETE("/config/cache", sid, auth, handler)
}
