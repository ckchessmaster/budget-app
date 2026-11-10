# Master Design Document: Personal & Household Zero-Based Budgeting System

---

## 1. Executive Summary & Core Philosophy

This platform is a private, self-hosted, multi-tenant financial platform designed to deliver the operational simplicity of **EveryDollar** paired with the mathematical solvency safeguards of **YNAB**. 

### Foundational Tenets
* **Zero-Based Allocation:** Every dollar of projected monthly income is assigned a job ($\text{Income} - \text{Expenses} = \$0$).
* **Frictionless Ingestion:** Bank feeds and credit union accounts (specifically VyStar) synchronize in the background without manual user refreshes.
* **Line-Item Reality:** Financial institutions transmit only high-level Level 1 data (merchant, date, total amount) rather than itemized receipt records. The system uses an inbound email forwarding pipeline to parse line-item SKUs and match split charges.
* **Debt-Aware Cash Flow:** Provides a transactor-friendly cash-flow interface while enforcing real-time liquid solvency buffers to eliminate accidental credit card float.
* **Zero Infrastructure Lock-In:** Fully orchestrated locally via Microsoft Aspire (TypeScript) and deployed into an on-premises Kubernetes cluster with zero external paid SaaS dependencies (aside from bank data access via SimpleFIN).

---

## 2. Core Financial Engine & Business Logic

### 2.1 The Zero-Based Allocation Ledger
The budget operates on strict calendar months, starting with projected monthly cash flow:

$$\text{Projected Income} - \sum \text{Allocated Category Budgets} = \$0.00$$

* **Baseline Freeze:** When a month begins, initial targets are locked into `monthly_category_budgets.original_planned`.
* **Audit Trail Reallocations:** Any inter-category reallocations (moving money from Groceries to Dining) are written to an append-only `budget_reallocations` ledger.
* **Dynamic Effective Target:**
  $$\text{Effective Target} = \text{Original Planned} + \sum \text{Reallocations In} - \sum \text{Reallocations Out}$$
* **Burn-Down Velocity:** Category health tracks posted plus pending charges against the effective target:
  $$\text{Available Balance} = \text{Effective Target} - \sum (\text{Posted Spend} + \text{Pending Spend})$$

```
Category: Dining Out
[Planned: $300] ──(+$50 Cover from Groceries)──► [Effective Target: $350]
Spend Progress: [████████████████████░░░░░░░░░░] $240 Spent | $110 Available
```

### 2.2 Overspending Interlock & Resolution
* **The Overspent State:** If a transaction causes $\text{Available Balance} < \$0.00$, the budget header triggers an `UNBALANCED` warning state.
* **Cover Drawer:** The UI prevents closing the month until the user selects a donor category with surplus funds to cover the deficit. The backend executes the transfer in a single atomic database transaction.
* **Month-End Rollover Boundary:** Any unresolved deficits at month-end are deducted directly from the next month's assignable income pool ("Ready to Assign").

### 2.3 Category Rollover & Sinking Fund Mechanics
Every category implements an explicit `rollover_mode`:

| Mode | Behavior at Month Boundary | Target Use Case |
| :--- | :--- | :--- |
| **`SWEEP`** | Unspent balance resets to $\$0.00$; surplus is redirected to a designated savings category or returned to next month's assignable pool. | Variable expenses (Dining, Entertainment, Groceries). |
| **`ROLLOVER_BALANCE`** | Unspent balance rolls forward into the next month's available balance: $\text{Available}_{M+1} = \text{Remaining}_M + \text{Planned}_{M+1}$. | Sinking funds (Auto Maintenance, Medical Buffer, Clothing). |
| **`TARGET_CAP`** | Replenishes only what was spent up to a fixed target limit: $\text{Planned}_{M+1} = \max(0, \text{Target Cap} - \text{Remaining}_M)$. | Fixed cash buffers (e.g., $\$1,000$ Emergency Buffer). |

* **Paced Sinking Funds:** For categories with a `target_amount` and `target_date`, the engine dynamically calculates the required monthly contribution:
  $$\text{Monthly Due} = \frac{\text{Target Amount} - \text{Current Balance}}{\text{Months Remaining Until Target Date}}$$

### 2.4 Credit Card Architecture: Transactors vs. Revolvers

```
                          ┌───────────────────────────┐
                          │ Credit Card Account Mode  │
                          └─────────────┬─────────────┘
                                        │
                 ┌──────────────────────┴──────────────────────┐
                 ▼                                             ▼
       [Mode: PAY_IN_FULL]                           [Mode: DEBT_PAYDOWN]
Transactions act as standard cash-flow.       Starting debt balance is frozen.
Card payments are neutral transfers.          Payments target a dedicated Category.
Enforces Liquid Solvency Buffer:              Active spending auto-splits payments
Liquid Cash ≥ Card Balances                   between transfers and debt reduction.
```

#### Mode A: Pay-in-Full (Transactor)
* **Transaction Math:** Spending on a card directly decrements the expense category (e.g., $\$50$ card charge reduces `Groceries` by $\$50$).
* **Payment Math:** Paying the statement from checking is logged as a **neutral internal transfer** (`Checking` $\rightarrow$ `Credit Card`) with zero category assignment.
* **The Solvency Safety Buffer:** To prevent "credit card float" (spending next month's unearned paycheck to pay this month's statement), the dashboard displays a persistent solvency check:
  $$\text{Solvency Buffer} = \sum \text{Liquid Cash (Checking + Savings)} - \sum \text{Active Card Balances}$$
  If $\text{Solvency Buffer} < \$0.00$, the UI displays an alert: *"Floating Debt Detected: Card balances exceed liquid cash by $\$X$."*
* **Statement Due Date Watchdog:** If an upcoming credit card statement due date is $\le 5$ days away and the checking account balance is lower than the statement balance, an urgent push notification is dispatched.

#### Mode B: Debt Paydown (Revolver)
* **Starting Baseline:** Pre-existing debt is frozen upon account creation (`initial_debt_baseline`).
* **Paydown Envelope:** The user assigns a dedicated budget category (e.g., `Chase Payoff: $400/mo`).
* **Active Spend Auto-Splitting:** If a user continues spending on a debt-carrying card, the payment is automatically split into two legs upon clearance:
  1. **Transfer Leg:** Reimburses the new monthly spending already budgeted in categories.
  2. **Expense Leg:** Allocates the remaining payment amount to the debt payoff category, reducing the pre-existing principal.
* **Amortization Burndown:** Tracks historical principal vs. interest progress and dynamically projects the debt-free date based on payment velocity.

---

## 3. Banking Rails & Reconciliation State Machine

### 3.1 Aggregation Strategy (SimpleFIN Bridge)
* **Provider:** SimpleFIN Bridge (utilizing MX data rails underneath).
* **Credit Union Resiliency:** Resolves token invalidation and Captcha friction with regional credit unions (specifically VyStar).
* **Ingestion Model:** Background polling executed by a Temporal Schedule every 4 to 6 hours. Polling throttles protect against bank rate limits.

### 3.2 Pending vs. Posted Reconciliation Engine
Merchants frequently alter final posted amounts (restaurant tips, fuel holds) and banks rotate transaction IDs during settlement. 

```
[Bank Feed] ──► Pending Record (Ext ID: ext_101, $40.00, PENDING)
                     │
                     ▼ (User assigns: "Dining Out", adds note: "Burger Bistro")
                     │
[Bank Feed] ──► Posted Record  (Ext ID: ext_999, $48.50, POSTED)
                     │
                     ▼ (Deduplication & Heuristic Matcher)
                Updates Record: (Ext ID: ext_999, $48.50, POSTED)
                Preserves: Category ("Dining Out"), User Notes ("Burger Bistro")
                UI Flag: Amber badge showing settled delta (+$8.50 tip)
```

1. **State Transition:** Pending transactions appear immediately with an italicized, muted UI badge. Users can categorize, tag, and split them immediately.
2. **Matching Heuristics:** When a `POSTED` transaction arrives:
   * Match against existing `PENDING` records in the same account where:
     $$\text{Date Delta} \le 4\text{ days} \quad \text{AND} \quad \text{LevenshteinSimilarity}(\text{Merchant}) \ge 0.80$$
   * **Tip Discrepancy Tolerance:** Amount is identical OR posted amount is between $0\%$ and $30\%$ higher.
3. **Field Preservation:** When matched, the internal UUID, category ID, and user notes are preserved; the external ID is updated to the posted ID, and status transitions to `POSTED`.

### 3.3 Internal Transfer Auto-Pairing
* When SimpleFIN syncs transactions across accounts, the engine evaluates unmatched records across different internal accounts.
* If a debit on Account A matches a credit on Account B:
  $$\text{Amount}_A = -\text{Amount}_B \quad \text{AND} \quad |\text{Date}_A - \text{Date}_B| \le 3\text{ days}$$
* The system automatically populates `transfer_linked_transaction_id` on both rows, classifies them as a neutral transfer, and removes any budget category requirement.

### 3.4 Offline Manual Entry & Conflict Resolution
* **Client Write:** When a transaction is logged while offline, the SvelteKit PWA generates a client UUID (v4) and persists it locally in `IndexedDB`, immediately decrementing category balances in the UI.
* **Heuristic Auto-Merge:** When internet connectivity returns, the local record is posted to the backend. When SimpleFIN pulls the matching bank record hours or days later:
  * The backend matches the manual entry by Account, Date ($\pm 2$ days), and Amount.
  * The manual record adopts the bank's `external_transaction_id` and promotes from `MANUAL_OFFLINE` to `BANK_FEED` silently without creating duplicates.

---

## 4. Receipt Parsing & Line-Item Split Architecture

### 4.1 Inbound Email Routing Pipeline
Because consumer payment rails transmit only Level 1 data, itemized receipt data must be ingested out-of-band. Residential ISPs routinely block Port 25, so direct self-hosted SMTP reception is avoided.

```
[Retailer Confirmation] ──► [User's Personal Gmail/Fastmail]
                                     │
                                     ▼ (Auto-Forward Filter)
                         [Cloudflare Email Routing]
                                     │
                             (HTTP Webhook POST)
                                     │
                         [K8s / Go Ingress Worker]
                                     │
                                     ▼
                        [Temporal Reconciliation Workflow]
```

1. **Address Allocation:** Each user workspace is assigned an inbound address: `workspace_hash@receipts.yourdomain.com`.
2. **Automated Filter Verification:** The Go ingress service inspects incoming email subjects. When a verification message from `forwarding-noreply@google.com` arrives, it automatically extracts the 6-to-9-digit verification code and exposes it in the frontend UI, allowing users to verify forwarding filters in two clicks.
3. **MIME Processing:** Cloudflare receives the email and invokes a webhook payload containing the raw RFC 822 MIME data to the backend `/api/v1/webhooks/receipts` endpoint.

### 4.2 Parsing & Local Extraction
1. **DOM Parsing (Tier 1):** For standard HTML receipts from major retailers (Amazon, Walmart, Target), headless parsers extract order IDs, merchant names, tax, and itemized lines directly from the HTML structure.
2. **Local Vision LLM (Tier 2):** For image attachments or irregular PDF receipts, a background job dispatches the image to an on-premises **Ollama** instance running `Qwen2.5-VL-7B` or `MiniCPM-V` with a strict JSON output schema.

### 4.3 Multi-Package Amazon Matching Engine
Amazon routinely splits single order receipts across multiple bank charges as items ship from different distribution centers.

```
Amazon Order #1002 ($100.00 Total)
├── Item A: $30.00 ──► Ships Mon ──► Bank Charge 1: $30.00 (Extracted)
└── Item B: $70.00 ──► Ships Wed ──► Bank Charge 2: $70.00 (Extracted)
```

1. **Durable Temporal Workflow:** A `ReceiptReconciliationWorkflow` is spawned for every parsed receipt, remaining active for up to 7 days.
2. **Subset-Sum Matcher:** The workflow monitors incoming bank transactions for the matching merchant:
   * **Exact Match:** Receipt Total = Single Bank Charge.
   * **Multi-Split Match:** Receipt Total = Sum of 2 or 3 unlinked bank charges from that merchant within a 5-day window.
3. **Execution:** When matched, child split records are generated under the respective parent transactions and mapped to specific category budgets. If unmatched after 7 days, the receipt is marked `ORPHANED` and surfaces in the manual review drawer.

---

## 5. Technology Stack & Operational Architecture

```
┌────────────────────────────────────────────────────────────────────────┐
│                          Client Tier (PWA)                             │
│       SvelteKit (Runes, TailwindCSS, IndexedDB, W3C Web Push)          │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ (JWT Bearer / JSON REST)
┌───────────────────────────────────▼────────────────────────────────────┐
│                           API & Worker Tier                            │
│                 Go (Chi Router) / Temporal Worker Pod                  │
└──────────────┬────────────────────────────┬────────────────────────────┘
               │                            │
               ▼                            ▼
┌──────────────────────────────┐   ┌─────────────────────────────────────┐
│       Data Storage Tier      │   │          Temporal Cluster           │
│     PostgreSQL 16 (RLS)      │   │ (Schedules, Workflows, Dev Server)  │
└──────────────────────────────┘   └─────────────────────────────────────┘
```

| Component | Technology | Rationale & Trade-offs |
| :--- | :--- | :--- |
| **Frontend** | **SvelteKit (PWA)** | Compiles to minimal JS ($\approx 30\text{ KB}$ runtime); fast TTI on mobile devices; simple Service Worker lifecycle for offline caching. |
| **Backend API** | **Go (Chi)** | Low idle memory footprint ($\approx 25\text{ MB}$ RAM); high concurrency; compiles to a single scratch-based container binary. |
| **Database** | **PostgreSQL 16** | Native Row-Level Security (RLS); arbitrary-precision `NUMERIC` financial math; `JSONB` for unstructured receipt metadata. |
| **Orchestration** | **Temporal** | Eliminates custom polling crons; provides durable multi-day workflows for receipt matching and exponential retry backoffs for external APIs. |
| **Inbound Email** | **Cloudflare Email Routing** | Bypasses residential ISP Port 25 blocking; forwards emails directly to backend webhooks. |
| **Local Dev Host** | **Microsoft Aspire (TypeScript)** | Single CLI command (`aspire run`) orchestrates Postgres, Dex, Temporal, Go, and Vite with unified distributed tracing and container telemetry. |

---

## 6. Complete Database Schema & Multi-Tenancy (PostgreSQL DDL)

```sql
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ============================================================================
-- 1. TENANCY & ACCESS CONTROL
-- ============================================================================
CREATE TABLE workspaces (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE workspace_members (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id UUID NOT NULL, -- Subject UUID from OIDC JWT
    role VARCHAR(50) NOT NULL DEFAULT 'owner',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (workspace_id, user_id)
);

CREATE INDEX idx_workspace_members_user ON workspace_members(user_id);

-- ============================================================================
-- 2. ACCOUNTS & FINANCIAL RAILS
-- ============================================================================
CREATE TYPE account_type AS ENUM ('checking', 'savings', 'credit_card', 'investment', 'loan');
CREATE TYPE cc_mode AS ENUM ('PAY_IN_FULL', 'DEBT_PAYDOWN');

CREATE TABLE accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    external_account_id VARCHAR(255),
    name VARCHAR(150) NOT NULL,
    type account_type NOT NULL,
    current_balance NUMERIC(12, 2) NOT NULL DEFAULT 0.00,
    is_on_budget BOOLEAN NOT NULL DEFAULT TRUE,
    
    -- Credit Card Specific Metadata
    is_credit_card BOOLEAN GENERATED ALWAYS AS (type = 'credit_card') STORED,
    card_mode cc_mode DEFAULT 'PAY_IN_FULL',
    initial_debt_baseline NUMERIC(12, 2) DEFAULT 0.00,
    apr_percentage NUMERIC(5, 2) DEFAULT 0.00,
    statement_due_day SMALLINT CHECK (statement_due_day BETWEEN 1 AND 31),
    debt_payoff_category_id UUID,
    
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- ============================================================================
-- 3. CATEGORIES & BUDGET PLANS
-- ============================================================================
CREATE TYPE rollover_type AS ENUM ('SWEEP', 'ROLLOVER_BALANCE', 'TARGET_CAP');
CREATE TYPE goal_cadence AS ENUM ('NONE', 'MONTHLY', 'ANNUAL', 'TARGET_DATE');

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

ALTER TABLE accounts 
    ADD CONSTRAINT fk_accounts_debt_category 
    FOREIGN KEY (debt_payoff_category_id) REFERENCES categories(id) ON DELETE SET NULL;

CREATE TABLE monthly_category_budgets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    category_id UUID NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    year_month DATE NOT NULL, -- Stored as first of month: 'YYYY-MM-01'
    original_planned NUMERIC(12, 2) NOT NULL DEFAULT 0.00,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(workspace_id, category_id, year_month)
);

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

-- ============================================================================
-- 4. TRANSACTIONS & TRANSFERS
-- ============================================================================
CREATE TYPE transaction_status AS ENUM ('PENDING', 'POSTED');
CREATE TYPE transaction_origin AS ENUM ('BANK_FEED', 'MANUAL_OFFLINE', 'RECURRING');

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

CREATE INDEX idx_transactions_workspace_lookup ON transactions(workspace_id, transaction_date DESC);
CREATE INDEX idx_transactions_dedup ON transactions(account_id, transaction_date, amount);

-- ============================================================================
-- 5. RECEIPT INGESTION & LINE ITEMS
-- ============================================================================
CREATE TYPE receipt_status AS ENUM ('UNMATCHED', 'MATCHED_SINGLE', 'MATCHED_MULTI', 'ORPHANED', 'MANUAL');

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

-- ============================================================================
-- 6. PUSH NOTIFICATIONS & SIMPLEFIN INTEGRATION
-- ============================================================================
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

-- ============================================================================
-- 7. ROW-LEVEL SECURITY POLICIES
-- ============================================================================
ALTER TABLE workspaces ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE categories ENABLE ROW LEVEL SECURITY;
ALTER TABLE monthly_category_budgets ENABLE ROW LEVEL SECURITY;
ALTER TABLE budget_reallocations ENABLE ROW LEVEL SECURITY;
ALTER TABLE transactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE receipts ENABLE ROW LEVEL SECURITY;

CREATE OR REPLACE FUNCTION current_app_user_id() RETURNS UUID AS $$
    SELECT NULLIF(current_setting('app.current_user_id', true), '')::UUID;
$$ LANGUAGE SQL STABLE;

CREATE POLICY workspace_isolation ON workspaces FOR ALL USING (
    id IN (SELECT workspace_id FROM workspace_members WHERE user_id = current_app_user_id())
);

CREATE POLICY members_isolation ON workspace_members FOR ALL USING (
    workspace_id IN (SELECT workspace_id FROM workspace_members WHERE user_id = current_app_user_id())
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

CREATE POLICY reallocations_isolation ON budget_reallocations FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

CREATE POLICY transactions_isolation ON transactions FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

CREATE POLICY receipts_isolation ON receipts FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);
```

---

## 7. Authentication & Identity Pipeline

Identity architecture abstracts the provider behind the standard OpenID Connect (OIDC) protocol. The application code interacts only with standard JWT claims.

### 7.1 Environment Identity Configurations
* **Local Development (Dex via Aspire):** Runs as a lightweight local OIDC container (`dex:latest`). Uses a static YAML password database (`admin@dev.local` / `password`), eliminating OAuth redirect configuration on localhost.
* **Production Deployment (Authentik on K8s):** Connects to the existing homelab Authentik deployment. Supports WebAuthn/Passkeys, invite links for friends, and multi-factor authentication (MFA).

### 7.2 Backend RLS Bridge
The Go API extracts the `sub` claim from the validated OIDC JWT and sets a localized session parameter before dispatching database queries within a transaction:

```go
func RunInWorkspaceContext(ctx context.Context, pool *pgxpool.Pool, userID string, fn func(pgx.Tx) error) error {
    tx, err := pool.Begin(ctx)
    if err != nil {
        return err
    }
    defer tx.Rollback(ctx)

    // Enforce Row-Level Security isolation for this user
    _, err = tx.Exec(ctx, "SET LOCAL app.current_user_id = $1", userID)
    if err != nil {
        return fmt.Errorf("failed to inject user RLS identity: %w", err)
    }

    if err := fn(tx); err != nil {
        return err
    }

    return tx.Commit(ctx)
}
```

---

## 8. Local Orchestration Specification (Microsoft Aspire)

Local development orchestrates all backing services, identity, workers, and frontend tools via Microsoft Aspire's TypeScript AppHost.

### 8.1 AppHost Configuration (`apphost.mts`)

```typescript
import { createBuilder } from './.aspire/modules/aspire.mjs';

const builder = await createBuilder();

// 1. Storage: PostgreSQL with persistent volume
const postgres = await builder
  .addPostgres('postgres')
  .withDataVolume('budget-local-db');

const budgetDb = await postgres.addDatabase('budgetdb');

// 2. Identity: Local Dex OIDC Broker
const dex = await builder
  .addContainer('dex', 'ghcr.io/dexidp/dex:latest')
  .withBindMount('./dex/config.yaml', '/etc/dex/config.docker.yaml')
  .withArgs(['dex', 'serve', '/etc/dex/config.docker.yaml'])
  .withHttpEndpoint({ port: 5556, targetPort: 5556, name: 'oidc' });

// 3. Workflow Engine: Temporal Dev Server
const temporal = await builder
  .addContainer('temporal', 'temporalio/dev-server:latest')
  .withEndpoint({ port: 7233, targetPort: 7233, name: 'grpc' })
  .withHttpEndpoint({ port: 8233, targetPort: 8233, name: 'ui' });

// 4. Backend Service (Go API & Temporal Worker)
const backend = await builder
  .addGolangApp('backend', '../backend')
  .withHttpEndpoint({ env: 'PORT' })
  .withReference(budgetDb)
  .withReference(dex)
  .withEnvironment('TEMPORAL_HOST', 'localhost:7233')
  .withEnvironment('OIDC_ISSUER_URL', 'http://localhost:5556/dex')
  .withEnvironment('OIDC_CLIENT_ID', 'budget-app-client')
  .waitFor(budgetDb)
  .waitFor(dex)
  .waitFor(temporal);

// 5. Client Application (SvelteKit PWA)
await builder
  .addViteApp('frontend', '../frontend')
  .withHttpEndpoint({ env: 'PORT' })
  .withExternalHttpEndpoints()
  .withReference(backend)
  .waitFor(backend);

await builder.build().run();
```

---

## 9. Backup, Security, & Disaster Recovery

### 9.1 Encryption Architecture
* **Volume Encryption (At Rest):** Kubernetes PersistentVolumes (PVs) run on encrypted storage pools (ZFS native encryption or LUKS block devices).
* **Application Secrets:** Sensitive external credentials (SimpleFIN setup tokens, email routing webhooks) are encrypted via AES-256-GCM before writing to PostgreSQL, using a key injected from Kubernetes Secrets.
* **Ledger Plaintext:** Financial amounts and transaction names remain unencrypted in PostgreSQL to preserve native indexing, SQL aggregations (`SUM`), and Row-Level Security evaluation.

### 9.2 Asymmetric Encrypted Google Drive Backup Pipeline
To guarantee disaster recovery without hosting private decryption keys on the Kubernetes cluster, backups are encrypted using an asymmetric `age` public key before transfer to Google Drive.

```
[K8s CronJob: Nightly at 02:00 UTC]
  ├── 1. Run `pg_dump` on PostgreSQL
  ├── 2. Stream through `gzip -9`
  ├── 3. Encrypt via `age -r <PUBLIC_KEY>`
  └── 4. Stream to Google Drive via `rclone copy`
```

* **Zero-Knowledge Compromise:** If an attacker breaches the Kubernetes cluster, they gain access only to the `age` **public key**. Historical backups stored in Google Drive cannot be decrypted without the offline private key stored in the administrator's password manager.
* **Restoration Verification:** Recovery requires pulling the `.sql.gz.age` archive and executing:
  ```bash
  age --decrypt -i identity.txt budget_backup.sql.gz.age | gunzip | psql -U postgres -d budgetdb
  ```

---

## 10. Implementation Roadmap

```
Phase 1: Foundation ──────► Phase 2: Ingestion ──────► Phase 3: Intelligence ──────► Phase 4: Production
• Aspire Dev Environment   • SimpleFIN Poller          • Cloudflare Receipts          • K8s Manifests
• PostgreSQL RLS Engine    • Dedup / Auto-Match        • Ollama Line-Item Split       • Authentik OIDC
• Dex Authentication       • Internal Transfers        • Multi-Package Matching       • Rclone Drive Backup
• Core Budget CRUD         • SvelteKit Offline PWA     • Solvency Warnings            • iOS Web Push Prep
```

1. **Phase 1: Local Foundation & Core Ledger**
   * Initialize Microsoft Aspire TypeScript AppHost with Dex, PostgreSQL, and Temporal dev containers.
   * Apply database migrations; verify Row-Level Security isolation tests.
   * Implement zero-based allocation API endpoints (planned vs. actual, overspend reallocation).
2. **Phase 2: Banking Integration & Mobile Client**
   * Build Temporal `BankSyncWorkflow` interfacing with SimpleFIN Bridge.
   * Implement pending vs. posted matching heuristics and internal transfer detection.
   * Build the SvelteKit PWA interface with offline `IndexedDB` transaction logging.
3. **Phase 3: Receipt Automation & Financial Safeguards**
   * Deploy Cloudflare Email Routing webhook endpoint.
   * Implement durable multi-package Amazon matching workflows in Temporal.
   * Build the "Pay-in-Full" Solvency Buffer gauge and "Debt Paydown" burndown views.
4. **Phase 4: Cluster Deployment & Hardening**
   * Deploy application workloads to the on-premise Kubernetes cluster.
   * Cut over OIDC authentication from local Dex to Authentik.
   * Configure the nightly `age` + `rclone` Google Drive backup CronJob.
   * Register W3C VAPID keys and verify Web Push notifications on iOS/Android home-screen PWAs.