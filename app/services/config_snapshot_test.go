package services

import (
	"errors"
	"strings"
	"testing"
)

func TestConfigSnapshotBusinessBoundaries(t *testing.T) {
	digest := Digest("synthetic-key")
	key := ProductKey{Digest: digest, Pcode: 10001}
	fixture := func() ConfigSnapshot {
		return ConfigSnapshot{Credentials: map[string]Credential{digest: {Version: 1, Secret: "synthetic-secret", Status: 1}}, Products: map[ProductKey]ProductAccess{key: {Version: 1, Status: 1}}, Mappings: map[int]map[string]int{10001: {"a": 0, "alias": 0}}}
	}
	for _, tc := range []struct {
		name   string
		change func(*ConfigSnapshot)
	}{
		{"nil凭据", func(s *ConfigSnapshot) { s.Credentials = nil }},
		{"nil授权", func(s *ConfigSnapshot) { s.Products = nil }},
		{"nil映射", func(s *ConfigSnapshot) { s.Mappings = nil }},
		{"错误摘要", func(s *ConfigSnapshot) { s.Credentials["raw-api-key"] = s.Credentials[digest] }},
		{"凭据版本", func(s *ConfigSnapshot) { v := s.Credentials[digest]; v.Version = 2; s.Credentials[digest] = v }},
		{"空secret", func(s *ConfigSnapshot) { v := s.Credentials[digest]; v.Secret = ""; s.Credentials[digest] = v }},
		{"过长secret", func(s *ConfigSnapshot) {
			v := s.Credentials[digest]
			v.Secret = strings.Repeat("s", 257)
			s.Credentials[digest] = v
		}},
		{"负凭据状态", func(s *ConfigSnapshot) { v := s.Credentials[digest]; v.Status = -1; s.Credentials[digest] = v }},
		{"非法凭据状态", func(s *ConfigSnapshot) { v := s.Credentials[digest]; v.Status = 2; s.Credentials[digest] = v }},
		{"负凭据到期", func(s *ConfigSnapshot) { v := s.Credentials[digest]; v.ExpiresAt = -1; s.Credentials[digest] = v }},
		{"孤立授权Key", func(s *ConfigSnapshot) { delete(s.Credentials, digest) }},
		{"孤立授权产品", func(s *ConfigSnapshot) { delete(s.Mappings, 10001) }},
		{"授权版本", func(s *ConfigSnapshot) { v := s.Products[key]; v.Version = 0; s.Products[key] = v }},
		{"负授权状态", func(s *ConfigSnapshot) { v := s.Products[key]; v.Status = -1; s.Products[key] = v }},
		{"非法授权状态", func(s *ConfigSnapshot) { v := s.Products[key]; v.Status = 2; s.Products[key] = v }},
		{"负授权到期", func(s *ConfigSnapshot) { v := s.Products[key]; v.ExpiresAt = -1; s.Products[key] = v }},
		{"产品下界", func(s *ConfigSnapshot) { s.Mappings[10000] = map[string]int{"a": 0} }},
		{"产品上界", func(s *ConfigSnapshot) { s.Mappings[100000] = map[string]int{"a": 0} }},
		{"空映射", func(s *ConfigSnapshot) { s.Mappings[10001] = map[string]int{} }},
		{"空字段", func(s *ConfigSnapshot) { s.Mappings[10001] = map[string]int{" ": 0} }},
		{"负下标", func(s *ConfigSnapshot) { s.Mappings[10001] = map[string]int{"a": -1} }},
		{"溢出下标", func(s *ConfigSnapshot) { s.Mappings[10001] = map[string]int{"a": int(^uint(0) >> 1)} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture()
			tc.change(&s)
			if !errors.Is(s.Validate(), ErrInvalidRecord) {
				t.Fatal("无效配置未失败关闭")
			}
		})
	}
	valid := fixture()
	for _, status := range []int{0, 1} {
		valid.Credentials[digest] = Credential{Version: 1, Secret: strings.Repeat("s", 256), Status: status, ExpiresAt: 1}
		valid.Products[key] = ProductAccess{Version: 1, Status: status, ExpiresAt: 1}
		valid.Mappings[99999] = map[string]int{"last": 0}
		if err := valid.Validate(); err != nil {
			t.Fatal("禁用或到期记录仍必须可同步")
		}
	}
	if err := (ConfigSnapshot{Credentials: map[string]Credential{}, Products: map[ProductKey]ProductAccess{}, Mappings: map[int]map[string]int{}}).Validate(); err != nil {
		t.Fatal("完整空快照不应被当成损坏")
	}
}

func TestValidDigestStrictFormat(t *testing.T) {
	valid := Digest("synthetic-key")
	for _, value := range []string{"", valid[:63], valid + "0", strings.ToUpper(valid), strings.Repeat("z", 64)} {
		if ValidDigest(value) {
			t.Fatal("接受了不规范摘要")
		}
	}
	if !ValidDigest(valid) {
		t.Fatal("拒绝合法摘要")
	}
}
