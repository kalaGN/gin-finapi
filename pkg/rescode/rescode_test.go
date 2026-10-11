package rescode

import (
	"testing"
)

func TestGetCodeMsg(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		{SuccessCode, "success"},
		{ErrInvalidParam, "参数错误"},
		{ErrInvalidPhone, "手机号格式错误"},
		{ErrInvalidPcode, "pcode参数错误，必须在10001-99999之间"},
		{ErrInvalidJSON, "JSON格式错误"},
		{ErrInvalidApikey, "apikey无效"},
		{ErrInvalidSign, "签名验证失败"},
		{ErrInvalidTimestamp, "时间戳无效或已过期"},
		{ErrDataNotFound, "数据不存在"},
		{ErrProductException, "产品异常"},
		{ErrDatabaseQuery, "数据库查询失败"},
		{ErrSystemError, "系统错误"},
		{99999, "未知错误码: 99999"},
	}

	for _, tt := range tests {
		got := GetCodeMsg(tt.code)
		if got != tt.expected {
			t.Errorf("GetCodeMsg(%d) = %s, want %s", tt.code, got, tt.expected)
		}
	}
}

func TestIsParamError(t *testing.T) {
	tests := []struct {
		code     int
		expected bool
	}{
		{ErrInvalidParam, true},
		{ErrMissingParam, true},
		{ErrInvalidPhone, true},
		{ErrInvalidApikey, false},
		{ErrDataNotFound, false},
	}

	for _, tt := range tests {
		got := IsParamError(tt.code)
		if got != tt.expected {
			t.Errorf("IsParamError(%d) = %v, want %v", tt.code, got, tt.expected)
		}
	}
}

func TestIsAuthError(t *testing.T) {
	if !IsAuthError(ErrInvalidApikey) {
		t.Error("ErrInvalidApikey should be auth error")
	}
	if IsAuthError(ErrInvalidParam) {
		t.Error("ErrInvalidParam should not be auth error")
	}
}
