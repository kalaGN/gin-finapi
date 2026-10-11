package loan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	appconfig "github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/kalaGN/gin-finapi/pkg/logger"
	"github.com/kalaGN/gin-finapi/pkg/mongo"
	"github.com/kalaGN/gin-finapi/pkg/rescode"
	"github.com/kalaGN/gin-finapi/pkg/utils"
)

type CommonRes struct {
	Status int            `json:"status"`
	Msg    string         `json:"msg"`
	Sid    string         `json:"sid"`
	Data   map[string]int `json:"data"`
}

type createRequest struct {
	Phone     strictString  `json:"phone"`
	Pcode     strictInteger `json:"pcode"`
	Apikey    string        `json:"apikey"`
	Timestamp strictInteger `json:"timestamp"`
	Sign      string        `json:"sign"`
}

type strictString struct {
	value string
	err   error
	set   bool
}

func (value *strictString) UnmarshalJSON(data []byte) error {
	value.set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		value.err = errors.New("value must be a JSON string")
		return nil
	}
	value.err = json.Unmarshal(data, &value.value)
	return nil
}

type strictInteger struct {
	value int64
	err   error
	set   bool
}

func (integer *strictInteger) UnmarshalJSON(data []byte) error {
	integer.set = true
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] == '"' {
		integer.err = errors.New("value must be a JSON integer")
		return nil
	}
	integer.value, integer.err = strconv.ParseInt(string(data), 10, 64)
	return nil
}

type queryFunc func(context.Context, mongo.Config) (map[string]int, error)
type sidFunc func(string, int) (string, error)

func Create(c *gin.Context) {
	create(c, mongo.GetMongo, utils.GenerateSID, appconfig.GetSecretKey())
}

func create(c *gin.Context, query queryFunc, generateSID sidFunc, secretKey string) {
	var body createRequest

	// 绑定 JSON
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, CommonRes{Status: rescode.ErrInvalidJSON, Msg: rescode.GetCodeMsg(rescode.ErrInvalidJSON), Sid: ""})
		return
	}

	if body.Phone.set && body.Phone.err != nil {
		c.JSON(http.StatusBadRequest, CommonRes{Status: rescode.ErrInvalidPhone, Msg: rescode.GetCodeMsg(rescode.ErrInvalidPhone), Sid: ""})
		return
	}
	if body.Phone.value == "" {
		c.JSON(http.StatusBadRequest, CommonRes{Status: rescode.ErrMissingParam, Msg: "phone is required", Sid: ""})
		return
	}

	if !body.Pcode.set || body.Pcode.err != nil {
		c.JSON(http.StatusBadRequest, CommonRes{Status: rescode.ErrInvalidPcode, Msg: "pcode must be a number", Sid: ""})
		return
	}
	if body.Pcode.value < 10001 || body.Pcode.value > 99999 {
		c.JSON(http.StatusBadRequest, CommonRes{Status: rescode.ErrInvalidPcode, Msg: rescode.GetCodeMsg(rescode.ErrInvalidPcode), Sid: ""})
		return
	}
	pcode := int(body.Pcode.value)

	// 验证 apikey
	if body.Apikey == "" {
		c.JSON(http.StatusBadRequest, CommonRes{Status: rescode.ErrInvalidApikey, Msg: rescode.GetCodeMsg(rescode.ErrInvalidApikey), Sid: ""})
		return
	}

	if !body.Timestamp.set || body.Timestamp.err != nil {
		c.JSON(http.StatusBadRequest, CommonRes{Status: rescode.ErrInvalidTimestamp, Msg: "timestamp must be a number", Sid: ""})
		return
	}
	timestamp := body.Timestamp.value
	if !utils.VerifyTimestamp(timestamp) {
		c.JSON(http.StatusBadRequest, CommonRes{Status: rescode.ErrInvalidTimestamp, Msg: rescode.GetCodeMsg(rescode.ErrInvalidTimestamp), Sid: ""})
		return
	}

	// 验证签名
	if body.Sign == "" {
		c.JSON(http.StatusBadRequest, CommonRes{Status: rescode.ErrInvalidSign, Msg: "sign is required", Sid: ""})
		return
	}

	// 构建签名参数
	signParams := map[string]interface{}{
		"phone":     body.Phone.value,
		"pcode":     pcode,
		"apikey":    body.Apikey,
		"timestamp": timestamp,
	}

	if !utils.VerifySign(signParams, body.Sign, secretKey) {
		c.JSON(http.StatusBadRequest, CommonRes{Status: rescode.ErrInvalidSign, Msg: rescode.GetCodeMsg(rescode.ErrInvalidSign), Sid: ""})
		return
	}

	sid, err := generateSID("100", 29)
	if err != nil {
		logInternalError("generate SID", err)
		c.JSON(http.StatusInternalServerError, CommonRes{Status: rescode.ErrInternalError, Msg: rescode.GetCodeMsg(rescode.ErrInternalError)})
		return
	}

	mongoConfig := appconfig.GetMongoConfig()
	queryConfig := mongo.Config{
		DSN:        mongoConfig.DSN,
		DB:         mongoConfig.Database,
		Collection: mongoConfig.Collection,
		Query:      body.Phone.value,
	}

	result, err := query(c.Request.Context(), queryConfig)
	if err != nil {
		writeQueryError(c, err)
		return
	}

	c.JSON(http.StatusOK, CommonRes{Status: rescode.SuccessCode, Msg: rescode.GetCodeMsg(rescode.SuccessCode), Sid: sid, Data: result})
}

func writeQueryError(c *gin.Context, err error) {
	logInternalError("query loan data", err)

	switch {
	case errors.Is(err, mongo.ErrNotFound):
		c.JSON(http.StatusOK, CommonRes{Status: rescode.ErrDataNotFound, Msg: rescode.GetCodeMsg(rescode.ErrDataNotFound)})
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		c.JSON(http.StatusGatewayTimeout, CommonRes{Status: rescode.ErrTimeout, Msg: rescode.GetCodeMsg(rescode.ErrTimeout)})
	case errors.Is(err, mongo.ErrUnavailable):
		c.JSON(http.StatusServiceUnavailable, CommonRes{Status: rescode.ErrServiceUnavailable, Msg: rescode.GetCodeMsg(rescode.ErrServiceUnavailable)})
	case errors.Is(err, mongo.ErrInvalidData):
		c.JSON(http.StatusInternalServerError, CommonRes{Status: rescode.ErrInternalError, Msg: rescode.GetCodeMsg(rescode.ErrInternalError)})
	default:
		c.JSON(http.StatusInternalServerError, CommonRes{Status: rescode.ErrInternalError, Msg: rescode.GetCodeMsg(rescode.ErrInternalError)})
	}
}

func logInternalError(operation string, err error) {
	if logger.Log != nil {
		logger.Log.WithError(err).WithField("operation", operation).Error("Request processing failed")
	}
}
