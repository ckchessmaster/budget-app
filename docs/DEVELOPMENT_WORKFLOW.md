# Developer Onboarding & Implementation Workflow

Welcome to the **Budget App** engineering team! This document outlines our development lifecycle, repository conventions, task execution standards, and PR workflows to ensure smooth, zero-ambiguity implementation for both human software engineers and autonomous AI coding agents.

---

## 1. Project Organization & Milestones

All work in this repository is tracked across 7 sequential Milestones, broken down into 19 functional Epics and 62 granular Tasks:

| Milestone | Focus Area | Epics | Tasks |
| :--- | :--- | :--- | :--- |
| **Milestone 1** | **Local Foundation & Core Scaffold** | M1.1 – M1.4 | TASK-1.1.1 – 1.4.3 (14 tasks) |
| **Milestone 2** | **Zero-Based Budget Engine & Ledger** | M2.1 – M2.3 | TASK-2.1.1 – 2.3.3 (10 tasks) |
| **Milestone 3** | **Transactions, Accounts, & Offline Client** | M3.1 – M3.3 | TASK-3.1.1 – 3.3.3 (9 tasks) |
| **Milestone 4** | **Banking Rails & Ingestion (SimpleFIN + Temporal)** | M4.1 – M4.2 | TASK-4.1.1 – 4.2.7 (10 tasks) |
| **Milestone 5** | **Month-End Rollover & Automated Transitions** | M5.1 | TASK-5.1.1 – 5.1.4 (4 tasks) |
| **Milestone 6** | **Receipt Ingestion, Parsing, & Multi-Package Matching** | M6.1 – M6.3 | TASK-6.1.1 – 6.3.3 (8 tasks) |
| **Milestone 7** | **Push Notifications, Backup Pipeline, & Hardening** | M7.1 – M7.3 | TASK-7.1.1 – 7.3.2 (7 tasks) |

For complete code contracts, SQL DDL schemas, mock fixtures, and mathematical formulas for every task, refer directly to [`docs/detailed_task_specifications.md`](detailed_task_specifications.md).

---

## 2. The 5-Part Task Contract

Every task issue and ticket strictly adheres to the following specification standard:

1. **Target Files:** Explicit paths to create, modify, or test. Developers must respect repository boundaries and not place code in arbitrary locations.
2. **Dependencies:** Prerequisite tasks that must be merged to `main` with passing tests before starting.
3. **Data Contracts & Interfaces:** Concrete Go structs, TypeScript interfaces, SQL DDL, or JSON payloads.
4. **Business Logic & Edge Cases:** Algorithmic rules, precision constraints (e.g. `NUMERIC(12, 2)` for currency; floating-point math is strictly forbidden), and error boundaries.
5. **Acceptance Criteria & Verification:** Deterministic CLI commands, unit tests, or integration tests with pass/fail criteria.

---

## 3. Engineering Workflow: From Issue to PR

### Step 1: Selecting a Task
1. Browse open issues on the [GitHub Issues](https://github.com/ckchessmaster/budget-app/issues) page or the Project Board.
2. Select an issue from the active Milestone whose prerequisites/dependencies are marked complete.
3. Assign the issue to yourself or comment on the issue.

### Step 2: Branch Naming Convention
Create a feature branch from the latest `main`:
```bash
git checkout main
git pull origin main
git checkout -b task/<task-id>-<short-description>
```
*Example:* `git checkout -b task/1.1.1-scaffold-aspire-workspace`

### Step 3: Implementation & Local Testing
1. Implement the required changes strictly touching the **Target Files**.
2. Run local tests and verification commands defined in the task's **Acceptance Criteria**.
3. Verify formatting and linting:
   - Go: `gofmt -s -w .` and `golangci-lint run`
   - Frontend / TypeScript: `npm run lint` and `npm run check`

### Step 4: Commit Message Convention
Use conventional commits that reference the task ID:
```
<type>(<scope>): <short description> [<TASK-ID>]
```
*Examples:*
- `feat(apphost): scaffold aspire typescript workspace [TASK-1.1.1]`
- `feat(db): implement go pgx migration runner [TASK-1.2.1]`
- `test(budget): add sinking fund pacing calculation unit tests [TASK-2.1.3]`

### Step 5: Pull Request & Review
1. Push branch to GitHub:
   ```bash
   git push -u origin task/<task-id>-<short-description>
   ```
2. Open a Pull Request targeting `main`.
3. In the PR description, use GitHub's closing keyword:
   ```markdown
   Closes #<issue_number>
   ```
4. Include the exact verification commands and test execution output.

---

## 4. Local Development Environment

### Prerequisites
- **Node.js**: v20+ LTS
- **Go**: v1.22+
- **Docker / Podman**: Required for Aspire container orchestration (Postgres, Dex, Temporal)
- **Microsoft Aspire CLI**: Installed globally or via `npx @microsoft/aspire-cli`

### Running the Full Local Stack
```bash
# Start Aspire AppHost (starts Postgres, Dex OIDC, Temporal, Go Backend, and SvelteKit Frontend)
npm run dev:aspire

# Or start services individually:
# Backend:
cd backend && go run cmd/api/main.go

# Frontend:
cd frontend && npm run dev
```

The Aspire dashboard is accessible at `http://localhost:18888`.
