package logger

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestRequestFormatter_MillisecondsAndNestedMessage(t *testing.T) {
	entry := &logrus.Entry{Time: time.UnixMilli(1791896584522), Level: logrus.InfoLevel, Message: "removed", Data: logrus.Fields{
		"msg": "removed too", "status": 0,
		"input":         json.RawMessage(`{"extra":{"value":1}}`),
		"response_data": json.RawMessage(`{"status":0,"msg":"success","data":{"x":1}}`),
	}}
	data, err := (RequestFormatter{}).Format(entry)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]json.RawMessage
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	if _, ok := row["msg"]; ok {
		t.Fatal("顶层 msg 应删除")
	}
	if string(row["time"]) != "1791896584522" || string(row["response_data"]) != string(entry.Data["response_data"].(json.RawMessage)) {
		t.Fatal("时间或响应内容被修改")
	}
}
