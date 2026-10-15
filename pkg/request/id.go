package request

import (
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

const requestIDKey = "gin-finapi.request-id"

var requestIDPrefix = strconv.FormatInt(time.Now().UnixNano(), 36)
var requestSequence atomic.Uint64

// ID 返回服务端生成的进程内唯一标识，仅用于日志关联，不是安全令牌或业务 SID。
func ID(c *gin.Context) string {
	if value, ok := c.Get(requestIDKey); ok {
		if id, ok := value.(string); ok && id != "" {
			return id
		}
	}
	id := requestIDPrefix + "-" + strconv.FormatUint(requestSequence.Add(1), 36)
	c.Set(requestIDKey, id)
	return id
}
