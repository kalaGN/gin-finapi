package utils

import (
	"crypto/rand"
	"fmt"
	"io"
	"strings"
)

// charset 用于生成随机字符串的字符集。
const charset = "abcdefghijklmnopqrstuvwxyz0123456789"

// GenerateSID 生成会话 ID
// prefix: ID 前缀
// length: 随机部分的长度
func GenerateSID(prefix string, length int) (string, error) {
	return generateSID(prefix, length, rand.Reader)
}

func generateSID(prefix string, length int, reader io.Reader) (string, error) {
	if length < 0 {
		return "", fmt.Errorf("SID length must not be negative")
	}

	var sb strings.Builder
	sb.Grow(len(prefix) + length)
	sb.WriteString(prefix)

	// 拒绝 252-255，避免 256 对字符集长度取模造成分布偏差。
	limit := byte(256 - 256%len(charset))
	var randomByte [1]byte
	for sb.Len() < len(prefix)+length {
		if _, err := io.ReadFull(reader, randomByte[:]); err != nil {
			return "", fmt.Errorf("generate SID randomness: %w", err)
		}
		if randomByte[0] >= limit {
			continue
		}
		sb.WriteByte(charset[int(randomByte[0])%len(charset)])
	}

	return sb.String(), nil
}
