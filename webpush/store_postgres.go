package webpush

import (
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
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
		CREATE TABLE IF NOT EXISTS schema_version (
			store TEXT PRIMARY KEY,
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
	pgUpdateSubscriptionUpdatedAtQuery   = `UPDATE webpush_subscription SET updated_at = $1 WHERE endpoint = $2`
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
	pgInsertSchemaVersion      = `INSERT INTO schema_version (store, version) VALUES ('webpush', $1)`
	pgSelectSchemaVersionQuery = `SELECT version FROM schema_version WHERE store = 'webpush'`
)

// NewPostgresStore creates a new PostgreSQL-backed web push store.
func NewPostgresStore(dsn string) (Store, error) {
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
	return &commonStore{
		db: db,
		queries: storeQueries{
			selectSubscriptionIDByEndpoint:             pgSelectSubscriptionIDByEndpoint,
			selectSubscriptionCountBySubscriberIP:      pgSelectSubscriptionCountBySubscriberIP,
			selectSubscriptionsForTopic:                pgSelectSubscriptionsForTopicQuery,
			selectSubscriptionsExpiringSoon:            pgSelectSubscriptionsExpiringSoonQuery,
			insertSubscription:                         pgInsertSubscriptionQuery,
			updateSubscriptionWarningSent:              pgUpdateSubscriptionWarningSentQuery,
			updateSubscriptionUpdatedAt:                pgUpdateSubscriptionUpdatedAtQuery,
			deleteSubscriptionByEndpoint:               pgDeleteSubscriptionByEndpointQuery,
			deleteSubscriptionByUserID:                 pgDeleteSubscriptionByUserIDQuery,
			deleteSubscriptionByAge:                    pgDeleteSubscriptionByAgeQuery,
			insertSubscriptionTopic:                    pgInsertSubscriptionTopicQuery,
			deleteSubscriptionTopicAll:                 pgDeleteSubscriptionTopicAllQuery,
			deleteSubscriptionTopicWithoutSubscription: pgDeleteSubscriptionTopicWithoutSubscription,
		},
	}, nil
}

func setupPostgresDB(db *sql.DB) error {
	// If 'schema_version' table does not exist or no webpush row, this must be a new database
	rows, err := db.Query(pgSelectSchemaVersionQuery)
	if err != nil {
		return setupNewPostgresDB(db)
	}
	defer rows.Close()
	if !rows.Next() {
		return setupNewPostgresDB(db)
	}
	return nil
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
