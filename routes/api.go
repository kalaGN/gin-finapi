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
	loanc "github.com/kalaGN/gin-finapi/app/Http/controllers/loan"
)

// RegisterAPIRoutes 注册网页相关路由
func RegisterAPIRoutes(router *gin.Engine) {
	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// Loan 路由
	router.POST("/loan", loanc.Create)
}
