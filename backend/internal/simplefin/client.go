package simplefin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidSetupToken = errors.New("invalid simplefin setup token")
	ErrInvalidAccessURL  = errors.New("invalid simplefin access URL")
)

type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("simplefin rate limited; retry after %s", e.RetryAfter)
}

// TerminalStatusError is a claim response the bank will keep answering the same way.
//
// A SimpleFIN claim token is single-use, so a 4xx on the claim endpoint means the token is
// malformed, expired or already spent. Re-sending it cannot succeed, and every attempt is
// another request to a provider that has already said no, so callers must treat this as
// terminal rather than retryable.
type TerminalStatusError struct {
	StatusCode int
	Operation  string
	Message    string
}

func (e *TerminalStatusError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("simplefin %s rejected the request (%d): %s", e.Operation, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("simplefin %s rejected the request (%d)", e.Operation, e.StatusCode)
}

type AuthError struct {
	StatusCode int
	Message    string
}

func (e *AuthError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("simplefin auth error (%d): %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("simplefin auth error (%d)", e.StatusCode)
}

var ErrRateLimited = &RateLimitError{RetryAfter: 60 * time.Second}

type Client interface {
	ClaimSetupToken(ctx context.Context, setupToken string) (accessURL string, err error)
	FetchData(ctx context.Context, accessURL string, startDate time.Time) (*DataResponse, error)
}

type httpClient struct {
	http *http.Client
}

func NewClient(hc *http.Client) Client {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &httpClient{http: hc}
}

func (c *httpClient) ClaimSetupToken(ctx context.Context, setupToken string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(setupToken))
	if err != nil {
		return "", ErrInvalidSetupToken
	}
	claimURL := strings.TrimSpace(string(decoded))
	parsed, err := url.Parse(claimURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", ErrInvalidSetupToken
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claimURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build claim request: %w", err)
	}
	req.Header.Set("Content-Length", "0")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("simplefin claim request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", parseRateLimitError(resp)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", &AuthError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(body))}
	}
	// Any other 4xx on a claim is terminal for the reasons on TerminalStatusError. 5xx is
	// deliberately left retryable: the token is still good and the bank is having a moment.
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", &TerminalStatusError{
			StatusCode: resp.StatusCode,
			Operation:  "claim",
			Message:    strings.TrimSpace(string(body)),
		}
	}
	if resp.StatusCode < 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("simplefin claim returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", fmt.Errorf("failed to read claim response: %w", err)
	}
	accessURL := strings.TrimSpace(string(body))
	if accessURL == "" {
		return "", ErrInvalidAccessURL
	}
	return accessURL, nil
}

func (c *httpClient) FetchData(ctx context.Context, accessURL string, startDate time.Time) (*DataResponse, error) {
	parsed, err := url.Parse(strings.TrimSpace(accessURL))
	if err != nil || parsed.User == nil {
		return nil, ErrInvalidAccessURL
	}

	username := parsed.User.Username()
	password, _ := parsed.User.Password()

	// Strip userinfo from URL and set Basic Auth header explicitly.
	cleanURL := *parsed
	cleanURL.User = nil
	cleanURL.Path = strings.TrimRight(cleanURL.Path, "/") + "/accounts"

	q := cleanURL.Query()
	if !startDate.IsZero() {
		q.Set("start-date", strconv.FormatInt(startDate.Unix(), 10))
	}
	cleanURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cleanURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build fetch request: %w", err)
	}
	req.SetBasicAuth(username, password)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("simplefin fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, parseRateLimitError(resp)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, &AuthError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(body))}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("simplefin fetch returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var data DataResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode simplefin response: %w", err)
	}

	// Flatten nested account transactions into DataResponse.Transactions while keeping AccountID populated.
	for i := range data.Accounts {
		acctID := data.Accounts[i].ID
		for j := range data.Accounts[i].Transactions {
			tx := data.Accounts[i].Transactions[j]
			if tx.AccountID == "" {
				tx.AccountID = acctID
			}
			data.Transactions = append(data.Transactions, tx)
		}
	}

	return &data, nil
}

func parseRateLimitError(resp *http.Response) error {
	retryAfter := 60 * time.Second
	if raw := resp.Header.Get("Retry-After"); raw != "" {
		if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
			retryAfter = time.Duration(secs) * time.Second
		}
	}
	return &RateLimitError{RetryAfter: retryAfter}
}
