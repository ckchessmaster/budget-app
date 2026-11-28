package server_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/internal/server"
)

const (
	allowedOrigin = "https://budget.example.com"
	siteOrigin    = "https://evil.example.com"
)

type countingHandler struct {
	calls int
}

func (c *countingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.calls++
	w.WriteHeader(http.StatusTeapot)
}

func newCORSHandler(policy *server.CORSPolicy) (http.Handler, *countingHandler) {
	downstream := &countingHandler{}
	return server.CORSMiddleware(policy)(downstream), downstream
}

func TestCORSMiddlewareGrantsAllowlistedOriginsOnly(t *testing.T) {
	policy := server.NewCORSPolicy([]string{allowedOrigin})
	handler, downstream := newCORSHandler(policy)

	tests := map[string]struct {
		origin         string
		wantGrant      string
		wantDownstream bool
	}{
		"allowlisted":                    {origin: allowedOrigin, wantGrant: allowedOrigin, wantDownstream: true},
		"not allowlisted":                {origin: siteOrigin, wantGrant: "", wantDownstream: true},
		"null origin":                    {origin: "null", wantGrant: "", wantDownstream: true},
		"allowlisted with tls downgrade": {origin: "http://budget.example.com", wantGrant: "", wantDownstream: true},
		"allowlisted subdomain":          {origin: "https://evil-budget.example.com", wantGrant: "", wantDownstream: true},
		"allowlisted suffix attack":      {origin: allowedOrigin + ".evil.example.com", wantGrant: "", wantDownstream: true},
		"no origin":                      {origin: "", wantGrant: "", wantDownstream: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			downstream.calls = 0
			req := httptest.NewRequest(http.MethodGet, "/api/v1/budget", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tc.wantGrant {
				t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, tc.wantGrant)
			}
			if downstream.calls != 1 {
				t.Fatalf("downstream calls = %d, want 1", downstream.calls)
			}
			if tc.origin == "" {
				for _, header := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Methods", "Vary"} {
					if rec.Header().Get(header) != "" {
						t.Fatalf("same-origin/no-origin request should be untouched, but %s = %q", header, rec.Header().Get(header))
					}
				}
				return
			}
			if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
				t.Fatalf("Vary = %q, want it to mention Origin", rec.Header().Get("Vary"))
			}
		})
	}
}

func TestCORSMiddlewarePreflight(t *testing.T) {
	policy := server.NewCORSPolicy([]string{allowedOrigin})

	tests := map[string]struct {
		origin          string
		method          string
		requestMethod   string
		requestHeaders  string
		wantStatus      int
		wantAllowOrigin string
		wantMethods     string
		wantHeaders     func(*testing.T, string)
		wantDownstream  bool
	}{
		"simple cross origin write": {
			origin:          allowedOrigin,
			requestMethod:   http.MethodPost,
			requestHeaders:  "authorization, content-type, x-workspace-id",
			wantStatus:      http.StatusNoContent,
			wantAllowOrigin: allowedOrigin,
			wantMethods:     http.MethodPost,
			wantHeaders: func(t *testing.T, got string) {
				t.Helper()
				for _, want := range []string{"authorization", "content-type", "x-workspace-id"} {
					if !strings.Contains(got, want) {
						t.Fatalf("Access-Control-Allow-Headers = %q, want it to include %q", got, want)
					}
				}
			},
		},
		"delete preflight": {
			origin:          allowedOrigin,
			requestMethod:   http.MethodDelete,
			wantStatus:      http.StatusNoContent,
			wantAllowOrigin: allowedOrigin,
			wantMethods:     http.MethodDelete,
		},
		"unknown requested header": {
			origin:          allowedOrigin,
			requestMethod:   http.MethodPost,
			requestHeaders:  "authorization, x-google-oauth-token",
			wantStatus:      http.StatusTeapot,
			wantAllowOrigin: "",
			wantDownstream:  true,
		},
		"disallowed method": {
			origin:          allowedOrigin,
			requestMethod:   http.MethodTrace,
			wantStatus:      http.StatusTeapot,
			wantAllowOrigin: "",
			wantDownstream:  true,
		},
		"preflight from unlisted origin": {
			origin:          siteOrigin,
			requestMethod:   http.MethodPost,
			wantStatus:      http.StatusTeapot,
			wantAllowOrigin: "",
			wantDownstream:  true,
		},
		"options without requested method is not a preflight": {
			origin:          allowedOrigin,
			wantStatus:      http.StatusTeapot,
			wantAllowOrigin: allowedOrigin,
			wantDownstream:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			handler, downstream := newCORSHandler(policy)

			req := httptest.NewRequest(http.MethodOptions, "/api/v1/demo/toggle", nil)
			req.Header.Set("Origin", tc.origin)
			if tc.requestMethod != "" {
				req.Header.Set("Access-Control-Request-Method", tc.requestMethod)
			}
			if tc.requestHeaders != "" {
				req.Header.Set("Access-Control-Request-Headers", tc.requestHeaders)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tc.wantAllowOrigin {
				t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, tc.wantAllowOrigin)
			}
			if tc.wantMethods != "" {
				if got := rec.Header().Get("Access-Control-Allow-Methods"); got != tc.wantMethods {
					t.Fatalf("Access-Control-Allow-Methods = %q, want %q", got, tc.wantMethods)
				}
			}
			if tc.wantHeaders != nil {
				tc.wantHeaders(t, rec.Header().Get("Access-Control-Allow-Headers"))
			}
			if tc.wantStatus == http.StatusNoContent {
				if got := rec.Header().Get("Access-Control-Max-Age"); got == "" {
					t.Fatal("expected preflight caching to be configured")
				}
				if strings.Contains(strings.ToLower(rec.Header().Get("Access-Control-Allow-Headers")), "x-csrf-token") && tc.requestHeaders == "" {
					t.Fatal("should not advertise headers the caller did not request")
				}
			}
			if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
				t.Fatalf("the bearer-token API must never grant credentialed CORS, got %q", got)
			}
			wantCalls := 0
			if tc.wantDownstream {
				wantCalls = 1
			}
			if downstream.calls != wantCalls {
				t.Fatalf("downstream calls = %d, want %d", downstream.calls, wantCalls)
			}
		})
	}
}

func TestCORSMiddlewareWithNoOriginsConfiguredFailsClosed(t *testing.T) {
	for name, policy := range map[string]*server.CORSPolicy{
		"nil policy":   nil,
		"empty policy": server.NewCORSPolicy(nil),
		"junk policy":  server.NewCORSPolicy([]string{"*", "not a url", "https://good.example.com/some/path", ""}),
	} {
		t.Run(name, func(t *testing.T) {
			handler, downstream := newCORSHandler(policy)

			preflight := httptest.NewRequest(http.MethodOptions, "/api/v1/demo/toggle", nil)
			preflight.Header.Set("Origin", "http://localhost:5173")
			preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
			preflight.Header.Set("Access-Control-Request-Headers", "authorization")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, preflight)

			if downstream.calls != 1 {
				t.Fatalf("expected preflight to fall through to the router, got %d downstream calls", downstream.calls)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Fatalf("expected no CORS grant without an explicit allowlist, got %q", got)
			}
		})
	}
}

func TestCORSPolicyMatchesNormalizedOrigins(t *testing.T) {
	policy := server.NewCORSPolicy([]string{"HTTPS://Budget.Example.COM/", "http://127.0.0.1:5173"})

	tests := map[string]bool{
		"https://budget.example.com":     true,
		"https://budget.example.com/":    true,
		"http://127.0.0.1:5173":          true,
		"http://localhost:5173":          false,
		"https://budget.example.com:443": false,
		"":                               false,
	}

	for origin, want := range tests {
		if got := policy.Allowed(origin); got != want {
			t.Fatalf("Allowed(%q) = %v, want %v", origin, got, want)
		}
	}

	if server.NewCORSPolicy(nil).Empty() != true {
		t.Fatal("expected an empty policy to report Empty")
	}
	if server.NewCORSPolicy([]string{"https://a.example.com"}).Empty() {
		t.Fatal("expected a configured policy not to report Empty")
	}
}

// stubVerifier reports whether a token reached verification, so a router-level test
// can prove that unauthenticated requests never become authorised.
type stubVerifier struct {
	subject string
	err     error
	calls   int
}

func (s *stubVerifier) Verify(ctx context.Context, rawToken string) (string, error) {
	s.calls++
	if s.err != nil {
		return "", s.err
	}
	return s.subject, nil
}

// TestServerDoesNotGrantWildcardCORS is the regression test for the reported
// behaviour: every path answered OPTIONS with 200 and a wildcard origin grant.
func TestServerDoesNotGrantWildcardCORS(t *testing.T) {
	verifier := &stubVerifier{err: errors.New("bad signature")}
	cfg := server.Config{
		Port:                "8080",
		OIDCIssuerURL:       "https://auth.example.com/application/o/budget-app/",
		OIDCClientID:        "budget-app-client",
		OIDCEnforceAudience: true,
		CORSAllowedOrigins:  []string{allowedOrigin},
	}
	router := server.NewServer(cfg, nil, verifier, nil, nil).Router()

	t.Run("preflight is only granted for the allowlisted origin", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/demo/toggle", nil)
		req.Header.Set("Origin", allowedOrigin)
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		req.Header.Set("Access-Control-Request-Headers", "content-type, authorization")
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != allowedOrigin {
			t.Fatalf("Access-Control-Allow-Origin = %q, want the exact origin %q", got, allowedOrigin)
		}
		if rec.Code == http.StatusOK {
			t.Fatal("preflight must not answer 200")
		}
	})

	t.Run("unlisted origin gets no grant", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/demo/toggle", nil)
		req.Header.Set("Origin", siteOrigin)
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		router.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("expected no grant for %q, got %q", siteOrigin, got)
		}
	})

	t.Run("authenticated traffic never sees a wildcard", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/budget", nil)
		req.Header.Set("Origin", allowedOrigin)
		req.Header.Set("Authorization", "Bearer forged.token.value")
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusUnauthorized, rec.Body.String())
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != allowedOrigin {
			t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, allowedOrigin)
		}
		if strings.Contains(rec.Header().Get("Access-Control-Allow-Origin"), "*") {
			t.Fatal("wildcard CORS grant is still present")
		}
		if verifier.calls != 1 {
			t.Fatalf("expected the token to be verified once, got %d calls", verifier.calls)
		}
	})

	t.Run("health endpoints are reachable without an origin", func(t *testing.T) {
		for _, path := range []string{"/livez", "/healthz"} {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusMethodNotAllowed {
				t.Fatalf("%s status = %d, want a probe-friendly response", path, rec.Code)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Fatalf("%s should not send CORS headers, got %q", path, got)
			}
		}
	})

	t.Run("unmatched paths are not answered with 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/nonexistent", nil)
		req.Header.Set("Origin", siteOrigin)
		req.Header.Set("Access-Control-Request-Method", http.MethodGet)
		router.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatal("OPTIONS on an unknown path must not return 200")
		}
	})
}

func TestServerRejectsUnsignedTokenEndToEnd(t *testing.T) {
	verifier := &stubVerifier{err: errors.New("token rejected: failed to verify signature")}
	cfg := server.Config{Port: "8080", OIDCIssuerURL: "https://auth.example.com/"}
	router := server.NewServer(cfg, nil, verifier, nil, nil).Router()

	for _, path := range []string{"/api/v1/budget", "/api/v1/accounts", "/api/v1/demo/toggle", "/api/v1/settings"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer unsigned-by-anyone")
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want 401 (body %s)", path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "invalid_token") {
			t.Fatalf("%s body = %s, want an invalid_token error", path, rec.Body.String())
		}
	}
}

func TestNewServerWithoutVerifierDoesNotExposeProtectedRoutes(t *testing.T) {
	router := server.NewServer(server.Config{Port: "8080"}, nil, nil, nil, nil).Router()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/budget", nil))
	if rec.Code == http.StatusOK {
		t.Fatal("protected routes must not be registered without a token verifier")
	}

	var _ middleware.OIDCVerifier = (*stubVerifier)(nil)
	var _ chi.Router = router
}
