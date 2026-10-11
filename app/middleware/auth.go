package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/pkg/rescode"
)

// AuthRequired 认证中间件
func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从请求头获取 token
		token := c.GetHeader("Authorization")

		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"status": 401,
				"msg":    "Authorization token required",
			})
			c.Abort()
			return
		}

		// TODO: 验证 token 的有效性
		// 这里可以添加 JWT 验证、Redis 验证等逻辑

		// 验证通过，继续处理
		c.Next()
	}
}

// APIKeyAuth API Key 认证中间件
func APIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.Query("apikey")
		if apiKey == "" {
			apiKey = c.GetHeader("X-API-Key")
		}

		if apiKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"status": rescode.Err1101,
				"msg":    rescode.GetCodeMsg(rescode.Err1101),
			})
			c.Abort()
			return
		}

		// TODO: 验证 API Key 的有效性
		// 可以从数据库或配置中验证

		c.Next()
	}
}
