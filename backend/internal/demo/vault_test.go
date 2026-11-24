package demo

import (
	"testing"

	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
)

// testVault returns a deterministic vault for tests only.
//
// The production path refuses to boot without VAULT_MASTER_KEY, so tests must build the
// vault explicitly instead of leaning on an environment default. A fixed all-zero key is
// fine here: nothing in these tests leaves the process.
func testVault(t *testing.T) crypto.Vault {
	t.Helper()

	vault, err := crypto.NewAESGCMVault(make([]byte, crypto.AES256KeySize))
	if err != nil {
		t.Fatalf("failed to build test vault: %v", err)
	}
	return vault
}
