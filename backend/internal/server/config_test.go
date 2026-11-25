package server_test

import (
	"strings"
	"testing"

	"github.com/ckchessmaster/budget-app/backend/internal/server"
)

// setOIDCEnv clears every variable LoadConfig reads for the token/CORS settings so
// a value inherited from the caller's shell cannot decide the outcome.
func setOIDCEnv(t *testing.T, env map[string]string) {
	t.Helper()

	for _, key := range []string{
		"OIDC_ISSUER_URL", "DEX_ISSUER_URL", "OIDC_CLIENT_ID",
		"OIDC_ENFORCE_AUDIENCE", "CORS_ALLOWED_ORIGINS",
		"VAPID_PUBLIC_KEY", "VAPID_PRIVATE_KEY", "VAPID_SUBJECT",
	} {
		t.Setenv(key, "")
	}
	for key, value := range env {
		t.Setenv(key, value)
	}
}

func TestLoadConfigOIDCAndCORSDefaults(t *testing.T) {
	setOIDCEnv(t, nil)

	cfg := server.LoadConfig()

	if cfg.OIDCIssuerURL == "" {
		t.Fatal("expected an issuer default so local development still starts")
	}
	if !strings.Contains(cfg.OIDCIssuerURL, "://") {
		t.Fatalf("OIDCIssuerURL = %q, want a URL", cfg.OIDCIssuerURL)
	}
	if cfg.OIDCClientID != "" {
		t.Fatalf("OIDCClientID = %q, want empty when unset", cfg.OIDCClientID)
	}
	if !cfg.OIDCEnforceAudience {
		t.Fatal("audience enforcement must default to on")
	}
	if len(cfg.CORSAllowedOrigins) != 0 {
		t.Fatalf("CORSAllowedOrigins = %#v, want no origins unless configured", cfg.CORSAllowedOrigins)
	}
}

func TestLoadConfigReadsSecuritySettings(t *testing.T) {
	tests := map[string]struct {
		env             map[string]string
		wantIssuer      string
		wantClientID    string
		wantEnforce     bool
		wantCORSOrigins []string
	}{
		"authentik style": {
			env: map[string]string{
				"OIDC_ISSUER_URL":      "https://authentik.example.com/application/o/budget-app/",
				"OIDC_CLIENT_ID":       " AZpJFqIzjrnW1GAOQyNopgtBeyyGnx7gu5aiMEtD ",
				"CORS_ALLOWED_ORIGINS": "https://budget.example.com",
			},
			wantIssuer:      "https://authentik.example.com/application/o/budget-app/",
			wantClientID:    "AZpJFqIzjrnW1GAOQyNopgtBeyyGnx7gu5aiMEtD",
			wantEnforce:     true,
			wantCORSOrigins: []string{"https://budget.example.com"},
		},
		"legacy dex variable still honoured": {
			env:         map[string]string{"DEX_ISSUER_URL": "http://localhost:5556/dex"},
			wantIssuer:  "http://localhost:5556/dex",
			wantEnforce: true,
		},
		"oidc wins over legacy dex": {
			env: map[string]string{
				"OIDC_ISSUER_URL": "https://new.example.com/",
				"DEX_ISSUER_URL":  "http://old.example.com/dex",
			},
			wantIssuer:  "https://new.example.com/",
			wantEnforce: true,
		},
		"audience enforcement can be disabled without a rebuild": {
			env:         map[string]string{"OIDC_ENFORCE_AUDIENCE": "false", "OIDC_CLIENT_ID": "client"},
			wantEnforce: false,
		},
		"invalid enforcement value keeps the secure default": {
			env:         map[string]string{"OIDC_ENFORCE_AUDIENCE": "maybe"},
			wantEnforce: true,
		},
		"multiple cors origins": {
			env:             map[string]string{"CORS_ALLOWED_ORIGINS": "https://a.example.com, http://localhost:5173"},
			wantEnforce:     true,
			wantCORSOrigins: []string{"https://a.example.com", "http://localhost:5173"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			setOIDCEnv(t, tc.env)
			cfg := server.LoadConfig()

			if tc.wantIssuer != "" && cfg.OIDCIssuerURL != tc.wantIssuer {
				t.Fatalf("OIDCIssuerURL = %q, want %q", cfg.OIDCIssuerURL, tc.wantIssuer)
			}
			if tc.wantClientID != "" && cfg.OIDCClientID != tc.wantClientID {
				t.Fatalf("OIDCClientID = %q, want %q", cfg.OIDCClientID, tc.wantClientID)
			}
			if cfg.OIDCEnforceAudience != tc.wantEnforce {
				t.Fatalf("OIDCEnforceAudience = %v, want %v", cfg.OIDCEnforceAudience, tc.wantEnforce)
			}
			if len(cfg.CORSAllowedOrigins) != len(tc.wantCORSOrigins) {
				t.Fatalf("CORSAllowedOrigins = %#v, want %#v", cfg.CORSAllowedOrigins, tc.wantCORSOrigins)
			}
			for i, want := range tc.wantCORSOrigins {
				if cfg.CORSAllowedOrigins[i] != want {
					t.Fatalf("CORSAllowedOrigins[%d] = %q, want %q", i, cfg.CORSAllowedOrigins[i], want)
				}
			}
		})
	}
}

func TestLoadConfigVAPIDOptIn(t *testing.T) {
	t.Run("disabled when unset", func(t *testing.T) {
		setOIDCEnv(t, nil)
		cfg := server.LoadConfig()
		if cfg.VAPID.Enabled() {
			t.Fatal("expected VAPID to default to disabled when env vars are unset")
		}
	})

	t.Run("enabled when all three variables are valid", func(t *testing.T) {
		setOIDCEnv(t, map[string]string{
			"VAPID_PUBLIC_KEY":  "test-pub-key",
			"VAPID_PRIVATE_KEY": "test-priv-key",
			"VAPID_SUBJECT":     "mailto:ops@example.com",
		})
		cfg := server.LoadConfig()
		if !cfg.VAPID.Enabled() {
			t.Fatal("expected VAPID to be enabled when all variables are set")
		}
		if cfg.VAPID.PublicKey != "test-pub-key" || cfg.VAPID.PrivateKey != "test-priv-key" || cfg.VAPID.Subject != "mailto:ops@example.com" {
			t.Fatalf("unexpected VAPID config: %+v", cfg.VAPID)
		}
	})

	t.Run("disabled when partial", func(t *testing.T) {
		setOIDCEnv(t, map[string]string{
			"VAPID_PUBLIC_KEY": "only-pub-key",
		})
		cfg := server.LoadConfig()
		if cfg.VAPID.Enabled() {
			t.Fatal("expected partial VAPID config to resolve as disabled")
		}
	})
}
