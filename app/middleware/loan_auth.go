package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/kalaGN/gin-finapi/pkg/rescode"
	"github.com/kalaGN/gin-finapi/pkg/utils"
)

// LoanAuthWithProduct 在 API Key 签名认证后校验对应 pcode 的授权。
func LoanAuthWithProduct(load func(context.Context, string) (services.Credential, error), loadProduct func(context.Context, string, int) error) gin.HandlerFunc {
	return loanAuth(load, loadProduct)
}

func loanAuth(load func(context.Context, string) (services.Credential, error), loadProduct func(context.Context, string, int) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 失败返回也记录鉴权结束；成功在Next前标记，同名仅记首次，不包含下游查询。
		defer utils.MarkTiming(c.Request.Context(), "auth")
		reject := func(code int, message string) {
			request.AbortWithStatusJSON(c, http.StatusBadRequest, request.Response{Status: code, Msg: message})
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
		if len(body.Phone.Value) != 32 && len(body.Phone.Value) != 64 {
			reject(rescode.ErrInvalidPhone, rescode.GetCodeMsg(rescode.ErrInvalidPhone))
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
		if body.Nonce.Set && body.Nonce.Err != nil {
			reject(rescode.ErrInvalidParam, rescode.MsgNonceMustBeString)
			return
		}
		if body.Nonce.Value == "" {
			reject(rescode.ErrMissingParam, rescode.MsgNonceRequired)
			return
		}
		if load == nil {
			request.AbortWithStatusJSON(c, http.StatusServiceUnavailable, request.Response{Status: rescode.ErrServiceUnavailable, Msg: rescode.GetCodeMsg(rescode.ErrServiceUnavailable)})
			return
		}
		credential, err := load(c.Request.Context(), body.Apikey)
		if errors.Is(err, services.ErrAPIKeyInvalid) {
			reject(rescode.ErrInvalidApikey, rescode.GetCodeMsg(rescode.ErrInvalidApikey))
			return
		}
		if err != nil || credential.Secret == "" {
			request.AbortWithStatusJSON(c, http.StatusServiceUnavailable, request.Response{Status: rescode.ErrServiceUnavailable, Msg: rescode.GetCodeMsg(rescode.ErrServiceUnavailable)})
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
			"apikey": body.Apikey, "timestamp": body.Timestamp.Value,
			"nonce": body.Nonce.Value,
		}
		if !utils.VerifySign(params, body.Sign, credential.Secret) {
			reject(rescode.ErrInvalidSign, rescode.GetCodeMsg(rescode.ErrInvalidSign))
			return
		}
		if loadProduct != nil {
			if err := loadProduct(c.Request.Context(), body.Apikey, int(body.Pcode.Value)); err != nil {
				if errors.Is(err, services.ErrProductDenied) {
					request.AbortWithStatusJSON(c, http.StatusForbidden, request.Response{Status: rescode.ErrPermissionDenied, Msg: rescode.GetCodeMsg(rescode.ErrPermissionDenied)})
					return
				}
				request.AbortWithStatusJSON(c, http.StatusServiceUnavailable, request.Response{Status: rescode.ErrServiceUnavailable, Msg: rescode.GetCodeMsg(rescode.ErrServiceUnavailable)})
				return
			}
		}
		request.SetAuthenticated(c, body)
		utils.MarkTiming(c.Request.Context(), "auth")
		c.Next()
	}
}
