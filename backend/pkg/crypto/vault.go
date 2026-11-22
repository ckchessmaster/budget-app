package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
)

const (
	AES256KeySize = 32
	GCMNonceSize  = 12
)

var (
	ErrInvalidKeySize   = errors.New("vault key must be exactly 32 bytes for AES-256-GCM")
	ErrInvalidCipher    = errors.New("invalid or corrupted ciphertext")
	ErrDecryptionFailed = errors.New("ciphertext authentication failed")
)

type Vault interface {
	Encrypt(plainText string) (string, error)
	Decrypt(cipherText string) (string, error)
}

type aesGCMVault struct {
	aead cipher.AEAD
}

// NewAESGCMVault constructs an AES-256-GCM Vault from a 32-byte key.
func NewAESGCMVault(key []byte) (Vault, error) {
	if len(key) != AES256KeySize {
		return nil, ErrInvalidKeySize
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}
	return &aesGCMVault{aead: aead}, nil
}

// NewAESGCMVaultFromBase64 decodes a base64-encoded 32-byte key and constructs a Vault.
func NewAESGCMVaultFromBase64(b64Key string) (Vault, error) {
	key, err := base64.StdEncoding.DecodeString(b64Key)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 vault key: %w", err)
	}
	return NewAESGCMVault(key)
}

func (v *aesGCMVault) Encrypt(plainText string) (string, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	sealed := v.aead.Seal(nonce, nonce, []byte(plainText), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (v *aesGCMVault) Decrypt(cipherText string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(cipherText)
	if err != nil {
		return "", ErrInvalidCipher
	}
	nonceSize := v.aead.NonceSize()
	if len(raw) < nonceSize {
		return "", ErrInvalidCipher
	}

	nonce, payload := raw[:nonceSize], raw[nonceSize:]
	plain, err := v.aead.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", ErrDecryptionFailed
	}
	return string(plain), nil
}

// LoadVaultFromEnv builds a Vault from VAULT_MASTER_KEY or a deterministic dev key.
func LoadVaultFromEnv() Vault {
	if b64 := os.Getenv("VAULT_MASTER_KEY"); b64 != "" {
		v, err := NewAESGCMVaultFromBase64(b64)
		if err == nil {
			return v
		}
		log.Printf("warning: invalid VAULT_MASTER_KEY (%v), falling back to derived key", err)
	}
	sum := sha256.Sum256([]byte("budget-app-dev-vault-key-change-in-production"))
	v, _ := NewAESGCMVault(sum[:])
	return v
}
