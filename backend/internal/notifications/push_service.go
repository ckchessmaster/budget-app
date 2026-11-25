package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrPushDisabled is returned when a caller attempts to subscribe while VAPID is unconfigured.
	ErrPushDisabled = errors.New("push notifications are disabled")
	// ErrInvalidSubscription is returned when a push subscription payload fails validation.
	ErrInvalidSubscription = errors.New("invalid push subscription")
)

type PushSubscriptionDTO struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

type NotificationPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
}

type PushService interface {
	Enabled() bool
	PublicKey() string
	Subscribe(ctx context.Context, userID uuid.UUID, sub PushSubscriptionDTO) error
	Unsubscribe(ctx context.Context, userID uuid.UUID, endpoint string) error
	SendNotification(ctx context.Context, userID uuid.UUID, payload NotificationPayload) error
}

type storedSubscription struct {
	ID       uuid.UUID
	Endpoint string
	P256dh   string
	Auth     string
}

type WebPushService struct {
	pool       *pgxpool.Pool
	cfg        VAPIDConfig
	httpClient webpush.HTTPClient
}

// NewWebPushService constructs an opt-in Web Push dispatcher backed by PostgreSQL.
// When cfg is empty/disabled, Subscribe returns ErrPushDisabled and SendNotification
// becomes a safe no-op.
func NewWebPushService(pool *pgxpool.Pool, cfg VAPIDConfig, httpClient webpush.HTTPClient) *WebPushService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &WebPushService{
		pool:       pool,
		cfg:        cfg,
		httpClient: httpClient,
	}
}

func (s *WebPushService) Enabled() bool {
	return s.cfg.Enabled()
}

func (s *WebPushService) PublicKey() string {
	if !s.cfg.Enabled() {
		return ""
	}
	return s.cfg.PublicKey
}

func (s *WebPushService) Subscribe(ctx context.Context, userID uuid.UUID, sub PushSubscriptionDTO) error {
	if !s.cfg.Enabled() {
		return ErrPushDisabled
	}

	sub.Endpoint = strings.TrimSpace(sub.Endpoint)
	sub.P256dh = strings.TrimSpace(sub.P256dh)
	sub.Auth = strings.TrimSpace(sub.Auth)

	if err := validateSubscription(sub); err != nil {
		return err
	}

	return db.WithWorkspaceTx(ctx, s.pool, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, updated_at)
			VALUES ($1, $2, $3, $4, NOW())
			ON CONFLICT (endpoint) DO UPDATE
			SET p256dh = EXCLUDED.p256dh,
			    auth = EXCLUDED.auth,
			    updated_at = NOW()
		`, userID, sub.Endpoint, sub.P256dh, sub.Auth)
		return err
	})
}

func (s *WebPushService) Unsubscribe(ctx context.Context, userID uuid.UUID, endpoint string) error {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return fmt.Errorf("%w: endpoint is required", ErrInvalidSubscription)
	}

	return db.WithWorkspaceTx(ctx, s.pool, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			DELETE FROM push_subscriptions
			WHERE user_id = $1 AND endpoint = $2
		`, userID, endpoint)
		return err
	})
}

func (s *WebPushService) SendNotification(ctx context.Context, userID uuid.UUID, payload NotificationPayload) error {
	if !s.cfg.Enabled() {
		return nil
	}

	var subs []storedSubscription
	err := db.WithWorkspaceTx(ctx, s.pool, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, endpoint, p256dh, auth
			FROM push_subscriptions
			WHERE user_id = $1
			ORDER BY created_at ASC
		`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var item storedSubscription
			if err := rows.Scan(&item.ID, &item.Endpoint, &item.P256dh, &item.Auth); err != nil {
				return err
			}
			subs = append(subs, item)
		}
		return rows.Err()
	})
	if err != nil {
		return fmt.Errorf("failed to load push subscriptions: %w", err)
	}
	if len(subs) == 0 {
		return nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal notification payload: %w", err)
	}

	opts := &webpush.Options{
		HTTPClient:      s.httpClient,
		Subscriber:      s.cfg.webpushSubscriber(),
		VAPIDPublicKey:  s.cfg.PublicKey,
		VAPIDPrivateKey: s.cfg.PrivateKey,
		TTL:             3600,
		Urgency:         webpush.UrgencyNormal,
	}

	var sendErrs []error
	for _, sub := range subs {
		wpSub := &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys: webpush.Keys{
				P256dh: sub.P256dh,
				Auth:   sub.Auth,
			},
		}

		resp, err := webpush.SendNotificationWithContext(ctx, body, wpSub, opts)
		if resp != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		if err != nil {
			sendErrs = append(sendErrs, fmt.Errorf("push send to subscription %s failed: %w", sub.ID, err))
			continue
		}
		if resp == nil {
			sendErrs = append(sendErrs, fmt.Errorf("push send to subscription %s returned nil response", sub.ID))
			continue
		}

		if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
			if delErr := s.deleteSubscriptionByID(ctx, userID, sub.ID); delErr != nil {
				sendErrs = append(sendErrs, fmt.Errorf("failed to prune stale subscription %s: %w", sub.ID, delErr))
			} else {
				slog.Info("pruned expired push subscription",
					"subscription_id", sub.ID,
					"user_id", userID,
					"status", resp.StatusCode)
			}
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			sendErrs = append(sendErrs, fmt.Errorf("push endpoint returned HTTP %d for subscription %s", resp.StatusCode, sub.ID))
		}
	}

	return errors.Join(sendErrs...)
}

func (s *WebPushService) deleteSubscriptionByID(ctx context.Context, userID, subID uuid.UUID) error {
	return db.WithWorkspaceTx(ctx, s.pool, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			DELETE FROM push_subscriptions
			WHERE id = $1 AND user_id = $2
		`, subID, userID)
		return err
	})
}

func validateSubscription(sub PushSubscriptionDTO) error {
	if sub.Endpoint == "" || sub.P256dh == "" || sub.Auth == "" {
		return fmt.Errorf("%w: endpoint, p256dh, and auth are required", ErrInvalidSubscription)
	}
	parsed, err := url.ParseRequestURI(sub.Endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("%w: endpoint must be a valid http or https URL", ErrInvalidSubscription)
	}
	return nil
}
