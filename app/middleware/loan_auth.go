package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/kalaGN/gin-finapi/pkg/rescode"
	"github.com/kalaGN/gin-finapi/pkg/utils"
)

// LoanAuth 单次解析 JSON，校验 API Key 和原始 phone 的签名。
func LoanAuth(apiKey, secret string) gin.HandlerFunc {
	expectedKey := sha256.Sum256([]byte(apiKey))
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
			reject(rescode.ErrMissingParam, "phone is required")
			return
		}
		if !body.Pcode.Set || body.Pcode.Err != nil {
			reject(rescode.ErrInvalidPcode, "pcode must be a number")
			return
		}
		if body.Pcode.Value < 10001 || body.Pcode.Value > 99999 {
			reject(rescode.ErrInvalidPcode, rescode.GetCodeMsg(rescode.ErrInvalidPcode))
			return
		}
		actualKey := sha256.Sum256([]byte(body.Apikey))
		if apiKey == "" || secret == "" || body.Apikey == "" || subtle.ConstantTimeCompare(actualKey[:], expectedKey[:]) != 1 {
			reject(rescode.ErrInvalidApikey, rescode.GetCodeMsg(rescode.ErrInvalidApikey))
			return
		}
		if !body.Timestamp.Set || body.Timestamp.Err != nil {
			reject(rescode.ErrInvalidTimestamp, "timestamp must be a number")
			return
		}
		if !utils.VerifyTimestamp(body.Timestamp.Value) {
			reject(rescode.ErrInvalidTimestamp, rescode.GetCodeMsg(rescode.ErrInvalidTimestamp))
			return
		}
		if body.Sign == "" {
			reject(rescode.ErrInvalidSign, "sign is required")
			return
		}
		params := map[string]interface{}{
			"phone": body.Phone.Value, "pcode": int(body.Pcode.Value),
			"apikey": body.Apikey, "timestamp": body.Timestamp.Value,
		}
		if !utils.VerifySign(params, body.Sign, secret) {
			reject(rescode.ErrInvalidSign, rescode.GetCodeMsg(rescode.ErrInvalidSign))
			return
		}
		request.SetAuthenticated(c, body)
		c.Next()
	}
}
