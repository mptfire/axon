package webpush

import (
	"database/sql"
	"errors"
	"net/netip"
	"time"

	"heckel.io/ntfy/v2/util"
)

const (
	subscriptionIDPrefix                     = "wps_"
	subscriptionIDLength                     = 10
	subscriptionEndpointLimitPerSubscriberIP = 10
)

// Errors returned by the store
var (
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

// storeQueries holds the database-specific SQL queries.
type storeQueries struct {
	selectSubscriptionIDByEndpoint             string
	selectSubscriptionCountBySubscriberIP      string
	selectSubscriptionsForTopic                string
	selectSubscriptionsExpiringSoon            string
	upsertSubscription                         string
	updateSubscriptionWarningSent              string
	updateSubscriptionUpdatedAt                string
	deleteSubscriptionByEndpoint               string
	deleteSubscriptionByUserID                 string
	deleteSubscriptionByAge                    string
	insertSubscriptionTopic                    string
	deleteSubscriptionTopicAll                 string
	deleteSubscriptionTopicWithoutSubscription string
}

// commonStore implements store operations that are identical across database backends.
type commonStore struct {
	db      *sql.DB
	queries storeQueries
}

// UpsertSubscription adds or updates Web Push subscriptions for the given topics and user ID.
func (s *commonStore) UpsertSubscription(endpoint string, auth, p256dh, userID string, subscriberIP netip.Addr, topics []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Read number of subscriptions for subscriber IP address
	var subscriptionCount int
	if err := tx.QueryRow(s.queries.selectSubscriptionCountBySubscriberIP, subscriberIP.String()).Scan(&subscriptionCount); err != nil {
		return err
	}
	// Read existing subscription ID for endpoint (or create new ID)
	var subscriptionID string
	if err := tx.QueryRow(s.queries.selectSubscriptionIDByEndpoint, endpoint).Scan(&subscriptionID); errors.Is(err, sql.ErrNoRows) {
		if subscriptionCount >= subscriptionEndpointLimitPerSubscriberIP {
			return ErrWebPushTooManySubscriptions
		}
		subscriptionID = util.RandomStringPrefix(subscriptionIDPrefix, subscriptionIDLength)
	} else if err != nil {
		return err
	}
	// Insert or update subscription
	updatedAt, warnedAt := time.Now().Unix(), 0
	if _, err := tx.Exec(s.queries.upsertSubscription, subscriptionID, endpoint, auth, p256dh, userID, subscriberIP.String(), updatedAt, warnedAt); err != nil {
		return err
	}
	// Replace all subscription topics
	if _, err := tx.Exec(s.queries.deleteSubscriptionTopicAll, subscriptionID); err != nil {
		return err
	}
	for _, topic := range topics {
		if _, err = tx.Exec(s.queries.insertSubscriptionTopic, subscriptionID, topic); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SubscriptionsForTopic returns all subscriptions for the given topic.
func (s *commonStore) SubscriptionsForTopic(topic string) ([]*Subscription, error) {
	rows, err := s.db.Query(s.queries.selectSubscriptionsForTopic, topic)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return subscriptionsFromRows(rows)
}

// SubscriptionsExpiring returns all subscriptions that have not been updated for a given time period.
func (s *commonStore) SubscriptionsExpiring(warnAfter time.Duration) ([]*Subscription, error) {
	rows, err := s.db.Query(s.queries.selectSubscriptionsExpiringSoon, time.Now().Add(-warnAfter).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return subscriptionsFromRows(rows)
}

// MarkExpiryWarningSent marks the given subscriptions as having received a warning about expiring soon.
func (s *commonStore) MarkExpiryWarningSent(subscriptions []*Subscription) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, subscription := range subscriptions {
		if _, err := tx.Exec(s.queries.updateSubscriptionWarningSent, time.Now().Unix(), subscription.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RemoveSubscriptionsByEndpoint removes the subscription for the given endpoint.
func (s *commonStore) RemoveSubscriptionsByEndpoint(endpoint string) error {
	_, err := s.db.Exec(s.queries.deleteSubscriptionByEndpoint, endpoint)
	return err
}

// RemoveSubscriptionsByUserID removes all subscriptions for the given user ID.
func (s *commonStore) RemoveSubscriptionsByUserID(userID string) error {
	if userID == "" {
		return ErrWebPushUserIDCannotBeEmpty
	}
	_, err := s.db.Exec(s.queries.deleteSubscriptionByUserID, userID)
	return err
}

// RemoveExpiredSubscriptions removes all subscriptions that have not been updated for a given time period.
func (s *commonStore) RemoveExpiredSubscriptions(expireAfter time.Duration) error {
	_, err := s.db.Exec(s.queries.deleteSubscriptionByAge, time.Now().Add(-expireAfter).Unix())
	if err != nil {
		return err
	}
	_, err = s.db.Exec(s.queries.deleteSubscriptionTopicWithoutSubscription)
	return err
}

// SetSubscriptionUpdatedAt updates the updated_at timestamp for a subscription by endpoint. This is
// exported for testing purposes.
func (s *commonStore) SetSubscriptionUpdatedAt(endpoint string, updatedAt int64) error {
	_, err := s.db.Exec(s.queries.updateSubscriptionUpdatedAt, updatedAt, endpoint)
	return err
}

// Close closes the underlying database connection.
func (s *commonStore) Close() error {
	return s.db.Close()
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
