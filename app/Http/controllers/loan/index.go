package loan

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/pkg/loanrequest"
	"github.com/kalaGN/gin-finapi/pkg/mongo"
	"github.com/kalaGN/gin-finapi/pkg/rescode"
	"github.com/sirupsen/logrus"
)

type CommonRes = loanrequest.Response
type QueryFunc func(context.Context, string) (map[string]int, error)
type SIDFunc func(string, int) (string, error)

var errPhoneConversionUnavailable = errors.New("SHA-256 phone conversion service is not connected")

func phoneLookupKey(phone string) (string, error) {
	if len(phone) == 32 || len(phone) == 64 {
		if _, err := hex.DecodeString(phone); err == nil {
			if len(phone) == 32 {
				return phone[:20], nil
			}
			// TODO: Call the future phone conversion service to map SHA-256(phone)
			// to MD5(phone), then use the first 20 characters of that MD5.
			// Do not hash the SHA-256 string: that would produce a different lookup key.
			return "", errPhoneConversionUnavailable
		}
	}
	// Preserve existing shortened identifiers and legacy nonempty lookup values.
	return phone, nil
}

// LoanHandler handles authenticated requests with explicitly supplied dependencies.
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
	body, ok := loanrequest.Authenticated(c)
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
	// Authentication uses the full request phone; conversion only affects the query.
	result, err := h.query(c.Request.Context(), queryKey)
	if err != nil {
		h.writeQueryError(c, err)
		return
	}
	c.JSON(http.StatusOK, CommonRes{Status: rescode.SuccessCode, Msg: rescode.GetCodeMsg(rescode.SuccessCode), Sid: sid, Data: result})
}

func (h *LoanHandler) writeQueryError(c *gin.Context, err error) {
	h.logInternalError("query loan data")
	switch {
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
		// Driver errors can contain credentials or query values; log only the operation.
		h.log.WithField("operation", operation).Error("Request processing failed")
	}
}
