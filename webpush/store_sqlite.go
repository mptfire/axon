package webpush

import (
	"database/sql"
	"net/netip"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite driver

	"heckel.io/ntfy/v2/util"
)

const (
	sqliteCreateWebPushSubscriptionsTableQuery = `
		BEGIN;
		CREATE TABLE IF NOT EXISTS subscription (
			id TEXT PRIMARY KEY,
			endpoint TEXT NOT NULL,
			key_auth TEXT NOT NULL,
			key_p256dh TEXT NOT NULL,
			user_id TEXT NOT NULL,		
			subscriber_ip TEXT NOT NULL,
			updated_at INT NOT NULL,
			warned_at INT NOT NULL DEFAULT 0
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_endpoint ON subscription (endpoint);
		CREATE INDEX IF NOT EXISTS idx_subscriber_ip ON subscription (subscriber_ip);
		CREATE TABLE IF NOT EXISTS subscription_topic (
			subscription_id TEXT NOT NULL,
			topic TEXT NOT NULL,
			PRIMARY KEY (subscription_id, topic),
			FOREIGN KEY (subscription_id) REFERENCES subscription (id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_topic ON subscription_topic (topic);
		CREATE TABLE IF NOT EXISTS schemaVersion (
			id INT PRIMARY KEY,
			version INT NOT NULL
		);			
		COMMIT;
	`
	sqliteBuiltinStartupQueries = `
		PRAGMA foreign_keys = ON;
	`

	sqliteSelectWebPushSubscriptionIDByEndpoint        = `SELECT id FROM subscription WHERE endpoint = ?`
	sqliteSelectWebPushSubscriptionCountBySubscriberIP = `SELECT COUNT(*) FROM subscription WHERE subscriber_ip = ?`
	sqliteSelectWebPushSubscriptionsForTopicQuery      = `
		SELECT id, endpoint, key_auth, key_p256dh, user_id
		FROM subscription_topic st
		JOIN subscription s ON s.id = st.subscription_id
		WHERE st.topic = ?
		ORDER BY endpoint
	`
	sqliteSelectWebPushSubscriptionsExpiringSoonQuery = `
		SELECT id, endpoint, key_auth, key_p256dh, user_id 
		FROM subscription 
		WHERE warned_at = 0 AND updated_at <= ?
	`
	sqliteInsertWebPushSubscriptionQuery = `
		INSERT INTO subscription (id, endpoint, key_auth, key_p256dh, user_id, subscriber_ip, updated_at, warned_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (endpoint) 
		DO UPDATE SET key_auth = excluded.key_auth, key_p256dh = excluded.key_p256dh, user_id = excluded.user_id, subscriber_ip = excluded.subscriber_ip, updated_at = excluded.updated_at, warned_at = excluded.warned_at
	`
	sqliteUpdateWebPushSubscriptionWarningSentQuery = `UPDATE subscription SET warned_at = ? WHERE id = ?`
	sqliteDeleteWebPushSubscriptionByEndpointQuery  = `DELETE FROM subscription WHERE endpoint = ?`
	sqliteDeleteWebPushSubscriptionByUserIDQuery    = `DELETE FROM subscription WHERE user_id = ?`
	sqliteDeleteWebPushSubscriptionByAgeQuery       = `DELETE FROM subscription WHERE updated_at <= ?` // Full table scan!

	sqliteInsertWebPushSubscriptionTopicQuery               = `INSERT INTO subscription_topic (subscription_id, topic) VALUES (?, ?)`
	sqliteDeleteWebPushSubscriptionTopicAllQuery            = `DELETE FROM subscription_topic WHERE subscription_id = ?`
	sqliteDeleteWebPushSubscriptionTopicWithoutSubscription = `DELETE FROM subscription_topic WHERE subscription_id NOT IN (SELECT id FROM subscription)`
)

// SQLite schema management queries
const (
	sqliteCurrentWebPushSchemaVersion     = 1
	sqliteInsertWebPushSchemaVersion      = `INSERT INTO schemaVersion VALUES (1, ?)`
	sqliteSelectWebPushSchemaVersionQuery = `SELECT version FROM schemaVersion WHERE id = 1`
)

// SQLiteStore is a web push subscription store backed by SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLite-backed web push store.
func NewSQLiteStore(filename, startupQueries string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", filename)
	if err != nil {
		return nil, err
	}
	if err := setupSQLiteWebPushDB(db); err != nil {
		return nil, err
	}
	if err := runSQLiteWebPushStartupQueries(db, startupQueries); err != nil {
		return nil, err
	}
	return &SQLiteStore{
		db: db,
	}, nil
}

func setupSQLiteWebPushDB(db *sql.DB) error {
	// If 'schemaVersion' table does not exist, this must be a new database
	rows, err := db.Query(sqliteSelectWebPushSchemaVersionQuery)
	if err != nil {
		return setupNewSQLiteWebPushDB(db)
	}
	return rows.Close()
}

func setupNewSQLiteWebPushDB(db *sql.DB) error {
	if _, err := db.Exec(sqliteCreateWebPushSubscriptionsTableQuery); err != nil {
		return err
	}
	if _, err := db.Exec(sqliteInsertWebPushSchemaVersion, sqliteCurrentWebPushSchemaVersion); err != nil {
		return err
	}
	return nil
}

func runSQLiteWebPushStartupQueries(db *sql.DB, startupQueries string) error {
	if _, err := db.Exec(startupQueries); err != nil {
		return err
	}
	if _, err := db.Exec(sqliteBuiltinStartupQueries); err != nil {
		return err
	}
	return nil
}

// UpsertSubscription adds or updates Web Push subscriptions for the given topics and user ID. It always first deletes all
// existing entries for a given endpoint.
func (c *SQLiteStore) UpsertSubscription(endpoint string, auth, p256dh, userID string, subscriberIP netip.Addr, topics []string) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Read number of subscriptions for subscriber IP address
	rowsCount, err := tx.Query(sqliteSelectWebPushSubscriptionCountBySubscriberIP, subscriberIP.String())
	if err != nil {
		return err
	}
	defer rowsCount.Close()
	var subscriptionCount int
	if !rowsCount.Next() {
		return ErrWebPushNoRows
	}
	if err := rowsCount.Scan(&subscriptionCount); err != nil {
		return err
	}
	if err := rowsCount.Close(); err != nil {
		return err
	}
	// Read existing subscription ID for endpoint (or create new ID)
	rows, err := tx.Query(sqliteSelectWebPushSubscriptionIDByEndpoint, endpoint)
	if err != nil {
		return err
	}
	defer rows.Close()
	var subscriptionID string
	if rows.Next() {
		if err := rows.Scan(&subscriptionID); err != nil {
			return err
		}
	} else {
		if subscriptionCount >= subscriptionEndpointLimitPerSubscriberIP {
			return ErrWebPushTooManySubscriptions
		}
		subscriptionID = util.RandomStringPrefix(subscriptionIDPrefix, subscriptionIDLength)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	// Insert or update subscription
	updatedAt, warnedAt := time.Now().Unix(), 0
	if _, err = tx.Exec(sqliteInsertWebPushSubscriptionQuery, subscriptionID, endpoint, auth, p256dh, userID, subscriberIP.String(), updatedAt, warnedAt); err != nil {
		return err
	}
	// Replace all subscription topics
	if _, err := tx.Exec(sqliteDeleteWebPushSubscriptionTopicAllQuery, subscriptionID); err != nil {
		return err
	}
	for _, topic := range topics {
		if _, err = tx.Exec(sqliteInsertWebPushSubscriptionTopicQuery, subscriptionID, topic); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SubscriptionsForTopic returns all subscriptions for the given topic.
func (c *SQLiteStore) SubscriptionsForTopic(topic string) ([]*Subscription, error) {
	rows, err := c.db.Query(sqliteSelectWebPushSubscriptionsForTopicQuery, topic)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return subscriptionsFromRows(rows)
}

// SubscriptionsExpiring returns all subscriptions that have not been updated for a given time period.
func (c *SQLiteStore) SubscriptionsExpiring(warnAfter time.Duration) ([]*Subscription, error) {
	rows, err := c.db.Query(sqliteSelectWebPushSubscriptionsExpiringSoonQuery, time.Now().Add(-warnAfter).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return subscriptionsFromRows(rows)
}

// MarkExpiryWarningSent marks the given subscriptions as having received a warning about expiring soon.
func (c *SQLiteStore) MarkExpiryWarningSent(subscriptions []*Subscription) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, subscription := range subscriptions {
		if _, err := tx.Exec(sqliteUpdateWebPushSubscriptionWarningSentQuery, time.Now().Unix(), subscription.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RemoveSubscriptionsByEndpoint removes the subscription for the given endpoint.
func (c *SQLiteStore) RemoveSubscriptionsByEndpoint(endpoint string) error {
	_, err := c.db.Exec(sqliteDeleteWebPushSubscriptionByEndpointQuery, endpoint)
	return err
}

// RemoveSubscriptionsByUserID removes all subscriptions for the given user ID.
func (c *SQLiteStore) RemoveSubscriptionsByUserID(userID string) error {
	if userID == "" {
		return ErrWebPushUserIDCannotBeEmpty
	}
	_, err := c.db.Exec(sqliteDeleteWebPushSubscriptionByUserIDQuery, userID)
	return err
}

// RemoveExpiredSubscriptions removes all subscriptions that have not been updated for a given time period.
func (c *SQLiteStore) RemoveExpiredSubscriptions(expireAfter time.Duration) error {
	_, err := c.db.Exec(sqliteDeleteWebPushSubscriptionByAgeQuery, time.Now().Add(-expireAfter).Unix())
	if err != nil {
		return err
	}
	_, err = c.db.Exec(sqliteDeleteWebPushSubscriptionTopicWithoutSubscription)
	return err
}

// SetSubscriptionUpdatedAt updates the updated_at timestamp for a subscription by endpoint. This is
// exported for testing purposes and is not part of the Store interface.
func (c *SQLiteStore) SetSubscriptionUpdatedAt(endpoint string, updatedAt int64) error {
	_, err := c.db.Exec("UPDATE subscription SET updated_at = ? WHERE endpoint = ?", updatedAt, endpoint)
	return err
}

// Close closes the underlying database connection.
func (c *SQLiteStore) Close() error {
	return c.db.Close()
}
