package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/auth"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/kalaGN/gin-finapi/pkg/rescode"
	"github.com/kalaGN/gin-finapi/pkg/utils"
)

// LoanAuth 单次解析 JSON，校验 API Key 和原始 phone 的签名。
func LoanAuth(load func(context.Context, string) (auth.Credential, error)) gin.HandlerFunc {
	return loanAuth(load, nil)
}

// LoanAuthWithProduct 在 API Key 签名认证后校验对应 pcode 的授权。
func LoanAuthWithProduct(load func(context.Context, string) (auth.Credential, error), loadProduct func(context.Context, string, int) error) gin.HandlerFunc {
	return loanAuth(load, loadProduct)
}

func loanAuth(load func(context.Context, string) (auth.Credential, error), loadProduct func(context.Context, string, int) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		reject := func(code int, message string) {
			c.AbortWithStatusJSON(http.StatusBadRequest, request.Response{Status: code, Msg: message})
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
		var body request.Request
		if c.ContentType() != "application/json" {
			reject(rescode.ErrInvalidJSON, rescode.GetCodeMsg(rescode.ErrInvalidJSON))
			return
		}
		decoder := json.NewDecoder(c.Request.Body)
		if err := decoder.Decode(&body); err != nil {
			reject(rescode.ErrInvalidJSON, rescode.GetCodeMsg(rescode.ErrInvalidJSON))
			return
		}
		// 只允许一个 JSON 值，并读到 EOF，确保请求体大小限制生效。
		if err := decoder.Decode(new(interface{})); err != io.EOF {
			reject(rescode.ErrInvalidJSON, rescode.GetCodeMsg(rescode.ErrInvalidJSON))
			return
		}
		if body.Phone.Set && body.Phone.Err != nil {
			reject(rescode.ErrInvalidPhone, rescode.GetCodeMsg(rescode.ErrInvalidPhone))
			return
		}
		if body.Phone.Value == "" {
			reject(rescode.ErrMissingParam, rescode.MsgPhoneRequired)
			return
		}
		if !body.Pcode.Set || body.Pcode.Err != nil {
			reject(rescode.ErrInvalidPcode, rescode.MsgPcodeMustBeNumber)
			return
		}
		if body.Pcode.Value < 10001 || body.Pcode.Value > 99999 {
			reject(rescode.ErrInvalidPcode, rescode.GetCodeMsg(rescode.ErrInvalidPcode))
			return
		}
		if body.Apikey == "" {
			reject(rescode.ErrInvalidApikey, rescode.GetCodeMsg(rescode.ErrInvalidApikey))
			return
		}
		if load == nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, request.Response{Status: rescode.ErrServiceUnavailable, Msg: rescode.GetCodeMsg(rescode.ErrServiceUnavailable)})
			return
		}
		credential, err := load(c.Request.Context(), body.Apikey)
		if errors.Is(err, auth.ErrInvalid) {
			reject(rescode.ErrInvalidApikey, rescode.GetCodeMsg(rescode.ErrInvalidApikey))
			return
		}
		if err != nil || credential.Secret == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, request.Response{Status: rescode.ErrServiceUnavailable, Msg: rescode.GetCodeMsg(rescode.ErrServiceUnavailable)})
			return
		}
		if !body.Timestamp.Set || body.Timestamp.Err != nil {
			reject(rescode.ErrInvalidTimestamp, rescode.MsgTimestampMustBeNumber)
			return
		}
		if !utils.VerifyTimestamp(body.Timestamp.Value) {
			reject(rescode.ErrInvalidTimestamp, rescode.GetCodeMsg(rescode.ErrInvalidTimestamp))
			return
		}
		if body.Sign == "" {
			reject(rescode.ErrInvalidSign, rescode.MsgSignRequired)
			return
		}
		params := map[string]interface{}{
			"phone": body.Phone.Value, "pcode": int(body.Pcode.Value),
			"apikey": body.Apikey, "timestamp": body.Timestamp.Value,
		}
		if !utils.VerifySign(params, body.Sign, credential.Secret) {
			reject(rescode.ErrInvalidSign, rescode.GetCodeMsg(rescode.ErrInvalidSign))
			return
		}
		if loadProduct != nil {
			if err := loadProduct(c.Request.Context(), body.Apikey, int(body.Pcode.Value)); err != nil {
				if errors.Is(err, auth.ErrProductDenied) {
					c.AbortWithStatusJSON(http.StatusForbidden, request.Response{Status: rescode.ErrPermissionDenied, Msg: rescode.GetCodeMsg(rescode.ErrPermissionDenied)})
					return
				}
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, request.Response{Status: rescode.ErrServiceUnavailable, Msg: rescode.GetCodeMsg(rescode.ErrServiceUnavailable)})
				return
			}
		}
		request.SetAuthenticated(c, body)
		c.Next()
	}
}
