package idp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ProvisionedUser struct {
	UserUUID         uuid.UUID
	Email            string
	DisplayName      string
	IsNewUser        bool
	PasswordSetupURL string
}

type IdentityProvisioner interface {
	EnsureUser(ctx context.Context, email string, displayName string) (ProvisionedUser, error)
}

type AuthentikProvisioner struct {
	BaseURL    string
	APIToken   string
	HTTPClient *http.Client
}

func NewAuthentikProvisioner(baseURL, apiToken string, httpClient *http.Client) *AuthentikProvisioner {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &AuthentikProvisioner{
		BaseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIToken:   strings.TrimSpace(apiToken),
		HTTPClient: httpClient,
	}
}

func (p *AuthentikProvisioner) EnsureUser(ctx context.Context, email string, displayName string) (ProvisionedUser, error) {
	trimmedEmail := strings.TrimSpace(email)
	if trimmedEmail == "" {
		return ProvisionedUser{}, fmt.Errorf("email is required for identity provisioning")
	}
	trimmedName := strings.TrimSpace(displayName)
	if trimmedName == "" {
		if at := strings.IndexByte(trimmedEmail, '@'); at > 0 {
			trimmedName = trimmedEmail[:at]
		} else {
			trimmedName = trimmedEmail
		}
	}

	// 1. Lookup existing user by email
	lookupURL := fmt.Sprintf("%s/api/v3/core/users/?email=%s", p.BaseURL, url.QueryEscape(trimmedEmail))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lookupURL, nil)
	if err != nil {
		return ProvisionedUser{}, fmt.Errorf("build authentik lookup request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.APIToken)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client().Do(req)
	if err != nil {
		return ProvisionedUser{}, fmt.Errorf("authentik user lookup failed: %w", err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ProvisionedUser{}, fmt.Errorf("authentik user lookup HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var listResp struct {
		Results []struct {
			PK    int    `json:"pk"`
			UUID  string `json:"uuid"`
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &listResp); err != nil {
		return ProvisionedUser{}, fmt.Errorf("decode authentik lookup response: %w", err)
	}

	for _, u := range listResp.Results {
		if strings.EqualFold(strings.TrimSpace(u.Email), trimmedEmail) {
			parsedUUID, err := uuid.Parse(strings.TrimSpace(u.UUID))
			if err != nil {
				return ProvisionedUser{}, fmt.Errorf("invalid authentik user uuid %q: %w", u.UUID, err)
			}
			resolvedName := strings.TrimSpace(u.Name)
			if resolvedName == "" {
				resolvedName = trimmedName
			}
			return ProvisionedUser{
				UserUUID:    parsedUUID,
				Email:       trimmedEmail,
				DisplayName: resolvedName,
				IsNewUser:   false,
			}, nil
		}
	}

	// 2. Net-new user: create active internal user in Authentik
	createPayload, err := json.Marshal(map[string]any{
		"username":  trimmedEmail,
		"email":     trimmedEmail,
		"name":      trimmedName,
		"is_active": true,
		"type":      "internal",
	})
	if err != nil {
		return ProvisionedUser{}, fmt.Errorf("marshal authentik create payload: %w", err)
	}

	createReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/api/v3/core/users/", bytes.NewReader(createPayload))
	if err != nil {
		return ProvisionedUser{}, fmt.Errorf("build authentik create request: %w", err)
	}
	createReq.Header.Set("Authorization", "Bearer "+p.APIToken)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Accept", "application/json")

	createResp, err := p.client().Do(createReq)
	if err != nil {
		return ProvisionedUser{}, fmt.Errorf("authentik user creation failed: %w", err)
	}
	createBody, _ := io.ReadAll(io.LimitReader(createResp.Body, 64*1024))
	createResp.Body.Close()

	if createResp.StatusCode < 200 || createResp.StatusCode >= 300 {
		return ProvisionedUser{}, fmt.Errorf("authentik user creation HTTP %d: %s", createResp.StatusCode, strings.TrimSpace(string(createBody)))
	}

	var created struct {
		PK   int    `json:"pk"`
		UUID string `json:"uuid"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(createBody, &created); err != nil {
		return ProvisionedUser{}, fmt.Errorf("decode authentik create response: %w", err)
	}
	parsedUUID, err := uuid.Parse(strings.TrimSpace(created.UUID))
	if err != nil {
		return ProvisionedUser{}, fmt.Errorf("invalid created authentik user uuid %q: %w", created.UUID, err)
	}

	// 3. Generate one-time password/passkey setup recovery link
	recURL := fmt.Sprintf("%s/api/v3/core/users/%d/recovery/", p.BaseURL, created.PK)
	recReq, err := http.NewRequestWithContext(ctx, http.MethodPost, recURL, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return ProvisionedUser{}, fmt.Errorf("build authentik recovery request: %w", err)
	}
	recReq.Header.Set("Authorization", "Bearer "+p.APIToken)
	recReq.Header.Set("Content-Type", "application/json")
	recReq.Header.Set("Accept", "application/json")

	recResp, err := p.client().Do(recReq)
	if err != nil {
		return ProvisionedUser{}, fmt.Errorf("authentik recovery link request failed: %w", err)
	}
	recBody, _ := io.ReadAll(io.LimitReader(recResp.Body, 32*1024))
	recResp.Body.Close()

	if recResp.StatusCode < 200 || recResp.StatusCode >= 300 {
		return ProvisionedUser{}, fmt.Errorf("authentik recovery link HTTP %d: %s", recResp.StatusCode, strings.TrimSpace(string(recBody)))
	}

	var recovery struct {
		Link string `json:"link"`
	}
	if err := json.Unmarshal(recBody, &recovery); err != nil {
		return ProvisionedUser{}, fmt.Errorf("decode authentik recovery response: %w", err)
	}

	resolvedName := strings.TrimSpace(created.Name)
	if resolvedName == "" {
		resolvedName = trimmedName
	}
	return ProvisionedUser{
		UserUUID:         parsedUUID,
		Email:            trimmedEmail,
		DisplayName:      resolvedName,
		IsNewUser:        true,
		PasswordSetupURL: strings.TrimSpace(recovery.Link),
	}, nil
}

func (p *AuthentikProvisioner) client() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// NoopIdentityProvisioner resolves deterministic local dev UUIDs (matching dex/config.yaml
// static accounts) when AUTHENTIK_API_TOKEN is empty.
type NoopIdentityProvisioner struct{}

var localDexUsers = map[string]uuid.UUID{
	"admin@dev.local":  uuid.MustParse("00000000-0000-0000-0000-000000000001"),
	"member@dev.local": uuid.MustParse("00000000-0000-0000-0000-000000000002"),
}

func (n *NoopIdentityProvisioner) EnsureUser(_ context.Context, email string, displayName string) (ProvisionedUser, error) {
	trimmedEmail := strings.TrimSpace(email)
	lower := strings.ToLower(trimmedEmail)
	id, ok := localDexUsers[lower]
	if !ok {
		id = uuid.NewSHA1(uuid.NameSpaceDNS, []byte("budget-app-dev:"+lower))
	}
	name := strings.TrimSpace(displayName)
	if name == "" {
		if at := strings.IndexByte(trimmedEmail, '@'); at > 0 {
			name = trimmedEmail[:at]
		} else {
			name = trimmedEmail
		}
	}
	return ProvisionedUser{
		UserUUID:    id,
		Email:       trimmedEmail,
		DisplayName: name,
		IsNewUser:   false,
	}, nil
}

func NewProvisionerFromEnv() IdentityProvisioner {
	token := strings.TrimSpace(os.Getenv("AUTHENTIK_API_TOKEN"))
	baseURL := strings.TrimSpace(os.Getenv("AUTHENTIK_BASE_URL"))
	if token == "" || baseURL == "" {
		return &NoopIdentityProvisioner{}
	}
	return NewAuthentikProvisioner(baseURL, token, nil)
}
