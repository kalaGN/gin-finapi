package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kalaGN/gin-finapi/bootstrap"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	err := bootstrap.Run(ctx)
	stop()
	if err != nil {
		// 底层错误可能包含连接串或凭据，只报告失败摘要。
		log.Print("应用运行或资源关闭失败")
		os.Exit(1)
	}
}
