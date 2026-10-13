package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/kalaGN/gin-finapi/pkg/rescode"
	"github.com/sirupsen/logrus"
)

// Logger 只记录请求元信息，不记录业务请求体或认证凭据。
func Logger(log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if log != nil {
			success, charge := 0, 0
			if status, ok := request.ResponseStatus(c); ok {
				if status == rescode.SuccessCode || status == rescode.ErrDataNotFound {
					success = 1
				}
				if status == rescode.SuccessCode {
					charge = 1
				}
			}
			log.WithFields(logrus.Fields{
				"method":      c.Request.Method,
				"path":        c.FullPath(),
				"http_status": c.Writer.Status(),
				"duration_ms": time.Since(start).Milliseconds(),
				"success":     success,
				"charge":      charge,
			}).Info("Request completed")
		}
	}
}
