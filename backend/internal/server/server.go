package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
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

	"github.com/ckchessmaster/budget-app/backend/internal/account"
	"github.com/ckchessmaster/budget-app/backend/internal/budget"
	"github.com/ckchessmaster/budget-app/backend/internal/demo"
	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/schedules"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/workflows"
	"github.com/ckchessmaster/budget-app/backend/internal/transaction"
	"github.com/ckchessmaster/budget-app/backend/internal/workspace"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
)

type Config struct {
	Port                 string
	DatabaseURL          string
	DBMaxConns           int
	DexIssuerURL         string
	MockSimpleFINBaseURL string
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

	dexURL := os.Getenv("DEX_ISSUER_URL")
	if dexURL == "" {
		dexURL = os.Getenv("OIDC_ISSUER_URL")
	}
	if dexURL == "" {
		dexURL = "http://localhost:5556/dex"
	}

	mockBaseURL := os.Getenv("MOCK_SIMPLEFIN_BASE_URL")
	if mockBaseURL == "" {
		mockBaseURL = fmt.Sprintf("http://127.0.0.1:%s", port)
	}

	return Config{
		Port:                 port,
		DatabaseURL:          databaseURL,
		DBMaxConns:           maxConns,
		DexIssuerURL:         dexURL,
		MockSimpleFINBaseURL: mockBaseURL,
	}
}

type Server struct {
	cfg         Config
	pool        *pgxpool.Pool
	router      chi.Router
	provisioner workspace.Provisioner
	verifier    middleware.OIDCVerifier
}

func NewServer(cfg Config, pool *pgxpool.Pool, verifier middleware.OIDCVerifier) *Server {
	s := &Server{
		cfg:         cfg,
		pool:        pool,
		router:      chi.NewRouter(),
		provisioner: workspace.NewProvisioner(),
		verifier:    verifier,
	}

	s.setupRoutes()
	return s
}

func (s *Server) Router() chi.Router {
	return s.router
}

func (s *Server) setupRoutes() {
	r := s.router

	// Standard Chi middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(LogServerErrors(nil))
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	// Basic CORS for development
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	// Health check endpoints
	r.Get("/livez", LivenessHandler())
	r.Get("/healthz", HealthHandler(s.pool))

	// Built-in fake SimpleFIN Bridge HTTP server (uses Basic Auth per demo workspace)
	demo.RegisterMockBridgeRoutes(r, s.pool)

	// Protected routes
	if s.verifier != nil {
		r.Group(func(protected chi.Router) {
			protected.Use(middleware.AuthMiddleware(s.verifier))

			// Workspace auto-provisioning / check endpoint
			protected.Post("/api/v1/workspaces/provision", s.handleWorkspaceProvision)
			protected.Get("/api/v1/workspaces/me", s.handleWorkspaceProvision)

			// Milestone 2: Zero-based budget engine & ledger routes
			budget.RegisterRoutes(protected, s.pool)

			// Milestone 3: Accounts, solvency, debt paydown, and transactions
			account.RegisterRoutes(protected, s.pool)
			transaction.RegisterRoutes(protected, s.pool)

			// Milestone 4: SimpleFIN banking integration & Temporal sync trigger
			simplefin.RegisterRoutes(protected, s.pool)
			protected.Post("/api/v1/bank/sync-now", s.handleBankSyncNow)
			protected.Get("/api/v1/bank/sync-status/{workflow_id}", s.handleBankSyncStatus)

			// Settings & Demo Mode simulator routes
			demo.RegisterRoutes(protected, s.pool, s.cfg.MockSimpleFINBaseURL, s.provisioner)
		})
	}
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

	tc, err := temporalinternal.NewClient(slog.Default())
	if err != nil {
		http.Error(w, `{"error": "temporal_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	defer tc.Close()

	_ = schedules.EnsureWorkspaceSyncSchedule(r.Context(), tc, wsID)
	workflowID, err := schedules.TriggerSyncNow(r.Context(), tc, wsID)
	if err != nil {
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

func (s *Server) handleBankSyncStatus(w http.ResponseWriter, r *http.Request) {
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
	if !strings.Contains(workflowID, wsID.String()) {
		http.Error(w, `{"error": "workflow_not_found"}`, http.StatusNotFound)
		return
	}

	tc, err := temporalinternal.NewClient(slog.Default())
	if err != nil {
		http.Error(w, `{"error": "temporal_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	defer tc.Close()

	desc, err := tc.DescribeWorkflowExecution(r.Context(), workflowID, "")
	if err != nil {
		var notFound *serviceerror.NotFound
		if errors.As(err, &notFound) {
			http.Error(w, `{"error": "workflow_not_found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error": "failed_to_describe_workflow"}`, http.StatusInternalServerError)
		return
	}

	type syncStatusResp struct {
		WorkflowID string                    `json:"workflow_id"`
		Status     string                    `json:"status"`
		Attempt    int32                     `json:"attempt,omitempty"`
		LastError  string                    `json:"last_error,omitempty"`
		Result     *workflows.BankSyncResult `json:"result,omitempty"`
	}

	resp := syncStatusResp{
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
		var result workflows.BankSyncResult
		if err := tc.GetWorkflow(r.Context(), workflowID, "").Get(getCtx, &result); err == nil {
			resp.Result = &result
		}
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT,
		enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED,
		enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		resp.Status = "FAILED"
		getCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := tc.GetWorkflow(r.Context(), workflowID, "").Get(getCtx, nil); err != nil {
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
		Addr:    addr,
		Handler: s.router,
	}

	serverCtx, serverStopCtx := context.WithCancel(context.Background())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sig
		log.Println("Received termination signal, shutting down gracefully...")

		shutdownCtx, shutdownCancel := context.WithTimeout(serverCtx, 10*time.Second)
		defer shutdownCancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server graceful shutdown failed: %v", err)
		}
		serverStopCtx()
	}()

	log.Printf("Starting Budget App API server on %s", addr)
	err := httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server error: %w", err)
	}

	<-serverCtx.Done()
	log.Println("Server exited cleanly.")
	return nil
}
