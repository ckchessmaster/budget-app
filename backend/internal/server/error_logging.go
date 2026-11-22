package server

import (
	"log/slog"
	"net/http"
	"strings"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// maxLoggedErrorBody bounds how much of an error response is copied into the log.
const maxLoggedErrorBody = 512

// probePaths are polled continuously by the kubelet. They already report their own check
// results, and a degraded dependency would otherwise bury real failures in probe noise.
var probePaths = map[string]bool{
	"/healthz": true,
	"/livez":   true,
}

// statusRecorder records the response status and, for server errors only, a short prefix
// of the response body.
type statusRecorder struct {
	http.ResponseWriter
	status    int
	errorBody []byte
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status >= http.StatusInternalServerError && len(r.errorBody) < maxLoggedErrorBody {
		room := maxLoggedErrorBody - len(r.errorBody)
		if len(p) < room {
			room = len(p)
		}
		r.errorBody = append(r.errorBody, p[:room]...)
	}
	return r.ResponseWriter.Write(p)
}

// Flush forwards to the wrapped writer when it supports flushing, so streaming responses
// keep working through the recorder.
func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// LogServerErrors logs every 5xx response together with the error payload the client was
// handed. Handlers report failures to the caller but discard the underlying cause, so
// without this the access log shows only a bare status line for a failing request.
func LogServerErrors(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(recorder, r)

			if recorder.status < http.StatusInternalServerError {
				return
			}
			if probePaths[r.URL.Path] {
				return
			}

			attrs := []any{
				"status", recorder.status,
				"method", r.Method,
				"path", r.URL.Path,
				"request_id", chimiddleware.GetReqID(r.Context()),
			}
			if len(recorder.errorBody) > 0 {
				attrs = append(attrs, "response_body", strings.TrimSpace(string(recorder.errorBody)))
			}
			logger.Error("request failed", attrs...)
		})
	}
}
