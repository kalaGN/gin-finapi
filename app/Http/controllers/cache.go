package controllers

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kalaGN/gin-finapi/app/configdata"
	"github.com/kalaGN/gin-finapi/pkg/request"
	"github.com/kalaGN/gin-finapi/pkg/rescode"
	"github.com/sirupsen/logrus"
)

// CacheHandler 只负责管理HTTP响应；清理能力由启动层注入，不访问底层存储。
type CacheHandler struct {
	clear func(context.Context) (configdata.ClearStats, error)
	log   *logrus.Logger
}

func NewCacheHandler(clear func(context.Context) (configdata.ClearStats, error), log *logrus.Logger) (*CacheHandler, error) {
	if clear == nil {
		return nil, errors.New("cache handler requires clear dependency")
	}
	return &CacheHandler{clear: clear, log: log}, nil
}

func (h *CacheHandler) Clear(c *gin.Context) {
	stats, err := h.clear(c.Request.Context())
	if err != nil {
		if h.log != nil {
			h.log.WithFields(logrus.Fields{"event": "config_cache_clear_failed", "sid": request.SID(c)}).Warn("内存缓存清理未完成")
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": rescode.GetCodeMsg(rescode.ErrServiceUnavailable)})
		return
	}
	if h.log != nil {
		h.log.WithFields(logrus.Fields{"event": "config_cache_cleared", "sid": request.SID(c), "credentials": stats.Credentials, "product_access": stats.ProductAccess, "product_maps": stats.ProductMaps}).Info("本进程配置缓存已清理")
	}
	// 不使用业务status字段，避免管理操作被请求日志误记为计费成功。
	c.JSON(http.StatusOK, gin.H{"cleared": stats})
}
