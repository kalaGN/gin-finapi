// Package logger 日志工具包
package logger

import (
	"os"

	"github.com/sirupsen/logrus"
)

var Log *logrus.Logger

// Init 初始化日志系统
func Init() {
	Log = logrus.New()

	// 设置输出
	Log.SetOutput(os.Stdout)

	// 设置日志级别
	Log.SetLevel(logrus.InfoLevel)

	// 设置日志格式
	Log.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02 15:04:05",
	})

	// 开发环境使用文本格式
	if os.Getenv("ENV") == "development" {
		Log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02 15:04:05",
		})
		Log.SetLevel(logrus.DebugLevel)
	}
}
