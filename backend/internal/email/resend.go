package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultResendBaseURL = "https://api.resend.com"
	DefaultFromAddress   = "Budget App <invites@localhost>"
)

type OutboundEmail struct {
	To             string
	Subject        string
	HTMLBody       string
	TextBody       string
	IdempotencyKey string
}

type Sender interface {
	Send(ctx context.Context, msg OutboundEmail) (messageID string, err error)
}

// APIError captures non-2xx responses from the email provider API so Temporal activities
// can distinguish retryable rate-limit/5xx faults from terminal 4xx validation errors.
type APIError struct {
	StatusCode int
	Body       string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("email provider HTTP %d: %s", e.StatusCode, e.Body)
}

func (e *APIError) IsRetryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

func (e *APIError) IsTerminal() bool {
	return e.StatusCode >= 400 && e.StatusCode < 500 && e.StatusCode != http.StatusTooManyRequests
}

type ResendSender struct {
	BaseURL     string
	APIKey      string
	FromAddress string
	HTTPClient  *http.Client
}

func NewResendSender(apiKey, fromAddress string, httpClient *http.Client) *ResendSender {
	if strings.TrimSpace(fromAddress) == "" {
		fromAddress = DefaultFromAddress
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &ResendSender{
		BaseURL:     DefaultResendBaseURL,
		APIKey:      strings.TrimSpace(apiKey),
		FromAddress: strings.TrimSpace(fromAddress),
		HTTPClient:  httpClient,
	}
}

func (s *ResendSender) Send(ctx context.Context, msg OutboundEmail) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(s.BaseURL), "/")
	if base == "" {
		base = DefaultResendBaseURL
	}

	payload := struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		HTML    string   `json:"html,omitempty"`
		Text    string   `json:"text,omitempty"`
	}{
		From:    s.FromAddress,
		To:      []string{strings.TrimSpace(msg.To)},
		Subject: msg.Subject,
		HTML:    msg.HTMLBody,
		Text:    msg.TextBody,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal resend payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/emails", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("build resend request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.APIKey)
	if strings.TrimSpace(msg.IdempotencyKey) != "" {
		req.Header.Set("Idempotency-Key", strings.TrimSpace(msg.IdempotencyKey))
	}

	client := s.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send email via resend: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &APIError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(respBody)),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}

	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("decode resend response: %w", err)
	}
	return out.ID, nil
}

func parseRetryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// NoopConsoleSender logs outbound invite emails cleanly to stdout when no Resend API key
// is configured (for local Aspire / offline development).
type NoopConsoleSender struct {
	FromAddress string
}

func (s *NoopConsoleSender) Send(_ context.Context, msg OutboundEmail) (string, error) {
	slog.Info("email delivery skipped (RESEND_API_KEY unset); logging invite to console",
		"from", s.FromAddress,
		"to", msg.To,
		"subject", msg.Subject,
		"idempotency_key", msg.IdempotencyKey,
		"text_body", msg.TextBody,
	)
	return "noop-" + msg.IdempotencyKey, nil
}

func NewSenderFromEnv() Sender {
	from := strings.TrimSpace(os.Getenv("EMAIL_FROM_ADDRESS"))
	if from == "" {
		from = DefaultFromAddress
	}
	apiKey := strings.TrimSpace(os.Getenv("RESEND_API_KEY"))
	if apiKey == "" {
		return &NoopConsoleSender{FromAddress: from}
	}
	return NewResendSender(apiKey, from, nil)
}

func RenderInviteEmail(workspaceName, inviterEmail, role, inviteURL string) (subject, htmlBody, textBody string) {
	ws := strings.TrimSpace(workspaceName)
	if ws == "" {
		ws = "Shared Budget Workspace"
	}
	inviter := strings.TrimSpace(inviterEmail)
	if inviter == "" {
		inviter = "A household member"
	}
	roleLabel := "Member"
	if role == "read_only" {
		roleLabel = "Read-Only Viewer"
	}

	subject = fmt.Sprintf("You're invited to join %s on Budget App", ws)
	textBody = fmt.Sprintf(
		"%s invited you to join the %q budget workspace as a %s.\n\nAccept your invitation (expires in 7 days):\n%s\n",
		inviter, ws, roleLabel, inviteURL,
	)
	htmlBody = fmt.Sprintf(
		`<div style="font-family:system-ui,-apple-system,sans-serif;max-width:520px;margin:0 auto;padding:24px;color:#0f172a;">`+
			`<h2 style="margin:0 0 12px;font-size:20px;">You're invited to %s</h2>`+
			`<p style="margin:0 0 16px;font-size:14px;line-height:1.5;color:#334155;">`+
			`<strong>%s</strong> invited you to collaborate in their Budget App workspace with the <strong>%s</strong> role.`+
			`</p>`+
			`<p style="margin:24px 0;">`+
			`<a href="%s" style="display:inline-block;background:#059669;color:#ffffff;text-decoration:none;font-weight:600;font-size:14px;padding:12px 20px;border-radius:8px;">Accept Invitation</a>`+
			`</p>`+
			`<p style="margin:0;font-size:12px;color:#64748b;">This invitation expires in 7 days. If the button does not work, copy and paste this link:<br/><code>%s</code></p>`+
			`</div>`,
		html.EscapeString(ws),
		html.EscapeString(inviter),
		html.EscapeString(roleLabel),
		html.EscapeString(inviteURL),
		html.EscapeString(inviteURL),
	)
	return subject, htmlBody, textBody
}
