package utils

import (
	"strings"
	"testing"
	"time"
)

func TestVerifyTimestamp(t *testing.T) {
	// 测试当前时间戳
	now := time.Now().UnixMilli()
	if !VerifyTimestamp(now) {
		t.Error("Current timestamp should be valid")
	}

	// 测试 1 分钟前的时间戳
	oneMinuteAgo := now - 60*1000
	if !VerifyTimestamp(oneMinuteAgo) {
		t.Error("1 minute ago timestamp should be valid")
	}

	// 测试 6 分钟前的时间戳（应该失败）
	sixMinutesAgo := now - 6*60*1000
	if VerifyTimestamp(sixMinutesAgo) {
		t.Error("6 minutes ago timestamp should be invalid")
	}

	// 测试未来的时间戳（应该失败）
	future := now + 60*1000
	if VerifyTimestamp(future) {
		t.Error("Future timestamp should be invalid")
	}
}

func TestGenerateSign(t *testing.T) {
	params := map[string]interface{}{
		"phone":     "r707qyr0k2xmucjp7lz0",
		"pcode":     50000,
		"apikey":    "test_api_key",
		"timestamp": int64(1732723200000),
	}

	secretKey := "test_secret"
	sign := GenerateSign(params, secretKey)

	if sign == "" {
		t.Error("Generated sign should not be empty")
	}

	// 验证签名长度（MD5 是 32 个字符）
	if len(sign) != 32 {
		t.Errorf("Sign length should be 32, got %d", len(sign))
	}

	// 测试相同参数生成相同签名
	sign2 := GenerateSign(params, secretKey)
	if sign != sign2 {
		t.Error("Same params should generate same sign")
	}

	// 测试不同参数生成不同签名
	params["phone"] = "different_phone"
	sign3 := GenerateSign(params, secretKey)
	if sign == sign3 {
		t.Error("Different params should generate different sign")
	}
}

func TestVerifySign(t *testing.T) {
	params := map[string]interface{}{
		"phone":     "r707qyr0k2xmucjp7lz0",
		"pcode":     50000,
		"apikey":    "test_api_key",
		"timestamp": int64(1732723200000),
	}

	secretKey := "test_secret"

	// 生成签名
	sign := GenerateSign(params, secretKey)

	// 验证正确的签名
	if !VerifySign(params, sign, secretKey) {
		t.Error("Valid sign should pass verification")
	}

	// 验证错误的签名
	if VerifySign(params, "INVALID_SIGN", secretKey) {
		t.Error("Invalid sign should fail verification")
	}

	// 验证错误的密钥
	if VerifySign(params, sign, "wrong_secret") {
		t.Error("Wrong secret key should fail verification")
	}

	// 保持现有大小写兼容契约。
	if !VerifySign(params, strings.ToLower(sign), secretKey) {
		t.Error("Lowercase valid sign should pass verification")
	}
}

// 固定向量由独立 MD5 实现计算，覆盖客户端原文顺序、前导零和大写输出。
func TestGenerateSignMatchesNonceClientVector(t *testing.T) {
	params := map[string]interface{}{"timestamp": int64(1791688734850), "nonce": "003093", "apikey": "test-key"}
	const want = "EF495B2BE79CC09C7FCFE59B726C6C53"
	if got := GenerateSign(params, "test-secret"); got != want {
		t.Fatalf("sign=%s want=%s", got, want)
	}
	if !VerifySign(params, want, "test-secret") {
		t.Fatal("client vector rejected")
	}
	params["nonce"] = "3093"
	if VerifySign(params, want, "test-secret") {
		t.Fatal("nonce leading zeros ignored")
	}
}
