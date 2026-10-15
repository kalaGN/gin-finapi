package services

import (
	"encoding/hex"
	"strings"
)

// ConfigSnapshot 是一次一致性读取的完整配置，不得输出到日志。
type ConfigSnapshot struct {
	Credentials map[string]Credential
	Products    map[ProductKey]ProductAccess
	Mappings    map[int]map[string]int
}

func ValidDigest(digest string) bool {
	if len(digest) != 64 || digest != strings.ToLower(digest) {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

// Validate 只检查格式和引用关系，禁用及到期记录仍须发布用于拒绝访问。
func (s ConfigSnapshot) Validate() error {
	if s.Credentials == nil || s.Products == nil || s.Mappings == nil {
		return ErrInvalidRecord
	}
	for digest, c := range s.Credentials {
		if !ValidDigest(digest) || c.Version != 1 || len(c.Secret) == 0 || len(c.Secret) > 256 || c.Status < 0 || c.Status > 1 || c.ExpiresAt < 0 {
			return ErrInvalidRecord
		}
	}
	for pcode, mapping := range s.Mappings {
		if pcode < 10001 || pcode > 99999 || len(mapping) == 0 {
			return ErrInvalidRecord
		}
		for name, index := range mapping {
			if strings.TrimSpace(name) == "" || index < 0 || index == int(^uint(0)>>1) {
				return ErrInvalidRecord
			}
		}
	}
	for key, access := range s.Products {
		if _, ok := s.Credentials[key.Digest]; !ok {
			return ErrInvalidRecord
		}
		if _, ok := s.Mappings[key.Pcode]; !ok {
			return ErrInvalidRecord
		}
		if access.Version != 1 || access.Status < 0 || access.Status > 1 || access.ExpiresAt < 0 {
			return ErrInvalidRecord
		}
	}
	return nil
}
