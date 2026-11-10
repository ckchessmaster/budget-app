# Budget App

A private, self-hosted, multi-tenant zero-based budgeting platform combining the operational simplicity of **EveryDollar** with the mathematical solvency safeguards of **YNAB**.

For the complete technical specification, architectural diagrams, and PostgreSQL schema, see the [Master Design Document](docs/master_design_document.md).

---

## Foundational Tenets

1. **Zero-Based Allocation:** Every dollar of projected monthly income is assigned a job ($\text{Income} - \text{Expenses} = \$0$).
2. **Frictionless Ingestion:** Automated bank and credit union account synchronization via SimpleFIN Bridge without manual refreshes.
3. **Line-Item Reality:** Inbound email forwarding pipeline to parse itemized receipt records (HTML DOM & local vision LLMs) and reconcile multi-package splits (e.g., Amazon orders).
4. **Debt-Aware Cash Flow:** Transactor-friendly cash-flow tracking paired with real-time liquid solvency buffers to prevent accidental credit card float.
5. **Zero Infrastructure Lock-In:** Local orchestration via Microsoft Aspire (TypeScript) and deployment to on-premises Kubernetes without paid SaaS dependencies.

---

## Technology Stack

| Layer | Technology | Details |
| :--- | :--- | :--- |
| **Frontend** | [SvelteKit](https://svelte.dev/) | Runes, TailwindCSS, IndexedDB offline caching, W3C Web Push PWA |
| **Backend API** | [Go](https://go.dev/) | Chi router, pgx driver, low memory footprint (~25 MB RAM) |
| **Database** | [PostgreSQL 16](https://www.postgresql.org/) | Native Row-Level Security (RLS) multi-tenancy, arbitrary-precision `NUMERIC` |
| **Workflows** | [Temporal](https://temporal.io/) | Durable execution for bank sync, receipt reconciliation, and retries |
| **Local Dev Host** | [Microsoft Aspire](https://learn.microsoft.com/en-us/dotnet/aspire/) | TypeScript AppHost orchestrating Postgres, Dex, Temporal, Go, and Vite |
| **Identity** | [Dex](https://github.com/dexidp/dex) / [Authentik](https://goauthentik.io/) | Standard OIDC with JWT Bearer tokens and DB RLS bridge |
| **Receipt Ingestion** | Cloudflare Email Routing & [Ollama](https://ollama.ai/) | Webhook-driven MIME parsing with local `Qwen2.5-VL-7B` / `MiniCPM-V` |

---

## Architecture Overview

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

---

## Planned Project Structure

```
budget-app/
├── .editorconfig              # Consistent code formatting rules
├── .env.example               # Template for environment variables
├── .gitignore                 # Git ignore rules for Go, Svelte, Aspire, etc.
├── LICENSE                    # Repository license
├── README.md                  # Project overview and quickstart
├── docs/
│   └── master_design_document.md  # Comprehensive architectural and system specs
├── apphost/                   # Microsoft Aspire TypeScript AppHost (orchestration)
├── backend/                   # Go API service and Temporal worker
├── frontend/                  # SvelteKit PWA client application
└── k8s/                       # Production Kubernetes deployment manifests
```

---

## Implementation Roadmap

* **Phase 1: Local Foundation & Core Ledger**
  * Microsoft Aspire TypeScript AppHost setup (PostgreSQL, Dex, Temporal)
  * PostgreSQL schema migrations with Row-Level Security (RLS)
  * Core zero-based allocation engine (planned vs. actual, overspend cover drawer)

* **Phase 2: Banking Integration & Mobile Client**
  * SimpleFIN Bridge ingestion via Temporal `BankSyncWorkflow`
  * Pending vs. posted transaction matching & internal transfer auto-pairing
  * SvelteKit PWA with offline `IndexedDB` transaction logging

* **Phase 3: Receipt Automation & Financial Safeguards**
  * Cloudflare Email Routing webhook receiver
  * Multi-package subset-sum Amazon reconciliation workflow in Temporal
  * Credit card Solvency Buffer gauge & debt paydown amortization

* **Phase 4: Cluster Deployment & Hardening**
  * Kubernetes manifests & Authentik OIDC integration
  * Nightly asymmetric `age` + `rclone` Google Drive backup CronJob
  * W3C Web Push notifications on mobile home-screen PWAs

---

## License

This project is licensed under the terms specified in the [LICENSE](LICENSE) file.