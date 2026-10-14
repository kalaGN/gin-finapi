package services

import (
	"errors"
	"strings"
	"testing"
)

func TestPhoneLookupKey_PreservesRules(t *testing.T) {
	for _, tc := range []struct {
		name, phone, want string
		err               error
	}{
		{"32位非十六进制", strings.Repeat("r", 32), strings.Repeat("r", 20), nil},
		{"32位小写", "0123456789abcdef0123456789abcdef", "0123456789abcdef0123", nil},
		{"32位大写", "0123456789ABCDEF0123456789ABCDEF", "0123456789ABCDEF0123", nil},
		{"64位小写SHA256", strings.Repeat("b", 64), "", ErrPhoneConversionUnavailable},
		{"64位大写SHA256", strings.Repeat("B", 64), "", ErrPhoneConversionUnavailable},
		{"64位非十六进制", strings.Repeat("r", 64), strings.Repeat("r", 64), nil},
		{"兼容原纯函数空值", "", "", nil},
		{"兼容原纯函数其他值", "short", "short", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PhoneLookupKey(tc.phone)
			if got != tc.want || !errors.Is(err, tc.err) {
				t.Fatalf("结果=%q 错误=%v", got, err)
			}
		})
	}
}
