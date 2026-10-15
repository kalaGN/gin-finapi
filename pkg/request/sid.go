package request

import "github.com/gin-gonic/gin"

const sidKey = "gin-finapi.sid"

// SetSID 仅由服务端中间件保存 SID，不从客户端请求提取。
func SetSID(c *gin.Context, sid string) { c.Set(sidKey, sid) }

// SID 返回本次请求已生成的业务标识，缺失时为空。
func SID(c *gin.Context) string {
	value, _ := c.Get(sidKey)
	sid, _ := value.(string)
	return sid
}
