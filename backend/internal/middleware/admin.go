package middleware

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const DefaultPlatformAdminGroup = "budget-admins"

// PlatformAdminConfig configures how RequirePlatformAdmin and IsPlatformAdmin
// identify platform operators. AdminEmails is intended for local Dex dev parity
// where staticPasswords do not carry a groups claim.
type PlatformAdminConfig struct {
	AdminGroup  string
	AdminEmails []string
}

func (c PlatformAdminConfig) normalizedGroup() string {
	if g := strings.TrimSpace(c.AdminGroup); g != "" {
		return g
	}
	return DefaultPlatformAdminGroup
}

// IsPlatformAdmin reports whether the authenticated caller belongs to the configured
// platform admin OIDC group or (when explicitly configured) matches AdminEmails.
func IsPlatformAdmin(ctx context.Context, cfg PlatformAdminConfig) bool {
	targetGroup := cfg.normalizedGroup()
	for _, g := range GetUserGroups(ctx) {
		if strings.TrimSpace(g) == targetGroup {
			return true
		}
	}

	if len(cfg.AdminEmails) > 0 {
		email := strings.ToLower(GetUserEmail(ctx))
		if email != "" {
			for _, allowed := range cfg.AdminEmails {
				if strings.EqualFold(strings.TrimSpace(allowed), email) {
					return true
				}
			}
		}
	}
	return false
}

// EnsureActivePlatformUser upserts the authenticated caller into platform_users on
// login (throttling last_login_at updates to every 5 minutes) and immediately rejects
// disabled accounts with 403 {"error": "account_disabled"}.
func EnsureActivePlatformUser(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if pool == nil {
				next.ServeHTTP(w, r)
				return
			}

			userID, err := GetUserID(r.Context())
			if err != nil {
				writeErrorJSON(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			email := strings.ToLower(GetUserEmail(r.Context()))
			displayName := GetUserName(r.Context())

			var isActive bool
			err = pool.QueryRow(r.Context(), `
				WITH resolved AS (
					SELECT
						$1::uuid AS user_id,
						CASE
							WHEN $2 <> '' AND NOT EXISTS (
								SELECT 1 FROM platform_users p WHERE LOWER(p.email) = LOWER($2) AND p.user_id <> $1::uuid
							) THEN LOWER($2)
							ELSE $1::text || '@unknown.local'
						END AS safe_email,
						$3::text AS safe_name
				)
				INSERT INTO platform_users (user_id, email, display_name, is_active, last_login_at)
				SELECT user_id, safe_email, safe_name, TRUE, NOW()
				FROM resolved
				ON CONFLICT (user_id) DO UPDATE
				SET email = CASE
						WHEN $2 <> '' AND NOT EXISTS (
							SELECT 1 FROM platform_users p2 WHERE LOWER(p2.email) = LOWER($2) AND p2.user_id <> platform_users.user_id
						) THEN LOWER($2)
						ELSE platform_users.email
					END,
					display_name = CASE
						WHEN $3 <> '' THEN $3
						ELSE platform_users.display_name
					END,
					last_login_at = CASE
						WHEN platform_users.last_login_at IS NULL OR platform_users.last_login_at < NOW() - INTERVAL '5 minutes'
						THEN NOW()
						ELSE platform_users.last_login_at
					END
				RETURNING is_active
			`, userID, email, displayName).Scan(&isActive)
			if err != nil {
				writeErrorJSON(w, http.StatusInternalServerError, "failed_to_sync_user")
				return
			}
			if !isActive {
				writeErrorJSON(w, http.StatusForbidden, "account_disabled")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequirePlatformAdmin verifies that the authenticated JWT carries the configured admin
// group (default: "budget-admins") in its `groups` claim (or matches AdminEmails for
// local dev parity) and that the user is marked is_active = true in platform_users.
func RequirePlatformAdmin(pool *pgxpool.Pool, cfg PlatformAdminConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := GetUserID(r.Context())
			if err != nil || userID == uuid.Nil {
				writeErrorJSON(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			if !IsPlatformAdmin(r.Context(), cfg) {
				writeErrorJSON(w, http.StatusForbidden, "platform_admin_required")
				return
			}
			if pool != nil {
				var isActive bool
				err := pool.QueryRow(r.Context(), `
					SELECT is_active FROM platform_users WHERE user_id = $1
				`, userID).Scan(&isActive)
				if err == nil && !isActive {
					writeErrorJSON(w, http.StatusForbidden, "account_disabled")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// LoadPlatformAdminConfigFromEnv loads PLATFORM_ADMIN_GROUP and PLATFORM_ADMIN_EMAILS.
func LoadPlatformAdminConfigFromEnv() PlatformAdminConfig {
	group := strings.TrimSpace(os.Getenv("PLATFORM_ADMIN_GROUP"))
	if group == "" {
		group = DefaultPlatformAdminGroup
	}
	var emails []string
	if raw := strings.TrimSpace(os.Getenv("PLATFORM_ADMIN_EMAILS")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if trimmed := strings.ToLower(strings.TrimSpace(part)); trimmed != "" {
				emails = append(emails, trimmed)
			}
		}
	}
	return PlatformAdminConfig{
		AdminGroup:  group,
		AdminEmails: emails,
	}
}
