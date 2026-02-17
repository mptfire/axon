package webpush

import (
	"database/sql"
	"errors"
	"net/netip"
	"time"

	"heckel.io/ntfy/v2/log"
)

const (
	subscriptionIDPrefix                     = "wps_"
	subscriptionIDLength                     = 10
	subscriptionEndpointLimitPerSubscriberIP = 10
)

var (
	ErrWebPushNoRows               = errors.New("no rows found")
	ErrWebPushTooManySubscriptions = errors.New("too many subscriptions")
	ErrWebPushUserIDCannotBeEmpty  = errors.New("user ID cannot be empty")
)

// Store is the interface for a web push subscription store.
type Store interface {
	UpsertSubscription(endpoint, auth, p256dh, userID string, subscriberIP netip.Addr, topics []string) error
	SubscriptionsForTopic(topic string) ([]*Subscription, error)
	SubscriptionsExpiring(warnAfter time.Duration) ([]*Subscription, error)
	MarkExpiryWarningSent(subscriptions []*Subscription) error
	RemoveSubscriptionsByEndpoint(endpoint string) error
	RemoveSubscriptionsByUserID(userID string) error
	RemoveExpiredSubscriptions(expireAfter time.Duration) error
	SetSubscriptionUpdatedAt(endpoint string, updatedAt int64) error
	Close() error
}

// Subscription represents a web push subscription.
type Subscription struct {
	ID       string
	Endpoint string
	Auth     string
	P256dh   string
	UserID   string
}

// Context returns the logging context for the subscription.
func (w *Subscription) Context() log.Context {
	return map[string]any{
		"web_push_subscription_id":       w.ID,
		"web_push_subscription_user_id":  w.UserID,
		"web_push_subscription_endpoint": w.Endpoint,
	}
}

func subscriptionsFromRows(rows *sql.Rows) ([]*Subscription, error) {
	subscriptions := make([]*Subscription, 0)
	for rows.Next() {
		var id, endpoint, auth, p256dh, userID string
		if err := rows.Scan(&id, &endpoint, &auth, &p256dh, &userID); err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, &Subscription{
			ID:       id,
			Endpoint: endpoint,
			Auth:     auth,
			P256dh:   p256dh,
			UserID:   userID,
		})
	}
	return subscriptions, nil
}
