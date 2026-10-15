package loan

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/kalaGN/gin-finapi/pkg/rescode"
	"github.com/sirupsen/logrus"
)

type CommonRes = request.Response
type QueryFunc = services.QueryFunc
type SIDFunc func(string, int) (string, error)

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
		request.AbortWithStatusJSON(c, http.StatusUnauthorized, CommonRes{Status: rescode.ErrInvalidApikey, Msg: rescode.GetCodeMsg(rescode.ErrInvalidApikey)})
		return
	}
	queryKey, err := services.PhoneLookupKey(body.Phone.Value)
	if err != nil {
		h.logFailure(c, "phone lookup", "phone_conversion", logrus.ErrorLevel)
		request.JSON(c, http.StatusServiceUnavailable, CommonRes{Status: rescode.ErrServiceUnavailable, Msg: rescode.GetCodeMsg(rescode.ErrServiceUnavailable)})
		return
	}
	sid, err := h.generateSID("100", 29)
	if err != nil {
		h.logFailure(c, "generate SID", "sid_generation", logrus.ErrorLevel)
		request.JSON(c, http.StatusInternalServerError, CommonRes{Status: rescode.ErrInternalError, Msg: rescode.GetCodeMsg(rescode.ErrInternalError)})
		return
	}
	// 标识归一化只影响数据库查询，phone 不参与签名。
	result, err := h.query(c.Request.Context(), queryKey, int(body.Pcode.Value))
	if err != nil {
		h.writeQueryError(c, err)
		return
	}
	request.JSON(c, http.StatusOK, CommonRes{Status: rescode.SuccessCode, Msg: rescode.GetCodeMsg(rescode.SuccessCode), Sid: sid, Data: result})
}

func (h *LoanHandler) writeQueryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrProductMapping):
		h.logFailure(c, "query loan data", "product_mapping", logrus.WarnLevel)
		request.JSON(c, http.StatusOK, CommonRes{Status: rescode.ErrProductException, Msg: rescode.GetCodeMsg(rescode.ErrProductException)})
	case errors.Is(err, services.ErrUnavailable):
		h.logFailure(c, "query loan data", "unavailable", logrus.ErrorLevel)
		request.JSON(c, http.StatusServiceUnavailable, CommonRes{Status: rescode.ErrServiceUnavailable, Msg: rescode.GetCodeMsg(rescode.ErrServiceUnavailable)})
	case errors.Is(err, services.ErrNotFound):
		request.JSON(c, http.StatusOK, CommonRes{Status: rescode.ErrDataNotFound, Msg: rescode.GetCodeMsg(rescode.ErrDataNotFound)})
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		kind := "timeout"
		if !errors.Is(err, context.DeadlineExceeded) {
			kind = "canceled"
		}
		h.logFailure(c, "query loan data", kind, logrus.ErrorLevel)
		request.JSON(c, http.StatusGatewayTimeout, CommonRes{Status: rescode.ErrTimeout, Msg: rescode.GetCodeMsg(rescode.ErrTimeout)})
	default:
		kind := "internal"
		if errors.Is(err, services.ErrInvalidData) {
			kind = "invalid_data"
		}
		h.logFailure(c, "query loan data", kind, logrus.ErrorLevel)
		request.JSON(c, http.StatusInternalServerError, CommonRes{Status: rescode.ErrInternalError, Msg: rescode.GetCodeMsg(rescode.ErrInternalError)})
	}
}

func (h *LoanHandler) logFailure(c *gin.Context, operation, kind string, level logrus.Level) {
	if h.log != nil {
		// 分类来自固定集合，禁止记录可能包含凭据或查询值的原始驱动错误。
		h.log.WithFields(logrus.Fields{
			"event": "request_processing_failed", "operation": operation,
			"error_kind": kind, "request_id": request.ID(c),
		}).Log(level, "Request processing failed")
	}
}
