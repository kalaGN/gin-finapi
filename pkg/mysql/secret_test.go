package mysql

import (
	"bytes"
	"encoding/base64"
	"errors"
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
