// Package request 定义共享的 HTTP 请求与响应类型。
package request

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
)

const contextKey = "gin-finapi.authenticated-loan-request"
const responseStatusKey = "gin-finapi.response-status"

type Response struct {
	Status int            `json:"status"`
	Msg    string         `json:"msg"`
	Sid    string         `json:"sid"`
	Data   map[string]int `json:"data"`
}

// JSON 记录安全的业务状态元信息，不保存响应体。
func JSON(c *gin.Context, httpStatus int, response Response) {
	c.Set(responseStatusKey, response.Status)
	c.JSON(httpStatus, response)
}

// AbortWithStatusJSON 终止处理链并写入带业务状态元信息的响应。
func AbortWithStatusJSON(c *gin.Context, httpStatus int, response Response) {
	c.Abort()
	JSON(c, httpStatus, response)
}

// ResponseStatus 区分业务成功 0 与没有业务状态的响应。
func ResponseStatus(c *gin.Context) (int, bool) {
	value, exists := c.Get(responseStatusKey)
	if !exists {
		return 0, false
	}
	status, ok := value.(int)
	return status, ok
}

type Request struct {
	Phone     StrictString  `json:"phone"`
	Pcode     StrictInteger `json:"pcode"`
	Apikey    string        `json:"apikey"`
	Nonce     StrictString  `json:"nonce"`
	Timestamp StrictInteger `json:"timestamp"`
	Sign      string        `json:"sign"`
}

type StrictString struct {
	Value string
	Err   error
	Set   bool
}

func (v *StrictString) UnmarshalJSON(data []byte) error {
	v.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		v.Err = errors.New("value must be a JSON string")
		return nil
	}
	v.Err = json.Unmarshal(data, &v.Value)
	return nil
}

type StrictInteger struct {
	Value int64
	Err   error
	Set   bool
}

func (v *StrictInteger) UnmarshalJSON(data []byte) error {
	v.Set = true
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] == '"' {
		v.Err = errors.New("value must be a JSON integer")
		return nil
	}
	v.Value, v.Err = strconv.ParseInt(string(data), 10, 64)
	return nil
}

// SetAuthenticated 仅在认证中间件校验成功后保存请求。
func SetAuthenticated(c *gin.Context, request Request) {
	c.Set(contextKey, request)
}

func Authenticated(c *gin.Context) (Request, bool) {
	value, ok := c.Get(contextKey)
	if !ok {
		return Request{}, false
	}
	request, ok := value.(Request)
	return request, ok
}
