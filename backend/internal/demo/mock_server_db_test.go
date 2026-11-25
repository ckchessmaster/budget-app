package demo

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	"github.com/ckchessmaster/budget-app/backend/internal/workspace"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// enableDemo provisions the user's real workspace, switches demo mode on through the
// handler, and returns the demo workspace id with the bridge credentials the seed
// stored for it. The credentials have to be read with the privileged role, because a
// session carrying no app.current_user_id cannot see the state row at all.
func enableDemo(t *testing.T, adminPool, appPool *pgxpool.Pool, userID uuid.UUID) (uuid.UUID, string, string) {
	t.Helper()

	ctx := context.Background()
	provisioner := workspace.NewProvisioner()
	if _, err := provisioner.EnsureWorkspaceProvisioned(ctx, appPool, userID); err != nil {
		t.Fatalf("failed to provision real workspace: %v", err)
	}

	h := NewHandler(appPool, testVault(t), DefaultMockBridgeBaseURL, provisioner, nil, nil)
	status, settings := postToggle(t, h, userID, true)
	if status != 200 {
		t.Fatalf("demo toggle status = %d, want 200", status)
	}

	var username, password string
	err := adminPool.QueryRow(ctx, `
		SELECT auth_username, auth_password
		FROM demo_simplefin_state
		WHERE workspace_id = $1
	`, settings.WorkspaceID).Scan(&username, &password)
	if err != nil {
		t.Fatalf("failed to load seeded bridge credentials: %v", err)
	}
	if username == "" || password == "" {
		t.Fatalf("seeded bridge credentials are empty: username=%q", username)
	}
	return settings.WorkspaceID, username, password
}

// newTestBridgeServer serves the production bridge router, so tests exercise the same
// routing and Basic-auth handling the API process serves on its loopback listener.
func newTestBridgeServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(MockBridgeRouter(pool))
}

// TestBridgeServesDataWithCredentialsButNoUserContext is the regression test for demo
// bank sync failing. LoadBridgeData runs in its own transaction with a bare Begin, so
// app.current_user_id is unset, every tenant table resolves to zero rows under FORCE ROW
// LEVEL SECURITY, and the credential lookup reported the bridge as missing. The symptom
// in production was a 404 from the demo SimpleFIN bridge even for a correct access URL.
//
// The bridge authenticates with the per-workspace credentials baked into the access URL
// and never has an OIDC subject, so the fix is a second, credential-matched arm on the
// demo_simplefin_* policies rather than a user identity the caller cannot supply.
func TestBridgeServesDataWithCredentialsButNoUserContext(t *testing.T) {
	adminPool, appPool, userID := openDemoTestDB(t)
	ctx := context.Background()

	workspaceID, username, password := enableDemo(t, adminPool, appPool, userID)
	store := NewMockBridgeStore(appPool)

	res, err := store.LoadBridgeData(ctx, workspaceID, username, password, nil)
	if err != nil {
		t.Fatalf("LoadBridgeData with valid credentials and no user context: %v", err)
	}
	if res == nil || res.Data == nil {
		t.Fatalf("LoadBridgeData returned no payload")
	}
	if len(res.Data.Accounts) == 0 {
		t.Fatalf("bridge served no accounts, want the seeded demo accounts")
	}

	var transactions int
	for _, account := range res.Data.Accounts {
		transactions += len(account.Transactions)
	}
	if transactions == 0 {
		t.Fatalf("bridge served accounts %d but no transactions, want seeded history", len(res.Data.Accounts))
	}

	// The credentials stay in force: get either half wrong and the bridge has to behave
	// exactly like an unknown workspace, so it never confirms which workspaces exist.
	wrongPassword := map[string]struct {
		user string
		pass string
	}{
		"wrong password":    {username, password + "x"},
		"wrong username":    {username + "x", password},
		"empty password":    {username, ""},
		"unknown workspace": {username, password},
	}
	for name, creds := range wrongPassword {
		t.Run(name, func(t *testing.T) {
			ws := workspaceID
			if name == "unknown workspace" {
				ws = uuid.New()
			}
			if _, err := store.LoadBridgeData(ctx, ws, creds.user, creds.pass, nil); !errors.Is(err, ErrBridgeNotFound) {
				t.Fatalf("err = %v, want ErrBridgeNotFound", err)
			}
		})
	}
}

// TestBridgeCredentialsDoNotCrossWorkspaces gives a second user their own demo workspace
// and checks that the credential-matched policy arm is bound to the workspace the
// credentials belong to. Without that binding, one tenant's credentials would widen
// another tenant's demo tables, because the arm cannot consult the membership tables
// that RLS itself hides.
func TestBridgeCredentialsDoNotCrossWorkspaces(t *testing.T) {
	adminPoolA, appPoolA, userA := openDemoTestDB(t)
	adminPoolB, appPoolB, userB := openDemoTestDB(t)
	ctx := context.Background()

	workspaceA, userAName, userAPass := enableDemo(t, adminPoolA, appPoolA, userA)
	workspaceB, userBName, userBPass := enableDemo(t, adminPoolB, appPoolB, userB)

	if workspaceA == workspaceB {
		t.Fatalf("both users received the same demo workspace %s", workspaceA)
	}

	// A's credentials must serve A's data.
	resA, err := NewMockBridgeStore(appPoolA).LoadBridgeData(ctx, workspaceA, userAName, userAPass, nil)
	if err != nil || len(resA.Data.Accounts) == 0 {
		t.Fatalf("own credentials failed to load own bridge: res=%+v err=%v", resA, err)
	}

	// A's credentials must not unlock B's workspace, on either connection pool.
	for name, store := range map[string]MockBridgeStore{
		"owner pool": NewMockBridgeStore(appPoolA),
		"other pool": NewMockBridgeStore(appPoolB),
	} {
		if _, err := store.LoadBridgeData(ctx, workspaceB, userAName, userAPass, nil); !errors.Is(err, ErrBridgeNotFound) {
			t.Fatalf("%s: loading another workspace with A's credentials err = %v, want ErrBridgeNotFound", name, err)
		}
	}

	// And B's own credentials keep working for B.
	resB, err := NewMockBridgeStore(appPoolB).LoadBridgeData(ctx, workspaceB, userBName, userBPass, nil)
	if err != nil || len(resB.Data.Accounts) == 0 {
		t.Fatalf("B's own credentials failed: res=%+v err=%v", resB, err)
	}
}

// TestBridgeClearsTransientFaultUnderCredentialArm exercises the write half of the bridge.
// LoadBridgeData takes the state row FOR UPDATE and clears a non-persistent fault, so the
// credential arm has to satisfy row locking and UPDATE as well as plain reads. A policy
// that only worked for SELECT would leave demo fault simulation half-broken, which is
// exactly the kind of thing that passes a read-only test.
func TestBridgeClearsTransientFaultUnderCredentialArm(t *testing.T) {
	adminPool, appPool, userID := openDemoTestDB(t)
	ctx := context.Background()

	workspaceID, username, password := enableDemo(t, adminPool, appPool, userID)
	store := NewMockBridgeStore(appPool)

	setFault := func(mode string, persistent bool) {
		if _, err := adminPool.Exec(ctx, `
			UPDATE demo_simplefin_state
			SET fault_mode = $2, fault_persistent = $3, updated_at = NOW()
			WHERE workspace_id = $1
		`, workspaceID, mode, persistent); err != nil {
			t.Fatalf("failed to set fault %s: %v", mode, err)
		}
	}
	readFault := func() string {
		var mode string
		if err := adminPool.QueryRow(ctx, `
			SELECT fault_mode FROM demo_simplefin_state WHERE workspace_id = $1
		`, workspaceID).Scan(&mode); err != nil {
			t.Fatalf("failed to read fault mode: %v", err)
		}
		return mode
	}

	setFault(FaultRateLimit429, false)
	res, err := store.LoadBridgeData(ctx, workspaceID, username, password, nil)
	if err != nil {
		t.Fatalf("LoadBridgeData with a transient fault: %v", err)
	}
	if res.FaultMode != FaultRateLimit429 {
		t.Fatalf("reported fault = %q, want %q", res.FaultMode, FaultRateLimit429)
	}
	if got := readFault(); got != FaultNone {
		t.Fatalf("transient fault left as %q, want it cleared to %q", got, FaultNone)
	}

	// Data must flow again once the one-shot fault has been consumed.
	res, err = store.LoadBridgeData(ctx, workspaceID, username, password, nil)
	if err != nil {
		t.Fatalf("LoadBridgeData after the transient fault cleared: %v", err)
	}
	if len(res.Data.Accounts) == 0 {
		t.Fatalf("no accounts after the transient fault cleared")
	}

	// A persistent fault is reported but must survive the read.
	setFault(FaultAuthError403, true)
	if res, err = store.LoadBridgeData(ctx, workspaceID, username, password, nil); err != nil {
		t.Fatalf("LoadBridgeData with a persistent fault: %v", err)
	}
	if res.FaultMode != FaultAuthError403 {
		t.Fatalf("reported fault = %q, want %q", res.FaultMode, FaultAuthError403)
	}
	if got := readFault(); got != FaultAuthError403 {
		t.Fatalf("persistent fault became %q, want it preserved", got)
	}
	setFault(FaultNone, false)
}

// TestCredentialArmRequiresTheFullHandshake checks the policy directly, because the store
// can only ever show one outcome. A session that supplies a workspace id without
// credentials, or credentials without a workspace id, must see nothing: the arm is an
// all-or-nothing match on the triple. This also proves the policy chain terminates, since
// any recursion through these tables would surface as "stack depth limit exceeded" rather
// than as an empty result -- the failure mode migration 000006 was written for.
func TestCredentialArmRequiresTheFullHandshake(t *testing.T) {
	adminPool, appPool, userID := openDemoTestDB(t)
	ctx := context.Background()

	workspaceID, username, password := enableDemo(t, adminPool, appPool, userID)

	counts := func(t *testing.T, workspaceParam, user, pass string) (int, int, int) {
		t.Helper()
		tx, err := appPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `
			SELECT set_config('app.demo_bridge_workspace_id', $1, true),
			       set_config('app.demo_bridge_username', $2, true),
			       set_config('app.demo_bridge_password', $3, true)
		`, workspaceParam, user, pass); err != nil {
			t.Fatalf("set bridge settings: %v", err)
		}

		var stateRows, accountRows, transactionRows int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM demo_simplefin_state WHERE workspace_id = $1
		`, workspaceID).Scan(&stateRows); err != nil {
			t.Fatalf("count state: %v", err)
		}
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM demo_simplefin_accounts WHERE workspace_id = $1
		`, workspaceID).Scan(&accountRows); err != nil {
			t.Fatalf("count accounts: %v", err)
		}
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM demo_simplefin_transactions WHERE workspace_id = $1
		`, workspaceID).Scan(&transactionRows); err != nil {
			t.Fatalf("count transactions: %v", err)
		}
		return stateRows, accountRows, transactionRows
	}

	cases := map[string]struct {
		ws   string
		user string
		pass string
		want int
	}{
		"full handshake":       {workspaceID.String(), username, password, 1},
		"workspace only":       {workspaceID.String(), "", "", 0},
		"credentials only":     {"", username, password, 0},
		"username only":        {workspaceID.String(), username, "", 0},
		"password only":        {workspaceID.String(), "", password, 0},
		"wrong credentials":    {workspaceID.String(), username, "wrong", 0},
		"foreign workspace id": {uuid.New().String(), username, password, 0},
		// A malformed setting must deny quietly. Policies run on every statement
		// against these tables, so a setting that raised an error would take ordinary
		// tenant reads down with it.
		"malformed workspace id": {"not-a-uuid", username, password, 0},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			stateRows, accountRows, transactionRows := counts(t, tc.ws, tc.user, tc.pass)

			if tc.want == 0 {
				if stateRows != 0 || accountRows != 0 || transactionRows != 0 {
					t.Fatalf("partial handshake leaked rows: state=%d accounts=%d transactions=%d",
						stateRows, accountRows, transactionRows)
				}
				return
			}

			if stateRows != tc.want {
				t.Fatalf("state rows visible = %d, want %d", stateRows, tc.want)
			}
			// A handshake that reached the state row but not the data behind it would
			// serve an empty bank feed, which reads as a working connection.
			if accountRows == 0 || transactionRows == 0 {
				t.Fatalf("handshake authorised state but not data: accounts=%d transactions=%d",
					accountRows, transactionRows)
			}
		})
	}

	// With no bridge settings and no user at all, the tables are invisible: this is the
	// state the API never enters, and the reason the bridge used to 404.
	tx, err := appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var bare int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM demo_simplefin_state`).Scan(&bare); err != nil {
		t.Fatalf("bare count: %v", err)
	}
	if bare != 0 {
		t.Fatalf("state rows visible with no identity at all = %d, want 0", bare)
	}
}

// TestBridgeSettingsDoNotLeakAcrossPoolCheckouts guards the one property that makes the
// credential arm safe on a connection pool. LoadBridgeData sets app.demo_bridge_* with
// is_local, so they must vanish when its transaction ends. If they ever persisted, the
// next unrelated request to borrow that physical connection would inherit another tenant's
// credentials and read its demo data through the ordinary API path.
func TestBridgeSettingsDoNotLeakAcrossPoolCheckouts(t *testing.T) {
	adminPool, appPool, userID := openDemoTestDB(t)
	ctx := context.Background()

	victimWS, victimUser, victimPass := enableDemo(t, adminPool, appPool, userID)

	store := NewMockBridgeStore(appPool)
	if _, err := store.LoadBridgeData(ctx, victimWS, victimUser, victimPass, nil); err != nil {
		t.Fatalf("seed read through the bridge failed: %v", err)
	}

	// Same pool, no bridge settings, no user: everything must be invisible again.
	// Retry a few times so the assertion cannot pass by accidentally landing on a
	// different physical connection than the one the bridge used.
	for i := 0; i < 8; i++ {
		tx, err := appPool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		var leakedState, leakedAccounts int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM demo_simplefin_state WHERE workspace_id = $1
		`, victimWS).Scan(&leakedState); err != nil {
			tx.Rollback(ctx)
			t.Fatalf("count state after bridge use: %v", err)
		}
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM demo_simplefin_accounts WHERE workspace_id = $1
		`, victimWS).Scan(&leakedAccounts); err != nil {
			tx.Rollback(ctx)
			t.Fatalf("count accounts after bridge use: %v", err)
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatalf("rollback: %v", err)
		}
		if leakedState != 0 || leakedAccounts != 0 {
			t.Fatalf("attempt %d: bridge settings leaked into a later checkout: state=%d accounts=%d",
				i+1, leakedState, leakedAccounts)
		}
	}
}

// TestBridgeServesRealSimpleFINClient is the end-to-end version of the demo sync path: a
// real access URL of the shape demo.buildMockAccessURL produces, fetched by the same
// simplefin.Client the claim handler and the Temporal worker use, served by MockBridgeRouter
// over HTTP against the RLS-restricted pool. Every layer between "user enabled demo mode"
// and "transactions appeared" is exercised, which is what the store-level tests cannot show.
func TestBridgeServesRealSimpleFINClient(t *testing.T) {
	adminPool, appPool, userID := openDemoTestDB(t)
	ctx := context.Background()

	workspaceID, username, password := enableDemo(t, adminPool, appPool, userID)

	srv := newTestBridgeServer(t, appPool)
	defer srv.Close()

	accessURL, err := buildMockAccessURL(srv.URL, workspaceID, username, password)
	if err != nil {
		t.Fatalf("failed to build access URL: %v", err)
	}

	data, err := simplefin.NewClient(nil).FetchData(ctx, accessURL, time.Now().UTC().AddDate(0, 0, -90))
	if err != nil {
		t.Fatalf("FetchData against the demo bridge failed: %v", err)
	}
	if len(data.Accounts) == 0 {
		t.Fatalf("bridge returned no accounts through the real client")
	}

	var transactions int
	for _, account := range data.Accounts {
		if account.Name == "" || account.Currency == "" {
			t.Fatalf("account is missing display fields: %+v", account)
		}
		transactions += len(account.Transactions)
	}
	if transactions == 0 {
		t.Fatalf("bridge returned %d accounts but no transactions", len(data.Accounts))
	}

	// A rotated or wrong credential has to look like a dead connection, not a empty one.
	badURL, err := buildMockAccessURL(srv.URL, workspaceID, username, "not-the-password")
	if err != nil {
		t.Fatalf("failed to build bad access URL: %v", err)
	}
	if _, err := simplefin.NewClient(nil).FetchData(ctx, badURL, time.Now().UTC().AddDate(0, 0, -90)); err == nil {
		t.Fatalf("FetchData accepted a wrong credential")
	}
}

// postReset drives the reset endpoint the way the settings UI does. The endpoint is
// asynchronous now and answers 202 with a workflow id, so the settings the old response
// carried are read back afterwards instead.
func postReset(t *testing.T, h *Handler, userID uuid.UUID) (int, *SettingsResponse) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/demo/reset", nil)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	rec := httptest.NewRecorder()
	h.ResetDemo(rec, req)

	if rec.Code != http.StatusAccepted {
		return rec.Code, nil
	}
	return rec.Code, settingsAfterReset(t, h, userID)
}

// TestResetDemoSeedsAWorkingAccessURL is the whole demo sync chain in one test: reset
// builds the SimpleFIN access URL, encrypts it, stores it on the connection, and then that
// stored value is read back, decrypted and fetched over a real loopback listener serving
// MockBridgeRouter, which is exactly how the API process serves it in production.
//
// It exists because resetting demo mode is the supported way to pick up a changed bridge
// address, so if the seed ever records a URL the router cannot serve, demo bank sync fails
// again with a 404 and no useful error. Asserting the stored prefix catches a base-URL
// regression, and fetching it catches a path or credential-shape regression.
func TestResetDemoSeedsAWorkingAccessURL(t *testing.T) {
	adminPool, appPool, userID := openDemoTestDB(t)
	ctx := context.Background()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot open a loopback listener: %v", err)
	}
	bridge := &http.Server{Handler: MockBridgeRouter(appPool), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if serveErr := bridge.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			t.Logf("bridge listener stopped: %v", serveErr)
		}
	}()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = bridge.Shutdown(shutdownCtx)
	})
	mockBase := "http://" + listener.Addr().String()

	provisioner := workspace.NewProvisioner()
	vault := testVault(t)
	h := NewHandler(appPool, vault, mockBase, provisioner, nil, nil)
	h.reset = inlineResetStarter{h: h}
	if _, err := provisioner.EnsureWorkspaceProvisioned(ctx, appPool, userID); err != nil {
		t.Fatalf("failed to provision real workspace: %v", err)
	}
	if status, _ := postToggle(t, h, userID, true); status != http.StatusOK {
		t.Fatalf("demo toggle status = %d, want 200", status)
	}

	status, settings := postReset(t, h, userID)
	if status != http.StatusAccepted {
		t.Fatalf("demo reset status = %d, want 202", status)
	}

	var encrypted string
	if err := adminPool.QueryRow(ctx, `
		SELECT encrypted_access_url
		FROM simplefin_connections
		WHERE workspace_id = $1
	`, settings.WorkspaceID).Scan(&encrypted); err != nil {
		t.Fatalf("failed to read the seeded connection: %v", err)
	}
	if encrypted == "" {
		t.Fatalf("reset stored no access URL")
	}

	accessURL, err := vault.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("failed to decrypt the stored access URL: %v", err)
	}
	// The bridge credentials live in the URL userinfo, exactly as SimpleFIN defines an
	// access URL, so compare the parsed parts rather than a naive prefix.
	parsed, err := url.Parse(accessURL)
	if err != nil {
		t.Fatalf("stored access URL %q is not parseable: %v", accessURL, err)
	}
	if parsed.Host != listener.Addr().String() {
		t.Fatalf("stored access URL host = %q, want the configured bridge %q", parsed.Host, listener.Addr())
	}
	if !strings.HasPrefix(parsed.Path, "/mock-simplefin/"+settings.WorkspaceID.String()) {
		t.Fatalf("stored access URL path = %q, want the bridge route for workspace %s",
			parsed.Path, settings.WorkspaceID)
	}

	data, err := simplefin.NewClient(nil).FetchData(ctx, accessURL, time.Now().UTC().AddDate(0, 0, -90))
	if err != nil {
		t.Fatalf("fetching the seeded access URL failed: %v", err)
	}
	if len(data.Accounts) == 0 {
		t.Fatalf("seeded access URL served no accounts")
	}
}
