package admin

import (
	"context"
	"fmt"
	"net/http"
	"time"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

type FailedWorkflowCounts struct {
	FailedBankSyncs        int `json:"failed_bank_syncs"`
	FailedReceiptWorkflows int `json:"failed_receipt_workflows"`
	FailedInviteEmails     int `json:"failed_invite_emails"`
}

type WorkflowVisibilityClient interface {
	ListFailedWorkflowsSince(ctx context.Context, since time.Time) (FailedWorkflowCounts, error)
}

type TemporalVisibilityClient struct {
	Client    client.Client
	Namespace string
}

func NewTemporalVisibilityClient(c client.Client, namespace string) *TemporalVisibilityClient {
	if c == nil {
		return nil
	}
	return &TemporalVisibilityClient{
		Client:    c,
		Namespace: namespace,
	}
}

func (v *TemporalVisibilityClient) ListFailedWorkflowsSince(ctx context.Context, since time.Time) (FailedWorkflowCounts, error) {
	var counts FailedWorkflowCounts
	if v == nil || v.Client == nil {
		return counts, fmt.Errorf("temporal client is not configured")
	}

	query := fmt.Sprintf("ExecutionStatus = 'Failed' AND CloseTime >= '%s'", since.UTC().Format(time.RFC3339))
	var nextToken []byte
	for page := 0; page < 5; page++ {
		resp, err := v.Client.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
			Namespace:     v.Namespace,
			PageSize:      200,
			NextPageToken: nextToken,
			Query:         query,
		})
		if err != nil {
			return counts, err
		}
		for _, exec := range resp.GetExecutions() {
			switch exec.GetType().GetName() {
			case "BankSyncWorkflow":
				counts.FailedBankSyncs++
			case "ReceiptReconcileWorkflow":
				counts.FailedReceiptWorkflows++
			case "SendWorkspaceInviteWorkflow", "SendPlatformInviteWorkflow":
				counts.FailedInviteEmails++
			}
		}
		if len(resp.GetNextPageToken()) == 0 {
			break
		}
		nextToken = resp.GetNextPageToken()
	}
	return counts, nil
}

type TemporalSDKHealthChecker struct {
	Client client.Client
}

func (t TemporalSDKHealthChecker) CheckHealth(ctx context.Context) error {
	return temporalinternal.HealthCheck(ctx, t.Client)
}

type DependencyHealth struct {
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
}

type StaleOrErroredSimpleFIN struct {
	WorkspaceID   uuid.UUID  `json:"workspace_id"`
	WorkspaceName string     `json:"workspace_name"`
	Status        string     `json:"status"`
	LastSyncedAt  *time.Time `json:"last_synced_at,omitempty"`
	ErrorSummary  *string    `json:"error_summary,omitempty"`
}

type SimpleFINHealth struct {
	ConfiguredWorkspaces int                       `json:"configured_workspaces"`
	StaleOrErrored       []StaleOrErroredSimpleFIN `json:"stale_or_errored"`
}

type OutboundEmailHealth24h struct {
	Sent   int `json:"sent"`
	Failed int `json:"failed"`
}

type HealthResponse struct {
	Status               string                      `json:"status"`
	CheckedAt            time.Time                   `json:"checked_at"`
	Dependencies         map[string]DependencyHealth `json:"dependencies"`
	SimpleFIN            SimpleFINHealth             `json:"simplefin"`
	TemporalWorkflows24h FailedWorkflowCounts        `json:"temporal_workflows_24h"`
	OutboundEmail24h     OutboundEmailHealth24h      `json:"outbound_email_24h"`
}

func (h *Handler) GetHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now().UTC()
	overallStatus := "ok"

	deps := make(map[string]DependencyHealth, 2)

	// 1. PostgreSQL ping + latency
	pgStart := time.Now()
	if h.pool == nil {
		deps["postgres"] = DependencyHealth{Status: "unconfigured", LatencyMS: 0}
		overallStatus = "degraded"
	} else if err := h.pool.Ping(ctx); err != nil {
		deps["postgres"] = DependencyHealth{Status: "error", LatencyMS: time.Since(pgStart).Milliseconds()}
		overallStatus = "degraded"
	} else {
		deps["postgres"] = DependencyHealth{Status: "ok", LatencyMS: time.Since(pgStart).Milliseconds()}
	}

	// 2. Temporal ping + latency
	tStart := time.Now()
	if h.temporalHealth == nil {
		deps["temporal"] = DependencyHealth{Status: "unconfigured", LatencyMS: 0}
		overallStatus = "degraded"
	} else {
		tCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		tErr := h.temporalHealth.CheckHealth(tCtx)
		cancel()
		if tErr != nil {
			deps["temporal"] = DependencyHealth{Status: "error", LatencyMS: time.Since(tStart).Milliseconds()}
			overallStatus = "degraded"
		} else {
			deps["temporal"] = DependencyHealth{Status: "ok", LatencyMS: time.Since(tStart).Milliseconds()}
		}
	}

	// 3. SimpleFIN metadata & 24h outbound email audit counts
	sfHealth := SimpleFINHealth{
		StaleOrErrored: []StaleOrErroredSimpleFIN{},
	}
	var emailHealth OutboundEmailHealth24h
	staleCutoff := now.Add(-12 * time.Hour)

	if h.pool != nil {
		err := db.WithPlatformAdminTx(ctx, h.pool, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT workspace_id, workspace_name, simplefin_last_synced_at, simplefin_status, simplefin_error
				FROM admin_workspace_overview
				WHERE simplefin_status <> 'NOT_CONFIGURED'
				ORDER BY workspace_name ASC
			`)
			if err != nil {
				return err
			}
			defer rows.Close()

			for rows.Next() {
				var wsID uuid.UUID
				var wsName string
				var lastSynced *time.Time
				var sfStatus string
				var sfErr *string
				if err := rows.Scan(&wsID, &wsName, &lastSynced, &sfStatus, &sfErr); err != nil {
					return err
				}
				sfHealth.ConfiguredWorkspaces++
				isStale := lastSynced == nil || lastSynced.Before(staleCutoff)
				if sfStatus == "ERROR" || isStale {
					reportStatus := sfStatus
					if reportStatus == "OK" && isStale {
						reportStatus = "STALE"
					}
					sfHealth.StaleOrErrored = append(sfHealth.StaleOrErrored, StaleOrErroredSimpleFIN{
						WorkspaceID:   wsID,
						WorkspaceName: wsName,
						Status:        reportStatus,
						LastSyncedAt:  lastSynced,
						ErrorSummary:  sfErr,
					})
				}
			}
			if err := rows.Err(); err != nil {
				return err
			}

			return tx.QueryRow(ctx, `
				SELECT
					COALESCE(COUNT(*) FILTER (WHERE status = 'SENT'), 0)::INT,
					COALESCE(COUNT(*) FILTER (WHERE status = 'FAILED'), 0)::INT
				FROM outbound_email_logs
				WHERE created_at >= $1
			`, now.Add(-24*time.Hour)).Scan(&emailHealth.Sent, &emailHealth.Failed)
		})
		if err != nil {
			overallStatus = "degraded"
		}
	}

	if len(sfHealth.StaleOrErrored) > 0 || emailHealth.Failed > 0 {
		overallStatus = "degraded"
	}

	// 4. Temporal failed workflows in last 24h
	var wfCounts FailedWorkflowCounts
	if h.visibility != nil {
		vCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		counts, vErr := h.visibility.ListFailedWorkflowsSince(vCtx, now.Add(-24*time.Hour))
		cancel()
		if vErr != nil {
			overallStatus = "degraded"
		} else {
			wfCounts = counts
			if wfCounts.FailedBankSyncs > 0 || wfCounts.FailedReceiptWorkflows > 0 || wfCounts.FailedInviteEmails > 0 {
				overallStatus = "degraded"
			}
		}
	}

	writeJSON(w, http.StatusOK, HealthResponse{
		Status:               overallStatus,
		CheckedAt:            now,
		Dependencies:         deps,
		SimpleFIN:            sfHealth,
		TemporalWorkflows24h: wfCounts,
		OutboundEmail24h:     emailHealth,
	})
}
