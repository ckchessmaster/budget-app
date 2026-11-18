package simplefin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSimpleFINClient(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/claim/abc":
			accessURL := "http://user123:secret456@" + server.Listener.Addr().String() + "/simplefin"
			_, _ = w.Write([]byte(accessURL))
		case r.Method == http.MethodGet && r.URL.Path == "/simplefin/accounts":
			user, pass, ok := r.BasicAuth()
			if !ok || user != "user123" || pass != "secret456" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(DataResponse{
				Accounts: []AccountDTO{
					{
						ID:       "ext-acct-1",
						Name:     "Main Checking",
						Currency: "USD",
						Balance:  "1250.00",
						Transactions: []TransactionDTO{
							{
								ID:          "ext-tx-1",
								Posted:      1700000000,
								Amount:      "-42.50",
								Description: "Coffee Shop",
								Pending:     false,
							},
						},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/ratelimited/accounts":
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	ctx := context.Background()

	t.Run("claims setup token and fetches accounts and transactions", func(t *testing.T) {
		token := base64.StdEncoding.EncodeToString([]byte(server.URL + "/claim/abc"))
		accessURL, err := client.ClaimSetupToken(ctx, token)
		if err != nil {
			t.Fatalf("ClaimSetupToken failed: %v", err)
		}

		data, err := client.FetchData(ctx, accessURL, time.Now().AddDate(0, 0, -30))
		if err != nil {
			t.Fatalf("FetchData failed: %v", err)
		}
		if len(data.Accounts) != 1 || data.Accounts[0].Name != "Main Checking" {
			t.Fatalf("unexpected accounts: %+v", data.Accounts)
		}
		if len(data.Transactions) != 1 || data.Transactions[0].AccountID != "ext-acct-1" {
			t.Fatalf("unexpected transactions: %+v", data.Transactions)
		}
	})

	t.Run("returns RateLimitError on HTTP 429", func(t *testing.T) {
		accessURL := "http://u:p@" + server.Listener.Addr().String() + "/ratelimited"
		_, err := client.FetchData(ctx, accessURL, time.Time{})
		var rlErr *RateLimitError
		if !errors.As(err, &rlErr) {
			t.Fatalf("expected *RateLimitError, got %v", err)
		}
		if rlErr.RetryAfter != 120*time.Second {
			t.Fatalf("RetryAfter = %s, want 120s", rlErr.RetryAfter)
		}
	})
}
