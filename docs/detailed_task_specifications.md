# Budget App: Agent-Ready Task Specifications & Engineering Breakdown

This document provides the exhaustive, zero-ambiguity task breakdown for the Budget App project. Every task contains explicit repository file boundaries, data contracts, API payloads, mathematical business rules, mock fixtures, and deterministic verification commands designed for direct execution by software engineers or autonomous coding agents.

## Specification Standards

Every task ticket adheres to the following five-part contract:

1. **Target Files**: Explicit file paths to create, modify, or test.
2. **Dependencies**: Prerequisite task IDs that must be completed and passing tests.
3. **Data Contracts & Interfaces**: Go structs, TypeScript interfaces, SQL DDL, or JSON schemas.
4. **Business Logic & Edge Cases**: Algorithmic rules, mathematical constraints, and error boundaries.
5. **Acceptance Criteria & Verification**: Deterministic CLI commands, unit tests, or integration tests with specific pass/fail criteria.

---

# Milestone 1: Local Foundation & Core Scaffold

## Epic M1.1: Local Orchestration with Microsoft Aspire

### `TASK-1.1.1`: Scaffold Aspire TypeScript AppHost Workspace
* **Target Files**:
  * `apphost/package.json`
  * `apphost/tsconfig.json`
  * `apphost/apphost.mts`
  * `package.json` (root workspace scripts)
* **Dependencies**: None
* **Data Contracts & Interfaces**:
  ```json
  {
    "name": "budget-apphost",
    "version": "0.1.0",
    "type": "module",
    "scripts": {
      "build": "tsc",
      "dev": "aspire run"
    },
    "dependencies": {
      "@microsoft/aspire-cli": "latest"
    },
    "devDependencies": {
      "typescript": "^5.5.0",
      "@types/node": "^20.0.0"
    }
  }
  ```
* **Business Logic & Edge Cases**:
  * Initialize an ES-Module TypeScript workspace configured for Node 20+.
  * Root `package.json` must provide proxy scripts: `npm run dev:aspire` pointing to `npm --prefix apphost run dev`.
  * TypeScript must target `ESNext` and module resolution `NodeNext`.
* **Acceptance Criteria & Verification**:
  * Run: `npm --prefix apphost run build`
  * Assert: Exit code `0` with zero diagnostic errors.
  * Run: `npx @microsoft/aspire-cli --version` prints a valid semver string.

### `TASK-1.1.2`: Configure PostgreSQL 16 Resource in AppHost
* **Target Files**:
  * `apphost/apphost.mts`
* **Dependencies**: `TASK-1.1.1`
* **Data Contracts & Interfaces**:
  ```typescript
  import { createBuilder } from './.aspire/modules/aspire.mjs';
  // Exposes resource references:
  // postgres: Host resource with named volume 'budget-db-data'
  // budgetDb: Database resource 'budgetdb' on port 5432
  ```
* **Business Logic & Edge Cases**:
  * Define Postgres 16 container with local named volume `budget-local-db`.
  * Database name: `budgetdb`, default superuser: `postgres`, local dev password: `postgrespassword`.
  * Bind container port `5432` to host port `5432` if unallocated; otherwise allow Aspire dynamic port assignment and export `DATABASE_URL`.
* **Acceptance Criteria & Verification**:
  * Command: `npm --prefix apphost run dev &`
  * Verify running container: `docker ps --filter "name=postgres" --format "{{.Status}}"` contains `Up`.
  * Verification query: `PGPASSWORD=postgrespassword psql -h localhost -p 5432 -U postgres -d budgetdb -c "SELECT 1;"` returns `1`.

### `TASK-1.1.3`: Configure Dex OIDC Dev Container in AppHost
* **Target Files**:
  * `apphost/apphost.mts`
  * `dex/config.yaml`
* **Dependencies**: `TASK-1.1.1`
* **Data Contracts & Interfaces**:
  ```yaml
  issuer: http://localhost:5556/dex
  storage:
    type: memory
  web:
    http: 0.0.0.0:5556
  staticClients:
    - id: budget-app-client
      redirectURIs:
        - 'http://localhost:5173/auth/callback'
      name: 'Local Dev Budget App'
      public: true
  enablePasswordDB: true
  staticPasswords:
    - email: "admin@dev.local"
      hash: "$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4W" # password
      username: "admin"
      userID: "00000000-0000-0000-0000-000000000001"
  ```
* **Business Logic & Edge Cases**:
  * Mount `dex/config.yaml` to `/etc/dex/config.docker.yaml` inside `ghcr.io/dexidp/dex:latest`.
  * Expose port `5556`.
  * Pass container arguments: `dex serve /etc/dex/config.docker.yaml`.
* **Acceptance Criteria & Verification**:
  * Run: `curl -s http://localhost:5556/dex/.well-known/openid-configuration | jq .issuer`
  * Assert output: `"http://localhost:5556/dex"`
  * Assert keys endpoint returns HTTP 200: `curl -I -s http://localhost:5556/dex/keys | grep "HTTP/1.1 200 OK"`

### `TASK-1.1.4`: Configure Temporal Dev Server Container in AppHost
* **Target Files**:
  * `apphost/apphost.mts`
* **Dependencies**: `TASK-1.1.1`
* **Data Contracts & Interfaces**:
  ```typescript
  const temporal = await builder
    .addContainer('temporal', 'temporalio/dev-server:latest')
    .withEndpoint({ port: 7233, targetPort: 7233, name: 'grpc' })
    .withHttpEndpoint({ port: 8233, targetPort: 8233, name: 'ui' });
  ```
* **Business Logic & Edge Cases**:
  * Expose gRPC port `7233` for worker client connections.
  * Expose HTTP port `8233` for the Temporal Web UI.
  * Set environment variable `TEMPORAL_DEV_SERVER_MEM_STORE=true` for local fast reset.
* **Acceptance Criteria & Verification**:
  * Command: `curl -I -s http://localhost:8233 | grep "HTTP/1.1 200 OK"`
  * Command: `docker run --rm --network host temporalio/admin-tools:latest temporal workflow list --address localhost:7233`
  * Assert: Command exits with status 0 without network connection errors.

---

## Epic M1.2: PostgreSQL Database Engine & RLS Schema

### `TASK-1.2.1`: Implement Go Migration Runner
* **Target Files**:
  * `backend/pkg/db/migrate.go`
  * `backend/cmd/migrate/main.go`
  * `backend/migrations/000001_initial_schema.up.sql`
  * `backend/migrations/000001_initial_schema.down.sql`
* **Dependencies**: `TASK-1.1.2`
* **Data Contracts & Interfaces**:
  ```go
  package db

  import "embed"

  //go:embed migrations/*.sql
  var MigrationFS embed.FS

  type Migrator interface {
      Up() error
      Down() error
      Version() (uint, bool, error)
  }
  func NewMigrator(databaseURL string) (Migrator, error)
  ```
* **Business Logic & Edge Cases**:
  * Embed SQL migration scripts directly into the Go binary using `embed.FS`.
  * Use `golang-migrate/migrate/v4` with the `pgx/v5` driver.
  * Implement graceful schema locking and lock timeout handling (abort after 15s if migration lock cannot be acquired).
* **Acceptance Criteria & Verification**:
  * Command: `go run backend/cmd/migrate/main.go up`
  * Check database: `psql -d budgetdb -c "SELECT version, dirty FROM schema_migrations;"`
  * Assert: `dirty` is `false`, `version` is `1`.

### `TASK-1.2.2`: Create Tenancy, Accounts, Categories, Incomes, Settings, & Transactions DDL
* **Target Files**:
  * `backend/migrations/000001_initial_schema.up.sql`
* **Dependencies**: `TASK-1.2.1`
* **Data Contracts & Interfaces**:
  ```sql
  CREATE EXTENSION IF NOT EXISTS "pgcrypto";

  CREATE TYPE account_type AS ENUM ('checking', 'savings', 'credit_card', 'investment', 'loan');
  CREATE TYPE cc_mode AS ENUM ('PAY_IN_FULL', 'DEBT_PAYDOWN');
  CREATE TYPE rollover_type AS ENUM ('SWEEP', 'ROLLOVER_BALANCE', 'TARGET_CAP');
  CREATE TYPE goal_cadence AS ENUM ('NONE', 'MONTHLY', 'ANNUAL', 'TARGET_DATE');
  CREATE TYPE transaction_status AS ENUM ('PENDING', 'POSTED');
  CREATE TYPE transaction_origin AS ENUM ('BANK_FEED', 'MANUAL_OFFLINE', 'RECURRING');
  CREATE TYPE receipt_status AS ENUM ('UNMATCHED', 'MATCHED_SINGLE', 'MATCHED_MULTI', 'ORPHANED', 'MANUAL', 'HANDLED_SYSTEM', 'DISMISSED');

  CREATE TABLE workspaces (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      name VARCHAR(255) NOT NULL,
      created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
  );

  CREATE TABLE workspace_members (
      workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      user_id UUID NOT NULL,
      role VARCHAR(50) NOT NULL DEFAULT 'owner',
      created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
      PRIMARY KEY (workspace_id, user_id)
  );

  CREATE TABLE workspace_settings (
      workspace_id UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
      gmail_verification_code VARCHAR(50),
      inbound_receipt_alias VARCHAR(100) UNIQUE NOT NULL,
      created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
      updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
  );

  CREATE TABLE accounts (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      external_account_id VARCHAR(255),
      name VARCHAR(150) NOT NULL,
      type account_type NOT NULL,
      current_balance NUMERIC(12, 2) NOT NULL DEFAULT 0.00,
      is_on_budget BOOLEAN NOT NULL DEFAULT TRUE,
      is_credit_card BOOLEAN GENERATED ALWAYS AS (type = 'credit_card') STORED,
      card_mode cc_mode DEFAULT 'PAY_IN_FULL',
      initial_debt_baseline NUMERIC(12, 2) DEFAULT 0.00,
      apr_percentage NUMERIC(5, 2) DEFAULT 0.00,
      statement_due_day SMALLINT CHECK (statement_due_day BETWEEN 1 AND 31),
      debt_payoff_category_id UUID,
      created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
      updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
  );

  CREATE TABLE categories (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      name VARCHAR(100) NOT NULL,
      group_name VARCHAR(100) NOT NULL,
      monthly_budget NUMERIC(12, 2) NOT NULL DEFAULT 0.00,
      rollover_mode rollover_type NOT NULL DEFAULT 'SWEEP',
      sweep_target_category_id UUID REFERENCES categories(id) ON DELETE SET NULL,
      target_amount NUMERIC(12, 2) DEFAULT NULL,
      target_date DATE DEFAULT NULL,
      target_cadence goal_cadence NOT NULL DEFAULT 'NONE',
      sort_order INT NOT NULL DEFAULT 0,
      created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
  );

  ALTER TABLE accounts ADD CONSTRAINT fk_accounts_debt_category
      FOREIGN KEY (debt_payoff_category_id) REFERENCES categories(id) ON DELETE SET NULL;

  CREATE TABLE monthly_category_budgets (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      category_id UUID NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
      year_month DATE NOT NULL,
      original_planned NUMERIC(12, 2) NOT NULL DEFAULT 0.00,
      created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
      UNIQUE(workspace_id, category_id, year_month)
  );

  CREATE TABLE monthly_planned_incomes (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      year_month DATE NOT NULL,
      name VARCHAR(150) NOT NULL,
      amount NUMERIC(12, 2) NOT NULL CHECK (amount > 0),
      created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
  );
  CREATE INDEX idx_planned_incomes_workspace_month ON monthly_planned_incomes(workspace_id, year_month);

  CREATE TABLE budget_reallocations (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      year_month DATE NOT NULL,
      from_category_id UUID NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
      to_category_id UUID NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
      amount NUMERIC(12, 2) NOT NULL CHECK (amount > 0),
      reason TEXT,
      created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
  );

  CREATE TABLE transactions (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
      parent_transaction_id UUID REFERENCES transactions(id) ON DELETE CASCADE,
      external_transaction_id VARCHAR(255),
      origin transaction_origin NOT NULL DEFAULT 'BANK_FEED',
      status transaction_status NOT NULL DEFAULT 'POSTED',
      amount NUMERIC(12, 2) NOT NULL,
      merchant_raw VARCHAR(255) NOT NULL,
      merchant_clean VARCHAR(255),
      category_id UUID REFERENCES categories(id) ON DELETE SET NULL,
      transfer_linked_transaction_id UUID REFERENCES transactions(id) ON DELETE SET NULL,
      is_transfer BOOLEAN GENERATED ALWAYS AS (transfer_linked_transaction_id IS NOT NULL) STORED,
      transaction_date DATE NOT NULL,
      user_notes TEXT,
      is_cleared BOOLEAN NOT NULL DEFAULT TRUE,
      created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
  );

  CREATE TABLE receipts (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
      sender_email VARCHAR(255) NOT NULL,
      order_id VARCHAR(100),
      merchant_name VARCHAR(150) NOT NULL,
      total_amount NUMERIC(12, 2) NOT NULL,
      receipt_date DATE NOT NULL,
      raw_html TEXT,
      status receipt_status NOT NULL DEFAULT 'UNMATCHED',
      created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
  );

  CREATE TABLE receipt_line_items (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      receipt_id UUID NOT NULL REFERENCES receipts(id) ON DELETE CASCADE,
      description VARCHAR(255) NOT NULL,
      amount NUMERIC(12, 2) NOT NULL,
      suggested_category_id UUID REFERENCES categories(id) ON DELETE SET NULL,
      created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
  );

  CREATE TABLE transaction_receipt_links (
      transaction_id UUID NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
      receipt_id UUID NOT NULL REFERENCES receipts(id) ON DELETE CASCADE,
      PRIMARY KEY (transaction_id, receipt_id)
  );

  CREATE TABLE push_subscriptions (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      user_id UUID NOT NULL,
      endpoint TEXT NOT NULL UNIQUE,
      p256dh TEXT NOT NULL,
      auth TEXT NOT NULL,
      created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
  );

  CREATE TABLE simplefin_connections (
      workspace_id UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
      encrypted_claim_token TEXT NOT NULL,
      encrypted_access_url TEXT,
      last_synced_at TIMESTAMP WITH TIME ZONE,
      created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
  );
  ```
* **Business Logic & Edge Cases**:
  * Monetary fields MUST be `NUMERIC(12, 2)`. Floats are strictly prohibited.
  * `amount` on transactions: Negative represents outflow (expenses, debits); positive represents inflow (paychecks, refunds).
  * Generated columns: `is_credit_card` and `is_transfer` must be `STORED` to allow index creation.
* **Acceptance Criteria & Verification**:
  * Execute migration: `go run backend/cmd/migrate/main.go up`
  * Run schema inspection: `psql -d budgetdb -c "\dt"`
  * Assert all 13 tables exist: `workspaces`, `workspace_members`, `workspace_settings`, `accounts`, `categories`, `monthly_category_budgets`, `monthly_planned_incomes`, `budget_reallocations`, `transactions`, `receipts`, `receipt_line_items`, `transaction_receipt_links`, `push_subscriptions`, `simplefin_connections`.

### `TASK-1.2.3`: Apply Row-Level Security (RLS) Isolation Policies
* **Target Files**:
  * `backend/migrations/000002_enable_rls.up.sql`
  * `backend/migrations/000002_enable_rls.down.sql`
  * `backend/pkg/db/rls_test.go`
* **Dependencies**: `TASK-1.2.2`
* **Data Contracts & Interfaces**:
  ```sql
  ALTER TABLE workspaces ENABLE ROW LEVEL SECURITY;
  ALTER TABLE workspace_members ENABLE ROW LEVEL SECURITY;
  ALTER TABLE workspace_settings ENABLE ROW LEVEL SECURITY;
  ALTER TABLE accounts ENABLE ROW LEVEL SECURITY;
  ALTER TABLE categories ENABLE ROW LEVEL SECURITY;
  ALTER TABLE monthly_category_budgets ENABLE ROW LEVEL SECURITY;
  ALTER TABLE monthly_planned_incomes ENABLE ROW LEVEL SECURITY;
  ALTER TABLE budget_reallocations ENABLE ROW LEVEL SECURITY;
  ALTER TABLE transactions ENABLE ROW LEVEL SECURITY;
  ALTER TABLE receipts ENABLE ROW LEVEL SECURITY;
  ALTER TABLE simplefin_connections ENABLE ROW LEVEL SECURITY;

  CREATE OR REPLACE FUNCTION current_app_user_id() RETURNS UUID AS $$
      SELECT NULLIF(current_setting('app.current_user_id', true), '')::UUID;
  $$ LANGUAGE SQL STABLE;

  CREATE POLICY workspace_isolation ON workspaces FOR ALL USING (
      id IN (SELECT workspace_id FROM workspace_members WHERE user_id = current_app_user_id())
  );
  CREATE POLICY members_isolation ON workspace_members FOR ALL USING (
      workspace_id IN (SELECT workspace_id FROM workspace_members WHERE user_id = current_app_user_id())
  );
  CREATE POLICY settings_isolation ON workspace_settings FOR ALL USING (
      workspace_id IN (SELECT id FROM workspaces)
  );
  CREATE POLICY accounts_isolation ON accounts FOR ALL USING (
      workspace_id IN (SELECT id FROM workspaces)
  );
  CREATE POLICY categories_isolation ON categories FOR ALL USING (
      workspace_id IN (SELECT id FROM workspaces)
  );
  CREATE POLICY budgets_isolation ON monthly_category_budgets FOR ALL USING (
      workspace_id IN (SELECT id FROM workspaces)
  );
  CREATE POLICY incomes_isolation ON monthly_planned_incomes FOR ALL USING (
      workspace_id IN (SELECT id FROM workspaces)
  );
  CREATE POLICY reallocations_isolation ON budget_reallocations FOR ALL USING (
      workspace_id IN (SELECT id FROM workspaces)
  );
  CREATE POLICY transactions_isolation ON transactions FOR ALL USING (
      workspace_id IN (SELECT id FROM workspaces)
  );
  CREATE POLICY receipts_isolation ON receipts FOR ALL USING (
      workspace_id IN (SELECT id FROM workspaces)
  );
  CREATE POLICY simplefin_isolation ON simplefin_connections FOR ALL USING (
      workspace_id IN (SELECT id FROM workspaces)
  );
  ```
* **Business Logic & Edge Cases**:
  * If `app.current_user_id` is empty or unset, `current_app_user_id()` returns `NULL`, causing all queries to return zero rows.
  * Application queries must always run under an unprivileged role (`budget_app_user`).
* **Acceptance Criteria & Verification**:
  * Automated Go test `backend/pkg/db/rls_test.go`:
    1. Create Workspace W1 with User U1. Create Workspace W2 with User U2.
    2. Insert Category C1 and Income I1 into W1; insert C2 and I2 into W2.
    3. Open connection, execute `SET LOCAL app.current_user_id = '<U1_UUID>'`.
    4. Assert: `SELECT * FROM categories;` returns only C1.
    5. Assert: `SELECT * FROM monthly_planned_incomes;` returns only I1.
  * Run command: `go test -v ./backend/pkg/db -run TestRLSIsolation`
  * Assert: Test passes.

---

## Epic M1.3: Go Backend API Scaffold, Auth Bridge, & Provisioning

### `TASK-1.3.1`: Initialize Go Module & Chi HTTP Server
* **Target Files**:
  * `backend/go.mod`
  * `backend/go.sum`
  * `backend/cmd/api/main.go`
  * `backend/internal/server/server.go`
  * `backend/internal/server/health.go`
* **Dependencies**: None
* **Data Contracts & Interfaces**:
  ```go
  type HealthResponse struct {
      Status    string            `json:"status"`
      Timestamp string            `json:"timestamp"`
      Checks    map[string]string `json:"checks"`
  }
  ```
* **Business Logic & Edge Cases**:
  * Use `go-chi/chi/v5` router with standard middleware: `RequestID`, `RealIP`, `Logger`, `Recoverer`.
  * Graceful shutdown: listen for `SIGINT` and `SIGTERM`, allow 10-second timeout for in-flight requests.
  * Database pool initialized via `pgxpool.New(ctx, databaseURL)` with max connections configured from `DB_MAX_CONNS` (default 25).
* **Acceptance Criteria & Verification**:
  * Run server: `go run backend/cmd/api/main.go`
  * Command: `curl -s http://localhost:8080/healthz | jq .`
  * Assert JSON response: `{ "status": "ok", "checks": { "postgres": "healthy" } }`

### `TASK-1.3.2`: Implement Generic OIDC JWT Validation Middleware
* **Target Files**:
  * `backend/internal/middleware/auth.go`
  * `backend/internal/middleware/auth_test.go`
* **Dependencies**: `TASK-1.3.1`, `TASK-1.1.3`
* **Data Contracts & Interfaces**:
  ```go
  package middleware

  import (
      "context"
      "net/http"
      "github.com/google/uuid"
  )

  type ContextKey string
  const UserIDKey ContextKey = "current_user_id"

  type OIDCVerifier interface {
      Verify(ctx context.Context, rawToken string) (string, error)
  }

  func AuthMiddleware(verifier OIDCVerifier) func(http.Handler) http.Handler
  func GetUserID(ctx context.Context) (uuid.UUID, error)
  ```
* **Business Logic & Edge Cases**:
  * Intercept `Authorization: Bearer <token>`.
  * Return HTTP `401 Unauthorized` with JSON `{"error": "missing_token"}` if header is absent or malformed.
  * Call `verifier.Verify(ctx, token)`. If expired or invalid signature, return HTTP `401` with `{"error": "invalid_token"}`.
  * Extract `idToken.Subject`, parse as UUID, and store into request context.
* **Acceptance Criteria & Verification**:
  * Run unit tests: `go test -v ./backend/internal/middleware -run TestAuthMiddleware`
  * Test Case 1: Missing auth header returns 401.
  * Test Case 2: Expired token returns 401.
  * Test Case 3: Valid token signed by Dex returns 200 with extracted UUID in context.

### `TASK-1.3.3`: Implement Database Transaction RLS Wrapper
* **Target Files**:
  * `backend/pkg/db/tenant_tx.go`
  * `backend/pkg/db/tenant_tx_test.go`
* **Dependencies**: `TASK-1.2.3`, `TASK-1.3.2`
* **Data Contracts & Interfaces**:
  ```go
  package db

  import (
      "context"
      "github.com/google/uuid"
      "github.com/jackc/pgx/v5"
      "github.com/jackc/pgx/v5/pgxpool"
  )

  type TxFunc func(tx pgx.Tx) error
  func WithWorkspaceTx(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, fn TxFunc) error
  ```
* **Business Logic & Edge Cases**:
  * Begin transaction: `tx, err := pool.Begin(ctx)`.
  * Defer `tx.Rollback(ctx)`.
  * Inject RLS: `_, err := tx.Exec(ctx, "SET LOCAL app.current_user_id = $1", userID.String())`.
  * If `SET LOCAL` fails, return error immediately.
  * Execute `fn(tx)`. If `fn` returns error, deferred rollback triggers; otherwise commit.
* **Acceptance Criteria & Verification**:
  * Test `TestWithWorkspaceTx_RollbackOnPanicAndError`: Insert category and return error; outer query confirms category does not exist.
  * Run: `go test -v ./backend/pkg/db -run TestWithWorkspaceTx`
  * Assert: PASS.

### `TASK-1.3.4`: Workspace Provisioning & Seed Factory (Day 0 Onboarding)
* **Target Files**:
  * `backend/internal/workspace/provision.go`
  * `backend/internal/workspace/provision_test.go`
  * `backend/internal/workspace/seed_categories.go`
  * `backend/internal/server/server.go`
* **Dependencies**: `TASK-1.3.3`
* **Data Contracts & Interfaces**:
  ```go
  package workspace

  import (
      "context"
      "github.com/google/uuid"
      "github.com/jackc/pgx/v5/pgxpool"
  )

  type ProvisionResult struct {
      WorkspaceID        uuid.UUID `json:"workspace_id"`
      InboundReceiptMail string    `json:"inbound_receipt_mail"`
      IsNewUser          bool      `json:"is_new_user"`
  }

  type Provisioner interface {
      EnsureWorkspaceProvisioned(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) (*ProvisionResult, error)
  }
  ```
* **Business Logic & Edge Cases**:
  * When an authenticated request arrives:
    1. Check `SELECT workspace_id FROM workspace_members WHERE user_id = $1 LIMIT 1`.
    2. If found, return existing `WorkspaceID` and `IsNewUser = false`.
    3. If not found (`IsNewUser = true`), open a provisioning transaction (using privileged pool connection, temporarily bypassing RLS for initial insert):
       - Insert into `workspaces (name)` values (`'Personal Workspace'`) RETURNING `id`.
       - Insert into `workspace_members (workspace_id, user_id, role)` values (`workspace_id`, `userID`, `'owner'`).
       - Generate inbound receipt alias: `w_` + first 8 characters of workspace UUID + `@receipts.yourdomain.com`.
       - Insert into `workspace_settings (workspace_id, inbound_receipt_alias)`.
       - Seed Default Categories:
         * Group: **Essentials** -> Rent/Mortgage, Utilities, Groceries, Transportation.
         * Group: **Lifestyle** -> Dining Out, Entertainment, Personal Care.
         * Group: **Sinking Funds** -> Auto Maintenance (mode: ROLLOVER_BALANCE), Medical Buffer (ROLLOVER_BALANCE), Emergency Fund (TARGET_CAP: $1000).
  * Wrap in middleware or authentication gate so every valid user is guaranteed to have a populated workspace.
* **Acceptance Criteria & Verification**:
  * Run unit test: `go test -v ./backend/internal/workspace -run TestEnsureWorkspaceProvisioned`
  * Test Case 1: Fresh user UUID calls `EnsureWorkspaceProvisioned`. Assert `IsNewUser == true`, workspace exists, 10 default categories seeded.
  * Test Case 2: Same user UUID calls again. Assert `IsNewUser == false`, no duplicate rows inserted.

---

## Epic M1.4: SvelteKit PWA Shell & Client Auth

### `TASK-1.4.1`: Scaffold SvelteKit Application with Tailwind CSS
* **Target Files**:
  * `frontend/package.json`
  * `frontend/svelte.config.js`
  * `frontend/vite.config.ts`
  * `frontend/tailwind.config.ts`
  * `frontend/src/app.html`
  * `frontend/src/routes/+layout.svelte`
* **Dependencies**: None
* **Data Contracts & Interfaces**:
  ```html
  <!-- frontend/src/app.html -->
  <meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no, viewport-fit=cover" />
  <meta name="apple-mobile-web-app-capable" content="yes" />
  <meta name="apple-mobile-web-app-status-bar-style" content="black-translucent" />
  <link rel="manifest" href="/manifest.webmanifest" />
  ```
* **Business Logic & Edge Cases**:
  * Configure Svelte 5 with Runes enabled.
  * Setup Tailwind CSS with viewport-height utilities (`h-dvh` to handle mobile address bars).
  * Configure `@sveltejs/adapter-static` with `fallback: 'index.html'` for offline PWA operation.
* **Acceptance Criteria & Verification**:
  * Run: `npm --prefix frontend install && npm --prefix frontend run build`
  * Assert: Build produces valid static bundle in `frontend/build` with exit code 0.

### `TASK-1.4.2`: Implement OIDC Client PKCE Authentication Flow
* **Target Files**:
  * `frontend/src/lib/auth/oidc.ts`
  * `frontend/src/lib/stores/auth.svelte.ts`
  * `frontend/src/routes/auth/callback/+page.svelte`
  * `frontend/src/routes/login/+page.svelte`
* **Dependencies**: `TASK-1.4.1`, `TASK-1.1.3`
* **Data Contracts & Interfaces**:
  ```typescript
  export interface UserProfile {
      id: string;
      email: string;
  }

  export class AuthStore {
      isAuthenticated = $state(false);
      user = $state<UserProfile | null>(null);
      token = $state<string | null>(null);

      login(): Promise<void>;
      handleCallback(): Promise<void>;
      logout(): Promise<void>;
  }
  export const auth = new AuthStore();
  ```
* **Business Logic & Edge Cases**:
  * Use `oidc-client-ts`.
  * Configuration: `authority: 'http://localhost:5556/dex'`, `client_id: 'budget-app-client'`, `redirect_uri: 'http://localhost:5173/auth/callback'`, `response_type: 'code'`, `scope: 'openid profile email'`.
  * Store tokens in `sessionStorage` or IndexedDB to prevent unauthorized cross-tab access.
* **Acceptance Criteria & Verification**:
  * In browser, navigate to `http://localhost:5173/login`, click Login.
  * Dex login page appears; enter `admin@dev.local` / `password`.
  * Redirect to `/auth/callback` sets `auth.isAuthenticated = true` and navigates to `/dashboard`.

### `TASK-1.4.3`: Wire Backend and Frontend into Aspire AppHost
* **Target Files**:
  * `apphost/apphost.mts`
* **Dependencies**: `TASK-1.1.2`, `TASK-1.1.3`, `TASK-1.1.4`, `TASK-1.3.1`, `TASK-1.4.1`
* **Business Logic & Edge Cases**:
  * Add backend Go application using `.addGolangApp('backend', '../backend')`.
  * Add frontend SvelteKit application using `.addViteApp('frontend', '../frontend')`.
  * Wire service references: `backend` receives `DATABASE_URL` and `DEX_ISSUER_URL`; `frontend` receives `BACKEND_URL`.
  * Add explicit health checks: frontend waits for backend; backend waits for database, Dex, and Temporal.
* **Acceptance Criteria & Verification**:
  * Command: `aspire run`
  * Navigate to Aspire Dashboard at `http://localhost:18888`.
  * Assert all 5 resources display healthy status: `postgres`, `dex`, `temporal`, `backend`, `frontend`.

---

# Milestone 2: Zero-Based Budget Engine & Ledger

## Epic M2.1: Category Hierarchy & Goals

### `TASK-2.1.1`: Category Repository & Data Access Layer
* **Target Files**:
  * `backend/internal/budget/category_repo.go`
  * `backend/internal/budget/category_repo_test.go`
  * `backend/internal/budget/models.go`
* **Dependencies**: `TASK-1.3.3`
* **Data Contracts & Interfaces**:
  ```go
  package budget

  import (
      "context"
      "time"
      "github.com/google/uuid"
      "github.com/jackc/pgx/v5"
      "github.com/shopspring/decimal"
  )

  type RolloverMode string
  const (
      RolloverSweep    RolloverMode = "SWEEP"
      RolloverBalance  RolloverMode = "ROLLOVER_BALANCE"
      RolloverTarget   RolloverMode = "TARGET_CAP"
  )

  type Category struct {
      ID                    uuid.UUID        `json:"id"`
      WorkspaceID           uuid.UUID        `json:"workspace_id"`
      Name                  string           `json:"name"`
      GroupName             string           `json:"group_name"`
      MonthlyBudget         decimal.Decimal  `json:"monthly_budget"`
      RolloverMode          RolloverMode     `json:"rollover_mode"`
      SweepTargetCategoryID *uuid.UUID       `json:"sweep_target_category_id"`
      TargetAmount          *decimal.Decimal `json:"target_amount"`
      TargetDate            *time.Time       `json:"target_date"`
      TargetCadence         string           `json:"target_cadence"`
      SortOrder             int              `json:"sort_order"`
  }

  type CategoryRepo interface {
      Create(ctx context.Context, tx pgx.Tx, c *Category) error
      GetByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Category, error)
      ListByWorkspace(ctx context.Context, tx pgx.Tx) ([]Category, error)
      Update(ctx context.Context, tx pgx.Tx, c *Category) error
      Delete(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
  }
  ```
* **Business Logic & Edge Cases**:
  * Prevent cyclic sweep references: `sweep_target_category_id` cannot equal `id`.
  * Deleting a category sets `transactions.category_id = NULL` (foreign key `ON DELETE SET NULL`).
  * If `RolloverMode` is not `SWEEP`, ensure `sweep_target_category_id` is set to `NULL`.
* **Acceptance Criteria & Verification**:
  * Run: `go test -v ./backend/internal/budget -run TestCategoryRepoCRUD`
  * Assert: Full lifecycle CRUD operations succeed within transaction boundary.

### `TASK-2.1.2`: Category HTTP REST Handlers
* **Target Files**:
  * `backend/internal/budget/handler.go`
  * `backend/internal/budget/dto.go`
  * `backend/internal/budget/routes.go`
* **Dependencies**: `TASK-2.1.1`
* **Data Contracts & Interfaces**:
  ```go
  type CreateCategoryRequest struct {
      Name                  string           `json:"name" validate:"required,min=1,max=100"`
      GroupName             string           `json:"group_name" validate:"required,min=1,max=100"`
      MonthlyBudget         decimal.Decimal  `json:"monthly_budget" validate:"min=0"`
      RolloverMode          string           `json:"rollover_mode" validate:"oneof=SWEEP ROLLOVER_BALANCE TARGET_CAP"`
      SweepTargetCategoryID *uuid.UUID       `json:"sweep_target_category_id"`
      TargetAmount          *decimal.Decimal `json:"target_amount"`
      TargetDate            *string          `json:"target_date"`
      TargetCadence         string           `json:"target_cadence" validate:"oneof=NONE MONTHLY ANNUAL TARGET_DATE"`
  }
  ```
* **Business Logic & Edge Cases**:
  * Validate JSON payload via `go-playground/validator/v10`.
  * Return HTTP 400 Bad Request on validation errors.
  * Return HTTP 404 Not Found if category ID does not exist in workspace.
* **Acceptance Criteria & Verification**:
  * Command: `curl -i -X POST http://localhost:8080/api/v1/categories -H "Authorization: Bearer $JWT" -H "Content-Type: application/json" -d '{"name": "Groceries", "group_name": "Food", "monthly_budget": 500.00, "rollover_mode": "SWEEP"}'`
  * Assert: HTTP 201 Created with JSON body containing generated UUID.

### `TASK-2.1.3`: Paced Sinking Fund Calculation Engine
* **Target Files**:
  * `backend/internal/budget/pacing.go`
  * `backend/internal/budget/pacing_test.go`
* **Dependencies**: `TASK-2.1.1`
* **Data Contracts & Interfaces**:
  ```go
  package budget

  import (
      "time"
      "github.com/shopspring/decimal"
  )

  type PacingInput struct {
      TargetAmount   decimal.Decimal
      CurrentBalance decimal.Decimal
      TargetDate     time.Time
      CurrentDate    time.Time
  }

  type PacingResult struct {
      MonthlyDue      decimal.Decimal
      MonthsRemaining int
      IsMet           bool
  }

  func CalculateSinkingFundPace(input PacingInput) PacingResult
  ```
* **Business Logic & Edge Cases**:
  * Formula:
    $$\text{Months Remaining} = (\text{Year}_{\text{target}} - \text{Year}_{\text{current}}) \times 12 + (\text{Month}_{\text{target}} - \text{Month}_{\text{current}})$$
  * If $\text{Months Remaining} \le 0$:
    $$\text{Monthly Due} = \max(0, \text{TargetAmount} - \text{CurrentBalance})$$
  * If $\text{CurrentBalance} \ge \text{TargetAmount}$:
    $\text{MonthlyDue} = 0.00$, $\text{IsMet} = \text{true}$.
  * If $\text{Months Remaining} > 0$:
    $$\text{Monthly Due} = \left\lceil \frac{\text{TargetAmount} - \text{CurrentBalance}}{\text{Months Remaining}} \right\rceil_{2\text{ decimals}}$$
* **Acceptance Criteria & Verification**:
  * Run unit tests: `go test -v ./backend/internal/budget -run TestCalculateSinkingFundPace`
  * Test case A: Target \$1200, Balance \$0, 12 months left -> Monthly Due = \$100.00.
  * Test case B: Target \$1000, Balance \$400, 3 months left -> Monthly Due = \$200.00.
  * Test case C: Target \$500, Balance \$600, 5 months left -> Monthly Due = \$0.00, IsMet = true.

---

## Epic M2.2: Monthly Budget Planning, Baseline Freeze, & Planned Incomes

### `TASK-2.2.1`: Monthly Baseline Schema & Service
* **Target Files**:
  * `backend/internal/budget/baseline_repo.go`
  * `backend/internal/budget/baseline_service.go`
  * `backend/internal/budget/baseline_test.go`
* **Dependencies**: `TASK-2.1.1`
* **Data Contracts & Interfaces**:
  ```go
  type MonthlyCategoryBudget struct {
      ID              uuid.UUID       `json:"id"`
      WorkspaceID     uuid.UUID       `json:"workspace_id"`
      CategoryID      uuid.UUID       `json:"category_id"`
      YearMonth       time.Time       `json:"year_month"`
      OriginalPlanned decimal.Decimal `json:"original_planned"`
  }

  type BaselineService interface {
      InitializeMonth(ctx context.Context, tx pgx.Tx, yearMonth time.Time) error
      GetMonthlyBaselines(ctx context.Context, tx pgx.Tx, yearMonth time.Time) ([]MonthlyCategoryBudget, error)
  }
  ```
* **Business Logic & Edge Cases**:
  * `year_month` normalized to midnight UTC on the 1st of that month.
  * On month init: query categories; if previous month exists, copy `original_planned`; otherwise use `category.monthly_budget`.
  * Use `ON CONFLICT (workspace_id, category_id, year_month) DO NOTHING`.
* **Acceptance Criteria & Verification**:
  * Unit test: `TestInitializeMonth_CopiesPreviousMonthBaseline` verifies subsequent runs do not overwrite user-adjusted planned targets.

### `TASK-2.2.2`: Budget Reallocation Ledger Engine
* **Target Files**:
  * `backend/internal/budget/reallocation_repo.go`
  * `backend/internal/budget/reallocation_service.go`
  * `backend/internal/budget/reallocation_test.go`
* **Dependencies**: `TASK-2.2.1`
* **Data Contracts & Interfaces**:
  ```go
  type ReallocationRequest struct {
      YearMonth      time.Time       `json:"year_month"`
      FromCategoryID uuid.UUID       `json:"from_category_id"`
      ToCategoryID   uuid.UUID       `json:"to_category_id"`
      Amount         decimal.Decimal `json:"amount"`
      Reason         string          `json:"reason"`
  }

  type ReallocationService interface {
      TransferFunds(ctx context.Context, tx pgx.Tx, req ReallocationRequest) error
      GetEffectiveTarget(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID, yearMonth time.Time) (decimal.Decimal, error)
  }
  ```
* **Business Logic & Edge Cases**:
  * `amount` must be strictly positive ($> 0$).
  * `FromCategoryID` and `ToCategoryID` cannot be identical.
  * Dynamic Effective Target formula:
    $$\text{Effective Target} = \text{OriginalPlanned} + \sum (\text{Transfers To}) - \sum (\text{Transfers From})$$
* **Acceptance Criteria & Verification**:
  * Integration test:
    1. Category A baseline = \$300. Category B baseline = \$100.
    2. Transfer \$50 from A to B.
    3. Assert effective targets: A = \$250.00, B = \$150.00. Total allocated across categories remains \$400.00.

### `TASK-2.2.3`: Ready-to-Assign & Net Balance Aggregator
* **Target Files**:
  * `backend/internal/budget/summary_service.go`
  * `backend/internal/budget/summary_test.go`
* **Dependencies**: `TASK-2.2.2`, `TASK-2.2.4`
* **Data Contracts & Interfaces**:
  ```go
  type MonthSummary struct {
      YearMonth           time.Time       `json:"year_month"`
      TotalIncomeExpected decimal.Decimal `json:"total_income_expected"`
      TotalAllocated      decimal.Decimal `json:"total_allocated"`
      ReadyToAssign       decimal.Decimal `json:"ready_to_assign"`
      TotalSpentCleared   decimal.Decimal `json:"total_spent_cleared"`
      TotalSpentPending   decimal.Decimal `json:"total_spent_pending"`
      IsUnbalanced        bool            `json:"is_unbalanced"`
  }

  func ComputeMonthSummary(ctx context.Context, tx pgx.Tx, yearMonth time.Time) (*MonthSummary, error)
  ```
* **Business Logic & Edge Cases**:
  * `TotalIncomeExpected` is computed from `monthly_planned_incomes`:
    $$\text{TotalIncomeExpected} = \sum \text{monthly\_planned\_incomes.amount}$$
  * Ready to Assign:
    $$\text{ReadyToAssign} = \text{TotalIncomeExpected} - \sum \text{EffectivePlanned}$$
  * Zero-based check: if $\text{ReadyToAssign} \ne 0.00$, flag warning.
  * Category overspends check:
    $$\text{Remaining} = \text{EffectiveTarget} - (\text{ClearedSpend} + \text{PendingSpend})$$
    If any category has $\text{Remaining} < 0.00$, set `IsUnbalanced = true`.
* **Acceptance Criteria & Verification**:
  * Test case: \$4000 planned income, \$4000 allocated -> `ReadyToAssign` = \$0.00, `IsUnbalanced` = false.
  * Record \$50 overspend on category -> `IsUnbalanced` = true.

### `TASK-2.2.4`: Planned Income Repository & Endpoints
* **Target Files**:
  * `backend/internal/budget/income_repo.go`
  * `backend/internal/budget/income_handler.go`
  * `backend/internal/budget/income_dto.go`
  * `backend/internal/budget/income_test.go`
* **Dependencies**: `TASK-1.2.2`, `TASK-1.3.3`
* **Data Contracts & Interfaces**:
  ```go
  package budget

  import (
      "context"
      "time"
      "github.com/google/uuid"
      "github.com/jackc/pgx/v5"
      "github.com/shopspring/decimal"
  )

  type MonthlyPlannedIncome struct {
      ID          uuid.UUID       `json:"id"`
      WorkspaceID uuid.UUID       `json:"workspace_id"`
      YearMonth   time.Time       `json:"year_month"`
      Name        string          `json:"name"`
      Amount      decimal.Decimal `json:"amount"`
      CreatedAt   time.Time       `json:"created_at"`
  }

  type PlannedIncomeRepo interface {
      Create(ctx context.Context, tx pgx.Tx, inc *MonthlyPlannedIncome) error
      ListByMonth(ctx context.Context, tx pgx.Tx, yearMonth time.Time) ([]MonthlyPlannedIncome, error)
      Delete(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
  }
  ```
* **Business Logic & Edge Cases**:
  * Endpoint `POST /api/v1/budget/incomes`:
    Payload: `{"year_month": "2026-11-01", "name": "Paycheck 1", "amount": 2500.00}`.
  * Normalize `year_month` to the first of the month in UTC.
  * `amount` must be greater than zero.
  * Endpoint `GET /api/v1/budget/incomes?month=2026-11` returns all planned incomes for that month.
  * Deleting an income entry immediately reduces `TotalIncomeExpected` in `ComputeMonthSummary`.
* **Acceptance Criteria & Verification**:
  * Run: `go test -v ./backend/internal/budget -run TestPlannedIncomeRepo`
  * Test Case 1: Insert "Paycheck 1" (\$2500.00) and "Paycheck 2" (\$2500.00). List by month returns \$5000.00 total.
  * Test Case 2: Attempt insert with amount `0.00` or `-50.00`; assert validation error returned.

---

## Epic M2.3: Budgeting Frontend Interface

### `TASK-2.3.1`: Category Row 3-Layer Visual Component
* **Target Files**:
  * `frontend/src/lib/components/budget/CategoryRow.svelte`
* **Dependencies**: `TASK-1.4.1`, `TASK-2.1.2`
* **Data Contracts & Interfaces**:
  ```typescript
  interface Props {
      category: {
          id: string;
          name: string;
          originalPlanned: number;
          effectiveTarget: number;
          spentCleared: number;
          spentPending: number;
          remaining: number;
      };
      onCoverClick: (categoryId: string) => void;
  }
  ```
* **Business Logic & Edge Cases**:
  * Render progress bar with 3 stacked layers:
    1. Solid bar: `spentCleared`.
    2. Striped bar: `spentPending`.
    3. Tick mark: `originalPlanned` (if different from `effectiveTarget`).
  * Color coding:
    * Green: $\text{Total Spent} \le 80\%$ of `effectiveTarget`.
    * Amber: $80\% < \text{Total Spent} \le 100\%$.
    * Red: $\text{Total Spent} > \text{effectiveTarget}$ (Overspent state). Clicking badge triggers `onCoverClick(category.id)`.
* **Acceptance Criteria & Verification**:
  * Component test (Vitest): render overspent category (-\$20.00); assert red badge rendered and clicking it fires `onCoverClick`.

### `TASK-2.3.2`: Inter-Category Cover Drawer Component
* **Target Files**:
  * `frontend/src/lib/components/budget/CoverDrawer.svelte`
* **Dependencies**: `TASK-2.3.1`, `TASK-2.2.2`
* **Data Contracts & Interfaces**:
  ```typescript
  interface CoverDrawerProps {
      isOpen: boolean;
      overspentCategory: CategoryDetail | null;
      availableDonors: Array<{ id: string; name: string; available: number }>;
      onClose: () => void;
      onSuccess: () => void;
  }
  ```
* **Business Logic & Edge Cases**:
  * Slide-over modal from bottom on mobile, right drawer on desktop.
  * Only categories with `available > 0` are selectable donors.
  * Amount input defaults to the exact deficit.
  * On submit: `POST /api/v1/budget/reallocate`. On success, invoke `onSuccess()`.
* **Acceptance Criteria & Verification**:
  * Vitest: select donor category with \$120 available to cover \$35 overspend. Assert payload sent with `{ from_category_id, to_category_id, amount: 35.00 }`.

### `TASK-2.3.3`: Budget Month Navigation & Summary Header
* **Target Files**:
  * `frontend/src/routes/(app)/budget/+page.svelte`
  * `frontend/src/lib/components/budget/BudgetHeader.svelte`
* **Dependencies**: `TASK-2.2.3`, `TASK-2.3.1`
* **Business Logic & Edge Cases**:
  * Displays "Ready to Assign" banner:
    * \$0.00: Neutral green background.
    * $> \$0.00$: Amber banner ("You have \$X to assign").
    * $< \$0.00$: Red alert ("Over-assigned by \$X").
  * Month chevron navigation updates URL query param `?month=YYYY-MM`.
* **Acceptance Criteria & Verification**:
  * Navigating to `/budget?month=2026-12` fetches December baselines and summary data without full-page reload.

---

# Milestone 3: Transactions, Accounts, & Offline Client

## Epic M3.1: Account Hierarchy, Credit Card Modes, & Revolver Splits

### `TASK-3.1.1`: Account Data Models & Service
* **Target Files**:
  * `backend/internal/account/repo.go`
  * `backend/internal/account/service.go`
  * `backend/internal/account/account_test.go`
* **Dependencies**: `TASK-1.3.3`
* **Data Contracts & Interfaces**:
  ```go
  package account

  import (
      "context"
      "github.com/google/uuid"
      "github.com/jackc/pgx/v5"
      "github.com/shopspring/decimal"
  )

  type AccountType string
  const (
      Checking   AccountType = "checking"
      Savings    AccountType = "savings"
      CreditCard AccountType = "credit_card"
      Investment AccountType = "investment"
      Loan       AccountType = "loan"
  )

  type CCMode string
  const (
      PayInFull   CCMode = "PAY_IN_FULL"
      DebtPaydown CCMode = "DEBT_PAYDOWN"
  )

  type Account struct {
      ID                   uuid.UUID       `json:"id"`
      WorkspaceID          uuid.UUID       `json:"workspace_id"`
      ExternalAccountID    *string         `json:"external_account_id"`
      Name                 string          `json:"name"`
      Type                 AccountType     `json:"type"`
      CurrentBalance       decimal.Decimal `json:"current_balance"`
      IsOnBudget           bool            `json:"is_on_budget"`
      CardMode             CCMode          `json:"card_mode"`
      InitialDebtBaseline  decimal.Decimal `json:"initial_debt_baseline"`
      APRPercentage        decimal.Decimal `json:"apr_percentage"`
      StatementDueDay      *int            `json:"statement_due_day"`
      DebtPayoffCategoryID *uuid.UUID      `json:"debt_payoff_category_id"`
  }
  ```
* **Business Logic & Edge Cases**:
  * Only `credit_card` accounts can have `card_mode = 'DEBT_PAYDOWN'`.
  * If `card_mode = 'DEBT_PAYDOWN'`, `debt_payoff_category_id` must not be null.
  * Credit card balances are stored as negative numbers when debt is owed.
* **Acceptance Criteria & Verification**:
  * Run: `go test -v ./backend/internal/account -run TestAccountService`
  * Assert validation error if `DEBT_PAYDOWN` is specified without `debt_payoff_category_id`.

### `TASK-3.1.2`: Solvency Buffer Calculation Service
* **Target Files**:
  * `backend/internal/account/solvency.go`
  * `backend/internal/account/solvency_test.go`
* **Dependencies**: `TASK-3.1.1`
* **Data Contracts & Interfaces**:
  ```go
  type SolvencyReport struct {
      TotalLiquidCash      decimal.Decimal `json:"total_liquid_cash"`
      TotalCreditCardDebt  decimal.Decimal `json:"total_credit_card_debt"`
      SolvencyBuffer       decimal.Decimal `json:"solvency_buffer"`
      IsFloatingDebt       bool            `json:"is_floating_debt"`
      FloatingDebtAmount   decimal.Decimal `json:"floating_debt_amount"`
  }

  func ComputeSolvencyReport(ctx context.Context, tx pgx.Tx) (*SolvencyReport, error)
  ```
* **Business Logic & Edge Cases**:
  * Total Liquid Cash = Sum of `current_balance` for `checking` + `savings` where `is_on_budget = true`.
  * Total Credit Card Debt = Absolute value of negative balances on `credit_card` accounts with `card_mode = 'PAY_IN_FULL'`.
  * Solvency Buffer:
    $$\text{Solvency Buffer} = \text{Total Liquid Cash} - \text{Total Credit Card Debt}$$
  * If $\text{Solvency Buffer} < 0.00$, set `IsFloatingDebt = true` and `FloatingDebtAmount = abs(SolvencyBuffer)`.
* **Acceptance Criteria & Verification**:
  * Checking: \$500, Card: -\$800 -> Solvency Buffer = -\$300, IsFloatingDebt = true, FloatingDebtAmount = \$300.00.

### `TASK-3.1.3`: Debt Paydown Burndown Projection Engine
* **Target Files**:
  * `backend/internal/account/debt_paydown.go`
  * `backend/internal/account/debt_paydown_test.go`
* **Dependencies**: `TASK-3.1.1`
* **Data Contracts & Interfaces**:
  ```go
  type DebtProjectionInput struct {
      CurrentBalance     decimal.Decimal
      MonthlyAllocation  decimal.Decimal
      APRPercentage      decimal.Decimal
  }

  type DebtProjectionResult struct {
      MonthsToPayoff     int             `json:"months_to_payoff"`
      TotalInterestPaid  decimal.Decimal `json:"total_interest_paid"`
      ProjectedPayoffDate time.Time      `json:"projected_payoff_date"`
      IsUnpayable        bool            `json:"is_unpayable"`
  }

  func CalculateDebtProjection(input DebtProjectionInput) DebtProjectionResult
  ```
* **Business Logic & Edge Cases**:
  * Monthly interest rate $r = \frac{\text{APR}}{100 \times 12}$.
  * Initial monthly interest $I_0 = \vert{}\text{CurrentBalance}\vert{} \times r$.
  * If $\text{MonthlyAllocation} \le I_0$, card cannot be paid off (`IsUnpayable = true`).
  * Iteratively compute principal reduction per month until balance $\ge 0.00$:
    $$I_m = B_m \times r, \quad P_m = \text{MonthlyAllocation} - I_m, \quad B_{m+1} = B_m - P_m$$
* **Acceptance Criteria & Verification**:
  * Unit test: \$4500 debt at 24% APR paying \$400/month computes \~14 months and expected interest within \$1.00.

### `TASK-3.1.4`: Revolver Payment Split Engine (Active Spender + Debt Paydown)
* **Target Files**:
  * `backend/internal/account/revolver_split.go`
  * `backend/internal/account/revolver_split_test.go`
* **Dependencies**: `TASK-3.1.1`, `TASK-3.2.1`
* **Data Contracts & Interfaces**:
  ```go
  package account

  import (
      "context"
      "github.com/google/uuid"
      "github.com/jackc/pgx/v5"
      "github.com/shopspring/decimal"
  )

  type PaymentSplitResult struct {
      TransferLegAmount decimal.Decimal `json:"transfer_leg_amount"`
      DebtLegAmount     decimal.Decimal `json:"debt_leg_amount"`
      DebtCategoryID    uuid.UUID       `json:"debt_category_id"`
  }

  func ProcessRevolverPayment(
      ctx context.Context,
      tx pgx.Tx,
      cardAccount Account,
      paymentAmount decimal.Decimal,
  ) (*PaymentSplitResult, error)
  ```
* **Business Logic & Edge Cases**:
  * When a payment transaction (inflow $> 0$) clears to a credit card where `card_mode = 'DEBT_PAYDOWN'`:
    1. Query all debit spending transactions on this card within the current billing cycle (or since the last payment):
       $$\text{ActiveSpendThisCycle} = \sum \vert{}\text{Amount}\vert{} \quad \text{where } \text{amount} < 0$$
    2. Calculate Transfer Leg (reimburses new purchases already categorized in budget):
       $$\text{TransferLegAmount} = \min(\text{paymentAmount}, \text{ActiveSpendThisCycle})$$
    3. Calculate Debt Reduction Leg (reduces frozen starting baseline):
       $$\text{DebtLegAmount} = \text{paymentAmount} - \text{TransferLegAmount}$$
    4. Execution:
       * Transfer leg created as neutral transfer linked to Checking account ($0 budget impact).
       * Debt leg tagged with `category_id = cardAccount.DebtPayoffCategoryID` (draws cash from the debt payoff category budget).
* **Acceptance Criteria & Verification**:
  * Run: `go test -v ./backend/internal/account -run TestProcessRevolverPayment`
  * Test Case: Card has \$200 new grocery spending this month and pre-existing debt. User makes \$600 payment.
  * Assert: Returns Transfer Leg = \$200.00, Debt Leg = \$400.00 allocated to `DebtPayoffCategoryID`.

---

## Epic M3.2: Transaction Core & Split Ledger

### `TASK-3.2.1`: Transaction Data Layer with Split Validation
* **Target Files**:
  * `backend/internal/transaction/repo.go`
  * `backend/internal/transaction/service.go`
  * `backend/internal/transaction/split_test.go`
* **Dependencies**: `TASK-3.1.1`, `TASK-2.1.1`
* **Data Contracts & Interfaces**:
  ```go
  package transaction

  type SplitItem struct {
      Amount     decimal.Decimal `json:"amount"`
      CategoryID *uuid.UUID      `json:"category_id"`
      UserNotes  string          `json:"user_notes"`
  }

  type CreateSplitTransactionRequest struct {
      AccountID       uuid.UUID       `json:"account_id"`
      TotalAmount     decimal.Decimal `json:"total_amount"`
      MerchantRaw     string          `json:"merchant_raw"`
      TransactionDate time.Time       `json:"transaction_date"`
      Splits          []SplitItem     `json:"splits"`
  }
  ```
* **Business Logic & Edge Cases**:
  * Split sum must match parent total down to 2 decimal places:
    $$\sum_{i=1}^n \text{Splits}[i].\text{Amount} == \text{TotalAmount}$$
  * If sum does not match, reject with HTTP 422 Unprocessable Entity (`ErrSplitSumMismatch`).
  * Parent row: `parent_transaction_id = NULL`, `category_id = NULL`. Child split rows: `parent_transaction_id = parent.ID`.
* **Acceptance Criteria & Verification**:
  * Unit test: inserting parent -\$100.00 with splits [-\$60.00, -\$39.99] aborts transaction and returns error.

### `TASK-3.2.2`: Transaction REST API Endpoints
* **Target Files**:
  * `backend/internal/transaction/handler.go`
  * `backend/internal/transaction/dto.go`
  * `backend/internal/transaction/routes.go`
* **Dependencies**: `TASK-3.2.1`
* **Data Contracts & Interfaces**:
  ```go
  type TransactionFilter struct {
      AccountID  *uuid.UUID `form:"account_id"`
      CategoryID *uuid.UUID `form:"category_id"`
      Status     *string    `form:"status"`
      Page       int        `form:"page,default=1"`
      Limit      int        `form:"limit,default=50"`
  }
  ```
* **Business Logic & Edge Cases**:
  * Endpoints: `GET /api/v1/transactions`, `POST /api/v1/transactions`, `PATCH /api/v1/transactions/:id`, `DELETE /api/v1/transactions/:id`.
  * Deleting a parent transaction cascades and removes all child split transactions.
* **Acceptance Criteria & Verification**:
  * Integration test: `POST /api/v1/transactions` returns 201; subsequent `GET` with pagination contains created transaction.

---

## Epic M3.3: Client Offline Engine (IndexedDB & Service Worker)

### `TASK-3.3.1`: IndexedDB Client Storage Wrapper
* **Target Files**:
  * `frontend/src/lib/offline/db.ts`
  * `frontend/src/lib/offline/db.test.ts`
* **Dependencies**: `TASK-1.4.1`
* **Data Contracts & Interfaces**:
  ```typescript
  export interface OfflineTransaction {
      localId: string;
      accountId: string;
      amount: number;
      merchantRaw: string;
      categoryId: string | null;
      transactionDate: string;
      syncStatus: 'QUEUED' | 'SYNCING' | 'FAILED';
      createdAt: number;
  }

  export class BudgetLocalDB {
      async queueTransaction(tx: OfflineTransaction): Promise<void>;
      async getPendingQueue(): Promise<OfflineTransaction[]>;
      async markSynced(localId: string): Promise<void>;
  }
  ```
* **Business Logic & Edge Cases**:
  * DB Name: `budget_app_offline_v1`. Stores: `transactions_queue`, `cached_categories`, `cached_accounts`.
  * Persist pending items across reloads and mobile app restarts.
* **Acceptance Criteria & Verification**:
  * Vitest: Queue 3 offline transactions, retrieve pending queue (length 3), mark one synced (length 2).

### `TASK-3.3.2`: Service Worker Background Queue & Sync Handler
* **Target Files**:
  * `frontend/src/service-worker.ts`
  * `frontend/src/lib/offline/sync.ts`
* **Dependencies**: `TASK-3.3.1`, `TASK-3.2.2`
* **Business Logic & Edge Cases**:
  * Cache PWA static assets (HTML, JS, CSS, WebManifest).
  * Listen for browser `online` and Background Sync `sync` events.
  * Flush `QUEUED` records to `POST /api/v1/transactions/sync` and clear on 200/207 response.
* **Acceptance Criteria & Verification**:
  * DevTools offline simulation: log transaction, verify local category balance decrements, toggle online, verify outgoing sync request executes.

### `TASK-3.3.3`: SvelteKit Transaction Stream & Visual Badges
* **Target Files**:
  * `frontend/src/routes/(app)/transactions/+page.svelte`
  * `frontend/src/lib/components/transactions/TransactionItem.svelte`
* **Dependencies**: `TASK-3.2.2`, `TASK-3.3.2`
* **Business Logic & Edge Cases**:
  * Badges:
    * `status == 'PENDING'`: Clock icon, italicized font, muted gray text.
    * `origin == 'MANUAL_OFFLINE' && syncStatus == 'QUEUED'`: Cloud upload icon.
    * `settledDelta`: Amber indicator when settled amount exceeds authorization.
* **Acceptance Criteria & Verification**:
  * Render component test with sample dataset; verify correct icon and badge rendering.

---

# Milestone 4: Banking Rails & Ingestion (SimpleFIN + Temporal)

## Epic M4.1: SimpleFIN Integration, Vault, & Setup UI

### `TASK-4.1.1`: Application-Level Credential Encryption Vault
* **Target Files**:
  * `backend/pkg/crypto/vault.go`
  * `backend/pkg/crypto/vault_test.go`
* **Dependencies**: `TASK-1.3.1`
* **Data Contracts & Interfaces**:
  ```go
  package crypto

  type Vault interface {
      Encrypt(plainText string) (string, error)
      Decrypt(cipherText string) (string, error)
  }

  func NewAESGCMVault(key []byte) (Vault, error)
  ```
* **Business Logic & Edge Cases**:
  * 32-byte key (AES-256-GCM).
  * Generate cryptographically secure random 12-byte nonce for each encryption (`crypto/rand`). Prepend nonce to ciphertext before base64 encoding.
  * Reject tampered data with authentication error.
* **Acceptance Criteria & Verification**:
  * Unit test: encrypt string, decrypt result (matches); flip 1 bit in ciphertext, decrypt fails.

### `TASK-4.1.2`: SimpleFIN REST API Client
* **Target Files**:
  * `backend/internal/simplefin/client.go`
  * `backend/internal/simplefin/models.go`
  * `backend/internal/simplefin/client_test.go`
* **Dependencies**: `TASK-4.1.1`
* **Data Contracts & Interfaces**:
  ```go
  package simplefin

  type AccountDTO struct {
      ID       string `json:"id"`
      Name     string `json:"name"`
      Currency string `json:"currency"`
      Balance  string `json:"balance"`
  }

  type TransactionDTO struct {
      ID          string `json:"id"`
      AccountID   string `json:"account"`
      Posted      int64  `json:"posted"`
      Amount      string `json:"amount"`
      Description string `json:"description"`
      Pending     bool   `json:"pending"`
  }

  type Client interface {
      ClaimSetupToken(ctx context.Context, setupToken string) (accessURL string, error)
      FetchData(ctx context.Context, accessURL string, startDate time.Time) (*DataResponse, error)
  }
  ```
* **Business Logic & Edge Cases**:
  * Decode base64 setup token into claim endpoint; perform claim handshake.
  * Parse basic auth credentials embedded in `accessURL`.
  * Return `ErrRateLimited` with backoff duration on HTTP 429.
* **Acceptance Criteria & Verification**:
  * Mock HTTP test: successfully execute claim token handshake and transaction delta fetch.

### `TASK-4.1.3`: SimpleFIN Connection Management API & Account Linker Modal
* **Target Files**:
  * `backend/internal/simplefin/handler.go`
  * `frontend/src/lib/components/accounts/SimpleFINSetupModal.svelte`
  * `frontend/src/routes/(app)/accounts/+page.svelte`
* **Dependencies**: `TASK-4.1.2`, `TASK-3.1.1`, `TASK-1.4.1`
* **Data Contracts & Interfaces**:
  ```go
  type ClaimTokenRequest struct {
      SetupToken string `json:"setup_token" validate:"required"`
  }

  type LinkAccountMapping struct {
      ExternalAccountID string `json:"external_account_id"`
      Name              string `json:"name"`
      Type              string `json:"type" validate:"oneof=checking savings credit_card investment loan"`
      CardMode          string `json:"card_mode,omitempty"`
  }

  type LinkAccountsRequest struct {
      Accounts []LinkAccountMapping `json:"accounts" validate:"required,dive"`
  }
  ```
* **Business Logic & Edge Cases**:
  * Endpoint `POST /api/v1/bank/simplefin/claim`:
    1. Call `client.ClaimSetupToken(setupToken)`.
    2. Encrypt resulting access URL using `Vault`.
    3. Upsert into `simplefin_connections`.
    4. Fetch remote accounts from SimpleFIN and return account list to frontend.
  * Endpoint `POST /api/v1/bank/simplefin/link`:
    * Iterate through mappings; insert or update `accounts` with `external_account_id`, `name`, `type`, and `is_on_budget`.
  * Frontend Modal:
    * Step 1: User pastes SimpleFIN setup token.
    * Step 2: Display list of discovered bank accounts with dropdowns to map type (Checking, Savings, Credit Card).
    * Step 3: On confirm, submit mapping and refresh account balances.
* **Acceptance Criteria & Verification**:
  * Integration test: Mock claim token endpoint returns 2 discovered accounts.
  * Link endpoint creates 2 rows in `accounts` table with correct `external_account_id` and workspace ID.

---

## Epic M4.2: Temporal Workflows, Reconciliation, & Normalization

### `TASK-4.2.1`: Temporal Worker Initialization & Client Setup
* **Target Files**:
  * `backend/cmd/worker/main.go`
  * `backend/internal/temporal/client.go`
* **Dependencies**: `TASK-1.1.4`, `TASK-1.3.1`
* **Business Logic & Edge Cases**:
  * Task Queue: `budget-sync-queue`.
  * Auto-reconnect on network interruptions to Temporal server.
  * Structured logging with `slog`.
* **Acceptance Criteria & Verification**:
  * Start worker: `go run backend/cmd/worker/main.go`
  * Command: `temporal worker list --address localhost:7233 --task-queue budget-sync-queue`
  * Assert: Active poller registered.

### `TASK-4.2.2`: BankSyncWorkflow & Fetch Activities
* **Target Files**:
  * `backend/internal/temporal/workflows/bank_sync.go`
  * `backend/internal/temporal/activities/fetch_simplefin.go`
  * `backend/internal/temporal/activities/persist_transactions.go`
* **Dependencies**: `TASK-4.1.2`, `TASK-4.2.1`
* **Data Contracts & Interfaces**:
  ```go
  type BankSyncInput struct {
      WorkspaceID uuid.UUID `json:"workspace_id"`
  }

  func (a *BankActivities) FetchSimpleFINDataActivity(ctx context.Context, workspaceID uuid.UUID) (*simplefin.DataResponse, error)
  ```
* **Business Logic & Edge Cases**:
  * Retry Policy: `InitialInterval: 15s`, `BackoffCoefficient: 2.0`, `MaximumInterval: 10m`, `MaximumAttempts: 5`.
  * Query SimpleFIN transactions since `last_synced_at`.
* **Acceptance Criteria & Verification**:
  * Start workflow: `temporal workflow start --task-queue budget-sync-queue --type BankSyncWorkflow --input '{"workspace_id": "00000000-0000-0000-0000-000000000001"}'`
  * Check Web UI: workflow transitions to `COMPLETED`.

### `TASK-4.2.3`: Pending vs. Posted Deduplication & Heuristic Matcher
* **Target Files**:
  * `backend/internal/temporal/activities/reconcile_transactions.go`
  * `backend/pkg/fuzzy/levenshtein.go`
  * `backend/pkg/fuzzy/levenshtein_test.go`
  * `backend/internal/temporal/activities/reconcile_test.go`
* **Dependencies**: `TASK-4.2.2`
* **Data Contracts & Interfaces**:
  ```go
  type MatchCandidate struct {
      ExistingID uuid.UUID
      IsMatched  bool
      TipDelta   decimal.Decimal
  }

  func MatchPostedToPending(posted TxRecord, pendingPool []TxRecord) MatchCandidate
  ```
* **Business Logic & Edge Cases**:
  * Matching criteria:
    1. `AccountID` matches exactly.
    2. Date difference: $\vert{}\text{Date}_{\text{posted}} - \text{Date}_{\text{pending}}\vert{} \le 4\text{ days}$.
    3. Normalized Merchant Levenshtein similarity:
       $$\text{Similarity}(S_1, S_2) = 1.0 - \frac{\text{LevenshteinDistance}(S_1, S_2)}{\max(\text{len}(S_1), \text{len}(S_2))} \ge 0.80$$
    4. Amount check: exact match OR posted amount is $0\%$ to $30\%$ higher (tip margin).
  * On match: update pending row with `external_transaction_id = posted.external_transaction_id`, `status = 'POSTED'`, `amount = posted.amount`; preserve `category_id` and `user_notes`.
* **Acceptance Criteria & Verification**:
  * Unit test: Pending \$40.00 "Burger Bar" matches Posted \$48.50 "BURGER BAR DOWNTOWN".
  * Unit test: Pending \$40.00 "Target" does not match Posted \$65.00 "Target" (exceeds 30%).

### `TASK-4.2.4`: Internal Account Transfer Auto-Pairing Engine
* **Target Files**:
  * `backend/internal/temporal/activities/detect_transfers.go`
  * `backend/internal/temporal/activities/detect_transfers_test.go`
* **Dependencies**: `TASK-4.2.3`
* **Business Logic & Edge Cases**:
  * Pair transactions across accounts where:
    $$\text{Amount}_A = -\text{Amount}_B \quad \text{AND} \quad \vert{}\text{Date}_A - \text{Date}_B\vert{} \le 3\text{ days}$$
  * Execute:
    ```sql
    UPDATE transactions SET transfer_linked_transaction_id = $1, category_id = NULL WHERE id = $2;
    UPDATE transactions SET transfer_linked_transaction_id = $2, category_id = NULL WHERE id = $1;
    ```
* **Acceptance Criteria & Verification**:
  * Integration test: -\$500 on Checking, +\$500 on Savings dated 1 day apart auto-pair; both rows have `is_transfer = true` and `category_id IS NULL`.

### `TASK-4.2.5`: Temporal Bank Sync Schedule Registration
* **Target Files**:
  * `backend/internal/temporal/schedules/sync_scheduler.go`
  * `backend/cmd/api/main.go`
* **Dependencies**: `TASK-4.2.2`
* **Business Logic & Edge Cases**:
  * Register schedule for linked workspaces: Interval 6 hours, Jitter 15 minutes.
  * API endpoint `POST /api/v1/bank/sync-now` to trigger immediate on-demand workflow.
* **Acceptance Criteria & Verification**:
  * Command: `temporal schedule list --address localhost:7233`
  * Assert: Schedule exists with interval `6h0m0s`.

### `TASK-4.2.6`: Manual-to-Bank Feed Deduplication Activity (Conflict Resolution Strategy 1)
* **Target Files**:
  * `backend/internal/temporal/activities/dedup_manual.go`
  * `backend/internal/temporal/activities/dedup_manual_test.go`
* **Dependencies**: `TASK-4.2.2`, `TASK-3.3.1`
* **Data Contracts & Interfaces**:
  ```go
  package activities

  type ManualMatchCandidate struct {
      ManualTxID       uuid.UUID
      Matched          bool
      BankExternalID   string
  }

  func MatchIncomingBankToManual(bankTx TxRecord, manualPool []TxRecord) ManualMatchCandidate
  ```
* **Business Logic & Edge Cases**:
  * When SimpleFIN sync delivers bank transactions, query existing offline manual entries where `origin = 'MANUAL_OFFLINE'`.
  * Matching criteria:
    1. Account ID must match exactly.
    2. Date difference: $\vert{}\text{Date}_{\text{bank}} - \text{Date}_{\text{manual}}\vert{} \le 2\text{ days}$.
    3. Amount matches exactly (or settled within 30% tip margin for dining).
  * On match:
    * Silently promote manual transaction: update `external_transaction_id = bankTx.external_id`, set `origin = 'BANK_FEED'`, and set `status = bankTx.status`.
    * Retain user's custom `category_id`, `user_notes`, and any attached receipts.
    * Do NOT create a second duplicate row for this bank charge.
* **Acceptance Criteria & Verification**:
  * Run: `go test -v ./backend/internal/temporal/activities -run TestMatchIncomingBankToManual`
  * Seed manual entry: Account 1, Date 2026-11-05, -\$14.50, `origin = 'MANUAL_OFFLINE'`, category "Coffee".
  * Process incoming bank charge: Account 1, Date 2026-11-06, -\$14.50, external_id "ext_987".
  * Assert: Row promoted to `BANK_FEED`, `external_transaction_id = 'ext_987'`, category remains "Coffee". Total transaction count in database remains 1.

### `TASK-4.2.7`: Merchant Raw String Normalization Pipeline
* **Target Files**:
  * `backend/pkg/merchant/normalizer.go`
  * `backend/pkg/merchant/normalizer_test.go`
  * `backend/internal/temporal/activities/normalize_merchant.go`
* **Dependencies**: `TASK-4.2.2`
* **Data Contracts & Interfaces**:
  ```go
  package merchant

  type Normalizer interface {
      Clean(raw string) string
  }

  func NewStandardNormalizer() Normalizer
  ```
* **Business Logic & Edge Cases**:
  * Step 1: Strip payment aggregator prefixes: `SQ *`, `TST*`, `PAYPAL *`, `SP *`, `STRIPE *`, `PY *`.
  * Step 2: Strip store/terminal location numbers: `#\d+`, `STORE \d+`, `\b\d{4,}\b`.
  * Step 3: Strip trailing city/state/country abbreviations: e.g., `ATLANTA GA`, `SAN FRANCISCO CA`, `US`.
  * Step 4: Title-case the cleaned merchant string and trim extraneous whitespace.
  * Save result into `transactions.merchant_clean`.
* **Acceptance Criteria & Verification**:
  * Unit test cases in `normalizer_test.go`:
    * `"SQ *COFFEE SHOP #1204 DOWNTOWN"` -> `"Coffee Shop"`
    * `"AMZN Mktp US*2A4F12"` -> `"Amzn Mktp"`
    * `"WAL-MART #2820 ATLANTA GA"` -> `"Wal-Mart"`
    * `"TST* BURGER BISTRO"` -> `"Burger Bistro"`

---

# Milestone 5: Month-End Rollover & Automated Budget Transitions

## Epic M5.1: Rollover Evaluation & Trend Recommendations

### `TASK-5.1.1`: Rollover Rules Processor
* **Target Files**:
  * `backend/internal/budget/rollover.go`
  * `backend/internal/budget/rollover_test.go`
* **Dependencies**: `TASK-2.2.3`
* **Data Contracts & Interfaces**:
  ```go
  type RolloverExecutionResult struct {
      CategoryID          uuid.UUID
      Mode                RolloverMode
      PriorRemaining      decimal.Decimal
      NextMonthAssigned   decimal.Decimal
      SweptAmount         decimal.Decimal
  }

  func EvaluateCategoryRollover(cat Category, remaining decimal.Decimal) RolloverExecutionResult
  ```
* **Business Logic & Edge Cases**:
  * `SWEEP`: If `remaining > 0`, redirect to `sweep_target_category_id` or assignable income.
  * `ROLLOVER_BALANCE`: Carry balance forward:
    $$\text{Available}_{M+1} = \text{Remaining}_M + \text{Planned}_{M+1}$$
  * `TARGET_CAP`:
    $$\text{Planned}_{M+1} = \max(0, \text{TargetAmount} - \text{Remaining}_M)$$
* **Acceptance Criteria & Verification**:
  * Unit tests: Sinking fund with \$450 remaining carries \$450 forward; sweep fund with \$50 remaining sweeps \$50.

### `TASK-5.1.2`: Credit Card Month-End Uncovered Debt Interlock
* **Target Files**:
  * `backend/internal/budget/cc_debt_interlock.go`
  * `backend/internal/budget/cc_debt_interlock_test.go`
* **Dependencies**: `TASK-5.1.1`, `TASK-3.1.1`
* **Business Logic & Edge Cases**:
  * If uncovered credit card spend exists at 23:59:59 on the last day of the month:
    1. Deduct total deficit from next month's `Ready to Assign`.
    2. Flag notification: Option A (keep deficit deducted from cash) vs. Option B (convert amount to card's `initial_debt_baseline`).
* **Acceptance Criteria & Verification**:
  * Unit test: Category overspent by \$45 on credit card reduces next month's Ready to Assign by \$45.00.

### `TASK-5.1.3`: MonthEndRolloverWorkflow in Temporal
* **Target Files**:
  * `backend/internal/temporal/workflows/month_end.go`
  * `backend/internal/temporal/activities/rollover_activities.go`
* **Dependencies**: `TASK-5.1.1`, `TASK-5.1.2`, `TASK-4.2.1`
* **Business Logic & Edge Cases**:
  * Schedule at `0 0 1 * *` (midnight UTC on 1st of month).
  * Executes in a single database transaction per workspace:
    1. Apply rollover logic across all categories.
    2. Freeze new monthly baseline in `monthly_category_budgets`.
    3. Audit entries in `budget_reallocations`.
* **Acceptance Criteria & Verification**:
  * Temporal test suite simulation: rollover completes and new month baselines exist.

### `TASK-5.1.4`: Historical Spend Trend & Budget Recommendation Engine
* **Target Files**:
  * `backend/internal/budget/trend_analysis.go`
  * `backend/internal/budget/trend_analysis_test.go`
  * `frontend/src/lib/components/budget/RecommendationBanner.svelte`
* **Dependencies**: `TASK-5.1.3`, `TASK-2.2.1`
* **Data Contracts & Interfaces**:
  ```go
  package budget

  import (
      "context"
      "github.com/google/uuid"
      "github.com/jackc/pgx/v5"
      "github.com/shopspring/decimal"
  )

  type SpendRecommendation struct {
      CategoryID          uuid.UUID       `json:"category_id"`
      CategoryName        string          `json:"category_name"`
      CurrentPlanned      decimal.Decimal `json:"current_planned"`
      TrailingMedianSpend decimal.Decimal `json:"trailing_median_spend"`
      PercentageDeviation decimal.Decimal `json:"percentage_deviation"`
      ShouldPrompt        bool            `json:"should_prompt"`
  }

  func ComputeSpendRecommendations(
      ctx context.Context,
      tx pgx.Tx,
      workspaceID uuid.UUID,
      yearMonth time.Time,
  ) ([]SpendRecommendation, error)
  ```
* **Business Logic & Edge Cases**:
  * Compute trailing 3-month median actual spend for each category:
    $$\text{HistoricalSpend}(c) = \text{Median}(\text{ActualSpent}_{M-1}, \text{ActualSpent}_{M-2}, \text{ActualSpent}_{M-3})$$
  * Deviation formula:
    $$\text{Deviation} = \frac{\vert{}\text{HistoricalSpend}(c) - \text{Planned}_{M}\vert{}}{\text{Planned}_{M}}$$
  * If $\text{Deviation} > 0.15$ ($15\%$ threshold) and $\text{Planned}_M > 0$:
    * Set `ShouldPrompt = true`.
    * Surface subtle inline recommendation banner in SvelteKit UI:
      *"You averaged \$420 on Dining over the last 3 months. Adjust target from \$500 to \$420?"*
    * Button: [ Apply Recommended Target ] updates `monthly_category_budgets.original_planned`.
* **Acceptance Criteria & Verification**:
  * Run: `go test -v ./backend/internal/budget -run TestComputeSpendRecommendations`
  * Test Case 1: Past 3 months spend = [\$410, \$420, \$430] (median \$420). Planned = \$500. Deviation = $16\%$. Assert `ShouldPrompt == true`.
  * Test Case 2: Past 3 months spend = [\$480, \$490, \$500] (median \$490). Planned = \$500. Deviation = $2\%$. Assert `ShouldPrompt == false`.

---

# Milestone 6: Receipt Ingestion, Parsing, & Multi-Package Matching

## Epic M6.1: Inbound Webhook & Verification Receiver

### `TASK-6.1.1`: Cloudflare Inbound Webhook Ingress Endpoint
* **Target Files**:
  * `backend/internal/receipts/handler.go`
  * `backend/internal/receipts/webhook_auth.go`
  * `backend/internal/receipts/routes.go`
* **Dependencies**: `TASK-1.3.1`
* **Data Contracts & Interfaces**:
  ```go
  type InboundEmailWebhookPayload struct {
      WorkspaceID string `json:"workspace_id"`
      From        string `json:"from"`
      To          string `json:"to"`
      Subject     string `json:"subject"`
      RawHTML     string `json:"raw_html"`
      RawText     string `json:"raw_text"`
  }
  ```
* **Business Logic & Edge Cases**:
  * Verify `X-Webhook-Secret` matches `RECEIPT_WEBHOOK_SECRET`.
  * Extract destination alias from `To` field; insert receipt with status `UNMATCHED`.
  * Return HTTP 202 Accepted.
* **Acceptance Criteria & Verification**:
  * Send test webhook: `curl -i -X POST http://localhost:8080/api/v1/webhooks/receipts -H "X-Webhook-Secret: testsecret123" ...`
  * Assert HTTP 202 Accepted and row created in `receipts`.

### `TASK-6.1.2`: Gmail Auto-Forward Verification Code Interceptor
* **Target Files**:
  * `backend/internal/receipts/forward_verification.go`
  * `backend/internal/receipts/forward_verification_test.go`
* **Dependencies**: `TASK-6.1.1`
* **Business Logic & Edge Cases**:
  * When sender contains `forwarding-noreply@google.com`:
    * Extract regex: `Confirmation code:\s*([0-9]{6,9})`.
    * Update `workspace_settings.gmail_verification_code`.
    * Mark receipt status as `HANDLED_SYSTEM`.
* **Acceptance Criteria & Verification**:
  * Unit test: Sample email with "Confirmation code: 48291038" extracts `"48291038"` and updates settings table.

### `TASK-6.1.3`: Receipt Forwarding Onboarding & Verification UI
* **Target Files**:
  * `backend/internal/receipts/settings_handler.go`
  * `frontend/src/routes/(app)/settings/receipts/+page.svelte`
  * `frontend/src/lib/components/receipts/GmailXmlGenerator.ts`
* **Dependencies**: `TASK-6.1.2`, `TASK-1.4.1`
* **Data Contracts & Interfaces**:
  ```typescript
  export interface ReceiptSettingsDTO {
      inboundEmailAlias: string;
      latestVerificationCode: string | null;
  }

  export function generateGmailFilterXml(forwardingAddress: string): string;
  ```
* **Business Logic & Edge Cases**:
  * Endpoint `GET /api/v1/workspace/receipt-settings` returns inbound alias and latest intercepted verification code.
  * SvelteKit UI:
    * Displays personal forwarding address (`w_789f@receipts.yourdomain.com`) with one-click copy.
    * Real-time polling/banner: When `gmail_verification_code` is detected, display:
      *"Gmail verification code detected: 482-910-38 [Copy Code]"*.
    * Button: [ Download Gmail Filter (.xml) ].
      Generates compliant XML filter matching retailers (`from:(auto-confirm@amazon.com OR order-update@walmart.com OR target.com)`) forwarding to the user's alias.
* **Acceptance Criteria & Verification**:
  * Playwright/Vitest: Render page, click Download XML, assert generated file starts with `<?xml version='1.0' encoding='UTF-8'?>` and contains the user's alias.

---

## Epic M6.2: Parsing Engines & Extraction Activities

### `TASK-6.2.1`: Retailer Headless HTML DOM Parser (Tier 1)
* **Target Files**:
  * `backend/internal/receipts/parser/html_parser.go`
  * `backend/internal/receipts/parser/amazon.go`
  * `backend/internal/receipts/parser/parser_test.go`
* **Dependencies**: `TASK-6.1.1`
* **Data Contracts & Interfaces**:
  ```go
  type ParsedReceipt struct {
      OrderID     string
      Merchant    string
      TotalAmount decimal.Decimal
      OrderDate   time.Time
      LineItems   []LineItem
  }

  type LineItem struct {
      Description string
      Amount      decimal.Decimal
  }

  func ParseRetailerHTML(rawHTML string) (*ParsedReceipt, error)
  ```
* **Business Logic & Edge Cases**:
  * Use `PuerkitoBio/goquery`.
  * Extract Amazon order ID: `[0-9]{3}-[0-9]{7}-[0-9]{7}`.
  * Extract total and line items; validate that sum of items plus tax equals total.
* **Acceptance Criteria & Verification**:
  * Unit test: Sample HTML parses order ID `"112-4019283-1928301"`, total \$54.20, and 2 item lines.

### `TASK-6.2.2`: Local Ollama Vision Extraction Client (Tier 2)
* **Target Files**:
  * `backend/internal/receipts/parser/ollama_client.go`
  * `backend/internal/receipts/parser/ollama_test.go`
* **Dependencies**: `TASK-6.1.1`
* **Data Contracts & Interfaces**:
  ```go
  type OllamaClient interface {
      ExtractReceiptFromImage(ctx context.Context, imageBytes []byte) (*ParsedReceipt, error)
  }
  ```
* **Business Logic & Edge Cases**:
  * Target model: `qwen2.5-vl:7b` via Ollama REST API (`/api/generate`).
  * Enforce strict JSON output schema.
  * Retry once with temperature `0.1` on malformed JSON.
* **Acceptance Criteria & Verification**:
  * Test against mock Ollama instance: returned struct contains merchant, date, total, and item lines.

---

## Epic M6.3: Temporal Receipt Matching & Multi-Package Reconciliation

### `TASK-6.3.1`: ReceiptReconciliationWorkflow Definition
* **Target Files**:
  * `backend/internal/temporal/workflows/receipt_reconcile.go`
* **Dependencies**: `TASK-6.2.1`, `TASK-4.2.1`
* **Business Logic & Edge Cases**:
  * Spawn workflow on receipt ingestion.
  * Wait up to 7 days for matching bank transaction signal or query:
    ```go
    workflow.AwaitWithTimeout(ctx, 7*24*time.Hour, func() bool {
        return matchedTransactionID != nil
    })
    ```
  * If timeout expires without match, update status to `ORPHANED`.
* **Acceptance Criteria & Verification**:
  * Temporal test suite simulation: Advance clock by 8 days; workflow completes and marks receipt orphaned.

### `TASK-6.3.2`: Multi-Package Subset-Sum Reconciliation Engine
* **Target Files**:
  * `backend/internal/receipts/matcher/subset_sum.go`
  * `backend/internal/receipts/matcher/subset_sum_test.go`
* **Dependencies**: `TASK-6.3.1`
* **Data Contracts & Interfaces**:
  ```go
  type MatchResult struct {
      MatchedTransactions []uuid.UUID
      IsSplitMatch        bool
  }

  func FindSubsetMatch(targetTotal decimal.Decimal, candidates []TxCandidate) *MatchResult
  ```
* **Business Logic & Edge Cases**:
  * Find unmatched charges from merchant within $\pm 5$ days of receipt date.
  * Subset-Sum algorithm for combinations of size 1, 2, or 3:
    $$\sum_{k \in S} \text{Candidate}[k].\text{Amount} == \text{targetTotal}$$
  * Link matches in `transaction_receipt_links` and split lines across matched charges.
* **Acceptance Criteria & Verification**:
  * Unit test: Receipt total \$100.00 matches candidate pair [-\$30.00, -\$70.00].

### `TASK-6.3.3`: Orphaned Receipt Queue & Review UI
* **Target Files**:
  * `frontend/src/routes/(app)/receipts/+page.svelte`
  * `frontend/src/lib/components/receipts/OrphanedReceiptCard.svelte`
* **Dependencies**: `TASK-6.3.2`, `TASK-1.4.1`
* **Business Logic & Edge Cases**:
  * Display receipts with `status == 'ORPHANED'`.
  * Actions: "Link to Bank Charge", "Convert to Manual Transaction", or "Discard".
* **Acceptance Criteria & Verification**:
  * Playwright: Clicking "Convert to Manual" sends `POST /api/v1/receipts/:id/convert` and removes card from view.

---

# Milestone 7: Push Notifications, Backup Pipeline, & Hardening

## Epic M7.1: W3C Web Push Notifications

### `TASK-7.1.1`: Web Push Subscription Management & VAPID Dispatcher
* **Target Files**:
  * `backend/internal/notifications/push_service.go`
  * `backend/internal/notifications/handler.go`
  * `backend/internal/notifications/push_test.go`
* **Dependencies**: `TASK-1.3.1`
* **Data Contracts & Interfaces**:
  ```go
  type PushSubscriptionDTO struct {
      Endpoint string `json:"endpoint"`
      P256dh   string `json:"p256dh"`
      Auth     string `json:"auth"`
  }

  type NotificationPayload struct {
      Title string `json:"title"`
      Body  string `json:"body"`
      URL   string `json:"url"`
  }

  type PushService interface {
      Subscribe(ctx context.Context, userID uuid.UUID, sub PushSubscriptionDTO) error
      SendNotification(ctx context.Context, userID uuid.UUID, payload NotificationPayload) error
  }
  ```
* **Business Logic & Edge Cases**:
  * Dispatch notifications using `SherClockHolmes/webpush-go`.
  * If push endpoint returns HTTP 410 Gone, delete subscription row automatically.
* **Acceptance Criteria & Verification**:
  * Test with mock push server: valid encrypted payload sent; 410 deletes subscription.

### `TASK-7.1.2`: iOS PWA Push Onboarding & Gesture Flow
* **Target Files**:
  * `frontend/src/lib/notifications/push.ts`
  * `frontend/src/lib/components/notifications/PushPromptModal.svelte`
* **Dependencies**: `TASK-7.1.1`, `TASK-1.4.1`
* **Business Logic & Edge Cases**:
  * Check iOS standalone mode: `('standalone' in window.navigator) && (window.navigator.standalone)`.
  * On explicit user tap "Enable Alerts", call `Notification.requestPermission()`.
  * Post subscription to `/api/v1/notifications/subscribe`.
* **Acceptance Criteria & Verification**:
  * Component test: Verify permission prompt binds only to user click gesture.

### `TASK-7.1.3`: Spending Velocity Notification Triggers & Cooldown
* **Target Files**:
  * `backend/internal/notifications/evaluator.go`
  * `backend/internal/notifications/evaluator_test.go`
* **Dependencies**: `TASK-7.1.1`, `TASK-2.2.3`, `TASK-3.1.2`
* **Business Logic & Edge Cases**:
  * Trigger 1: Burn ratio $\ge 0.80$ with 24-hour category cooldown.
  * Trigger 2: Statement due in $\le 5$ days AND Checking Balance $<$ Statement Balance.
* **Acceptance Criteria & Verification**:
  * Unit test: Dispatches alert at 82% spend; second charge within 1 hour is suppressed by cooldown.

---

## Epic M7.2: Encrypted Google Drive Backup Pipeline

### `TASK-7.2.1`: Asymmetric age & rclone Container Backup Script
* **Target Files**:
  * `scripts/backup/backup.sh`
  * `scripts/backup/Dockerfile`
  * `scripts/backup/backup_test.sh`
* **Dependencies**: `TASK-1.2.2`
* **Business Logic & Edge Cases**:
  * Stream: `pg_dump | gzip -9 | age -r "$AGE_PUBLIC_KEY" > /tmp/archive.sql.gz.age`.
  * Transfer: `rclone --config /etc/rclone/rclone.conf copy /tmp/archive.sql.gz.age gdrive:HomelabBackups/BudgetApp/`.
  * Private key never stored on cluster.
* **Acceptance Criteria & Verification**:
  * Run local test `backup_test.sh`: public key encrypts; private key decrypts valid SQL dump.

### `TASK-7.2.2`: Kubernetes Backup CronJob Manifest
* **Target Files**:
  * `k8s/base/cronjob-backup.yaml`
  * `k8s/base/backup-secrets.yaml.example`
* **Dependencies**: `TASK-7.2.1`
* **Business Logic & Edge Cases**:
  * Schedule: `0 2 * * *`. `concurrencyPolicy: Forbid`. Mount rclone config as secret volume.
* **Acceptance Criteria & Verification**:
  * Command: `kubectl apply --dry-run=client -f k8s/base/cronjob-backup.yaml` returns valid syntax.

---

## Epic M7.3: Kubernetes Deployment & Production Hardening

### `TASK-7.3.1`: Production Multi-Stage Dockerfiles
* **Target Files**:
  * `backend/Dockerfile`
  * `frontend/Dockerfile`
* **Dependencies**: `TASK-1.3.1`, `TASK-1.4.1`
* **Business Logic & Edge Cases**:
  * Go binary built via `CGO_ENABLED=0`; final image `alpine:3.20` or `scratch` running as `10001:10001`.
  * Frontend built via Node 20; served via unprivileged `nginx:alpine-slim`.
* **Acceptance Criteria & Verification**:
  * `docker build -t budget-backend:test -f backend/Dockerfile ./backend` succeeds with image $\le 30\text{MB}$.

### `TASK-7.3.2`: Production Kubernetes Kustomize Manifests
* **Target Files**:
  * `k8s/base/kustomization.yaml`
  * `k8s/base/deployment-backend.yaml`
  * `k8s/base/deployment-frontend.yaml`
  * `k8s/base/service.yaml`
  * `k8s/base/ingress.yaml`
* **Dependencies**: `TASK-7.3.1`
* **Business Logic & Edge Cases**:
  * Probes: HTTP `/livez` and `/healthz`.
  * Ingress: `/api/*` -> backend service; `/*` -> frontend service.
* **Acceptance Criteria & Verification**:
  * Command: `kubectl kustomize k8s/base` produces valid Kubernetes manifest YAML.