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
		request.JSON(c, http.StatusServiceUnavailable, CommonRes{Status: rescode.ErrServiceUnavailable, Msg: rescode.GetCodeMsg(rescode.ErrServiceUnavailable)})
		return
	}
	sid, err := h.generateSID("100", 29)
	if err != nil {
		h.logInternalError("generate SID")
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
	h.logInternalError("query loan data")
	switch {
	case errors.Is(err, services.ErrProductMapping):
		request.JSON(c, http.StatusOK, CommonRes{Status: rescode.ErrProductException, Msg: rescode.GetCodeMsg(rescode.ErrProductException)})
	case errors.Is(err, services.ErrUnavailable):
		request.JSON(c, http.StatusServiceUnavailable, CommonRes{Status: rescode.ErrServiceUnavailable, Msg: rescode.GetCodeMsg(rescode.ErrServiceUnavailable)})
	case errors.Is(err, services.ErrNotFound):
		request.JSON(c, http.StatusOK, CommonRes{Status: rescode.ErrDataNotFound, Msg: rescode.GetCodeMsg(rescode.ErrDataNotFound)})
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		request.JSON(c, http.StatusGatewayTimeout, CommonRes{Status: rescode.ErrTimeout, Msg: rescode.GetCodeMsg(rescode.ErrTimeout)})
	default:
		request.JSON(c, http.StatusInternalServerError, CommonRes{Status: rescode.ErrInternalError, Msg: rescode.GetCodeMsg(rescode.ErrInternalError)})
	}
}

func (h *LoanHandler) logInternalError(operation string) {
	if h.log != nil {
		// 驱动错误可能包含凭据或查询值，因此日志只记录操作名称。
		h.log.WithField("operation", operation).Error("Request processing failed")
	}
}
