package webpush

import (
	"database/sql"
	"errors"
	"net/netip"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver

	"heckel.io/ntfy/v2/util"
)

const (
	pgCreateTablesQuery = `
		CREATE TABLE IF NOT EXISTS webpush_subscription (
			id TEXT PRIMARY KEY,
			endpoint TEXT NOT NULL UNIQUE,
			key_auth TEXT NOT NULL,
			key_p256dh TEXT NOT NULL,
			user_id TEXT NOT NULL,
			subscriber_ip TEXT NOT NULL,
			updated_at BIGINT NOT NULL,
			warned_at BIGINT NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_webpush_subscriber_ip ON webpush_subscription (subscriber_ip);
		CREATE TABLE IF NOT EXISTS webpush_subscription_topic (
			subscription_id TEXT NOT NULL REFERENCES webpush_subscription (id) ON DELETE CASCADE,
			topic TEXT NOT NULL,
			PRIMARY KEY (subscription_id, topic)
		);
		CREATE INDEX IF NOT EXISTS idx_webpush_topic ON webpush_subscription_topic (topic);
		CREATE TABLE IF NOT EXISTS webpush_schema_version (
			id INT PRIMARY KEY,
			version INT NOT NULL
		);
	`

	pgSelectSubscriptionIDByEndpoint        = `SELECT id FROM webpush_subscription WHERE endpoint = $1`
	pgSelectSubscriptionCountBySubscriberIP = `SELECT COUNT(*) FROM webpush_subscription WHERE subscriber_ip = $1`
	pgSelectSubscriptionsForTopicQuery      = `
		SELECT s.id, s.endpoint, s.key_auth, s.key_p256dh, s.user_id
		FROM webpush_subscription_topic st
		JOIN webpush_subscription s ON s.id = st.subscription_id
		WHERE st.topic = $1
		ORDER BY s.endpoint
	`
	pgSelectSubscriptionsExpiringSoonQuery = `
		SELECT id, endpoint, key_auth, key_p256dh, user_id
		FROM webpush_subscription
		WHERE warned_at = 0 AND updated_at <= $1
	`
	pgInsertSubscriptionQuery = `
		INSERT INTO webpush_subscription (id, endpoint, key_auth, key_p256dh, user_id, subscriber_ip, updated_at, warned_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (endpoint)
		DO UPDATE SET key_auth = excluded.key_auth, key_p256dh = excluded.key_p256dh, user_id = excluded.user_id, subscriber_ip = excluded.subscriber_ip, updated_at = excluded.updated_at, warned_at = excluded.warned_at
	`
	pgUpdateSubscriptionWarningSentQuery = `UPDATE webpush_subscription SET warned_at = $1 WHERE id = $2`
	pgDeleteSubscriptionByEndpointQuery  = `DELETE FROM webpush_subscription WHERE endpoint = $1`
	pgDeleteSubscriptionByUserIDQuery    = `DELETE FROM webpush_subscription WHERE user_id = $1`
	pgDeleteSubscriptionByAgeQuery       = `DELETE FROM webpush_subscription WHERE updated_at <= $1`

	pgInsertSubscriptionTopicQuery               = `INSERT INTO webpush_subscription_topic (subscription_id, topic) VALUES ($1, $2)`
	pgDeleteSubscriptionTopicAllQuery            = `DELETE FROM webpush_subscription_topic WHERE subscription_id = $1`
	pgDeleteSubscriptionTopicWithoutSubscription = `DELETE FROM webpush_subscription_topic WHERE subscription_id NOT IN (SELECT id FROM webpush_subscription)`
)

// PostgreSQL schema management queries
const (
	pgCurrentSchemaVersion     = 1
	pgInsertSchemaVersion      = `INSERT INTO webpush_schema_version VALUES (1, $1)`
	pgSelectSchemaVersionQuery = `SELECT version FROM webpush_schema_version WHERE id = 1`
)

// PostgresStore is a web push subscription store backed by PostgreSQL.
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore creates a new PostgreSQL-backed web push store.
func NewPostgresStore(dsn string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	if err := setupPostgresDB(db); err != nil {
		return nil, err
	}
	return &PostgresStore{
		db: db,
	}, nil
}

func setupPostgresDB(db *sql.DB) error {
	// If 'webpush_schema_version' table does not exist, this must be a new database
	rows, err := db.Query(pgSelectSchemaVersionQuery)
	if err != nil {
		return setupNewPostgresDB(db)
	}
	return rows.Close()
}

func setupNewPostgresDB(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(pgCreateTablesQuery); err != nil {
		return err
	}
	if _, err := tx.Exec(pgInsertSchemaVersion, pgCurrentSchemaVersion); err != nil {
		return err
	}
	return tx.Commit()
}

// UpsertSubscription adds or updates Web Push subscriptions for the given topics and user ID.
func (c *PostgresStore) UpsertSubscription(endpoint string, auth, p256dh, userID string, subscriberIP netip.Addr, topics []string) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Read number of subscriptions for subscriber IP address
	var subscriptionCount int
	if err := tx.QueryRow(pgSelectSubscriptionCountBySubscriberIP, subscriberIP.String()).Scan(&subscriptionCount); err != nil {
		return err
	}
	// Read existing subscription ID for endpoint (or create new ID)
	var subscriptionID string
	err = tx.QueryRow(pgSelectSubscriptionIDByEndpoint, endpoint).Scan(&subscriptionID)
	if errors.Is(err, sql.ErrNoRows) {
		if subscriptionCount >= subscriptionEndpointLimitPerSubscriberIP {
			return ErrWebPushTooManySubscriptions
		}
		subscriptionID = util.RandomStringPrefix(subscriptionIDPrefix, subscriptionIDLength)
	} else if err != nil {
		return err
	}
	// Insert or update subscription
	updatedAt, warnedAt := time.Now().Unix(), 0
	if _, err = tx.Exec(pgInsertSubscriptionQuery, subscriptionID, endpoint, auth, p256dh, userID, subscriberIP.String(), updatedAt, warnedAt); err != nil {
		return err
	}
	// Replace all subscription topics
	if _, err := tx.Exec(pgDeleteSubscriptionTopicAllQuery, subscriptionID); err != nil {
		return err
	}
	for _, topic := range topics {
		if _, err = tx.Exec(pgInsertSubscriptionTopicQuery, subscriptionID, topic); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SubscriptionsForTopic returns all subscriptions for the given topic.
func (c *PostgresStore) SubscriptionsForTopic(topic string) ([]*Subscription, error) {
	rows, err := c.db.Query(pgSelectSubscriptionsForTopicQuery, topic)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return subscriptionsFromRows(rows)
}

// SubscriptionsExpiring returns all subscriptions that have not been updated for a given time period.
func (c *PostgresStore) SubscriptionsExpiring(warnAfter time.Duration) ([]*Subscription, error) {
	rows, err := c.db.Query(pgSelectSubscriptionsExpiringSoonQuery, time.Now().Add(-warnAfter).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return subscriptionsFromRows(rows)
}

// MarkExpiryWarningSent marks the given subscriptions as having received a warning about expiring soon.
func (c *PostgresStore) MarkExpiryWarningSent(subscriptions []*Subscription) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, subscription := range subscriptions {
		if _, err := tx.Exec(pgUpdateSubscriptionWarningSentQuery, time.Now().Unix(), subscription.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RemoveSubscriptionsByEndpoint removes the subscription for the given endpoint.
func (c *PostgresStore) RemoveSubscriptionsByEndpoint(endpoint string) error {
	_, err := c.db.Exec(pgDeleteSubscriptionByEndpointQuery, endpoint)
	return err
}

// RemoveSubscriptionsByUserID removes all subscriptions for the given user ID.
func (c *PostgresStore) RemoveSubscriptionsByUserID(userID string) error {
	if userID == "" {
		return ErrWebPushUserIDCannotBeEmpty
	}
	_, err := c.db.Exec(pgDeleteSubscriptionByUserIDQuery, userID)
	return err
}

// RemoveExpiredSubscriptions removes all subscriptions that have not been updated for a given time period.
func (c *PostgresStore) RemoveExpiredSubscriptions(expireAfter time.Duration) error {
	_, err := c.db.Exec(pgDeleteSubscriptionByAgeQuery, time.Now().Add(-expireAfter).Unix())
	if err != nil {
		return err
	}
	_, err = c.db.Exec(pgDeleteSubscriptionTopicWithoutSubscription)
	return err
}

// SetSubscriptionUpdatedAt updates the updated_at timestamp for a subscription by endpoint. This is
// exported for testing purposes and is not part of the Store interface.
func (c *PostgresStore) SetSubscriptionUpdatedAt(endpoint string, updatedAt int64) error {
	_, err := c.db.Exec("UPDATE webpush_subscription SET updated_at = $1 WHERE endpoint = $2", updatedAt, endpoint)
	return err
}

// Close closes the underlying database connection.
func (c *PostgresStore) Close() error {
	return c.db.Close()
}
