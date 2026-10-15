package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/pkg/rescode"
)

// ConfigCacheAuth 使用独立管理凭据；不读取客户API Key、Query或Body。
func ConfigCacheAuth(token string) gin.HandlerFunc {
	expected := sha256.Sum256([]byte(token))
	return func(c *gin.Context) {
		if token == "" {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		values := c.Request.Header.Values("Authorization")
		var supplied string
		valid := false
		if len(values) == 1 {
			scheme, value, ok := strings.Cut(values[0], " ")
			valid = ok && strings.EqualFold(scheme, "Bearer") && value != "" && len(strings.Fields(value)) == 1 && strings.TrimSpace(value) == value
			supplied = value
		}
		actual := sha256.Sum256([]byte(supplied))
		if subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 || !valid {
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": rescode.GetCodeMsg(rescode.ErrUnauthorized)})
			return
		}
		c.Next()
	}
}
