package message

import (
	"database/sql"
	"fmt"
)

// Initial PostgreSQL schema
const (
	postgresCreateTablesQuery = `
		CREATE TABLE IF NOT EXISTS message (
			id BIGSERIAL PRIMARY KEY,
			mid TEXT NOT NULL,
			sequence_id TEXT NOT NULL,
			time BIGINT NOT NULL,
			event TEXT NOT NULL,
			expires BIGINT NOT NULL,
			topic TEXT NOT NULL,
			message TEXT NOT NULL,
			title TEXT NOT NULL,
			priority INT NOT NULL,
			tags TEXT NOT NULL,
			click TEXT NOT NULL,
			icon TEXT NOT NULL,
			actions TEXT NOT NULL,
			attachment_name TEXT NOT NULL,
			attachment_type TEXT NOT NULL,
			attachment_size BIGINT NOT NULL,
			attachment_expires BIGINT NOT NULL,
			attachment_url TEXT NOT NULL,
			attachment_deleted BOOLEAN NOT NULL DEFAULT FALSE,
			sender TEXT NOT NULL,
			user_id TEXT NOT NULL,
			content_type TEXT NOT NULL,
			encoding TEXT NOT NULL,
			published BOOLEAN NOT NULL DEFAULT FALSE
		);
		CREATE INDEX IF NOT EXISTS idx_message_mid ON message (mid);
		CREATE INDEX IF NOT EXISTS idx_message_sequence_id ON message (sequence_id);
		CREATE INDEX IF NOT EXISTS idx_message_topic_published_time ON message (topic, published, time, id);
		CREATE INDEX IF NOT EXISTS idx_message_published_expires ON message (published, expires);
		CREATE INDEX IF NOT EXISTS idx_message_sender_attachment_expires ON message (sender, attachment_expires) WHERE user_id = '';
		CREATE INDEX IF NOT EXISTS idx_message_user_id_attachment_expires ON message (user_id, attachment_expires);
		CREATE TABLE IF NOT EXISTS message_stats (
			key TEXT PRIMARY KEY,
			value BIGINT
		);
		INSERT INTO message_stats (key, value) VALUES ('messages', 0);
		CREATE TABLE IF NOT EXISTS schema_version (
			store TEXT PRIMARY KEY,
			version INT NOT NULL
		);
	`
)

// PostgreSQL schema management queries
const (
	pgCurrentSchemaVersion           = 14
	postgresInsertSchemaVersionQuery = `INSERT INTO schema_version (store, version) VALUES ('message', $1)`
	postgresSelectSchemaVersionQuery = `SELECT version FROM schema_version WHERE store = 'message'`
)

func setupPostgresDB(db *sql.DB) error {
	var schemaVersion int
	err := db.QueryRow(postgresSelectSchemaVersionQuery).Scan(&schemaVersion)
	if err != nil {
		return setupNewPostgresDB(db)
	}
	if schemaVersion > pgCurrentSchemaVersion {
		return fmt.Errorf("unexpected schema version: version %d is higher than current version %d", schemaVersion, pgCurrentSchemaVersion)
	}
	return nil
}

func setupNewPostgresDB(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(postgresCreateTablesQuery); err != nil {
		return err
	}
	if _, err := tx.Exec(postgresInsertSchemaVersionQuery, pgCurrentSchemaVersion); err != nil {
		return err
	}
	return tx.Commit()
}
