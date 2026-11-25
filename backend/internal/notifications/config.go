package notifications

import (
	"errors"
	"os"
	"strings"
)

var (
	// ErrPartialVAPIDConfig is returned when only a subset of VAPID_PUBLIC_KEY,
	// VAPID_PRIVATE_KEY, and VAPID_SUBJECT is configured.
	ErrPartialVAPIDConfig = errors.New("partial VAPID configuration: VAPID_PUBLIC_KEY, VAPID_PRIVATE_KEY, and VAPID_SUBJECT must all be set")
	// ErrInvalidVAPIDSubject is returned when VAPID_SUBJECT is neither mailto: nor https://.
	ErrInvalidVAPIDSubject = errors.New("invalid VAPID_SUBJECT: must start with mailto: or https://")
)

// VAPIDConfig holds the optional Web Push VAPID credentials.
// When all fields are empty, Web Push is cleanly disabled.
type VAPIDConfig struct {
	PublicKey  string
	PrivateKey string
	Subject    string
}

// Enabled reports whether complete VAPID credentials are configured.
func (c VAPIDConfig) Enabled() bool {
	return c.PublicKey != "" && c.PrivateKey != "" && c.Subject != ""
}

// Validate checks that either all VAPID fields are empty (disabled) or all are
// set with a valid RFC 8292 subject (mailto: or https://).
func (c VAPIDConfig) Validate() error {
	if c.PublicKey == "" && c.PrivateKey == "" && c.Subject == "" {
		return nil
	}
	if !c.Enabled() {
		return ErrPartialVAPIDConfig
	}
	if !strings.HasPrefix(c.Subject, "mailto:") && !strings.HasPrefix(c.Subject, "https://") {
		return ErrInvalidVAPIDSubject
	}
	return nil
}

// webpushSubscriber returns the subscriber string formatted for SherClockHolmes/webpush-go,
// which automatically prepends "mailto:" unless the value starts with "https:".
func (c VAPIDConfig) webpushSubscriber() string {
	if strings.HasPrefix(c.Subject, "mailto:") {
		return strings.TrimPrefix(c.Subject, "mailto:")
	}
	return c.Subject
}

// LoadVAPIDConfigFromEnv reads and validates VAPID_PUBLIC_KEY, VAPID_PRIVATE_KEY,
// and VAPID_SUBJECT from the process environment. If validation fails, a zero
// VAPIDConfig (disabled) is returned alongside the error.
func LoadVAPIDConfigFromEnv() (VAPIDConfig, error) {
	cfg := VAPIDConfig{
		PublicKey:  strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY")),
		PrivateKey: strings.TrimSpace(os.Getenv("VAPID_PRIVATE_KEY")),
		Subject:    strings.TrimSpace(os.Getenv("VAPID_SUBJECT")),
	}
	if err := cfg.Validate(); err != nil {
		return VAPIDConfig{}, err
	}
	return cfg, nil
}
