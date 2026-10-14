package services

import (
	"encoding/hex"
	"errors"
)

var ErrPhoneConversionUnavailable = errors.New("SHA-256 phone conversion service is not connected")

// PhoneLookupKey 归一化查询标识，不访问存储；公开请求的长度由中间件校验。
func PhoneLookupKey(phone string) (string, error) {
	if len(phone) == 32 {
		return phone[:20], nil
	}
	if len(phone) == 64 {
		if _, err := hex.DecodeString(phone); err == nil {
			// TODO: 接入手机号转换服务，将 SHA-256(phone) 映射为 MD5(phone) 后取前 20 位。
			// 不得对 SHA-256 字符串重新哈希，否则与原手机号的查询标识不一致。
			return "", ErrPhoneConversionUnavailable
		}
	}
	return phone, nil
}
