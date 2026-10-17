package config

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestBuildVersionFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{"无信息", nil, "unknown"},
		{"开发构建", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "unknown"},
		{"模块版本", &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, "v1.2.3"},
		{"修订版本", &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}}}, "git:abc123"},
		{"脏构建", &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}, {Key: "vcs.modified", Value: "true"}}}, "git:abc123+dirty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildVersion(tc.info); got != tc.want {
				t.Fatalf("version=%s", got)
			}
		})
	}
}

func TestLogIdentityDefaultsAndEnvironment(t *testing.T) {
	defaults := ServerConfig{}.WithLogIdentityDefaults()
	if !strings.Contains(defaults.InstanceID, ":") || defaults.Version == "" {
		t.Fatal("缺少默认身份")
	}
	if got := defaults.WithLogIdentityDefaults(); got != defaults {
		t.Fatal("默认身份重复解析不稳定")
	}
	previous := Config
	t.Cleanup(func() { Config = previous })
	t.Setenv("MYSQL_ENABLED", "false")
	t.Setenv("INSTANCE_ID", "synthetic-instance")
	t.Setenv("APP_VERSION", "v-test")
	if err := Load(); err != nil {
		t.Fatal(err)
	}
	if Config.Server.InstanceID != "synthetic-instance" || Config.Server.Version != "v-test" {
		t.Fatal("未加载实例/版本覆盖")
	}
}
