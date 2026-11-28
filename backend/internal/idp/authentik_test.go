package idp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAuthentikProvisioner_ExistingUser(t *testing.T) {
	existingUUID := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ak_secret" {
			t.Fatalf("missing bearer token: %q", r.Header.Get("Authorization"))
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/v3/core/users/" {
			if got := r.URL.Query().Get("email"); got != "Jane@Example.com" {
				t.Fatalf("query email = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"pk": 42, "uuid": existingUUID.String(), "email": "jane@example.com", "name": "Jane Kingdon"},
				},
			})
			return
		}
		t.Fatalf("unexpected call %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	p := NewAuthentikProvisioner(srv.URL, "ak_secret", srv.Client())
	got, err := p.EnsureUser(context.Background(), "Jane@Example.com", "Jane")
	if err != nil {
		t.Fatalf("EnsureUser failed: %v", err)
	}
	if got.UserUUID != existingUUID || got.IsNewUser || got.PasswordSetupURL != "" {
		t.Fatalf("unexpected result: %+v", got)
	}
	if got.DisplayName != "Jane Kingdon" {
		t.Fatalf("DisplayName = %q, want Jane Kingdon", got.DisplayName)
	}
}

func TestAuthentikProvisioner_NewUserAndRecoveryLink(t *testing.T) {
	createdUUID := uuid.New()
	var createdPayload map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/core/users/":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/core/users/":
			_ = json.NewDecoder(r.Body).Decode(&createdPayload)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"pk":   77,
				"uuid": createdUUID.String(),
				"name": "Jane Kingdon",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/core/users/77/recovery/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"link": "https://authentik.example.com/recovery/token-xyz",
			})
		default:
			t.Fatalf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := NewAuthentikProvisioner(srv.URL, "ak_secret", srv.Client())
	got, err := p.EnsureUser(context.Background(), "jane@example.com", "Jane Kingdon")
	if err != nil {
		t.Fatalf("EnsureUser failed: %v", err)
	}
	if !got.IsNewUser || got.UserUUID != createdUUID {
		t.Fatalf("unexpected user result: %+v", got)
	}
	if got.PasswordSetupURL != "https://authentik.example.com/recovery/token-xyz" {
		t.Fatalf("PasswordSetupURL = %q", got.PasswordSetupURL)
	}
	if createdPayload["type"] != "internal" || createdPayload["is_active"] != true {
		t.Fatalf("unexpected create payload: %+v", createdPayload)
	}
}

func TestAuthentikProvisioner_Errors(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusInternalServerError} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte("error"))
		}))
		p := NewAuthentikProvisioner(srv.URL, "ak_secret", srv.Client())
		_, err := p.EnsureUser(context.Background(), "jane@example.com", "Jane")
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), "HTTP") {
			t.Fatalf("code %d: expected HTTP error, got %v", code, err)
		}
	}
}

func TestNoopIdentityProvisioner(t *testing.T) {
	p := &NoopIdentityProvisioner{}
	u1, err := p.EnsureUser(context.Background(), "member@dev.local", "Dev Member")
	if err != nil {
		t.Fatalf("EnsureUser failed: %v", err)
	}
	if u1.UserUUID.String() != "00000000-0000-0000-0000-000000000002" || u1.IsNewUser {
		t.Fatalf("unexpected dev member: %+v", u1)
	}

	u2, err := p.EnsureUser(context.Background(), "custom@example.com", "")
	if err != nil {
		t.Fatalf("EnsureUser failed: %v", err)
	}
	u2b, _ := p.EnsureUser(context.Background(), "CUSTOM@example.com", "")
	if u2.UserUUID == uuid.Nil || u2.UserUUID != u2b.UserUUID {
		t.Fatalf("expected deterministic UUID for case-insensitive email, got %s vs %s", u2.UserUUID, u2b.UserUUID)
	}
}

func TestAuthentikProvisioner_PlatformLifecycle(t *testing.T) {
	t.Run("CreateUserAndRecoveryLink creates user with username", func(t *testing.T) {
		userUUID := uuid.New()
		var createdBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/core/users/":
				_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
			case r.Method == http.MethodPost && r.URL.Path == "/api/v3/core/users/":
				_ = json.NewDecoder(r.Body).Decode(&createdBody)
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]any{"pk": 91, "uuid": userUUID.String()})
			case r.Method == http.MethodPost && r.URL.Path == "/api/v3/core/users/91/recovery/":
				_ = json.NewEncoder(w).Encode(map[string]any{"link": "https://auth.example.com/rec/abc"})
			default:
				t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
			}
		}))
		defer srv.Close()

		p := NewAuthentikProvisioner(srv.URL, "ak_tok", srv.Client())
		gotID, setupURL, err := p.CreateUserAndRecoveryLink(context.Background(), "mike@example.com", "Mike", "mike")
		if err != nil {
			t.Fatalf("CreateUserAndRecoveryLink failed: %v", err)
		}
		if gotID != userUUID || setupURL != "https://auth.example.com/rec/abc" {
			t.Fatalf("got (%s, %q)", gotID, setupURL)
		}
		if createdBody["username"] != "mike" || createdBody["email"] != "mike@example.com" || createdBody["name"] != "Mike" {
			t.Fatalf("unexpected create payload: %+v", createdBody)
		}
	})

	t.Run("CreateUserAndRecoveryLink maps duplicate email and username", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("email") == "taken@example.com" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"results": []map[string]any{{"email": "taken@example.com"}},
				})
				return
			}
			if r.URL.Query().Get("username") == "takenuser" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"results": []map[string]any{{"username": "takenuser"}},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
		}))
		defer srv.Close()

		p := NewAuthentikProvisioner(srv.URL, "ak_tok", srv.Client())
		if _, _, err := p.CreateUserAndRecoveryLink(context.Background(), "taken@example.com", "A", "a"); !errors.Is(err, ErrEmailAlreadyExists) {
			t.Fatalf("expected ErrEmailAlreadyExists, got %v", err)
		}
		if _, _, err := p.CreateUserAndRecoveryLink(context.Background(), "free@example.com", "A", "takenuser"); !errors.Is(err, ErrUsernameAlreadyExists) {
			t.Fatalf("expected ErrUsernameAlreadyExists, got %v", err)
		}
	})

	t.Run("SetUserActive disables and revokes sessions", func(t *testing.T) {
		userUUID := uuid.New()
		var patchedActive any
		var deletedSession bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/core/users/":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"results": []map[string]any{{"pk": 55, "uuid": userUUID.String()}},
				})
			case r.Method == http.MethodPatch && r.URL.Path == "/api/v3/core/users/55/":
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				patchedActive = body["is_active"]
				_ = json.NewEncoder(w).Encode(map[string]any{"pk": 55})
			case r.Method == http.MethodGet && r.URL.Path == "/api/v3/core/authenticated_sessions/":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"results": []map[string]any{{"uuid": "sess-1"}},
				})
			case r.Method == http.MethodDelete && r.URL.Path == "/api/v3/core/authenticated_sessions/sess-1/":
				deletedSession = true
				w.WriteHeader(http.StatusNoContent)
			default:
				t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
			}
		}))
		defer srv.Close()

		p := NewAuthentikProvisioner(srv.URL, "ak_tok", srv.Client())
		if err := p.SetUserActive(context.Background(), userUUID, false); err != nil {
			t.Fatalf("SetUserActive(false) failed: %v", err)
		}
		if patchedActive != false || !deletedSession {
			t.Fatalf("patchedActive=%v deletedSession=%v", patchedActive, deletedSession)
		}
	})

	t.Run("DeleteUser is idempotent on 404 and fails closed on 500", func(t *testing.T) {
		userUUID := uuid.New()
		mode := "missing"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch mode {
			case "missing":
				_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
			case "delete_ok":
				if r.Method == http.MethodGet {
					_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"pk": 12, "uuid": userUUID.String()}}})
					return
				}
				w.WriteHeader(http.StatusNoContent)
			case "server_error":
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
		defer srv.Close()

		p := NewAuthentikProvisioner(srv.URL, "ak_tok", srv.Client())
		if err := p.DeleteUser(context.Background(), userUUID); err != nil {
			t.Fatalf("expected nil on missing user, got %v", err)
		}
		mode = "delete_ok"
		if err := p.DeleteUser(context.Background(), userUUID); err != nil {
			t.Fatalf("expected nil on delete_ok, got %v", err)
		}
		mode = "server_error"
		if err := p.DeleteUser(context.Background(), userUUID); err == nil {
			t.Fatal("expected error when Authentik returns 500")
		}
	})
}
