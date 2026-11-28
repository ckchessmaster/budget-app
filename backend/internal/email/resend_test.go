package email

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestResendSender_SuccessAndHeaders(t *testing.T) {
	var gotAuth, gotIdem string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/emails" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotIdem = r.Header.Get("Idempotency-Key")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"re_msg_123"}`))
	}))
	defer srv.Close()

	sender := NewResendSender("re_test_key", "Budget App <invites@example.com>", srv.Client())
	sender.BaseURL = srv.URL

	id, err := sender.Send(context.Background(), OutboundEmail{
		To:             "jane@example.com",
		Subject:        "Join Household",
		HTMLBody:       "<p>Hi</p>",
		TextBody:       "Hi",
		IdempotencyKey: "invite-1-abc",
	})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if id != "re_msg_123" {
		t.Fatalf("message id = %q, want re_msg_123", id)
	}
	if gotAuth != "Bearer re_test_key" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotIdem != "invite-1-abc" {
		t.Fatalf("Idempotency-Key = %q", gotIdem)
	}
	if gotBody["from"] != "Budget App <invites@example.com>" {
		t.Fatalf("from = %v", gotBody["from"])
	}
}

func TestResendSender_ErrorClassification(t *testing.T) {
	var status atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := int(status.Load())
		if code == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "4")
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))
	defer srv.Close()

	sender := NewResendSender("re_test_key", "Budget App <invites@example.com>", srv.Client())
	sender.BaseURL = srv.URL

	for _, tc := range []struct {
		code      int
		retryable bool
		terminal  bool
		retryWait time.Duration
	}{
		{http.StatusBadRequest, false, true, 0},
		{http.StatusForbidden, false, true, 0},
		{http.StatusTooManyRequests, true, false, 4 * time.Second},
		{http.StatusServiceUnavailable, true, false, 0},
	} {
		status.Store(int32(tc.code))
		_, err := sender.Send(context.Background(), OutboundEmail{To: "a@b.com", Subject: "s", IdempotencyKey: "k"})
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("code %d: expected APIError, got %v", tc.code, err)
		}
		if apiErr.IsRetryable() != tc.retryable || apiErr.IsTerminal() != tc.terminal {
			t.Fatalf("code %d: retryable=%v terminal=%v, want retryable=%v terminal=%v",
				tc.code, apiErr.IsRetryable(), apiErr.IsTerminal(), tc.retryable, tc.terminal)
		}
		if apiErr.RetryAfter != tc.retryWait {
			t.Fatalf("code %d: RetryAfter=%v, want %v", tc.code, apiErr.RetryAfter, tc.retryWait)
		}
	}
}

func TestNoopConsoleSender(t *testing.T) {
	sender := &NoopConsoleSender{FromAddress: "Budget App <invites@dev.local>"}
	id, err := sender.Send(context.Background(), OutboundEmail{
		To:             "member@dev.local",
		Subject:        "Invite",
		TextBody:       "http://localhost:5173/invite?token=abc",
		IdempotencyKey: "k1",
	})
	if err != nil {
		t.Fatalf("NoopConsoleSender failed: %v", err)
	}
	if id != "noop-k1" {
		t.Fatalf("id = %q, want noop-k1", id)
	}
}

func TestRenderPlatformInviteEmail(t *testing.T) {
	exp := time.Date(2026, 12, 5, 12, 0, 0, 0, time.UTC)
	subj, htmlBody, textBody := RenderPlatformInviteEmail("For Mike", "chris@example.com", "https://budget.example.com/signup?invite=tok123", exp)
	if subj == "" || !stringsContainsAll(htmlBody, "chris@example.com", "For Mike", "https://budget.example.com/signup?invite=tok123") {
		t.Fatalf("unexpected htmlBody: %s", htmlBody)
	}
	if !stringsContainsAll(textBody, "chris@example.com", "https://budget.example.com/signup?invite=tok123") {
		t.Fatalf("unexpected textBody: %s", textBody)
	}
}

func TestLoggingSender_NilPoolDelegates(t *testing.T) {
	inner := &NoopConsoleSender{FromAddress: "Budget <i@dev.local>"}
	wrapped := NewLoggingSender(inner, nil)
	id, err := wrapped.Send(context.Background(), OutboundEmail{To: "a@b.com", Subject: "s", IdempotencyKey: "k2", Template: "platform_invite"})
	if err != nil || id != "noop-k2" {
		t.Fatalf("got (%q, %v)", id, err)
	}
}

func stringsContainsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
