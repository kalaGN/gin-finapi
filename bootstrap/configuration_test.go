package bootstrap

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestFallbackReporterSafeStructuredEvents(t *testing.T) {
	for _, reason := range []string{"missing", "invalid_record", "unavailable"} {
		t.Run(reason, func(t *testing.T) {
			var output bytes.Buffer
			log := logrus.New()
			log.SetOutput(&output)
			log.SetFormatter(&logrus.JSONFormatter{})
			log.SetLevel(logrus.DebugLevel)
			fallbackReporter(log, "credential")(reason)
			var event map[string]any
			if err := json.Unmarshal(output.Bytes(), &event); err != nil {
				t.Fatal(err)
			}
			level := "warning"
			if reason == "missing" {
				level = "debug"
			}
			if event["event"] != "config_source_fallback" || event["reason"] != reason || event["data_type"] != "credential" || event["level"] != level || len(event) != 6 {
				t.Fatal("回源日志缺失固定分类或存在额外字段")
			}
		})
	}
	fallbackReporter(nil, "credential")("unavailable")
}
