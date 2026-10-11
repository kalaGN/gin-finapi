package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const shutdownTimeout = 5 * time.Second

// httpLifecycle 在测试中替换外部服务边界，不启动实际监听端口。
type httpLifecycle struct {
	serve    func() error
	shutdown func(context.Context) error
	close    func() error
}

func runHTTP(ctx context.Context, server httpLifecycle, closeDependencies func(context.Context) error) error {
	result := make(chan error, 1)
	go func() { result <- server.serve() }()
	var serveErr error
	finished := false
	select {
	case serveErr = <-result:
		finished = true
	case <-ctx.Done():
	}
	cleanupErr := shutdownResources(server.shutdown, server.close, closeDependencies)
	if !finished {
		// 正常 HTTP Shutdown/Close 会终止 Serve；异常实现也不能无限阻塞退出。
		timer := time.NewTimer(shutdownTimeout)
		defer timer.Stop()
		select {
		case serveErr = <-result:
		case <-timer.C:
			serveErr = errors.New("HTTP 服务退出等待超时")
		}
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(serveErr, cleanupErr)
}

func shutdownResources(shutdownHTTP func(context.Context) error, forceClose func() error, closeDependencies func(context.Context) error) error {
	httpCtx, cancelHTTP := context.WithTimeout(context.Background(), shutdownTimeout)
	httpErr := shutdownHTTP(httpCtx)
	cancelHTTP()
	var errs []error
	if httpErr != nil {
		errs = append(errs, fmt.Errorf("关闭 HTTP: %w", httpErr))
		if forceClose != nil {
			if err := forceClose(); err != nil {
				errs = append(errs, fmt.Errorf("强制关闭 HTTP: %w", err))
			}
		}
	}
	dependencyCtx, cancelDependencies := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelDependencies()
	if err := closeDependencies(dependencyCtx); err != nil {
		errs = append(errs, fmt.Errorf("关闭依赖: %w", err))
	}
	return errors.Join(errs...)
}
