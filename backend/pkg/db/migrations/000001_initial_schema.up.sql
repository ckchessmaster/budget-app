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
