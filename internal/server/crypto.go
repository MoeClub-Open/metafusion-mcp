package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
)

func randomToken() string {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func hash(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func challenge(s string) string {
	v := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(v[:])
}
func encrypt(key []byte, id, secret string) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return a.Seal(nonce, nonce, []byte(secret), []byte(id)), nil
}
func decrypt(key []byte, id string, data []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	a, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < a.NonceSize() {
		return "", errors.New("invalid ciphertext")
	}
	plain, err := a.Open(nil, data[:a.NonceSize()], data[a.NonceSize():], []byte(id))
	return string(plain), err
}
