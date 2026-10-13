package user

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// AIConsent records an account owner's acceptance of a specific provider configuration.
// It is separate from preferences so a concurrent settings update cannot undo revocation.
type AIConsent struct {
	ProviderConfig string
	GrantedAt      time.Time
}

// AIConsent loads the account's current consent from the primary; nil means none.
func (a *Manager) AIConsent(ctx context.Context, userID string) (*AIConsent, error) {
	var configuration string
	var granted int64
	err := a.db.QueryRowContext(ctx, a.queries.selectAIConsent, userID).Scan(&configuration, &granted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &AIConsent{ProviderConfig: configuration, GrantedAt: time.Unix(granted, 0)}, nil
}

// GrantAIConsent records explicit owner consent; providerConfig is an opaque 256-bit identity.
func (a *Manager) GrantAIConsent(ctx context.Context, userID, providerConfig string) error {
	if bytes, err := hex.DecodeString(providerConfig); err != nil || len(bytes) != 32 {
		return errors.New("invalid AI provider configuration identity")
	}
	_, err := a.db.ExecContext(ctx, a.queries.upsertAIConsent, userID, providerConfig, time.Now().Unix())
	return err
}

// RevokeAIConsent prevents subsequent provider calls for this account.
func (a *Manager) RevokeAIConsent(ctx context.Context, userID string) error {
	_, err := a.db.ExecContext(ctx, a.queries.deleteAIConsent, userID)
	return err
}

// AIConsentSecret returns the persistent HMAC key for opaque provider identities.
// A keyed identity does not expose a brute-forceable hash of secret URL components.
func (a *Manager) AIConsentSecret(ctx context.Context) ([]byte, error) {
	var encoded string
	err := a.db.QueryRowContext(ctx, a.queries.selectAIConsentSecret).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, err
		}
		if _, err := a.db.ExecContext(ctx, a.queries.insertAIConsentSecret, hex.EncodeToString(secret)); err != nil {
			return nil, err
		}
		err = a.db.QueryRowContext(ctx, a.queries.selectAIConsentSecret).Scan(&encoded)
	}
	if err != nil {
		return nil, err
	}
	secret, err := hex.DecodeString(encoded)
	if err != nil || len(secret) != 32 {
		return nil, errors.New("invalid AI consent metadata")
	}
	return secret, nil
}
