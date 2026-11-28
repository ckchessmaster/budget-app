package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/ckchessmaster/budget-app/backend/internal/account"
	"github.com/ckchessmaster/budget-app/backend/internal/admin"
	"github.com/ckchessmaster/budget-app/backend/internal/budget"
	"github.com/ckchessmaster/budget-app/backend/internal/categorization"
	"github.com/ckchessmaster/budget-app/backend/internal/demo"
	"github.com/ckchessmaster/budget-app/backend/internal/household"
	"github.com/ckchessmaster/budget-app/backend/internal/idp"
	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/internal/notifications"
	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/schedules"
	"github.com/ckchessmaster/budget-app/backend/internal/transaction"
	"github.com/ckchessmaster/budget-app/backend/internal/workspace"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
)

type Config struct {
	Port                string
	DatabaseURL         string
	DBMaxConns          int
	OIDCIssuerURL       string
	OIDCClientID        string
	OIDCEnforceAudience bool
	CORSAllowedOrigins  []string
	// MockSimpleFINListenAddr binds the built-in fake SimpleFIN bridge. It defaults
	// to loopback, which only the API process can reach; the chart sets a routable
	// bind here when the sync worker runs as its own Deployment.
	MockSimpleFINListenAddr string
	MockSimpleFINBaseURL    string
	AppPublicURL            string
	VAPID                   notifications.VAPIDConfig
	PlatformAdminGroup      string
	PlatformAdminEmails     []string
}

func LoadConfig() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://postgres:postgres@localhost:5432/budgetdb?sslmode=disable"
	}

	maxConns := 25
	if mcStr := os.Getenv("DB_MAX_CONNS"); mcStr != "" {
		if mc, err := strconv.Atoi(mcStr); err == nil && mc > 0 {
			maxConns = mc
		}
	}

	issuerURL := os.Getenv("OIDC_ISSUER_URL")
	if issuerURL == "" {
		issuerURL = os.Getenv("DEX_ISSUER_URL")
	}
	if issuerURL == "" {
		issuerURL = "http://localhost:5556/dex"
	}

	clientID := strings.TrimSpace(os.Getenv("OIDC_CLIENT_ID"))

	// Audience enforcement can be turned off without a rebuild if an identity
	// provider ever issues tokens for this client with a different audience.
	enforceAudience := true
	if raw := strings.TrimSpace(os.Getenv("OIDC_ENFORCE_AUDIENCE")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			slog.Warn("ignoring invalid OIDC_ENFORCE_AUDIENCE; keeping audience enforcement on", "value", raw)
		} else {
			enforceAudience = parsed
		}
	}
	if enforceAudience && clientID == "" {
		slog.Error("audience enforcement is on but OIDC_CLIENT_ID is unset, so the aud claim cannot be verified; set OIDC_CLIENT_ID or disable enforcement explicitly")
	}

	bridgeListenAddr := strings.TrimSpace(os.Getenv("MOCK_SIMPLEFIN_LISTEN_ADDR"))
	if bridgeListenAddr == "" {
		bridgeListenAddr = demo.DefaultMockBridgeListenAddr
	}

	mockBaseURL := strings.TrimSpace(os.Getenv("MOCK_SIMPLEFIN_BASE_URL"))
	if mockBaseURL == "" {
		mockBaseURL = mockBridgeBaseURLFor(bridgeListenAddr)
	}

	appPublicURL := strings.TrimSpace(os.Getenv("APP_PUBLIC_URL"))
	if appPublicURL == "" {
		appPublicURL = "http://localhost:5173"
	}

	vapidCfg, err := notifications.LoadVAPIDConfigFromEnv()
	if err != nil {
		slog.Error("invalid or partial VAPID configuration; web push notifications disabled", "error", err)
	}
	adminCfg := middleware.LoadPlatformAdminConfigFromEnv()

	return Config{
		Port:                    port,
		DatabaseURL:             databaseURL,
		DBMaxConns:              maxConns,
		OIDCIssuerURL:           issuerURL,
		OIDCClientID:            clientID,
		OIDCEnforceAudience:     enforceAudience,
		CORSAllowedOrigins:      parseCORSAllowedOrigins(os.Getenv("CORS_ALLOWED_ORIGINS")),
		MockSimpleFINListenAddr: bridgeListenAddr,
		MockSimpleFINBaseURL:    mockBaseURL,
		AppPublicURL:            appPublicURL,
		VAPID:                   vapidCfg,
		PlatformAdminGroup:      adminCfg.AdminGroup,
		PlatformAdminEmails:     adminCfg.AdminEmails,
	}
}

type Server struct {
	cfg          Config
	pool         *pgxpool.Pool
	router       chi.Router
	bridgeRouter chi.Router
	provisioner  *workspace.DefaultProvisioner
	verifier     middleware.OIDCVerifier
	temporal     client.Client
	vault        crypto.Vault
	push         notifications.PushService
	evaluator    notifications.Evaluator
	household    *household.Handler
	admin        *admin.Handler
	adminCfg     middleware.PlatformAdminConfig
}

type rolloverSchedulerAdapter struct {
	client client.Client
}

func (a rolloverSchedulerAdapter) EnsureWorkspaceRolloverSchedule(ctx context.Context, scope temporalinternal.SyncScope) error {
	return schedules.EnsureWorkspaceRolloverSchedule(ctx, a.client, scope)
}

// NewServer builds the API. temporal and vault are dependencies the process cannot do
// bank sync or store credentials without; cmd/api fails to start unless both resolve.
// Unit tests may pass nil to exercise routing alone, and every path that needs them
// reports a clear failure instead of dialling a connection per request.
func NewServer(
	cfg Config,
	pool *pgxpool.Pool,
	verifier middleware.OIDCVerifier,
	temporalClient client.Client,
	vault crypto.Vault,
) *Server {
	pushSvc := notifications.NewWebPushService(pool, cfg.VAPID, nil)
	prov := workspace.NewProvisioner()
	var inviteWF household.InviteEmailWorkflow
	var platformInviteWF admin.PlatformInviteEmailWorkflow
	var rolloverSched household.RolloverScheduler
	var visClient admin.WorkflowVisibilityClient
	var temporalHealth admin.TemporalHealthChecker
	if temporalClient != nil {
		inviteWF = schedules.InviteEmailStarter{Client: temporalClient}
		platformInviteWF = schedules.PlatformInviteEmailStarter{Client: temporalClient}
		rolloverSched = rolloverSchedulerAdapter{client: temporalClient}
		visClient = admin.NewTemporalVisibilityClient(temporalClient, temporalinternal.NamespaceFromEnv())
		temporalHealth = admin.TemporalSDKHealthChecker{Client: temporalClient}
	}
	adminCfg := middleware.PlatformAdminConfig{
		AdminGroup:  cfg.PlatformAdminGroup,
		AdminEmails: cfg.PlatformAdminEmails,
	}
	s := &Server{
		cfg:         cfg,
		pool:        pool,
		router:      chi.NewRouter(),
		provisioner: prov,
		verifier:    verifier,
		temporal:    temporalClient,
		vault:       vault,
		push:        pushSvc,
		evaluator:   notifications.NewEvaluator(pool, pushSvc),
		household: household.NewHandler(
			pool, vault, prov, idp.NewProvisionerFromEnv(),
			verifier, inviteWF, rolloverSched, cfg.AppPublicURL,
		),
		admin: admin.NewHandler(
			pool, vault, idp.NewPlatformAdminFromEnv(cfg.AppPublicURL),
			platformInviteWF, visClient, temporalHealth, cfg.AppPublicURL,
		),
		adminCfg: adminCfg,
	}
	s.bridgeRouter = demo.MockBridgeRouter(pool)

	s.setupRoutes()
	return s
}

func (s *Server) Router() chi.Router {
	return s.router
}

// BridgeRouter is the fake SimpleFIN bridge. It is served on its own listener
// rather than on the public router, so it never appears in the API's routes.
func (s *Server) BridgeRouter() chi.Router {
	return s.bridgeRouter
}

func (s *Server) setupRoutes() {
	r := s.router

	// Standard Chi middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(LogServerErrors(nil))
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	// Cross-origin requests are only honoured for explicitly configured origins.
	// With no CORS_ALLOWED_ORIGINS set, no CORS grant is issued at all.
	r.Use(CORSMiddleware(NewCORSPolicy(s.cfg.CORSAllowedOrigins)))

	// Health check endpoints
	r.Get("/livez", LivenessHandler())
	r.Get("/healthz", HealthHandler(s.pool, s.temporal))

	// Public invitation preview & acceptance endpoints + platform invite signup endpoints
	household.RegisterPublicRoutes(r, s.household)
	admin.RegisterPublicRoutes(r, s.admin)

	// Protected routes
	if s.verifier != nil {
		r.Group(func(protected chi.Router) {
			protected.Use(middleware.AuthMiddleware(s.verifier))
			protected.Use(middleware.EnsureActivePlatformUser(s.pool))

			protected.Get("/api/v1/auth/me", s.handleAuthMe)

			// Platform Admin routes (group-gated + metadata-only)
			protected.Group(func(adm chi.Router) {
				adm.Use(middleware.RequirePlatformAdmin(s.pool, s.adminCfg))
				admin.RegisterAdminRoutes(adm, s.admin)
			})

			// User-scoped workspace discovery, creation, provisioning, Demo toggle & Web Push
			protected.Post("/api/v1/workspaces/provision", s.handleWorkspaceProvision)
			protected.Get("/api/v1/workspaces/me", s.handleWorkspaceProvision)
			household.RegisterUserRoutes(protected, s.household)

			var demoReset demo.ResetWorkflow
			if s.temporal != nil {
				demoReset = schedules.DemoResetStarter{Client: s.temporal}
			}
			demo.RegisterUserRoutes(protected, s.pool, s.cfg.MockSimpleFINBaseURL, s.provisioner, s.vault, s.temporal, demoReset)
			notifications.RegisterRoutes(protected, s.push)

			// Workspace-scoped routes enforced by X-Workspace-ID + role RBAC
			protected.Group(func(ws chi.Router) {
				ws.Use(middleware.RequireWorkspace(s.pool, s.provisioner))

				budget.RegisterRoutes(ws, s.pool)
				categorization.RegisterRoutes(ws, s.pool)
				account.RegisterRoutes(ws, s.pool)

				var replay transaction.ReplayWorkflow
				if s.temporal != nil {
					replay = schedules.OfflineReplayStarter{Client: s.temporal}
				}
				transaction.RegisterRoutes(ws, s.pool, replay, s.evaluator)
				ws.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).
					Get("/api/v1/transactions/workflow-status/{workflow_id}", s.handleWorkflowStatus)

				var claimWorkflow simplefin.ClaimWorkflow
				if s.temporal != nil {
					claimWorkflow = schedules.SimpleFINClaimStarter{Client: s.temporal}
				}
				simplefin.RegisterRoutes(ws, s.pool, s.vault, claimWorkflow)
				ws.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).
					Post("/api/v1/bank/sync-now", s.handleBankSyncNow)
				ws.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).
					Get("/api/v1/bank/workflow-status/{workflow_id}", s.handleWorkflowStatus)
				ws.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).
					Get("/api/v1/bank/sync-status/{workflow_id}", s.handleWorkflowStatus)

				demo.RegisterWorkspaceRoutes(ws, s.pool, s.cfg.MockSimpleFINBaseURL, s.provisioner, s.vault, s.temporal, demoReset)
				household.RegisterWorkspaceRoutes(ws, s.household)
			})
		})
	}
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"user_id":           userID,
		"email":             middleware.GetUserEmail(r.Context()),
		"display_name":      middleware.GetUserName(r.Context()),
		"is_platform_admin": middleware.IsPlatformAdmin(r.Context(), s.adminCfg),
		"is_active":         true,
	})
}

func (s *Server) handleWorkspaceProvision(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	res, err := s.provisioner.EnsureWorkspaceProvisioned(r.Context(), s.pool, userID)
	if err != nil {
		log.Printf("failed to provision workspace: %v", err)
		http.Error(w, `{"error": "failed_to_provision_workspace"}`, http.StatusInternalServerError)
		return
	}

	if s.temporal != nil {
		scope := temporalinternal.SyncScope{WorkspaceID: res.WorkspaceID, UserID: userID}
		if err := schedules.EnsureWorkspaceRolloverSchedule(r.Context(), s.temporal, scope); err != nil {
			slog.Warn("failed to ensure month-end rollover schedule",
				"workspace_id", res.WorkspaceID, "error", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *Server) handleBankSyncNow(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var wsID uuid.UUID
	err = db.WithWorkspaceTx(r.Context(), s.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		wsID, txErr = db.ActiveWorkspaceID(r.Context(), tx)
		return txErr
	})
	if err != nil {
		http.Error(w, `{"error": "workspace_not_found"}`, http.StatusNotFound)
		return
	}

	if s.temporal == nil {
		slog.Error("bank sync requested but no temporal client is configured",
			"workspace_id", wsID, "address", temporalinternal.AddressFromEnv(),
			"namespace", temporalinternal.NamespaceFromEnv())
		http.Error(w, `{"error": "temporal_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	scope := temporalinternal.SyncScope{WorkspaceID: wsID, UserID: userID}
	if err := schedules.EnsureWorkspaceSyncSchedule(r.Context(), s.temporal, scope); err != nil {
		// The recurring schedule is a convenience; an on-demand sync must still run.
		slog.Warn("failed to ensure recurring sync schedule",
			"workspace_id", wsID, "error", err)
	}

	workflowID, err := schedules.TriggerSyncNow(r.Context(), s.temporal, scope)
	if err != nil {
		slog.Error("failed to trigger bank sync workflow",
			"workspace_id", wsID, "address", temporalinternal.AddressFromEnv(),
			"namespace", temporalinternal.NamespaceFromEnv(), "error", err)
		http.Error(w, `{"error": "failed_to_trigger_sync"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"workflow_id": workflowID,
		"status":      "accepted",
	})
}

// handleWorkflowStatus reports the state of any budget-app workflow the caller's workspace
// owns: bank sync, a SimpleFIN claim, a demo reset, or an offline transaction replay.
//
// It replaces the sync-only status endpoint, which decoded the result into a concrete
// BankSyncResult and so could describe exactly one workflow. The result is passed through
// as raw JSON instead: the API has no business interpreting it, and the caller already
// knows which workflow it started. The old /bank/sync-status path stays routed to this
// handler so a cached frontend bundle keeps working.
//
// Authorization is mayReadWorkflowStatus below, checked deliberately before Temporal is
// contacted, so one workspace cannot probe another's history by guessing IDs.
func (s *Server) handleWorkflowStatus(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	workflowID := strings.TrimSpace(chi.URLParam(r, "workflow_id"))
	if workflowID == "" {
		http.Error(w, `{"error": "workflow_id_required"}`, http.StatusBadRequest)
		return
	}

	var wsID uuid.UUID
	err = db.WithWorkspaceTx(r.Context(), s.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		wsID, txErr = db.ActiveWorkspaceID(r.Context(), tx)
		return txErr
	})
	if err != nil {
		http.Error(w, `{"error": "workspace_not_found"}`, http.StatusNotFound)
		return
	}
	if !mayReadWorkflowStatus(workflowID, wsID) {
		http.Error(w, `{"error": "workflow_not_found"}`, http.StatusNotFound)
		return
	}

	if s.temporal == nil {
		http.Error(w, `{"error": "temporal_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	desc, err := s.temporal.DescribeWorkflowExecution(r.Context(), workflowID, "")
	if err != nil {
		var notFound *serviceerror.NotFound
		if errors.As(err, &notFound) {
			http.Error(w, `{"error": "workflow_not_found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error": "failed_to_describe_workflow"}`, http.StatusInternalServerError)
		return
	}

	type workflowStatusResp struct {
		WorkflowID string          `json:"workflow_id"`
		Status     string          `json:"status"`
		Attempt    int32           `json:"attempt,omitempty"`
		LastError  string          `json:"last_error,omitempty"`
		Result     json.RawMessage `json:"result,omitempty"`
	}

	resp := workflowStatusResp{
		WorkflowID: workflowID,
		Status:     "RUNNING",
	}

	wfStatus := desc.GetWorkflowExecutionInfo().GetStatus()
	switch wfStatus {
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:
		for _, pa := range desc.GetPendingActivities() {
			if pa.GetAttempt() > 1 || pa.GetLastFailure() != nil {
				resp.Status = "RETRYING"
				resp.Attempt = pa.GetAttempt()
				if pa.GetLastFailure() != nil {
					resp.LastError = pa.GetLastFailure().GetMessage()
				}
				break
			}
		}
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		resp.Status = "COMPLETED"
		getCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var raw json.RawMessage
		if err := s.temporal.GetWorkflow(r.Context(), workflowID, "").Get(getCtx, &raw); err == nil && len(raw) > 0 {
			resp.Result = raw
		}
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT,
		enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED,
		enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		resp.Status = "FAILED"
		getCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := s.temporal.GetWorkflow(r.Context(), workflowID, "").Get(getCtx, nil); err != nil {
			resp.LastError = err.Error()
		} else {
			resp.LastError = wfStatus.String()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) Run() error {
	addr := fmt.Sprintf(":%s", s.cfg.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
	}
	bridgeServer := &http.Server{
		Addr:              s.cfg.MockSimpleFINListenAddr,
		Handler:           s.bridgeRouter,
		ReadHeaderTimeout: 10 * time.Second,
	}

	shutdownAll := func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server graceful shutdown failed: %v", err)
		}
		if err := bridgeServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("simplefin bridge server graceful shutdown failed: %v", err)
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)

	// The bridge listener is demo-only infrastructure: a bind failure must not take
	// the API down with it. It reports through its own channel, the loop below logs
	// it, and the API keeps serving while demo bank sync surfaces its own errors.
	bridgeErr := make(chan error, 1)
	go func() {
		slog.Info("serving demo SimpleFIN bridge",
			"addr", s.cfg.MockSimpleFINListenAddr,
			"scope", bridgeListenScope(s.cfg.MockSimpleFINListenAddr))
		if err := bridgeServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			bridgeErr <- err
		}
	}()

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("Starting Budget App API server on %s", addr)
		serveErr <- httpServer.ListenAndServe()
	}()

	// One loop owns every exit path. Returning from here always follows a shutdown
	// that both listeners have observed, so a signal never leaves Run blocked.
	for {
		select {
		case err := <-bridgeErr:
			slog.Error("demo simplefin bridge listener stopped; demo bank sync will fail",
				"addr", s.cfg.MockSimpleFINListenAddr, "err", err)
		case err := <-serveErr:
			shutdownAll()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("server error: %w", err)
			}
			log.Println("Server exited cleanly.")
			return nil
		case received := <-sig:
			log.Printf("Received %v, shutting down gracefully...", received)
			shutdownAll()
			log.Println("Server exited cleanly.")
			return nil
		}
	}
}

// bridgeListenScope describes who can reach a bridge bind address, so a routable
// override is visible in the logs instead of being mislabelled as loopback-only.
func bridgeListenScope(listenAddr string) string {
	host, _, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "unknown"
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" || host == "0.0.0.0" || host == "::" || host == "*" {
		return "all interfaces"
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return "loopback only"
	}
	return "routable address"
}

// mockBridgeBaseURLFor turns a bind address into the URL an in-process caller dials.
// A wildcard bind is still dialled over loopback, because that is where the API
// process itself can always reach it.
func mockBridgeBaseURLFor(listenAddr string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return demo.DefaultMockBridgeBaseURL
	}

	dialHost := strings.TrimSpace(host)
	switch dialHost {
	case "", "0.0.0.0", "::", "*", "[::]":
		dialHost = "127.0.0.1"
	}

	return "http://" + net.JoinHostPort(dialHost, port)
}

// mayReadWorkflowStatus decides whether the caller of the shared status endpoint may see
// this workflow.
//
// Every workflow ID in this app embeds the workspace it runs for, so a substring test
// against the caller's active workspace is the rule. The demo reset is the workflow that
// breaks it: it deletes the sandbox named in its own ID and seeds a replacement under
// temporalinternal.DemoSandboxID, so the instant the reset lands, the caller's workspace is
// the derived one and the embedded workspace no longer exists. Authorizing only the exact
// derivation keeps the reset readable by whoever started it — an attacker would have to own
// a workspace whose ID is the SHA-1 of someone else's workflow ID — without loosening any
// other workflow. Without it the reset's own completion poll answers 404, which the demo UI
// reports as a failed reset that actually succeeded.
func mayReadWorkflowStatus(workflowID string, activeWorkspaceID uuid.UUID) bool {
	if strings.Contains(workflowID, activeWorkspaceID.String()) {
		return true
	}
	return strings.HasPrefix(workflowID, temporalinternal.DemoResetWorkflowIDPrefix) &&
		temporalinternal.DemoSandboxID(workflowID) == activeWorkspaceID
}
