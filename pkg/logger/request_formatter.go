package logger

import (
	"encoding/json"

	"github.com/sirupsen/logrus"
)

// RequestFormatter 只影响请求日志：保留完整载荷，移除顶层 msg，时间使用毫秒整数。
type RequestFormatter struct{}

func (RequestFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	fields := make(map[string]interface{}, len(entry.Data)+2)
	for key, value := range entry.Data {
		if key != "msg" {
			fields[key] = value
		}
	}
	fields["time"], fields["level"] = entry.Time.UnixMilli(), entry.Level.String()
	data, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
