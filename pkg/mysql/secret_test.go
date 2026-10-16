package mysql

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/kalaGN/gin-finapi/app/services"
)

func TestSecretRoundTripAndBinding(t *testing.T) {
	codec, err := newSecretCodec(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), "test-v1")
	if err != nil {
		t.Fatal(err)
	}
	digest := services.Digest("synthetic-key")
	first, err := codec.encrypt(digest, "synthetic-secret")
	if err != nil {
		t.Fatal(err)
	}
	second, err := codec.encrypt(digest, "synthetic-secret")
	if err != nil || bytes.Equal(first, second) {
		t.Fatal("nonce 未随机生成")
	}
	plain, err := codec.decrypt(digest, "test-v1", first)
	if err != nil || plain != "synthetic-secret" {
		t.Fatal("密钥往返失败")
	}
	for _, tc := range []struct {
		digest, keyID string
		data          []byte
	}{
		{services.Digest("other-key"), "test-v1", first}, {digest, "other-v1", first}, {digest, "test-v1", []byte{1}},
	} {
		if _, err := codec.decrypt(tc.digest, tc.keyID, tc.data); !errors.Is(err, services.ErrInvalidRecord) {
			t.Fatal("无效或错绑密文未拒绝")
		}
	}
	first[len(first)-1] ^= 1
	if _, err := codec.decrypt(digest, "test-v1", first); !errors.Is(err, services.ErrInvalidRecord) {
		t.Fatal("篡改密文未拒绝")
	}
}

func TestSecretConfigurationAndInputBoundaries(t *testing.T) {
	for _, tc := range []struct{ key, id string }{{"sensitive-invalid-key", "v1"}, {base64.StdEncoding.EncodeToString(make([]byte, 31)), "v1"}, {base64.StdEncoding.EncodeToString(make([]byte, 32)), ""}} {
		if _, err := newSecretCodec(tc.key, tc.id); err == nil || strings.Contains(err.Error(), "sensitive-invalid-key") {
			t.Fatal("无效加密配置未安全拒绝")
		}
	}
	codec, err := newSecretCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)), "test-v1")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ digest, secret string }{{"raw-key", "secret"}, {services.Digest("key"), ""}, {services.Digest("key"), strings.Repeat("s", 257)}} {
		if _, err := codec.encrypt(tc.digest, tc.secret); !errors.Is(err, services.ErrInvalidRecord) {
			t.Fatal("无效secret或摘要不应加密存储")
		}
	}
}
