package user

import (
	"heckel.io/ntfy/v2/db/schema"
)

// Initial PostgreSQL schema
const (
	postgresCreateTablesQueries = `
		CREATE TABLE IF NOT EXISTS tier (
			id TEXT PRIMARY KEY,
			code TEXT NOT NULL,
			name TEXT NOT NULL,
			messages_limit BIGINT NOT NULL,
			messages_expiry_duration BIGINT NOT NULL,
			emails_limit BIGINT NOT NULL,
			calls_limit BIGINT NOT NULL,
			reservations_limit BIGINT NOT NULL,
			attachment_file_size_limit BIGINT NOT NULL,
			attachment_total_size_limit BIGINT NOT NULL,
			attachment_expiry_duration BIGINT NOT NULL,
			attachment_bandwidth_limit BIGINT NOT NULL,
			stripe_monthly_price_id TEXT,
			stripe_yearly_price_id TEXT,
			UNIQUE(code),
			UNIQUE(stripe_monthly_price_id),
			UNIQUE(stripe_yearly_price_id)
		);
		CREATE TABLE IF NOT EXISTS "user" (
		    id TEXT PRIMARY KEY,
			tier_id TEXT REFERENCES tier(id),
			user_name TEXT NOT NULL UNIQUE,
			pass TEXT NOT NULL,
			role TEXT NOT NULL CHECK (role IN ('anonymous', 'admin', 'user')),
			prefs JSONB NOT NULL DEFAULT '{}',
			sync_topic TEXT NOT NULL,
			provisioned BOOLEAN NOT NULL,
			stats_messages BIGINT NOT NULL DEFAULT 0,
			stats_emails BIGINT NOT NULL DEFAULT 0,
			stats_calls BIGINT NOT NULL DEFAULT 0,
			stripe_customer_id TEXT UNIQUE,
			stripe_subscription_id TEXT UNIQUE,
			stripe_subscription_status TEXT,
			stripe_subscription_interval TEXT,
			stripe_subscription_paid_until BIGINT,
			stripe_subscription_cancel_at BIGINT,
			created BIGINT NOT NULL,
			deleted BIGINT
		);
		CREATE TABLE IF NOT EXISTS user_access (
			user_id TEXT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
			topic TEXT NOT NULL,
			read BOOLEAN NOT NULL,
			write BOOLEAN NOT NULL,
			owner_user_id TEXT REFERENCES "user"(id) ON DELETE CASCADE,
			provisioned BOOLEAN NOT NULL,
			PRIMARY KEY (user_id, topic)
		);
		CREATE TABLE IF NOT EXISTS user_token (
			user_id TEXT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
			token TEXT NOT NULL UNIQUE,
			label TEXT NOT NULL,
			last_access BIGINT NOT NULL,
			last_origin TEXT NOT NULL,
			expires BIGINT NOT NULL,
			provisioned BOOLEAN NOT NULL,
			scopes TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, token)
		);
		CREATE TABLE IF NOT EXISTS user_phone (
			user_id TEXT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
			phone_number TEXT NOT NULL,
			PRIMARY KEY (user_id, phone_number)
		);
		CREATE TABLE IF NOT EXISTS user_email (
			user_id TEXT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
			email TEXT NOT NULL,
			is_primary BOOLEAN NOT NULL DEFAULT FALSE,
			PRIMARY KEY (user_id, email)
		);
		CREATE UNIQUE INDEX idx_user_email_primary_user ON user_email (user_id) WHERE is_primary;
		CREATE UNIQUE INDEX idx_user_email_primary_addr ON user_email (email) WHERE is_primary;
		CREATE TABLE IF NOT EXISTS user_magic_link (
			token_hash TEXT NOT NULL,
			kind TEXT NOT NULL,
			user_id TEXT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
			email TEXT,
			expires BIGINT NOT NULL,
			created BIGINT NOT NULL,
			PRIMARY KEY (token_hash)
		);
		CREATE INDEX idx_magic_link_user_kind ON user_magic_link (user_id, kind);
		INSERT INTO "user" (id, user_name, pass, role, sync_topic, provisioned, created)
		VALUES ('` + everyoneID + `', '*', '', 'anonymous', '', false, EXTRACT(EPOCH FROM NOW())::BIGINT)
		ON CONFLICT (id) DO NOTHING;
	CREATE TABLE IF NOT EXISTS user_device (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			token TEXT,
			label TEXT NOT NULL DEFAULT '',
			config TEXT NOT NULL DEFAULT '{}',
			created_at INT NOT NULL,
			updated_at INT NOT NULL,
			last_seen INT NOT NULL DEFAULT 0,
			config_version BIGINT NOT NULL DEFAULT 1,
			applied_version BIGINT NOT NULL DEFAULT 0,
			applied_at BIGINT NOT NULL DEFAULT 0,
			FOREIGN KEY (user_id) REFERENCES "user" (id) ON DELETE CASCADE
		);
		CREATE UNIQUE INDEX idx_user_device_token ON user_device (token) WHERE token IS NOT NULL;
		CREATE INDEX idx_user_device_user ON user_device (user_id);
		CREATE TABLE IF NOT EXISTS user_pairing_code (
			code TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			expires_at INT NOT NULL,
			used INT NOT NULL DEFAULT 0,
			created_at INT NOT NULL,
			FOREIGN KEY (user_id) REFERENCES "user" (id) ON DELETE CASCADE
		);
		CREATE INDEX idx_user_pairing_code_expiry ON user_pairing_code (expires_at);
`
)

const (
	// axon: fork migration 9 -> 10 adds token scopes (see sqlite side)
	postgresCurrentSchemaVersion = 12
)

const (
	postgresMigrate6To7UpdateQueries = `
		CREATE TABLE IF NOT EXISTS user_email (
			user_id TEXT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
			email TEXT NOT NULL,
			PRIMARY KEY (user_id, email)
		);
	`

	// 7 -> 8: primary (recovery) email + magic-link table for verification/reset.
	// No backfill -- existing verified emails stay non-primary.
	postgresMigrate7To8UpdateQueries = `
		ALTER TABLE user_email ADD COLUMN is_primary BOOLEAN NOT NULL DEFAULT FALSE;
		CREATE UNIQUE INDEX idx_user_email_primary_user ON user_email (user_id) WHERE is_primary;
		CREATE UNIQUE INDEX idx_user_email_primary_addr ON user_email (email) WHERE is_primary;
		CREATE TABLE IF NOT EXISTS user_magic_link (
			token_hash TEXT NOT NULL,
			kind TEXT NOT NULL,
			user_id TEXT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
			email TEXT,
			expires BIGINT NOT NULL,
			created BIGINT NOT NULL,
			PRIMARY KEY (token_hash)
		);
		CREATE INDEX idx_magic_link_user_kind ON user_magic_link (user_id, kind);
	`
	// 9 -> 10: axon fork migration — token scopes for AI agents
	postgresMigrate9To10UpdateQueries = `
		ALTER TABLE user_token ADD COLUMN scopes TEXT NOT NULL DEFAULT '';
	`
)

var (
	postgresCreateTables = schema.AsMigrateFunc(postgresCreateTablesQueries)

	// postgresMigrations maps a schema version to the migration upgrading it to the next
	// version. Always append migrations at the end, never insert in the middle.
	postgresMigrate11To12UpdateQueries = `
		ALTER TABLE user_device ADD COLUMN config_version BIGINT NOT NULL DEFAULT (1);
		ALTER TABLE user_device ADD COLUMN applied_version BIGINT NOT NULL DEFAULT (0);
		ALTER TABLE user_device ADD COLUMN applied_at BIGINT NOT NULL DEFAULT (0);
	`

	postgresMigrations = map[int]schema.MigrateFunc{
		6:  schema.AsMigrateFunc(postgresMigrate6To7UpdateQueries),
		7:  schema.AsMigrateFunc(postgresMigrate7To8UpdateQueries),
		8:  schema.NopMigrateFunc,                                    // 8 -> 9 repairs a SQLite-only foreign key defect; nothing to do on Postgres
		9:  schema.AsMigrateFunc(postgresMigrate9To10UpdateQueries),  // axon
		10: schema.AsMigrateFunc(postgresMigrate10To11UpdateQueries), // axon: devices + pairing
		11: schema.AsMigrateFunc(postgresMigrate11To12UpdateQueries), // axon: applied-config ack
	}
)

// axon: migration 10 -> 11 — device registry + pairing codes (Postgres twin of
// the SQLite migration).
const postgresMigrate10To11UpdateQueries = `
		CREATE TABLE IF NOT EXISTS user_device (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			token TEXT,
			label TEXT NOT NULL DEFAULT '',
			config TEXT NOT NULL DEFAULT '{}',
			created_at INT NOT NULL,
			updated_at INT NOT NULL,
			last_seen INT NOT NULL DEFAULT 0,
			FOREIGN KEY (user_id) REFERENCES "user" (id) ON DELETE CASCADE
		);
		CREATE UNIQUE INDEX idx_user_device_token ON user_device (token) WHERE token IS NOT NULL;
		CREATE INDEX idx_user_device_user ON user_device (user_id);
		CREATE TABLE IF NOT EXISTS user_pairing_code (
			code TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			expires_at INT NOT NULL,
			used INT NOT NULL DEFAULT 0,
			created_at INT NOT NULL,
			FOREIGN KEY (user_id) REFERENCES "user" (id) ON DELETE CASCADE
		);
		CREATE INDEX idx_user_pairing_code_expiry ON user_pairing_code (expires_at);
	`
