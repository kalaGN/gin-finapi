package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// Logger records metadata only; loan payloads and credentials must not be logged.
func Logger(log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if log != nil {
			log.WithFields(logrus.Fields{
				"method":      c.Request.Method,
				"path":        c.FullPath(),
				"http_status": c.Writer.Status(),
				"duration_ms": time.Since(start).Milliseconds(),
			}).Info("Request completed")
		}
	}
}
