# Feature Specification: Platform Admin Interface & Invite-Only Friend Onboarding

## 1. Overview & Architectural Decisions

### 1.1 Architecture Topology: Integrated App with Strict Route & Data Isolation
The Platform Admin capability is built directly into the existing **Go API / Temporal Worker binary** and **SvelteKit PWA**:
- **Backend Boundary (`/api/v1/admin/*`):** Guarded by `RequirePlatformAdmin` middleware that validates the Authentik OIDC JWT `groups` claim for `PLATFORM_ADMIN_GROUP` (default `budget-admins`), with an explicit `PLATFORM_ADMIN_EMAILS` fallback for local Dex development (`admin@dev.local`). `EnsureActivePlatformUser` upserts `platform_users` on authenticated logins (throttled `last_login_at` updates) and immediately rejects disabled accounts (`is_active = false`) with `403 {"error": "account_disabled"}`.
- **Privacy-By-Design under `FORCE RLS`:** Tenant ledger RLS on `workspaces`, `accounts`, `transactions`, `categories`, and `receipts` remains untouched. Cross-tenant admin metadata queries run inside `db.WithPlatformAdminTx` (`app.platform_admin_mode = 'true'`) exclusively against `admin_workspace_overview` (excluding `is_demo = TRUE`) and `SECURITY DEFINER` functions (`admin_list_users`, `admin_delete_user`).
- **Frontend Boundary (`frontend/src/routes/admin/*`):** SvelteKit code-splits route bundles; `/admin/+layout.svelte` checks `auth.isPlatformAdmin` (backed by `/api/v1/auth/me`) and redirects non-admins to `/`. Navigation surfaces a subtle Admin shield icon next to Settings only when `auth.isPlatformAdmin === true`.

### 1.2 Invite-Only Friend Onboarding (Open Single-Use Tokens)
- **Single-Use Platform Invite Links:** An admin generates a single-use invite token (optional label, configurable expiration defaulting to 7 days, and optional email address). When an email is provided, the raw token is sealed with `crypto.Vault` and dispatched via Temporal `SendPlatformInviteWorkflow` without placing secrets in Temporal history. The invite remains an open single-use link and pre-fills the email input on `/signup`.
- **Atomic Redemption & Authentik Provisioning:** `/api/v1/signup/redeem` validates and consumes the token inside a `SERIALIZABLE` transaction (`FOR UPDATE`), calls `idp.PlatformIdentityAdmin.CreateUserAndRecoveryLink` (`/api/v3/core/users/` + `/api/v3/core/users/{id}/recovery/`), inserts `platform_users`, marks the invite `REDEEMED`, and returns `setup_url`. Duplicate email/username conflicts return `409` without consuming the token.
- **Workspace Limit Preservation:** Existing Day 0 workspace provisioning (`idx_unique_owner_per_user`) applies unchanged upon first login.

### 1.3 User Lifecycle, Metadata-Only Workspace Oversight & System Health
- **User Lifecycle Management:** View `platform_users` with owned/member non-demo workspace counts, disable/re-enable accounts (syncing `is_active` and revoking Authentik `authenticated_sessions`), and permanently delete users (fail-closed Authentik deletion + cascading cleanup of owned workspaces and shared memberships via `admin_delete_user`). Self-disable and self-delete are blocked (`400 Bad Request`).
- **Metadata-Only Workspace Oversight:** View non-demo workspace metadata (`admin_workspace_overview`: workspace name, owner email, member count, SimpleFIN status/last sync/sanitized error).
- **System Health Telemetry (`GET /api/v1/admin/health`):** Reports PostgreSQL & Temporal ping + latency, stale (>12h) or errored non-demo SimpleFIN connections, 24h failed Temporal workflows (`BankSyncWorkflow`, `ReceiptReconcileWorkflow`, invite email workflows) via Temporal Visibility, and 24h `outbound_email_logs` counts (`sent` vs `failed`).

## 2. Database Migration & Implemented Components
- **Migration:** `backend/migrations/000020_platform_admin.{up,down}.sql`
- **Backend Packages:**
  - `backend/pkg/db/tenant_tx.go` (`BeginPlatformAdminTx`, `BeginPlatformAdminSerializableTx`, `WithPlatformAdminTx`, `BeginPlatformInviteTokenTx`)
  - `backend/internal/middleware/{oidc_verifier,auth,admin}.go`
  - `backend/internal/idp/authentik.go` (`PlatformIdentityAdmin`)
  - `backend/internal/email/resend.go` (`LoggingSender`, `RenderPlatformInviteEmail`)
  - `backend/internal/temporal/{activities,workflows,schedules}/platform_invite_email.go`, `activities/sync_status.go`, `workflows/bank_sync.go`
  - `backend/internal/admin/{routes,admin,signup,health}.go`
  - `backend/internal/server/server.go`
- **Frontend:**
  - `frontend/src/lib/auth/oidc.ts`, `frontend/src/lib/stores/auth.svelte.ts`, `frontend/src/lib/api/admin.ts`
  - `frontend/src/routes/signup/+page.svelte`
  - `frontend/src/routes/admin/{+layout,+page}.svelte`
  - `frontend/src/routes/+layout.svelte`, `frontend/src/lib/components/budget/BudgetHeader.svelte`
