package middleware_test

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
)

const testClientID = "budget-app-client"

// fakeOIDCProvider is a minimal OpenID Connect issuer: a discovery document, a
// JWKS endpoint with rotatable keys, and helpers for minting tokens.
type fakeOIDCProvider struct {
	t      *testing.T
	server *httptest.Server
	issuer string

	mu       sync.RWMutex
	keys     map[string]*rsa.PrivateKey
	kids     []string
	failJWKS bool

	discoveryHits int64
	jwksHits      int64
}

func newFakeOIDCProvider(t *testing.T) *fakeOIDCProvider {
	t.Helper()

	p := &fakeOIDCProvider{t: t, keys: map[string]*rsa.PrivateKey{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", p.handleDiscovery)
	mux.HandleFunc("/jwks", p.handleJWKS)
	p.server = httptest.NewServer(mux)
	t.Cleanup(p.server.Close)

	// Authentik advertises a trailing-slash issuer, so exercise that shape too.
	p.issuer = p.server.URL + "/"
	p.addKey("key-1")
	return p
}

func (p *fakeOIDCProvider) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&p.discoveryHits, 1)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                p.issuer,
		"authorization_endpoint":                p.server.URL + "/authorize",
		"token_endpoint":                        p.server.URL + "/token",
		"jwks_uri":                              p.server.URL + "/jwks",
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
	})
}

func (p *fakeOIDCProvider) handleJWKS(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&p.jwksHits, 1)

	p.mu.RLock()
	failing := p.failJWKS
	keys := p.keys
	kids := append([]string(nil), p.kids...)
	p.mu.RUnlock()

	if failing {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>upstream unavailable</html>"))
		return
	}

	advertised := make([]map[string]string, 0, len(kids))
	for _, kid := range kids {
		key := keys[kid]
		advertised = append(advertised, map[string]string{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": kid,
			"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": advertised})
}

func (p *fakeOIDCProvider) setFailJWKS(fail bool) {
	p.mu.Lock()
	p.failJWKS = fail
	p.mu.Unlock()
}

func (p *fakeOIDCProvider) addKey(kid string) *rsa.PrivateKey {
	p.t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		p.t.Fatalf("generate key: %v", err)
	}

	p.mu.Lock()
	p.keys[kid] = key
	p.kids = append(p.kids, kid)
	p.mu.Unlock()
	return key
}

// removeKey withdraws a key from the advertised JWKS, as an issuer does after
// rotating its signing keys.
func (p *fakeOIDCProvider) removeKey(kid string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.keys, kid)
	remaining := make([]string, 0, len(p.kids))
	for _, existing := range p.kids {
		if existing != kid {
			remaining = append(remaining, existing)
		}
	}
	p.kids = remaining
}

func (p *fakeOIDCProvider) key(kid string) *rsa.PrivateKey {
	p.mu.RLock()
	defer p.mu.RUnlock()

	key, ok := p.keys[kid]
	if !ok {
		p.t.Fatalf("test key %q was never added", kid)
	}
	return key
}

func (p *fakeOIDCProvider) hits() (int64, int64) {
	return atomic.LoadInt64(&p.discoveryHits), atomic.LoadInt64(&p.jwksHits)
}

// claims is shaped like an Authentik ID token.
func (p *fakeOIDCProvider) claims(subject string) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss":       p.issuer,
		"sub":       subject,
		"aud":       testClientID,
		"exp":       now.Add(5 * time.Minute).Unix(),
		"iat":       now.Unix(),
		"auth_time": now.Unix(),
		"sid":       "sessionhashvalue",
		"acr":       "goauthentik.io/default-flow",
	}
}

// accessTokenClaims mirrors what this API actually receives: Authentik sends the
// access token first, which is the ID token claim set plus azp/uid/scope.
func (p *fakeOIDCProvider) accessTokenClaims(subject string) map[string]any {
	claims := p.claims(subject)
	claims["azp"] = testClientID
	claims["scope"] = "openid profile email"
	claims["uid"] = "tokenuidvalue"
	return claims
}

func encodeSegment(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func (p *fakeOIDCProvider) signToken(kid string, claims map[string]any) string {
	p.t.Helper()
	return p.signWithKey(kid, p.key(kid), "RS256", claims)
}

func (p *fakeOIDCProvider) signWithKey(kid string, key *rsa.PrivateKey, alg string, claims map[string]any) string {
	p.t.Helper()

	header := map[string]any{"alg": alg, "typ": "JWT", "kid": kid}
	signingInput := encodeSegment(header) + "." + encodeSegment(claims)

	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		p.t.Fatalf("sign token: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// signUnpublished mints a token with a key the JWKS endpoint never advertises.
func (p *fakeOIDCProvider) signUnpublished(claims map[string]any) string {
	p.t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		p.t.Fatalf("generate rogue key: %v", err)
	}
	return p.signWithKey("never-published", key, "RS256", claims)
}

// swapClaims keeps a valid signature but replaces the payload. This is the exact
// privilege escalation the old verifier allowed.
func (p *fakeOIDCProvider) swapClaims(token string, claims map[string]any) string {
	p.t.Helper()

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		p.t.Fatalf("unexpected token shape: %d segments", len(parts))
	}
	return parts[0] + "." + encodeSegment(claims) + "." + parts[2]
}

// breakSignature keeps the payload but replaces the signature bytes.
func (p *fakeOIDCProvider) breakSignature(token string) string {
	p.t.Helper()

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		p.t.Fatalf("unexpected token shape: %d segments", len(parts))
	}
	parts[2] = base64.RawURLEncoding.EncodeToString(make([]byte, len(parts[2])))
	return strings.Join(parts, ".")
}

// unsignedToken is the "alg":"none" forgery: correct shape, no signature.
func (p *fakeOIDCProvider) unsignedToken(claims map[string]any) string {
	return encodeSegment(map[string]any{"alg": "none", "typ": "JWT"}) + "." + encodeSegment(claims) + "."
}

// hmacToken is an algorithm-confusion forgery: HS256 signed using the issuer's own
// public key as the HMAC secret.
func (p *fakeOIDCProvider) hmacToken(kid string, claims map[string]any) string {
	p.t.Helper()

	der, err := x509.MarshalPKIXPublicKey(&p.key(kid).PublicKey)
	if err != nil {
		p.t.Fatalf("marshal public key: %v", err)
	}

	signingInput := encodeSegment(map[string]any{"alg": "HS256", "typ": "JWT", "kid": kid}) + "." + encodeSegment(claims)
	mac := hmac.New(sha256.New, der)
	_, _ = mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func withClaim(claims map[string]any, name string, value any) map[string]any {
	out := make(map[string]any, len(claims)+1)
	for key, existing := range claims {
		out[key] = existing
	}
	out[name] = value
	return out
}

func withoutClaim(claims map[string]any, name string) map[string]any {
	out := make(map[string]any, len(claims))
	for key, existing := range claims {
		if key == name {
			continue
		}
		out[key] = existing
	}
	return out
}

// newTestVerifier builds a verifier against the fake issuer. Expiry leeway is
// disabled so each test controls expiry explicitly.
func newTestVerifier(t *testing.T, provider *fakeOIDCProvider, mutate func(*middleware.JWTVerifierConfig)) *middleware.JWTVerifier {
	t.Helper()

	cfg := middleware.JWTVerifierConfig{
		IssuerURL:        provider.issuer,
		ClientID:         testClientID,
		EnforceAudience:  true,
		DiscoveryBackoff: time.Hour,
		DiscoveryTimeout: 3 * time.Second,
		ExpiryLeeway:     -1,
		HTTPClient:       provider.server.Client(),
	}
	if mutate != nil {
		mutate(&cfg)
	}

	verifier := middleware.NewJWTVerifier(cfg)
	if err := verifier.Warm(context.Background()); err != nil {
		t.Fatalf("warm: %v", err)
	}
	return verifier
}

func TestJWTVerifierAcceptsLegitTokens(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	verifier := newTestVerifier(t, provider, nil)
	userID := uuid.NewString()

	for name, claims := range map[string]map[string]any{
		"access token": provider.accessTokenClaims(userID),
		"id token":     provider.claims(userID),
	} {
		t.Run(name, func(t *testing.T) {
			subject, err := verifier.Verify(context.Background(), provider.signToken("key-1", claims))
			if err != nil {
				t.Fatalf("expected a legit %s to verify, got: %v", name, err)
			}
			if subject != userID {
				t.Fatalf("subject = %q, want %q", subject, userID)
			}
		})
	}

	for i := 0; i < 3; i++ {
		if _, err := verifier.Verify(context.Background(), provider.signToken("key-1", provider.claims(userID))); err != nil {
			t.Fatalf("verification %d failed: %v", i, err)
		}
	}

	if discoveryHits, _ := provider.hits(); discoveryHits != 1 {
		t.Fatalf("expected discovery to be cached, got %d discovery requests", discoveryHits)
	}
}

func TestJWTVerifierRejectsForgedTokens(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	verifier := newTestVerifier(t, provider, nil)

	owner := uuid.NewString()
	attacker := uuid.NewString()
	legitID := provider.signToken("key-1", provider.claims(owner))
	legitAccess := provider.signToken("key-1", provider.accessTokenClaims(owner))

	tests := map[string]struct {
		token func() string
		why   string
	}{
		"swapped subject": {
			token: func() string { return provider.swapClaims(legitID, provider.claims(attacker)) },
			why:   "a valid signature over someone else's claims must not be trusted",
		},
		"swapped subject on access token": {
			token: func() string { return provider.swapClaims(legitAccess, provider.accessTokenClaims(attacker)) },
			why:   "the same swap against the token type the frontend actually sends",
		},
		"unsigned alg none": {
			token: func() string { return provider.unsignedToken(provider.claims(attacker)) },
			why:   "the none algorithm must never be accepted",
		},
		"hs256 key confusion": {
			token: func() string { return provider.hmacToken("key-1", provider.claims(attacker)) },
			why:   "an RSA public key must not be usable as an HMAC secret",
		},
		"broken signature": {
			token: func() string { return provider.breakSignature(legitID) },
			why:   "tampered signature bytes must be rejected",
		},
		"unpublished key": {
			token: func() string { return provider.signUnpublished(provider.claims(attacker)) },
			why:   "a key the issuer never advertised must not verify",
		},
		"not a jwt": {
			token: func() string { return "hunter2" },
			why:   "non-JWT bearer values must be rejected",
		},
		"empty": {
			token: func() string { return "" },
			why:   "empty bearer tokens must be rejected",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			subject, err := verifier.Verify(context.Background(), tc.token())
			if err == nil {
				t.Fatalf("forged token accepted (%s); returned subject %q", tc.why, subject)
			}
			if subject != "" {
				t.Fatalf("forged token returned subject %q alongside error %v", subject, err)
			}
		})
	}
}

func TestJWTVerifierRejectsInvalidRegisteredClaims(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	verifier := newTestVerifier(t, provider, nil)
	userID := uuid.NewString()

	tests := map[string]map[string]any{
		"wrong issuer":                 withClaim(provider.claims(userID), "iss", "https://evil.example.com/"),
		"wrong audience":               withClaim(provider.claims(userID), "aud", "some-other-client"),
		"audience list without client": withClaim(provider.claims(userID), "aud", []string{"other-a", "other-b"}),
		"expired":                      withClaim(provider.claims(userID), "exp", time.Now().Add(-10*time.Minute).Unix()),
		"missing subject":              withoutClaim(provider.claims(userID), "sub"),
		"empty subject":                withClaim(provider.claims(userID), "sub", ""),
	}

	for name, claims := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := verifier.Verify(context.Background(), provider.signToken("key-1", claims)); err == nil {
				t.Fatal("expected rejection, but the token was accepted")
			}
		})
	}
}

func TestJWTVerifierAudienceEnforcementIsToggleable(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	userID := uuid.NewString()
	foreignAudience := provider.signToken("key-1", withClaim(provider.claims(userID), "aud", "another-client"))

	t.Run("enforced rejects a foreign audience", func(t *testing.T) {
		verifier := newTestVerifier(t, provider, nil)
		if _, err := verifier.Verify(context.Background(), foreignAudience); err == nil {
			t.Fatal("expected a token issued for another audience to be rejected")
		}
	})

	t.Run("disabled accepts a foreign audience", func(t *testing.T) {
		verifier := newTestVerifier(t, provider, func(cfg *middleware.JWTVerifierConfig) {
			cfg.EnforceAudience = false
		})
		if _, err := verifier.Verify(context.Background(), foreignAudience); err != nil {
			t.Fatalf("expected the audience check to be skipped, got: %v", err)
		}
	})

	t.Run("missing client id cannot enforce an audience", func(t *testing.T) {
		verifier := newTestVerifier(t, provider, func(cfg *middleware.JWTVerifierConfig) {
			cfg.ClientID = ""
		})
		if _, err := verifier.Verify(context.Background(), foreignAudience); err != nil {
			t.Fatalf("expected the audience check to be skipped without a client id, got: %v", err)
		}
	})

	t.Run("signature is still enforced without an audience check", func(t *testing.T) {
		verifier := newTestVerifier(t, provider, func(cfg *middleware.JWTVerifierConfig) {
			cfg.EnforceAudience = false
		})
		if _, err := verifier.Verify(context.Background(), provider.unsignedToken(provider.claims(userID))); err == nil {
			t.Fatal("unsigned token accepted while the audience check was off")
		}
	})

	t.Run("audience list containing this client is accepted", func(t *testing.T) {
		verifier := newTestVerifier(t, provider, nil)
		token := provider.signToken("key-1", withClaim(provider.claims(userID), "aud", []string{"other", testClientID}))
		if subject, err := verifier.Verify(context.Background(), token); err != nil {
			t.Fatalf("expected a multi-audience token to verify, got: %v", err)
		} else if subject != userID {
			t.Fatalf("subject = %q, want %q", subject, userID)
		}
	})
}

// verifyWithin polls verification until it produces the wanted outcome, or the
// deadline passes.
//
// The JWKS cache in go-oidc hands a fetch result to its waiters before it
// publishes the new key set and clears the in-flight request
// (go-oidc/v3 oidc/jwks.go, keysFromRemote). The outcome of the first verify
// after an issuer rotates keys, withdraws one, or starts serving them again is
// therefore eventually consistent rather than atomic, and CI runners under load
// lose that race often enough to matter.
//
// The security property under test does not move: a withdrawn signing key ends
// up rejected, and every request in between is either a genuine signature from
// a key the issuer still advertised or a rejection. How quickly the cache
// converges is the issuer's decision, not a per-request guarantee.
func verifyWithin(t *testing.T, verifier *middleware.JWTVerifier, token string, wantErr bool, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		subject, err := verifier.Verify(context.Background(), token)
		if (err != nil) == wantErr {
			if !wantErr && subject == "" {
				t.Fatal("verification succeeded with an empty subject")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("verification never converged (wantErr %v, subject %q): last error %v", wantErr, subject, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestJWTVerifierHandlesKeyRotation(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	verifier := newTestVerifier(t, provider, nil)
	userID := uuid.NewString()

	oldToken := provider.signToken("key-1", provider.claims(userID))
	if _, err := verifier.Verify(context.Background(), oldToken); err != nil {
		t.Fatalf("expected the original key to verify: %v", err)
	}
	_, hitsBefore := provider.hits()

	// Rotate forward the way Authentik does: publish the new key and keep the old one
	// advertised for an overlap window so in-flight tokens stay valid.
	provider.addKey("key-2")
	newToken := provider.signToken("key-2", provider.claims(userID))
	if _, err := verifier.Verify(context.Background(), newToken); err != nil {
		t.Fatalf("expected the rotated key to be fetched and verified: %v", err)
	}

	if _, hitsAfter := provider.hits(); hitsAfter <= hitsBefore {
		t.Fatalf("an unknown key id should trigger a JWKS refresh (before %d, after %d)", hitsBefore, hitsAfter)
	}

	// Tokens minted with the previous key still verify while the issuer advertises it.
	if subject, err := verifier.Verify(context.Background(), oldToken); err != nil {
		t.Fatalf("expected a token from the overlapping key to verify, got: %v", err)
	} else if subject != userID {
		t.Fatalf("subject = %q, want %q", subject, userID)
	}

	// Once the issuer withdraws the old key entirely, tokens signed by it stop being
	// accepted. Key lifetime is the issuer's decision, not this service's.
	provider.removeKey("key-1")
	provider.addKey("key-3")
	if _, err := verifier.Verify(context.Background(), provider.signToken("key-3", provider.claims(userID))); err != nil {
		t.Fatalf("expected the newest key to be fetched and verified: %v", err)
	}
	verifyWithin(t, verifier, oldToken, true, 3*time.Second)
}

// TestJWTVerifierServesLastKnownKeysThroughJWKSError proves an identity provider
// outage cannot log users out: keys already fetched keep validating tokens while
// the JWKS endpoint is unhealthy.
func TestJWTVerifierServesLastKnownKeysThroughJWKSError(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	verifier := newTestVerifier(t, provider, nil)
	userID := uuid.NewString()

	token := provider.signToken("key-1", provider.claims(userID))
	if _, err := verifier.Verify(context.Background(), token); err != nil {
		t.Fatalf("warm-up verification failed: %v", err)
	}

	provider.setFailJWKS(true)
	provider.removeKey("key-1")
	_, jwksBefore := provider.hits()

	for i := 0; i < 3; i++ {
		subject, err := verifier.Verify(context.Background(), token)
		if err != nil {
			t.Fatalf("verification %d failed while the JWKS endpoint was down: %v", i, err)
		}
		if subject != userID {
			t.Fatalf("subject = %q, want %q", subject, userID)
		}
	}

	if _, jwksAfter := provider.hits(); jwksAfter != jwksBefore {
		t.Fatalf("cached keys should avoid new JWKS traffic (before %d, after %d)", jwksBefore, jwksAfter)
	}

	// A key the endpoint never served while healthy cannot come from the cache.
	provider.addKey("key-9")
	if _, err := verifier.Verify(context.Background(), provider.signToken("key-9", provider.claims(userID))); err == nil {
		t.Fatal("expected a token from an unreachable key to be rejected while the endpoint is down")
	}

	provider.setFailJWKS(false)
	verifyWithin(t, verifier, provider.signToken("key-9", provider.claims(userID)), false, 3*time.Second)
}

func TestJWTVerifierFailsClosedWhenDiscoveryIsDown(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	userID := uuid.NewString()
	token := provider.signToken("key-1", provider.claims(userID))

	var hits int64
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(down.Close)

	verifier := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		IssuerURL:        down.URL + "/",
		ClientID:         testClientID,
		EnforceAudience:  true,
		DiscoveryBackoff: time.Hour,
		DiscoveryTimeout: 2 * time.Second,
	})

	for i := 0; i < 5; i++ {
		subject, err := verifier.Verify(context.Background(), token)
		if !errors.Is(err, middleware.ErrVerifierUnavailable) {
			t.Fatalf("attempt %d: expected ErrVerifierUnavailable, got %v (subject %q)", i, err, subject)
		}
		if subject != "" {
			t.Fatalf("attempt %d: returned subject %q despite an unusable issuer", i, subject)
		}
	}

	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Fatalf("expected discovery backoff to collapse 5 attempts into 1 request, got %d", got)
	}
}

func TestJWTVerifierRecoversWhenTheIssuerHeals(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	userID := uuid.NewString()
	token := provider.signToken("key-1", provider.claims(userID))

	// Discovery works, but signing keys cannot be served: an upstream 502 on JWKS.
	provider.setFailJWKS(true)
	verifier := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		IssuerURL:        provider.issuer,
		ClientID:         testClientID,
		EnforceAudience:  true,
		DiscoveryBackoff: time.Millisecond,
		DiscoveryTimeout: 2 * time.Second,
		ExpiryLeeway:     -1,
		HTTPClient:       provider.server.Client(),
	})

	if _, err := verifier.Verify(context.Background(), token); err == nil {
		t.Fatal("expected verification to fail while the issuer cannot serve keys")
	}

	provider.setFailJWKS(false)
	verifyWithin(t, verifier, token, false, 3*time.Second)
}

func TestJWTVerifierExpiryLeeway(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	userID := uuid.NewString()
	recentlyExpired := provider.signToken("key-1", withClaim(provider.claims(userID), "exp", time.Now().Add(-20*time.Second).Unix()))

	t.Run("no leeway rejects", func(t *testing.T) {
		verifier := newTestVerifier(t, provider, func(cfg *middleware.JWTVerifierConfig) {
			cfg.ExpiryLeeway = -1
		})
		if _, err := verifier.Verify(context.Background(), recentlyExpired); err == nil {
			t.Fatal("expected an expired token to be rejected without leeway")
		}
	})

	t.Run("leeway tolerates clock skew", func(t *testing.T) {
		verifier := newTestVerifier(t, provider, func(cfg *middleware.JWTVerifierConfig) {
			cfg.ExpiryLeeway = time.Minute
		})
		if _, err := verifier.Verify(context.Background(), recentlyExpired); err != nil {
			t.Fatalf("expected skew tolerance to accept the token, got: %v", err)
		}
	})

	t.Run("leeway does not accept long-expired tokens", func(t *testing.T) {
		verifier := newTestVerifier(t, provider, func(cfg *middleware.JWTVerifierConfig) {
			cfg.ExpiryLeeway = time.Minute
		})
		longExpired := provider.signToken("key-1", withClaim(provider.claims(userID), "exp", time.Now().Add(-30*time.Minute).Unix()))
		if _, err := verifier.Verify(context.Background(), longExpired); err == nil {
			t.Fatal("expected a long-expired token to be rejected even with leeway")
		}
	})
}

// TestAuthMiddlewareRejectsForgedToken covers the reported vulnerability as a
// regression test: a self-issued token carrying a real subject must be refused.
func TestAuthMiddlewareRejectsForgedToken(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	verifier := newTestVerifier(t, provider, nil)

	userID := uuid.New()
	legit := provider.signToken("key-1", provider.accessTokenClaims(userID.String()))
	forged := provider.swapClaims(legit, provider.accessTokenClaims(uuid.NewString()))
	unsigned := provider.unsignedToken(provider.accessTokenClaims(userID.String()))

	handler := middleware.AuthMiddleware(verifier)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, err := middleware.GetUserID(r.Context())
		if err != nil {
			t.Errorf("expected a user id in context: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if got != userID {
			t.Errorf("user id = %s, want %s", got, userID)
		}
		w.WriteHeader(http.StatusOK)
	}))

	tests := map[string]struct {
		token string
		want  int
	}{
		"legit access token": {token: legit, want: http.StatusOK},
		"forged subject":     {token: forged, want: http.StatusUnauthorized},
		"alg none":           {token: unsigned, want: http.StatusUnauthorized},
		"garbage":            {token: "absolutely-not-a-token", want: http.StatusUnauthorized},
		"no header":          {token: "", want: http.StatusUnauthorized},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/budget", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestJWTVerifierAgainstLiveIssuer probes the real identity provider. A fake
// issuer cannot prove that discovery and JWKS parsing work against Authentik, so
// this runs on demand when both variables are set.
func TestJWTVerifierAgainstLiveIssuer(t *testing.T) {
	issuer := os.Getenv("OIDC_LIVE_ISSUER")
	clientID := os.Getenv("OIDC_LIVE_CLIENT_ID")
	if issuer == "" || clientID == "" {
		t.Skip("set OIDC_LIVE_ISSUER and OIDC_LIVE_CLIENT_ID to probe a real issuer")
	}

	verifier := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		IssuerURL:        issuer,
		ClientID:         clientID,
		EnforceAudience:  true,
		DiscoveryTimeout: 15 * time.Second,
		DiscoveryBackoff: time.Second,
	})
	if err := verifier.Warm(context.Background()); err != nil {
		t.Fatalf("live discovery against %s failed: %v", issuer, err)
	}

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT","kid":"forged"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(liveClaimsJSON(issuer, clientID)))
	forged := header + "." + payload + "." + base64.RawURLEncoding.EncodeToString([]byte("forged"))

	if _, err := verifier.Verify(context.Background(), forged); err == nil {
		t.Fatal("the live verifier accepted a forged token")
	} else {
		t.Logf("live verifier rejected the forgery as expected: %v", err)
	}
}

func liveClaimsJSON(issuer, clientID string) string {
	encoded, err := json.Marshal(map[string]any{
		"iss": issuer,
		"sub": uuid.NewString(),
		"aud": clientID,
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func TestJWTVerifierExtractsGroupsClaim(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	verifier := newTestVerifier(t, provider, nil)
	userID := uuid.NewString()

	claims := withClaim(provider.claims(userID), "groups", []string{"users", " budget-admins ", ""})
	token := provider.signToken("key-1", claims)

	got, err := verifier.VerifyClaims(context.Background(), token)
	if err != nil {
		t.Fatalf("VerifyClaims failed: %v", err)
	}
	if len(got.Groups) != 2 || got.Groups[0] != "users" || got.Groups[1] != "budget-admins" {
		t.Fatalf("Groups = %#v, want [users budget-admins]", got.Groups)
	}
}
