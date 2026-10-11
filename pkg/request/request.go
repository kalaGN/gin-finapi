// Package request defines shared HTTP request and response types.
package request

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
)

const contextKey = "gin-finapi.authenticated-loan-request"

type Response struct {
	Status int            `json:"status"`
	Msg    string         `json:"msg"`
	Sid    string         `json:"sid"`
	Data   map[string]int `json:"data"`
}

type Request struct {
	Phone     StrictString  `json:"phone"`
	Pcode     StrictInteger `json:"pcode"`
	Apikey    string        `json:"apikey"`
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

// SetAuthenticated is called only after the authentication middleware succeeds.
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
