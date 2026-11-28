package idp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrEmailAlreadyExists    = errors.New("email already exists in identity provider")
	ErrUsernameAlreadyExists = errors.New("username already exists in identity provider")
	ErrUserNotFound          = errors.New("user not found in identity provider")
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

// PlatformIdentityAdmin manages user lifecycle in the configured identity provider
// (Authentik in production, deterministic no-op in local Dex development).
type PlatformIdentityAdmin interface {
	CreateUserAndRecoveryLink(ctx context.Context, email, name, username string) (userUUID uuid.UUID, setupURL string, err error)
	SetUserActive(ctx context.Context, userUUID uuid.UUID, active bool) error
	DeleteUser(ctx context.Context, userUUID uuid.UUID) error
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

func (p *AuthentikProvisioner) doJSONRequest(ctx context.Context, method, fullURL string, payload any) (int, []byte, error) {
	var bodyReader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, fmt.Errorf("marshal authentik payload: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return 0, nil, fmt.Errorf("build authentik request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.APIToken)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("authentik request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return resp.StatusCode, respBody, nil
}

func (p *AuthentikProvisioner) lookupUserPKByUUID(ctx context.Context, userUUID uuid.UUID) (int, error) {
	lookupURL := fmt.Sprintf("%s/api/v3/core/users/?uuid=%s", p.BaseURL, url.QueryEscape(userUUID.String()))
	status, body, err := p.doJSONRequest(ctx, http.MethodGet, lookupURL, nil)
	if err != nil {
		return 0, err
	}
	if status < 200 || status >= 300 {
		return 0, fmt.Errorf("authentik user uuid lookup HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}
	var listResp struct {
		Results []struct {
			PK   int    `json:"pk"`
			UUID string `json:"uuid"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &listResp); err != nil {
		return 0, fmt.Errorf("decode authentik uuid lookup response: %w", err)
	}
	for _, u := range listResp.Results {
		if strings.EqualFold(strings.TrimSpace(u.UUID), userUUID.String()) {
			return u.PK, nil
		}
	}
	return 0, ErrUserNotFound
}

// CreateUserAndRecoveryLink provisions a net-new Authentik user with an explicit
// email, display name, and username, then generates a one-time recovery/setup URL.
func (p *AuthentikProvisioner) CreateUserAndRecoveryLink(ctx context.Context, email, name, username string) (uuid.UUID, string, error) {
	trimmedEmail := strings.TrimSpace(email)
	trimmedName := strings.TrimSpace(name)
	trimmedUsername := strings.TrimSpace(username)
	if trimmedEmail == "" || trimmedUsername == "" {
		return uuid.Nil, "", fmt.Errorf("email and username are required")
	}
	if trimmedName == "" {
		trimmedName = trimmedUsername
	}

	// Check existing email
	emailURL := fmt.Sprintf("%s/api/v3/core/users/?email=%s", p.BaseURL, url.QueryEscape(trimmedEmail))
	status, body, err := p.doJSONRequest(ctx, http.MethodGet, emailURL, nil)
	if err != nil {
		return uuid.Nil, "", err
	}
	if status < 200 || status >= 300 {
		return uuid.Nil, "", fmt.Errorf("authentik email lookup HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}
	var emailList struct {
		Results []struct {
			Email string `json:"email"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &emailList); err != nil {
		return uuid.Nil, "", fmt.Errorf("decode authentik email lookup: %w", err)
	}
	for _, u := range emailList.Results {
		if strings.EqualFold(strings.TrimSpace(u.Email), trimmedEmail) {
			return uuid.Nil, "", ErrEmailAlreadyExists
		}
	}

	// Check existing username
	userURL := fmt.Sprintf("%s/api/v3/core/users/?username=%s", p.BaseURL, url.QueryEscape(trimmedUsername))
	status, body, err = p.doJSONRequest(ctx, http.MethodGet, userURL, nil)
	if err != nil {
		return uuid.Nil, "", err
	}
	if status < 200 || status >= 300 {
		return uuid.Nil, "", fmt.Errorf("authentik username lookup HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}
	var userList struct {
		Results []struct {
			Username string `json:"username"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &userList); err != nil {
		return uuid.Nil, "", fmt.Errorf("decode authentik username lookup: %w", err)
	}
	for _, u := range userList.Results {
		if strings.EqualFold(strings.TrimSpace(u.Username), trimmedUsername) {
			return uuid.Nil, "", ErrUsernameAlreadyExists
		}
	}

	// Create internal user
	createStatus, createBody, err := p.doJSONRequest(ctx, http.MethodPost, p.BaseURL+"/api/v3/core/users/", map[string]any{
		"username":  trimmedUsername,
		"email":     trimmedEmail,
		"name":      trimmedName,
		"is_active": true,
		"type":      "internal",
	})
	if err != nil {
		return uuid.Nil, "", err
	}
	if createStatus == http.StatusBadRequest || createStatus == http.StatusConflict {
		lower := strings.ToLower(string(createBody))
		if strings.Contains(lower, "email") {
			return uuid.Nil, "", ErrEmailAlreadyExists
		}
		if strings.Contains(lower, "username") {
			return uuid.Nil, "", ErrUsernameAlreadyExists
		}
	}
	if createStatus < 200 || createStatus >= 300 {
		return uuid.Nil, "", fmt.Errorf("authentik user creation HTTP %d: %s", createStatus, strings.TrimSpace(string(createBody)))
	}

	var created struct {
		PK   int    `json:"pk"`
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(createBody, &created); err != nil {
		return uuid.Nil, "", fmt.Errorf("decode authentik create response: %w", err)
	}
	parsedUUID, err := uuid.Parse(strings.TrimSpace(created.UUID))
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("invalid created authentik user uuid %q: %w", created.UUID, err)
	}

	recURL := fmt.Sprintf("%s/api/v3/core/users/%d/recovery/", p.BaseURL, created.PK)
	recStatus, recBody, err := p.doJSONRequest(ctx, http.MethodPost, recURL, map[string]any{})
	if err != nil {
		return uuid.Nil, "", err
	}
	if recStatus < 200 || recStatus >= 300 {
		return uuid.Nil, "", fmt.Errorf("authentik recovery link HTTP %d: %s", recStatus, strings.TrimSpace(string(recBody)))
	}
	var recovery struct {
		Link string `json:"link"`
	}
	if err := json.Unmarshal(recBody, &recovery); err != nil {
		return uuid.Nil, "", fmt.Errorf("decode authentik recovery response: %w", err)
	}

	return parsedUUID, strings.TrimSpace(recovery.Link), nil
}

// SetUserActive updates is_active in Authentik and revokes authenticated sessions
// when disabling an account.
func (p *AuthentikProvisioner) SetUserActive(ctx context.Context, userUUID uuid.UUID, active bool) error {
	pk, err := p.lookupUserPKByUUID(ctx, userUUID)
	if err != nil {
		return err
	}
	patchURL := fmt.Sprintf("%s/api/v3/core/users/%d/", p.BaseURL, pk)
	status, body, err := p.doJSONRequest(ctx, http.MethodPatch, patchURL, map[string]any{
		"is_active": active,
	})
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("authentik set active HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}

	if !active {
		sessURL := fmt.Sprintf("%s/api/v3/core/authenticated_sessions/?user=%d", p.BaseURL, pk)
		sStatus, sBody, err := p.doJSONRequest(ctx, http.MethodGet, sessURL, nil)
		if err != nil {
			return err
		}
		if sStatus < 200 || sStatus >= 300 {
			return fmt.Errorf("authentik session list HTTP %d: %s", sStatus, strings.TrimSpace(string(sBody)))
		}
		var sessions struct {
			Results []struct {
				UUID string `json:"uuid"`
			} `json:"results"`
		}
		if err := json.Unmarshal(sBody, &sessions); err != nil {
			return fmt.Errorf("decode authentik sessions response: %w", err)
		}
		for _, s := range sessions.Results {
			if strings.TrimSpace(s.UUID) == "" {
				continue
			}
			delURL := fmt.Sprintf("%s/api/v3/core/authenticated_sessions/%s/", p.BaseURL, url.PathEscape(strings.TrimSpace(s.UUID)))
			dStatus, dBody, err := p.doJSONRequest(ctx, http.MethodDelete, delURL, nil)
			if err != nil {
				return err
			}
			if dStatus != http.StatusNotFound && (dStatus < 200 || dStatus >= 300) {
				return fmt.Errorf("authentik delete session HTTP %d: %s", dStatus, strings.TrimSpace(string(dBody)))
			}
		}
	}

	return nil
}

// DeleteUser removes the user from Authentik. Missing users (404 / ErrUserNotFound)
// are treated as idempotent success; 5xx and network errors fail closed.
func (p *AuthentikProvisioner) DeleteUser(ctx context.Context, userUUID uuid.UUID) error {
	pk, err := p.lookupUserPKByUUID(ctx, userUUID)
	if errors.Is(err, ErrUserNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	delURL := fmt.Sprintf("%s/api/v3/core/users/%d/", p.BaseURL, pk)
	status, body, err := p.doJSONRequest(ctx, http.MethodDelete, delURL, nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return nil
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("authentik delete user HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}
	return nil
}

// NoopIdentityProvisioner resolves deterministic local dev UUIDs (matching dex/config.yaml
// static accounts) when AUTHENTIK_API_TOKEN is empty.
type NoopIdentityProvisioner struct {
	AppPublicURL string
}

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

func (n *NoopIdentityProvisioner) CreateUserAndRecoveryLink(_ context.Context, email, _ string, _ string) (uuid.UUID, string, error) {
	trimmedEmail := strings.TrimSpace(email)
	lower := strings.ToLower(trimmedEmail)
	id, ok := localDexUsers[lower]
	if !ok {
		id = uuid.NewSHA1(uuid.NameSpaceDNS, []byte("budget-app-dev:"+lower))
	}
	baseURL := strings.TrimRight(strings.TrimSpace(n.AppPublicURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:5173"
	}
	return id, baseURL + "/login", nil
}

func (n *NoopIdentityProvisioner) SetUserActive(_ context.Context, _ uuid.UUID, _ bool) error {
	return nil
}

func (n *NoopIdentityProvisioner) DeleteUser(_ context.Context, _ uuid.UUID) error {
	return nil
}

func NewProvisionerFromEnv() IdentityProvisioner {
	token := strings.TrimSpace(os.Getenv("AUTHENTIK_API_TOKEN"))
	baseURL := strings.TrimSpace(os.Getenv("AUTHENTIK_BASE_URL"))
	if token == "" || baseURL == "" {
		return &NoopIdentityProvisioner{}
	}
	return NewAuthentikProvisioner(baseURL, token, nil)
}

func NewPlatformAdminFromEnv(appPublicURL string) PlatformIdentityAdmin {
	token := strings.TrimSpace(os.Getenv("AUTHENTIK_API_TOKEN"))
	baseURL := strings.TrimSpace(os.Getenv("AUTHENTIK_BASE_URL"))
	if token == "" || baseURL == "" {
		return &NoopIdentityProvisioner{AppPublicURL: appPublicURL}
	}
	return NewAuthentikProvisioner(baseURL, token, nil)
}
