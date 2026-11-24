package temporal

import "testing"

// budget-app must never run in Temporal's shared "default" namespace, and the code path
// that reads the environment has to hold that line even when the deployment forgets to
// set it, because a typo there silently co-locates banking workflow history with every
// other unnamed workload on the cluster.
func TestNamespaceFromEnv(t *testing.T) {
	t.Run("unset falls back to the app namespace", func(t *testing.T) {
		t.Setenv("TEMPORAL_NAMESPACE", "")

		if got := NamespaceFromEnv(); got != DefaultNamespace {
			t.Fatalf("NamespaceFromEnv() = %q, want %q", got, DefaultNamespace)
		}
	})

	t.Run("default is never used as a fallback", func(t *testing.T) {
		if DefaultNamespace == "default" {
			t.Fatal("DefaultNamespace must not be Temporal's shared namespace")
		}
	})

	t.Run("explicit namespace wins", func(t *testing.T) {
		t.Setenv("TEMPORAL_NAMESPACE", "budget-app-staging")

		if got := NamespaceFromEnv(); got != "budget-app-staging" {
			t.Fatalf("NamespaceFromEnv() = %q", got)
		}
	})
}

func TestRetentionFromEnv(t *testing.T) {
	t.Run("invalid values fall back to the default", func(t *testing.T) {
		for _, raw := range []string{"", "nonsense", "0s", "-1h"} {
			t.Setenv("TEMPORAL_RETENTION", raw)

			if got := RetentionFromEnv(); got != defaultRetention {
				t.Fatalf("RetentionFromEnv(%q) = %v, want %v", raw, got, defaultRetention)
			}
		}
	})

	t.Run("valid value is honoured", func(t *testing.T) {
		t.Setenv("TEMPORAL_RETENTION", "168h")

		if got := RetentionFromEnv().String(); got != "168h0m0s" {
			t.Fatalf("RetentionFromEnv() = %s", got)
		}
	})
}
