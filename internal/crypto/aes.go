// Package crypto provides AES-GCM encryption of expansion values and access to
// the Windows Credential Manager for the master password.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

const (
	// EncPrefix marks a config value as AES-GCM encrypted + base64.
	EncPrefix = "ENC:"
	// SaltLen is the PBKDF2 salt length in bytes.
	SaltLen = 16
	// pbkdf2Iter is the PBKDF2 iteration count (compile-time constant).
	pbkdf2Iter = 100_000
	// keyLen is the derived AES-256 key length in bytes.
	keyLen = 32
	// nonceLen is the AES-GCM nonce length in bytes.
	nonceLen = 12
)

// DeriveKey derives a 32-byte AES key from the master password and salt using
// PBKDF2-SHA256 with 100,000 iterations.
func DeriveKey(password string, salt []byte) []byte {
	return pbkdf2.Key([]byte(password), salt, pbkdf2Iter, keyLen, sha256.New)
}

// Encrypt encrypts plaintext with AES-GCM under key and returns
// "ENC:<base64(nonce|ciphertext)>". A fresh random nonce is used each call.
func Encrypt(plaintext string, key []byte) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ct := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	blob := append(nonce, ct...)
	return EncPrefix + base64.StdEncoding.EncodeToString(blob), nil
}

// Decrypt reverses Encrypt. The input must carry the "ENC:" prefix.
func Decrypt(ciphertext string, key []byte) (string, error) {
	if !strings.HasPrefix(ciphertext, EncPrefix) {
		return "", fmt.Errorf("value is not %s-prefixed", EncPrefix)
	}
	blob, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, EncPrefix))
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	if len(blob) < nonceLen {
		return "", fmt.Errorf("ciphertext too short")
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce, ct := blob[:nonceLen], blob[nonceLen:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt (wrong password or corrupt data): %w", err)
	}
	return string(pt), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return gcm, nil
}
