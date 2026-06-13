package crypto

import (
	"strings"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := DeriveKey("hunter2", []byte("0123456789abcdef"))
	for _, pt := range []string{"hello world", "p@ssw0rd!", "", "unicode: café ☕ 日本語"} {
		enc, err := Encrypt(pt, key)
		if err != nil {
			t.Fatalf("encrypt %q: %v", pt, err)
		}
		if !strings.HasPrefix(enc, EncPrefix) {
			t.Errorf("ciphertext %q missing %s prefix", enc, EncPrefix)
		}
		got, err := Decrypt(enc, key)
		if err != nil {
			t.Fatalf("decrypt %q: %v", pt, err)
		}
		if got != pt {
			t.Errorf("round trip = %q, want %q", got, pt)
		}
	}
}

func TestEncryptUsesFreshNonce(t *testing.T) {
	key := DeriveKey("pw", []byte("0123456789abcdef"))
	a, _ := Encrypt("same", key)
	b, _ := Encrypt("same", key)
	if a == b {
		t.Error("two encryptions of the same plaintext produced identical output (nonce reuse)")
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	salt := []byte("0123456789abcdef")
	enc, _ := Encrypt("secret", DeriveKey("right", salt))
	if _, err := Decrypt(enc, DeriveKey("wrong", salt)); err == nil {
		t.Error("expected error decrypting with wrong key, got nil")
	}
}

func TestDecryptMissingPrefixFails(t *testing.T) {
	key := DeriveKey("pw", []byte("0123456789abcdef"))
	if _, err := Decrypt("plaintext-no-prefix", key); err == nil {
		t.Error("expected error for non-ENC: value, got nil")
	}
}

func TestDecryptTamperedFails(t *testing.T) {
	key := DeriveKey("pw", []byte("0123456789abcdef"))
	enc, _ := Encrypt("secret", key)
	tampered := enc[:len(enc)-2] + "AA"
	if _, err := Decrypt(tampered, key); err == nil {
		t.Error("expected error for tampered ciphertext, got nil")
	}
}

func TestDeriveKeyLength(t *testing.T) {
	if got := len(DeriveKey("pw", []byte("0123456789abcdef"))); got != 32 {
		t.Errorf("derived key length = %d, want 32", got)
	}
}
