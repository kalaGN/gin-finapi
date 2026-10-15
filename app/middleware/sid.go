package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/kalaGN/gin-finapi/pkg/rescode"
	"github.com/sirupsen/logrus"
)

// RequestSID 在参数解析和认证前生成业务 SID；失败时不再执行后续链路。
func RequestSID(generate func(string, int) (string, error), log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var sid string
		var err error
		if generate != nil {
			sid, err = generate("100", 29)
		}
		if err != nil || sid == "" {
			if log != nil {
				// 不记录随机源的原始错误，也不降级为可预测的 SID。
				log.WithFields(logrus.Fields{
					"event": "request_processing_failed", "operation": "generate SID",
					"error_kind": "sid_generation", "sid": "",
				}).Error("Request processing failed")
			}
			request.AbortWithStatusJSON(c, http.StatusInternalServerError, request.Response{
				Status: rescode.ErrInternalError, Msg: rescode.GetCodeMsg(rescode.ErrInternalError),
			})
			return
		}
		request.SetSID(c, sid)
		c.Next()
	}
}
