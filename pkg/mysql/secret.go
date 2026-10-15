package mysql

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"

	"github.com/kalaGN/gin-finapi/app/services"
)

type secretCodec struct {
	aead  cipher.AEAD
	keyID string
}

func newSecretCodec(key, keyID string) (*secretCodec, error) {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(raw) != 32 || keyID == "" {
		return nil, errors.New("invalid MySQL secret encryption configuration")
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, errors.New("cannot initialize MySQL secret encryption")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("cannot initialize MySQL secret encryption")
	}
	return &secretCodec{aead: aead, keyID: keyID}, nil
}

func (c *secretCodec) encrypt(digest, secret string) ([]byte, error) {
	if !services.ValidDigest(digest) || len(secret) == 0 || len(secret) > 256 {
		return nil, services.ErrInvalidRecord
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, errors.New("cannot generate MySQL secret nonce")
	}
	return c.aead.Seal(nonce, nonce, []byte(secret), []byte(c.keyID+":"+digest)), nil
}

func (c *secretCodec) decrypt(digest, keyID string, data []byte) (string, error) {
	n := c.aead.NonceSize()
	if keyID != c.keyID || !services.ValidDigest(digest) || len(data) < n+c.aead.Overhead() {
		return "", services.ErrInvalidRecord
	}
	plain, err := c.aead.Open(nil, data[:n], data[n:], []byte(keyID+":"+digest))
	if err != nil || len(plain) == 0 || len(plain) > 256 {
		return "", services.ErrInvalidRecord
	}
	return string(plain), nil
}
