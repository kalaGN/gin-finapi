package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/kalaGN/gin-finapi/bootstrap"
	"github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/kalaGN/gin-finapi/pkg/logger"
	"github.com/kalaGN/gin-finapi/pkg/mongo"
)

func main() {
	// 加载环境变量
	_ = godotenv.Load()

	// 初始化日志系统
	logger.Init()
	logger.Log.Info("Starting application...")

	// 加载配置
	if err := config.Load(); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if err := config.Config.Validate(); err != nil {
		log.Fatalf("Invalid config: %v", err)
	}
	logger.Log.Info("Configuration loaded")

	// 设置 Gin 模式
	if config.Config.Server.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	// 初始化 Gin 应用
	router, closeRedis, err := bootstrap.NewRouter(*config.Config, logger.Log)
	if err != nil {
		log.Fatal(err)
	}

	// 获取服务端口
	port, _ := config.LoadPort()

	// 创建 HTTP 服务器
	srv := &http.Server{
		Addr:           ":" + port,
		Handler:        router,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB
	}

	// 启动服务器（在 goroutine 中）
	go func() {
		logger.Log.Infof("Server starting on port %s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// 等待中断信号以优雅关闭服务器
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Log.Info("Shutting down gracefully...")

	// 设置关闭超时时间
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := shutdownResources(ctx, srv.Shutdown, mongo.Close, closeRedis); err != nil {
		logger.Log.Errorf("Graceful shutdown completed with errors: %v", err)
	}

	logger.Log.Info("Application stopped")
}

func shutdownResources(
	ctx context.Context,
	shutdownHTTP func(context.Context) error,
	closeMongo func(context.Context) error,
	closeRedis ...func() error,
) error {
	httpErr := shutdownHTTP(ctx)
	mongoErr := closeMongo(ctx)

	var errs []error
	if httpErr != nil {
		errs = append(errs, fmt.Errorf("shutdown HTTP server: %w", httpErr))
	}
	if mongoErr != nil {
		errs = append(errs, fmt.Errorf("close MongoDB: %w", mongoErr))
	}
	for _, close := range closeRedis {
		if err := close(); err != nil {
			errs = append(errs, fmt.Errorf("close Redis: %w", err))
		}
	}
	return errors.Join(errs...)
}
