package crypto

import (
	"errors"
	"testing"
)

// The vault key used to fall back to a key derived from a literal in this file, so a
// deployment with no secret looked healthy while sealing bank credentials with a key any
// reader of the repository could reproduce. There is no fallback any more, and these
// assert that a missing or malformed key is a hard error.
func TestLoadVaultFromEnvRequiresAKey(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		t.Setenv("VAULT_MASTER_KEY", "")

		if _, err := LoadVaultFromEnv(); !errors.Is(err, ErrMissingVaultKey) {
			t.Fatalf("err = %v, want ErrMissingVaultKey", err)
		}
	})

	t.Run("wrong length", func(t *testing.T) {
		t.Setenv("VAULT_MASTER_KEY", "c2hvcnQ=") // "short"

		if _, err := LoadVaultFromEnv(); err == nil {
			t.Fatal("expected a 5-byte key to be rejected")
		}
	})

	t.Run("not base64", func(t *testing.T) {
		t.Setenv("VAULT_MASTER_KEY", "not base64 at all!!")

		if _, err := LoadVaultFromEnv(); err == nil {
			t.Fatal("expected malformed base64 to be rejected")
		}
	})

	t.Run("valid key", func(t *testing.T) {
		t.Setenv("VAULT_MASTER_KEY", "YnVkZ2V0LWFwcC1kZXZlbG9wbWVudC12YXVsdC1rZXk=")

		vault, err := LoadVaultFromEnv()
		if err != nil {
			t.Fatalf("expected a 32-byte key to load, got %v", err)
		}

		sealed, err := vault.Encrypt("postgres://user:pass@host/db")
		if err != nil {
			t.Fatalf("encrypt failed: %v", err)
		}
		opened, err := vault.Decrypt(sealed)
		if err != nil {
			t.Fatalf("decrypt failed: %v", err)
		}
		if opened != "postgres://user:pass@host/db" {
			t.Fatalf("round trip = %q", opened)
		}
	})
}
