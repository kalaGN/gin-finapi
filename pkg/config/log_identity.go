package config

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
)

// WithLogIdentityDefaults 只在启动/构造时解析实例与构建身份，不执行Git或读取额外配置。
func (c ServerConfig) WithLogIdentityDefaults() ServerConfig {
	c.InstanceID = strings.TrimSpace(c.InstanceID)
	c.Version = strings.TrimSpace(c.Version)
	if c.InstanceID == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "unknown"
		}
		c.InstanceID = fmt.Sprintf("%s:%d", host, os.Getpid())
	}
	if c.Version == "" {
		info, _ := debug.ReadBuildInfo()
		c.Version = buildVersion(info)
	}
	return c
}

func buildVersion(info *debug.BuildInfo) string {
	if info == nil {
		return "unknown"
	}
	if version := info.Main.Version; version != "" && version != "(devel)" {
		return version
	}
	var revision string
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return "unknown"
	}
	version := "git:" + revision
	if modified {
		version += "+dirty"
	}
	return version
}
