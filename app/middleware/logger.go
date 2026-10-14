package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/kalaGN/gin-finapi/pkg/rescode"
	"github.com/sirupsen/logrus"
)

// Logger 按用户确认记录完整请求体和响应；不采集 Header 或 URL Query。
func Logger(log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if log == nil {
			c.Next()
			return
		}
		start := time.Now()
		input, truncated := captureInput(c)
		writer := &responseLogWriter{ResponseWriter: c.Writer}
		c.Writer = writer
		c.Next()
		if log != nil {
			success, charge := 0, 0
			code, ok := request.ResponseStatus(c)
			if !ok {
				var result struct {
					Status *int `json:"status"`
				}
				if json.Unmarshal(writer.body.Bytes(), &result) == nil && result.Status != nil {
					code, ok = *result.Status, true
				}
			}
			var status interface{}
			if ok {
				status = code
				if code == rescode.SuccessCode || code == rescode.ErrDataNotFound {
					success = 1
				}
				if code == rescode.SuccessCode {
					charge = 1
				}
			}
			fields := logrus.Fields{
				"method":        c.Request.Method,
				"path":          c.FullPath(),
				"http_status":   c.Writer.Status(),
				"duration_ms":   time.Since(start).Milliseconds(),
				"success":       success,
				"charge":        charge,
				"status":        status,
				"input":         input,
				"response_data": logPayload(writer.body.Bytes()),
			}
			if truncated {
				fields["input_truncated"] = true
			}
			log.WithFields(fields).Info("Request completed")
		}
	}
}

const maxLoggedRequestBody = 64 << 10

// 读取后重放给原解析链，连读取错误一起保留，避免改变原请求校验。
func captureInput(c *gin.Context) (interface{}, bool) {
	if c.Request.Body == nil {
		return "", false
	}
	original := c.Request.Body
	data, err := io.ReadAll(io.LimitReader(original, maxLoggedRequestBody+1))
	var tail io.Reader = original
	if err != nil {
		tail = readError{err}
	}
	c.Request.Body = replayBody{Reader: io.MultiReader(bytes.NewReader(data), tail), Closer: original}
	truncated := len(data) > maxLoggedRequestBody
	if truncated {
		data = data[:maxLoggedRequestBody]
	}
	return logPayload(data), truncated
}

func logPayload(data []byte) interface{} {
	if json.Valid(data) {
		return json.RawMessage(data)
	}
	return string(data)
}

type replayBody struct {
	io.Reader
	io.Closer
}
type readError struct{ err error }

func (r readError) Read([]byte) (int, error) { return 0, r.err }

type responseLogWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *responseLogWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.body.Write(data[:n])
	return n, err
}
func (w *responseLogWriter) WriteString(data string) (int, error) {
	n, err := w.ResponseWriter.WriteString(data)
	w.body.WriteString(data[:n])
	return n, err
}
