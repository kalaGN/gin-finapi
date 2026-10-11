package loan

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/pkg/mongo"
	appredis "github.com/kalaGN/gin-finapi/pkg/redis"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/kalaGN/gin-finapi/pkg/rescode"
	"github.com/sirupsen/logrus"
)

type CommonRes = request.Response
type QueryFunc func(context.Context, string, int) (map[string]int, error)
type SIDFunc func(string, int) (string, error)

var errPhoneConversionUnavailable = errors.New("SHA-256 phone conversion service is not connected")

func phoneLookupKey(phone string) (string, error) {
	if len(phone) == 32 || len(phone) == 64 {
		if _, err := hex.DecodeString(phone); err == nil {
			if len(phone) == 32 {
				return phone[:20], nil
			}
			// TODO: 接入后续手机号转换服务，将 SHA-256(phone) 映射为 MD5(phone)，
			// 再取 MD5 的前 20 位作为查询标识。
			// 不得对 SHA-256 字符串重新哈希，否则得到的查询标识与原手机号不一致。
			return "", errPhoneConversionUnavailable
		}
	}
	// 兼容已有的截短标识和历史非空查询值，保持原样查询。
	return phone, nil
}

// LoanHandler 使用显式注入的依赖处理已认证请求。
type LoanHandler struct {
	query       QueryFunc
	generateSID SIDFunc
	log         *logrus.Logger
}

func NewLoanHandler(query QueryFunc, sid SIDFunc, log *logrus.Logger) (*LoanHandler, error) {
	if query == nil || sid == nil {
		return nil, errors.New("loan handler requires query and SID dependencies")
	}
	return &LoanHandler{query: query, generateSID: sid, log: log}, nil
}

func (h *LoanHandler) Create(c *gin.Context) {
	body, ok := request.Authenticated(c)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, CommonRes{Status: rescode.ErrInvalidApikey, Msg: rescode.GetCodeMsg(rescode.ErrInvalidApikey)})
		return
	}
	queryKey, err := phoneLookupKey(body.Phone.Value)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, CommonRes{Status: rescode.ErrServiceUnavailable, Msg: rescode.GetCodeMsg(rescode.ErrServiceUnavailable)})
		return
	}
	sid, err := h.generateSID("100", 29)
	if err != nil {
		h.logInternalError("generate SID")
		c.JSON(http.StatusInternalServerError, CommonRes{Status: rescode.ErrInternalError, Msg: rescode.GetCodeMsg(rescode.ErrInternalError)})
		return
	}
	// 认证使用请求中的完整 phone，转换仅影响数据库查询标识。
	result, err := h.query(c.Request.Context(), queryKey, int(body.Pcode.Value))
	if err != nil {
		h.writeQueryError(c, err)
		return
	}
	c.JSON(http.StatusOK, CommonRes{Status: rescode.SuccessCode, Msg: rescode.GetCodeMsg(rescode.SuccessCode), Sid: sid, Data: result})
}

func (h *LoanHandler) writeQueryError(c *gin.Context, err error) {
	h.logInternalError("query loan data")
	switch {
	case errors.Is(err, appredis.ErrProductMapping):
		c.JSON(http.StatusOK, CommonRes{Status: rescode.ErrProductException, Msg: rescode.GetCodeMsg(rescode.ErrProductException)})
	case errors.Is(err, appredis.ErrMappingUnavailable):
		c.JSON(http.StatusServiceUnavailable, CommonRes{Status: rescode.ErrServiceUnavailable, Msg: rescode.GetCodeMsg(rescode.ErrServiceUnavailable)})
	case errors.Is(err, mongo.ErrNotFound):
		c.JSON(http.StatusOK, CommonRes{Status: rescode.ErrDataNotFound, Msg: rescode.GetCodeMsg(rescode.ErrDataNotFound)})
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		c.JSON(http.StatusGatewayTimeout, CommonRes{Status: rescode.ErrTimeout, Msg: rescode.GetCodeMsg(rescode.ErrTimeout)})
	case errors.Is(err, mongo.ErrUnavailable):
		c.JSON(http.StatusServiceUnavailable, CommonRes{Status: rescode.ErrServiceUnavailable, Msg: rescode.GetCodeMsg(rescode.ErrServiceUnavailable)})
	default:
		c.JSON(http.StatusInternalServerError, CommonRes{Status: rescode.ErrInternalError, Msg: rescode.GetCodeMsg(rescode.ErrInternalError)})
	}
}

func (h *LoanHandler) logInternalError(operation string) {
	if h.log != nil {
		// 驱动错误可能包含凭据或查询值，因此日志只记录操作名称。
		h.log.WithField("operation", operation).Error("Request processing failed")
	}
}
