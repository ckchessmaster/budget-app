package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
)

func TestAESGCMVault(t *testing.T) {
	key := make([]byte, AES256KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}

	vault, err := NewAESGCMVault(key)
	if err != nil {
		t.Fatalf("NewAESGCMVault failed: %v", err)
	}

	t.Run("encrypts and decrypts matching plaintext", func(t *testing.T) {
		secret := "https://user:pass@bridge.simplefin.org/simplefin"
		ct, err := vault.Encrypt(secret)
		if err != nil {
			t.Fatalf("Encrypt failed: %v", err)
		}
		if ct == secret {
			t.Fatalf("ciphertext must not equal plaintext")
		}

		pt, err := vault.Decrypt(ct)
		if err != nil {
			t.Fatalf("Decrypt failed: %v", err)
		}
		if pt != secret {
			t.Fatalf("Decrypt = %q, want %q", pt, secret)
		}
	})

	t.Run("rejects tampered ciphertext when 1 bit is flipped", func(t *testing.T) {
		ct, err := vault.Encrypt("sensitive-access-url")
		if err != nil {
			t.Fatalf("Encrypt failed: %v", err)
		}

		raw, err := base64.StdEncoding.DecodeString(ct)
		if err != nil {
			t.Fatalf("DecodeString failed: %v", err)
		}
		raw[len(raw)-1] ^= 0x01 // flip 1 bit in the authentication tag/payload
		tampered := base64.StdEncoding.EncodeToString(raw)

		_, err = vault.Decrypt(tampered)
		if !errors.Is(err, ErrDecryptionFailed) {
			t.Fatalf("expected ErrDecryptionFailed, got %v", err)
		}
	})

	t.Run("rejects invalid key length", func(t *testing.T) {
		if _, err := NewAESGCMVault([]byte("too-short")); !errors.Is(err, ErrInvalidKeySize) {
			t.Fatalf("expected ErrInvalidKeySize, got %v", err)
		}
	})
}
