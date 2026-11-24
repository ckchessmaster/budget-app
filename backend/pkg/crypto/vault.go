package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	AES256KeySize = 32
	GCMNonceSize  = 12
)

var (
	ErrInvalidKeySize   = errors.New("vault key must be exactly 32 bytes for AES-256-GCM")
	ErrMissingVaultKey  = errors.New("VAULT_MASTER_KEY is required: set it to the base64 of exactly 32 random bytes (openssl rand -base64 32)")
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

// LoadVaultFromEnv builds a Vault from VAULT_MASTER_KEY, which must be the base64
// encoding of exactly 32 random bytes.
//
// There is deliberately no fallback key. Deriving one from a literal in this
// repository would seal stored bank credentials with a key anyone who can read the
// source can reproduce, and a missing key would look like a working deployment right
// up to the moment a real SimpleFIN connection has to be decrypted. Callers must
// treat an error here as fatal at boot.
func LoadVaultFromEnv() (Vault, error) {
	b64 := strings.TrimSpace(os.Getenv("VAULT_MASTER_KEY"))
	if b64 == "" {
		return nil, ErrMissingVaultKey
	}

	vault, err := NewAESGCMVaultFromBase64(b64)
	if err != nil {
		return nil, fmt.Errorf("invalid VAULT_MASTER_KEY: %w", err)
	}
	return vault, nil
}
