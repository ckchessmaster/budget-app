package server_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/ckchessmaster/budget-app/backend/internal/server"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func newTestLogger() (*slog.Logger, *lockedBuffer) {
	out := &lockedBuffer{}
	return slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug})), out
}

// chain mirrors the middleware order NewServer installs.
func chain(handler http.Handler, logger *slog.Logger) http.Handler {
	handler = server.LogServerErrors(logger)(handler)
	handler = chimiddleware.RequestID(handler)
	return handler
}

func TestLogServerErrorsRecordsFailedRequests(t *testing.T) {
	logger, out := newTestLogger()

	handler := chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"failed_to_load_month_budget"}` + "\n"))
	}), logger)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/budget?month=2026-11", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("client status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "failed_to_load_month_budget") {
		t.Fatalf("client body = %q, want the error payload preserved", rec.Body.String())
	}

	logged := out.String()
	for _, want := range []string{
		"level=ERROR",
		"status=500",
		"path=/api/v1/budget",
		"failed_to_load_month_budget",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("expected log to contain %q, got: %s", want, logged)
		}
	}
}

func TestLogServerErrorsSilentForSuccess(t *testing.T) {
	logger, out := newTestLogger()

	handler := chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}), logger)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/accounts", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("client status = %d, want 200", rec.Code)
	}
	if logged := out.String(); logged != "" {
		t.Errorf("expected no log output for a successful request, got: %s", logged)
	}
}

func TestLogServerErrorsSkipsProbes(t *testing.T) {
	logger, out := newTestLogger()

	handler := chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"degraded"}`))
	}), logger)

	for _, path := range []string{"/healthz", "/livez"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d, want 503", path, rec.Code)
		}
	}

	if logged := out.String(); logged != "" {
		t.Errorf("expected probe failures not to be logged, got: %s", logged)
	}
}

func TestLogServerErrorsCapturesPanicAsFiveHundred(t *testing.T) {
	logger, out := newTestLogger()

	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(errors.New("boom"))
	})
	handler = chimiddleware.Recoverer(handler)
	handler = server.LogServerErrors(logger)(handler)
	handler = chimiddleware.RequestID(handler)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/provision", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("client status = %d, want 500", rec.Code)
	}
	if logged := out.String(); !strings.Contains(logged, "path=/api/v1/workspaces/provision") {
		t.Errorf("expected panicked request to be logged, got: %s", logged)
	}
}

func TestLogServerErrorsTruncatesLargeBody(t *testing.T) {
	logger, out := newTestLogger()

	handler := chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("x", 4096)))
	}), logger)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/transactions", nil))

	if rec.Body.Len() != 4096 {
		t.Fatalf("client body length = %d, want the full 4096 bytes untruncated", rec.Body.Len())
	}

	logged := out.String()
	idx := strings.Index(logged, "response_body=")
	if idx < 0 {
		t.Fatalf("expected a response_body attribute in: %s", logged)
	}
	// The quoted body plus the trailing "msg=request failed" suffix must stay bounded.
	if len(logged)-idx > 1024 {
		t.Errorf("logged body not truncated: %d bytes after response_body=", len(logged)-idx)
	}
}

func TestStatusRecorderForwardsFlush(t *testing.T) {
	logger, _ := newTestLogger()

	flushed := false
	handler := chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Fatalf("wrapped writer lost http.Flusher")
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		flushed = true
	}), logger)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/bank/sync-now", nil))

	if !flushed {
		t.Error("expected Flush to reach the underlying writer")
	}
	if rec.Body.String() != "partial" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "partial")
	}
}

func TestStatusRecorderDoesNotBufferSuccessBodies(t *testing.T) {
	logger, _ := newTestLogger()

	payload := strings.Repeat("y", 4096)
	handler := chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}), logger)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/budget", nil))

	if rec.Body.String() != payload {
		t.Errorf("successful body was altered: length %d, want %d", rec.Body.Len(), len(payload))
	}
}
