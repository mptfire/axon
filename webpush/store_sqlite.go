package webpush

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
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
	sqliteUpdateWebPushSubscriptionUpdatedAtQuery   = `UPDATE subscription SET updated_at = ? WHERE endpoint = ?`
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

// NewSQLiteStore creates a new SQLite-backed web push store.
func NewSQLiteStore(filename, startupQueries string) (Store, error) {
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
	return &commonStore{
		db: db,
		queries: storeQueries{
			selectSubscriptionIDByEndpoint:             sqliteSelectWebPushSubscriptionIDByEndpoint,
			selectSubscriptionCountBySubscriberIP:      sqliteSelectWebPushSubscriptionCountBySubscriberIP,
			selectSubscriptionsForTopic:                sqliteSelectWebPushSubscriptionsForTopicQuery,
			selectSubscriptionsExpiringSoon:            sqliteSelectWebPushSubscriptionsExpiringSoonQuery,
			insertSubscription:                         sqliteInsertWebPushSubscriptionQuery,
			updateSubscriptionWarningSent:              sqliteUpdateWebPushSubscriptionWarningSentQuery,
			updateSubscriptionUpdatedAt:                sqliteUpdateWebPushSubscriptionUpdatedAtQuery,
			deleteSubscriptionByEndpoint:               sqliteDeleteWebPushSubscriptionByEndpointQuery,
			deleteSubscriptionByUserID:                 sqliteDeleteWebPushSubscriptionByUserIDQuery,
			deleteSubscriptionByAge:                    sqliteDeleteWebPushSubscriptionByAgeQuery,
			insertSubscriptionTopic:                    sqliteInsertWebPushSubscriptionTopicQuery,
			deleteSubscriptionTopicAll:                 sqliteDeleteWebPushSubscriptionTopicAllQuery,
			deleteSubscriptionTopicWithoutSubscription: sqliteDeleteWebPushSubscriptionTopicWithoutSubscription,
		},
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
