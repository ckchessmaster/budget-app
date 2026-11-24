package server_test

import (
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ckchessmaster/budget-app/backend/internal/demo"
	"github.com/ckchessmaster/budget-app/backend/internal/server"
)

// TestMockBridgeIsNotOnPublicRouter pins the exposure fix: the fake SimpleFIN
// bridge must not be reachable through the API router that the ingress fronts.
func TestMockBridgeIsNotOnPublicRouter(t *testing.T) {
	verifier := &stubVerifier{err: errors.New("bad signature")}
	cfg := server.Config{Port: "8080", OIDCIssuerURL: "https://auth.example.com/"}
	router := server.NewServer(cfg, nil, verifier, nil, nil).Router()

	path := "/mock-simplefin/" + uuid.NewString() + "/accounts"

	tests := map[string]struct {
		path   string
		header string
	}{
		"no credentials":    {path: path},
		"with credentials":  {path: path, header: "Basic " + base64.StdEncoding.EncodeToString([]byte("demouser:demopass"))},
		"guessed workspace": {path: "/mock-simplefin/00000000-0000-0000-0000-000000000000/accounts"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code == http.StatusOK || rec.Code == http.StatusUnauthorized {
				t.Fatalf("bridge is still served on the public router: status %d", rec.Code)
			}
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (the route should not exist publicly)", rec.Code)
			}
			if got := rec.Header().Get("Www-Authenticate"); got != "" {
				t.Fatalf("public router still advertises the bridge auth challenge: %q", got)
			}
		})
	}
}

// TestBridgeRouterServesTheMockBridge proves the route was moved rather than
// deleted: the dedicated router still answers it, and still demands the
// per-workspace capability.
func TestBridgeRouterServesTheMockBridge(t *testing.T) {
	srv := server.NewServer(server.Config{
		Port:                    "8080",
		MockSimpleFINListenAddr: demo.DefaultMockBridgeListenAddr,
		MockSimpleFINBaseURL:    demo.DefaultMockBridgeBaseURL,
	}, nil, nil, nil, nil)

	bridge := srv.BridgeRouter()
	if bridge == nil {
		t.Fatal("expected a bridge router")
	}

	path := "/mock-simplefin/" + uuid.NewString() + "/accounts"

	t.Run("missing credentials are challenged", func(t *testing.T) {
		rec := httptest.NewRecorder()
		bridge.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if got := rec.Header().Get("Www-Authenticate"); got == "" {
			t.Fatal("expected a Basic auth challenge so the SimpleFIN client retries with credentials")
		}
	})

	t.Run("health endpoints stay on the public router only", func(t *testing.T) {
		rec := httptest.NewRecorder()
		bridge.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if rec.Code == http.StatusOK {
			t.Fatal("the loopback router must not expose the public health endpoints")
		}
	})
}

// TestDefaultMockBridgeListensOnLoopback is the core of the change: with no
// configuration the bridge is unreachable from another pod.
func TestDefaultMockBridgeListensOnLoopback(t *testing.T) {
	t.Setenv("MOCK_SIMPLEFIN_LISTEN_ADDR", "")
	t.Setenv("MOCK_SIMPLEFIN_BASE_URL", "")

	cfg := server.LoadConfig()

	if cfg.MockSimpleFINListenAddr != "127.0.0.1:8090" {
		t.Fatalf("MockSimpleFINListenAddr = %q, want 127.0.0.1:8090", cfg.MockSimpleFINListenAddr)
	}
	if cfg.MockSimpleFINBaseURL != "http://127.0.0.1:8090" {
		t.Fatalf("MockSimpleFINBaseURL = %q, want the loopback default", cfg.MockSimpleFINBaseURL)
	}
	host, _, err := net.SplitHostPort(cfg.MockSimpleFINListenAddr)
	if err != nil {
		t.Fatalf("default listen address %q is not host:port: %v", cfg.MockSimpleFINListenAddr, err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("default bridge host = %q, want 127.0.0.1 so other pods cannot reach it", host)
	}
	bridgePort := cfg.MockSimpleFINListenAddr[strings.LastIndex(cfg.MockSimpleFINListenAddr, ":")+1:]
	if bridgePort == cfg.Port {
		t.Fatalf("the bridge must not share the API port: api=%s bridge=%s", cfg.Port, bridgePort)
	}
}

func TestMockBridgeConfigOverrides(t *testing.T) {
	tests := map[string]struct {
		listen     string
		base       string
		wantListen string
		wantBase   string
	}{
		"explicit base url wins": {
			listen:     "",
			base:       "http://bridge.internal:8090",
			wantListen: "127.0.0.1:8090",
			wantBase:   "http://bridge.internal:8090",
		},
		"worker in another pod binds a routable address": {
			listen:     "0.0.0.0:8090",
			base:       "http://budget-app-budget-app-backend.budget-app.svc:8090",
			wantListen: "0.0.0.0:8090",
			wantBase:   "http://budget-app-budget-app-backend.budget-app.svc:8090",
		},
		"routable bind still dials loopback by default": {
			listen:     "0.0.0.0:9090",
			base:       "",
			wantListen: "0.0.0.0:9090",
			wantBase:   "http://127.0.0.1:9090",
		},
		"named port override": {
			listen:     "127.0.0.1:18090",
			base:       "",
			wantListen: "127.0.0.1:18090",
			wantBase:   "http://127.0.0.1:18090",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("MOCK_SIMPLEFIN_LISTEN_ADDR", tc.listen)
			t.Setenv("MOCK_SIMPLEFIN_BASE_URL", tc.base)

			cfg := server.LoadConfig()
			if cfg.MockSimpleFINListenAddr != tc.wantListen {
				t.Fatalf("listen = %q, want %q", cfg.MockSimpleFINListenAddr, tc.wantListen)
			}
			if cfg.MockSimpleFINBaseURL != tc.wantBase {
				t.Fatalf("base url = %q, want %q", cfg.MockSimpleFINBaseURL, tc.wantBase)
			}
		})
	}
}
