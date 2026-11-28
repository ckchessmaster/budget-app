package middleware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	defaultDiscoveryTimeout = 10 * time.Second
	defaultDiscoveryBackoff = 15 * time.Second
	// defaultExpiryLeeway tolerates small clock skew between this service and the
	// identity provider. Tokens are still rejected once past exp plus this window.
	defaultExpiryLeeway = 30 * time.Second
)

// ErrVerifierUnavailable means the signing keys for the configured issuer could
// not be loaded. Tokens are rejected while that is true: the API fails closed
// rather than trusting unverified tokens.
var ErrVerifierUnavailable = errors.New("oidc provider unavailable")

// JWTVerifierConfig configures validation of the bearer tokens the API receives.
//
// The frontend sends the OIDC access token when it is a JWT and falls back to the
// ID token. Authentik signs both with the same key and gives both the same
// iss/exp/aud claims, so both token types are validated with the same rules.
type JWTVerifierConfig struct {
	// IssuerURL is the OIDC issuer, resolved through its well-known configuration.
	IssuerURL string
	// ClientID is this application's OIDC client ID, expected in the aud claim.
	ClientID string
	// EnforceAudience requires ClientID to appear in the token's aud claim. It is
	// only applied when ClientID is set.
	EnforceAudience bool
	// HTTPClient is used for discovery and JWKS fetches. Defaults to a client with a
	// request timeout so a stalled identity provider cannot pin requests open.
	HTTPClient *http.Client
	// DiscoveryTimeout bounds a single discovery attempt.
	DiscoveryTimeout time.Duration
	// DiscoveryBackoff is how long to wait before retrying a failed discovery.
	DiscoveryBackoff time.Duration
	// ExpiryLeeway is the clock tolerance granted to the exp claim. Zero uses the
	// default; a negative value disables tolerance and rejects at the exp instant.
	ExpiryLeeway time.Duration
}

// JWTVerifier validates OIDC tokens against the issuer's published signing keys.
type JWTVerifier struct {
	issuerURL     string
	clientID      string
	enforceAud    bool
	baseCtx       context.Context
	discoveryTO   time.Duration
	discoveryBack time.Duration
	expiryLeeway  time.Duration

	mu          sync.Mutex
	verifier    *oidc.IDTokenVerifier
	lastAttempt time.Time
	lastErr     error
}

func NewJWTVerifier(cfg JWTVerifierConfig) *JWTVerifier {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: defaultDiscoveryTimeout}
	}
	if cfg.DiscoveryTimeout <= 0 {
		cfg.DiscoveryTimeout = defaultDiscoveryTimeout
	}
	if cfg.DiscoveryBackoff <= 0 {
		cfg.DiscoveryBackoff = defaultDiscoveryBackoff
	}
	leeway := cfg.ExpiryLeeway
	switch {
	case leeway == 0:
		leeway = defaultExpiryLeeway
	case leeway < 0:
		leeway = 0
	}

	return &JWTVerifier{
		issuerURL:     strings.TrimSpace(cfg.IssuerURL),
		clientID:      strings.TrimSpace(cfg.ClientID),
		enforceAud:    cfg.EnforceAudience && strings.TrimSpace(cfg.ClientID) != "",
		baseCtx:       oidc.ClientContext(context.Background(), cfg.HTTPClient),
		discoveryTO:   cfg.DiscoveryTimeout,
		discoveryBack: cfg.DiscoveryBackoff,
		expiryLeeway:  leeway,
	}
}

// Warm performs discovery eagerly so a misconfigured issuer or unreachable
// identity provider shows up at startup instead of on the first protected
// request. Failures are returned for the caller to log; the verifier keeps
// retrying lazily so a temporary issuer outage degrades to 401s rather than
// crash-looping the API.
func (v *JWTVerifier) Warm(ctx context.Context) error {
	_, err := v.currentVerifier(ctx)
	return err
}

// TokenClaims carries the verified identity attributes extracted from an OIDC JWT.
type TokenClaims struct {
	Subject string
	Email   string
	Name    string
}

// ClaimsVerifier is an optional extension of OIDCVerifier for verifiers that also
// extract email and display name claims alongside the subject.
type ClaimsVerifier interface {
	OIDCVerifier
	VerifyClaims(ctx context.Context, rawToken string) (TokenClaims, error)
}

// Verify checks the token's signature, issuer, expiry, and (when configured)
// audience, then returns the subject claim.
func (v *JWTVerifier) Verify(ctx context.Context, rawToken string) (string, error) {
	claims, err := v.VerifyClaims(ctx, rawToken)
	if err != nil {
		return "", err
	}
	return claims.Subject, nil
}

// VerifyClaims checks the token's signature, issuer, expiry, and (when configured)
// audience, then returns the verified subject, email, and display name claims.
func (v *JWTVerifier) VerifyClaims(ctx context.Context, rawToken string) (TokenClaims, error) {
	verifier, err := v.currentVerifier(ctx)
	if err != nil {
		return TokenClaims{}, err
	}

	token, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		return TokenClaims{}, fmt.Errorf("token rejected: %w", err)
	}
	if token.Subject == "" {
		return TokenClaims{}, errors.New("sub claim missing or empty")
	}

	var parsed struct {
		Email             string `json:"email"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	_ = token.Claims(&parsed)

	displayName := strings.TrimSpace(parsed.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(parsed.PreferredUsername)
	}

	return TokenClaims{
		Subject: token.Subject,
		Email:   strings.TrimSpace(parsed.Email),
		Name:    displayName,
	}, nil
}

// currentVerifier returns the cached verifier, discovering the provider on first
// use. A successful result is kept forever: the JWKS is refreshed on demand
// (including for unknown key IDs), so an issuer outage does not log users out.
func (v *JWTVerifier) currentVerifier(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.verifier != nil {
		return v.verifier, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVerifierUnavailable, err)
	}
	if !v.lastAttempt.IsZero() && time.Since(v.lastAttempt) < v.discoveryBack {
		return nil, fmt.Errorf("%w: last discovery attempt failed: %v", ErrVerifierUnavailable, v.lastErr)
	}
	v.lastAttempt = time.Now()

	if v.issuerURL == "" {
		v.lastErr = errors.New("no OIDC issuer configured")
		return nil, fmt.Errorf("%w: %v", ErrVerifierUnavailable, v.lastErr)
	}

	initCtx, cancel := context.WithTimeout(v.baseCtx, v.discoveryTO)
	defer cancel()

	provider, err := oidc.NewProvider(initCtx, v.issuerURL)
	if err != nil {
		v.lastErr = err
		slog.Warn("oidc discovery failed; rejecting tokens until the issuer is reachable",
			"issuer", v.issuerURL, "err", err)
		return nil, fmt.Errorf("%w: discover %s: %v", ErrVerifierUnavailable, v.issuerURL, err)
	}

	oidcCfg := &oidc.Config{
		SkipClientIDCheck: !v.enforceAud,
		Now:               v.now,
	}
	if v.enforceAud {
		oidcCfg.ClientID = v.clientID
	} else if strings.TrimSpace(v.clientID) != "" {
		slog.Warn("oidc audience check disabled by configuration", "issuer", v.issuerURL)
	}
	v.verifier = provider.VerifierContext(v.baseCtx, oidcCfg)
	v.lastErr = nil
	slog.Info("oidc verifier ready", "issuer", v.issuerURL, "audience_enforced", v.enforceAud)

	return v.verifier, nil
}

func (v *JWTVerifier) now() time.Time {
	return time.Now().Add(-v.expiryLeeway)
}
