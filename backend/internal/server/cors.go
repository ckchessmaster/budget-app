package server

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// CORS defaults for preflight responses.
const (
	corsMaxAge = "600"

	corsMethodGET     = "GET"
	corsMethodPOST    = "POST"
	corsMethodPUT     = "PUT"
	corsMethodPATCH   = "PATCH"
	corsMethodDELETE  = "DELETE"
	corsMethodOPTIONS = "OPTIONS"
	corsMethodHEAD    = "HEAD"
)

// CORSPolicy is an allowlist of browser origins that may call the API from
// another site. Requests without an Origin header (curl, Kubernetes probes, and
// same-origin traffic proxied by the frontend's own nginx) are never touched.
//
// An empty policy means "send no CORS headers at all", which fails closed: the
// browser blocks cross-site calls, while same-origin and server-to-server
// traffic keeps working.
type CORSPolicy struct {
	allowedOrigins map[string]struct{}
	allowedMethods map[string]struct{}
	allowedHeaders map[string]struct{}
	methodList     string
}

// NewCORSPolicy builds a policy from raw origin strings, dropping malformed or
// wildcard entries rather than silently broadening access.
func NewCORSPolicy(origins []string) *CORSPolicy {
	p := &CORSPolicy{
		allowedOrigins: make(map[string]struct{}),
		allowedMethods: make(map[string]struct{}),
		allowedHeaders: make(map[string]struct{}),
	}

	for _, raw := range origins {
		normalized, ok := normalizeOrigin(raw)
		if !ok {
			if trimmed := strings.TrimSpace(raw); trimmed != "" {
				slog.Warn("ignoring invalid CORS origin; expected a scheme and host with no path", "origin", trimmed)
			}
			continue
		}
		p.allowedOrigins[normalized] = struct{}{}
	}

	for _, method := range []string{corsMethodGET, corsMethodPOST, corsMethodPUT, corsMethodPATCH, corsMethodDELETE, corsMethodOPTIONS, corsMethodHEAD} {
		p.allowedMethods[method] = struct{}{}
	}
	p.methodList = strings.Join([]string{corsMethodGET, corsMethodPOST, corsMethodPUT, corsMethodPATCH, corsMethodDELETE, corsMethodHEAD, corsMethodOPTIONS}, ", ")

	// The frontend only ever sends a bearer token and a JSON body on top of Accept.
	for _, header := range []string{"accept", "accept-language", "authorization", "content-type", "x-csrf-token"} {
		p.allowedHeaders[header] = struct{}{}
	}

	return p
}

// Empty reports whether the policy allows no origins at all.
func (p *CORSPolicy) Empty() bool {
	return p == nil || len(p.allowedOrigins) == 0
}

// Allowed reports whether origin is on the allowlist.
func (p *CORSPolicy) Allowed(origin string) bool {
	if p == nil {
		return false
	}
	normalized, ok := normalizeOrigin(origin)
	if !ok {
		return false
	}
	_, ok = p.allowedOrigins[normalized]
	return ok
}

// CORSMiddleware answers preflight requests for allowlisted origins and mirrors
// the request's own origin on real responses. Because the API is reachable with
// a bearer token, credentialed CORS is never granted and the origin is never
// echoed back unless it is allowlisted.
func CORSMiddleware(policy *CORSPolicy) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin == "" || policy.Empty() {
				next.ServeHTTP(w, r)
				return
			}

			// Any response here depends on the Origin header, so caches must not
			// reuse one origin's answer for another.
			w.Header().Add("Vary", "Origin")

			if !policy.Allowed(origin) {
				next.ServeHTTP(w, r)
				return
			}

			header := w.Header()
			header.Set("Access-Control-Allow-Origin", origin)

			if !isPreflight(r) {
				next.ServeHTTP(w, r)
				return
			}

			requestedMethod := strings.ToUpper(strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")))
			requestedHeaders, ok := policy.requestHeaders(r.Header.Get("Access-Control-Request-Headers"))
			if _, methodOK := policy.allowedMethods[requestedMethod]; !ok || !methodOK {
				// Not a preflight this API grants: let the router answer normally and
				// withhold the grant so the browser blocks the call.
				header.Del("Access-Control-Allow-Origin")
				next.ServeHTTP(w, r)
				return
			}

			header.Set("Access-Control-Allow-Methods", requestedMethod)
			if requestedHeaders != "" {
				header.Set("Access-Control-Allow-Headers", requestedHeaders)
			}
			header.Set("Access-Control-Max-Age", corsMaxAge)
			w.WriteHeader(http.StatusNoContent)
		})
	}
}

func isPreflight(r *http.Request) bool {
	return r.Method == http.MethodOptions && strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")) != ""
}

// requestHeaders echoes the requested headers when every one of them is allowed,
// and reports false when any of them is not.
func (p *CORSPolicy) requestHeaders(raw string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", true
	}

	granted := make([]string, 0, 4)
	for _, header := range strings.Split(raw, ",") {
		normalized := strings.ToLower(strings.TrimSpace(header))
		if normalized == "" {
			continue
		}
		if _, ok := p.allowedHeaders[normalized]; !ok {
			return "", false
		}
		granted = append(granted, normalized)
	}
	if len(granted) == 0 {
		return "", true
	}
	return strings.Join(granted, ", "), true
}

// normalizeOrigin lowercases an origin and validates that it is only a scheme and
// host. Wildcards, paths, and bare hosts are rejected: an exact match is the only
// safe grant for a token-authenticated API.
func normalizeOrigin(raw string) (string, bool) {
	origin := strings.ToLower(strings.TrimSpace(raw))
	origin = strings.TrimSuffix(origin, "/")
	if origin == "" || !strings.Contains(origin, "://") {
		return "", false
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return "", false
	}
	if strings.Trim(parsed.Path, "/") != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	if strings.Contains(origin, "*") {
		return "", false
	}

	return origin, true
}

// parseCORSAllowedOrigins splits a comma or space separated origin list.
func parseCORSAllowedOrigins(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})

	origins := make([]string, 0, len(fields))
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}
