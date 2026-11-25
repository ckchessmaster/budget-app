# AGENTS.md

Working notes for automated agents and new contributors. Repository conventions — branch
naming, commit format, the 5-part task contract, PR rules — live in
[`docs/DEVELOPMENT_WORKFLOW.md`](docs/DEVELOPMENT_WORKFLOW.md) and are not repeated here.
What follows is the material that is expensive to rediscover: the commands that actually work
and the invariants that fail silently when broken.

## Commands

Go (always from `backend/`, always `GOWORK=off`, matching CI):

```bash
cd backend
GOWORK=off go build ./... && GOWORK=off go vet ./...
GOWORK=off go run ./cmd/migrate up
GOWORK=off go test -race -count=1 ./...
```

The Go tests are not hermetic: they exercise real row level security against Postgres and need
two URLs — `DATABASE_URL` as a superuser (bypasses RLS) and `TEST_DATABASE_URL` as
`budget_app_user` (subject to it). A suite that passes only with the superuser URL has proven
nothing about scoping. `.github/workflows/ci.yml` shows the role provisioning SQL.

Frontend:

```bash
cd frontend
npm run test     # vitest
npm run check    # svelte-check (typechecking; catches what the build will not)
npm run build
```

There is **no** `npm run lint` script, despite the mention in the onboarding doc.

Helm:

```bash
helm lint --strict charts/budget-app
helm template charts/budget-app --set temporal.address=x:7233 --set vault.existingSecret=s
helm template charts/budget-app        # fails on purpose: no temporal.address
```

Backup:

```bash
bash -n scripts/backup/backup.sh scripts/backup/backup_test.sh
DATABASE_URL=postgres://postgres:postgres@127.0.0.1:5432/budgetdb?sslmode=disable bash scripts/backup/backup_test.sh
```

Local stack: `npm run dev:aspire` from the repository root brings up Postgres (5432), Dex OIDC
(5556), Temporal gRPC (7233), Temporal UI (8233), the API, the worker, and Vite (5173), with
the Aspire dashboard on 18888. The AppHost supplies dev values for `TEMPORAL_ADDRESS`,
`TEMPORAL_NAMESPACE`, and `VAULT_MASTER_KEY`; those are development-only and must never reach a
cluster.

## Temporal

### What is orchestrated today

Four workflows are registered on task queue `budget-sync-queue` in namespace `budget-app`:

- `BankSyncWorkflow` in
  [`backend/internal/temporal/workflows/bank_sync.go`](backend/internal/temporal/workflows/bank_sync.go):
  five sequential activities — fetch SimpleFIN, persist (which also does manual dedup and
  pending→posted reconciliation), detect and link transfers, normalize merchants, and evaluate
  notifications (`EvaluateNotificationsActivity`, non-fatal warning on failure, 24h RLS-scoped
  cooldowns).
- `MonthEndRolloverWorkflow` in
  [`backend/internal/temporal/workflows/month_end.go`](backend/internal/temporal/workflows/month_end.go):
  per-workspace month-end rollover (`month-end-rollover-<workspaceID>`, `0 0 1 * *` UTC) running
  inside `SyncScope` for `FORCE RLS`.
- `SimpleFINClaimWorkflow` in
  [`backend/internal/temporal/workflows/simplefin_claim.go`](backend/internal/temporal/workflows/simplefin_claim.go):
  exchanges a staged setup token for an access URL, then lists the accounts behind it.
  `POST /api/v1/bank/simplefin/claim` returns 202 with a workflow id and the browser polls
  `GET /api/v1/bank/workflow-status/{workflow_id}`, which serves every workflow and passes the
  result through as raw JSON.
- `DemoResetWorkflow` in
  [`backend/internal/temporal/workflows/demo_reset.go`](backend/internal/temporal/workflows/demo_reset.go):
  tears the demo sandbox down and rebuilds it. `POST /api/v1/demo/reset` answers 202 and the
  browser polls. Its ID is `demo-reset-<workspaceID>` with no timestamp, so Temporal itself
  refuses a second concurrent reset and the endpoint returns 409.

Claim and fetch are separate activities because a claim token is single-use at the bank: one
activity doing both would re-claim on every fetch retry.

Two rules the reset workflow encodes that are easy to break by reordering:

- `demo_mode_enabled` must be true before anything is deleted or seeded. The workspace
  policies hide a demo workspace until it is set, so a delete issued first cannot see the
  rows it removes, and seeding first fails on `workspace_settings`.
- The sandbox workspace ID is derived with `uuid.NewSHA1(namespace, workflowID)`, not
  generated. Workflow code must stay deterministic, and a retry has to rebuild the sandbox
  under the same key rather than stack a second one. Anything that generates an ID inside a
  retried activity has the same problem.

Triggers for bank sync are `POST /api/v1/bank/sync-now`, the demo simulator's `syncNow` flag, and
a per-workspace Temporal Schedule (`bank-sync-<workspaceID>`, every 6h with 15m jitter) created
lazily on first sync. Both schedules are created at runtime rather than by a migration, so a
workspace that predates them only starts running on its next provision or sync. Progress comes
from `DescribeWorkflowExecution` behind `GET /api/v1/bank/sync-status/{workflow_id}`. There are
no signals, queries, child workflows, or continue-as-new yet.
### Required configuration, with no fallbacks

| Variable | Rule |
| --- | --- |
| `TEMPORAL_ADDRESS` | Required. The API exits at boot without it; it does not dial `localhost`. |
| `TEMPORAL_NAMESPACE` | Required, and must never be `default`. The chart refuses `default` at render time. |
| `TEMPORAL_RETENTION` | Optional, defaults to `72h`. |
| `VAULT_MASTER_KEY` | Required: base64 of exactly 32 random bytes. There is no derived key. |

Both binaries call `temporalinternal.Prepare` at startup and treat failure as fatal. Do not
add a fallback, a default, or a "warn and continue" path to make a local run easier — every
one of those has already caused an incident here. A missing key that degrades to a derived key
seals bank credentials with something readable in the repository; a missing address that
degrades to `localhost` accepts sync requests into a queue nobody polls.

Opt-in W3C Web Push (`VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY`, `VAPID_SUBJECT`) is all-or-nothing:
leaving all three unset disables push cleanly, while a partial configuration or an invalid subject
(must start with `mailto:` or `https://`) logs an error and disables push. When `vapid.enabled=true`
in Helm, both `backend` and `sync-worker` receive identical credentials via `budget-app.syncEnv`,
and `VAPID_PRIVATE_KEY` is never placed in ConfigMaps or workflow payloads.

### Rules for adding or changing a workflow

1. **Activities must carry the scope.** Pass `temporalinternal.SyncScope{WorkspaceID, UserID}`
   as input and open `db.BeginWorkspaceTx` / `db.WithWorkspaceTx`. Banking tables are
   `FORCE ROW LEVEL SECURITY` keyed on the session GUC `app.current_user_id`, so a bare pooled
   connection matches zero rows, the activity reports success, and nothing is synced. This is
   the single most expensive bug in the codebase's history.
2. **Workflow code stays deterministic.** No `time.Now()`, no `rand`, no HTTP, no DB in
   workflow functions — use `workflow.Now(ctx)`, side effects, and activities. Replay
   re-executes the function from scratch.
3. **Set `ActivityOptions` explicitly.** See `bank_sync.go`: a start-to-close timeout plus a
   `RetryPolicy` with bounded attempts. Classify errors — validation failures are terminal via
   `temporal.NewNonRetryableApplicationError`, transport and rate-limit failures retry. When an
   upstream sends `Retry-After`, honour it instead of using generic backoff.
4. **Assume at-least-once.** Every activity needs an idempotent effect: an upsert, a unique
   constraint, or a natural key. Retries are not exceptional.
5. **Register the workflow and activities** in
   [`backend/cmd/worker/main.go`](backend/cmd/worker/main.go). The worker binary is built into
   the backend image at `/usr/local/bin/worker`.
6. **Remember where the worker runs.** It is its own Deployment (`<release>-sync-worker`,
   scaled by `temporal.worker.replicaCount`), not a sidecar in the API pod. It reaches the demo
   SimpleFIN bridge over the cluster-internal `<release>-backend-bridge` Service, and the chart
   refuses to render a loopback `MOCK_SIMPLEFIN_BASE_URL` while the worker is separate, because
   that fails inside the workflow rather than at install time. The bridge is deliberately kept
   off the ingress; narrowing it further is #129.
7. **Workflow IDs embed the workspace ID.** `handleBankSyncStatus` authorizes a status read by
   checking that the ID contains the caller's workspace. Preserve that invariant for any new
   workflow, or fix the authorization deliberately rather than by accident.
8. **Do not orchestrate single-transaction CRUD.** Categories, accounts, budget rows, and
   settings are one ACID transaction with no outbound I/O and a result the UI renders
   immediately. A workflow there buys a durable record of something that either committed or
   did not, and costs a poll round-trip, history volume, and idempotency keys. Route work in
   when it makes an outbound call, spans more than one transaction boundary, must survive a
   restart, needs backoff, fans out per item, or has history that is itself the audit trail.
9. **Retention is 72h in this namespace.** Anything meant to be auditable for longer needs the
   retention raised on purpose, not a workflow that assumes history persists.
10. **Never put credentials in workflow inputs, results, or error strings.** SimpleFIN access
    URLs are bearer credentials and Temporal history is readable by operators and retained for
    the retention window.

    This includes the setup token, which is why it is not a workflow argument anywhere. The API
    seals it into `simplefin_connections.pending_claim_token`, starts the workflow with a scope
    alone, and the worker reads it back from there and clears it in the same statement that
    writes the access URL. That also makes the claim idempotent — a late retry finds nothing
    pending instead of spending a token twice — and leaves a working connection intact when a
    new claim fails. If you need to pass a secret to a workflow, stage it and pass the scope;
    do not add a token field to an input struct. `TestSimpleFINClaimWorkflowInputCarriesNoCredential`
    fails if one appears.

    The same seam applies to packages that need to start a workflow but are already imported
    by something in the Temporal chain. `internal/demo` and `internal/simplefin` each declare
    a one-method interface (`ResetWorkflow`, `ClaimWorkflow`) implemented by a starter in
    `internal/temporal/schedules` and wired in by the server. Importing the workflow packages
    from those API packages closes an import cycle.

### Verifying a Temporal change

```bash
cd backend && GOWORK=off go test -race -count=1 ./...
helm lint --strict charts/budget-app
```

The chart render guards are part of the contract: a missing `temporal.address`, a missing
namespace, `temporal.namespace=default`, and an absent vault key must all fail the render, and
`.github/workflows/ci.yml` asserts that they do. Milestone 7 guards also require a valid
`age1...` `backup.agePublicKey` (refusing `AGE-SECRET-KEY-*`) and `backup.rclone.existingSecret`
when `backup.enabled=true`, and public key, private key (or `vapid.existingSecret`), and a
`mailto:` or `https://` subject when `vapid.enabled=true`. Add assertions in `.github/workflows/ci.yml`
when you add a guard.

Against a live cluster, from the node (`ssh` then `kubectl`; `rg` is not installed there):

```bash
kubectl -n temporal exec <admintools-pod> -- temporal operator namespace describe \
  --namespace budget-app --address temporal-frontend.temporal.svc.cluster.local:7233
kubectl -n temporal exec <admintools-pod> -- temporal task-queue describe \
  --namespace budget-app --task-queue budget-sync-queue   # expect workflow + activity pollers
kubectl -n budget-app exec <backend-pod> -c backend -- wget -qO- http://127.0.0.1:8080/healthz
```

`/healthz` reports `temporal` alongside `postgres` and returns 503 when Temporal is unhealthy.
That is intentional: Temporal is a hard dependency, so an outage takes the API out of rotation
rather than accepting work nobody will ever run.
