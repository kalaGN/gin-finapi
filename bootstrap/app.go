package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/kalaGN/gin-finapi/pkg/logger"
)

// Run 装配应用并统一处理服务退出、信号和资源清理；底层不直接退出进程。
func Run(ctx context.Context) error {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return errors.New("加载 .env 失败，请检查格式和读取权限")
	}
	logger.Init()
	if err := config.Load(); err != nil {
		return fmt.Errorf("加载配置: %w", err)
	}
	cfg := *config.Config
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("校验配置: %w", err)
	}
	if cfg.Server.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}
	router, closeDependencies, err := NewRouter(cfg, logger.Log)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: ":" + cfg.Server.Port, Handler: router,
		ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, MaxHeaderBytes: 1 << 20,
	}
	logger.Log.Info("应用启动")
	err = runHTTP(ctx, httpLifecycle{serve: server.ListenAndServe, shutdown: server.Shutdown, close: server.Close}, closeDependencies)
	logger.Log.Info("应用已停止")
	return err
}
