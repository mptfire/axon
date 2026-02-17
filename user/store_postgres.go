package user

import (
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
)

// PostgreSQL schema and queries
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
			PRIMARY KEY (user_id, token)
		);
		CREATE TABLE IF NOT EXISTS user_phone (
			user_id TEXT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
			phone_number TEXT NOT NULL,
			PRIMARY KEY (user_id, phone_number)
		);
		CREATE TABLE IF NOT EXISTS schema_version (
			store TEXT PRIMARY KEY,
			version INT NOT NULL
		);
		INSERT INTO "user" (id, user_name, pass, role, sync_topic, provisioned, created)
		VALUES ('` + everyoneID + `', '*', '', 'anonymous', '', false, EXTRACT(EPOCH FROM NOW())::BIGINT)
		ON CONFLICT (id) DO NOTHING;
	`

	// User queries
	postgresSelectUserByID = `
		SELECT u.id, u.user_name, u.pass, u.role, u.prefs, u.sync_topic, u.provisioned, u.stats_messages, u.stats_emails, u.stats_calls, u.stripe_customer_id, u.stripe_subscription_id, u.stripe_subscription_status, u.stripe_subscription_interval, u.stripe_subscription_paid_until, u.stripe_subscription_cancel_at, u.deleted, t.id, t.code, t.name, t.messages_limit, t.messages_expiry_duration, t.emails_limit, t.calls_limit, t.reservations_limit, t.attachment_file_size_limit, t.attachment_total_size_limit, t.attachment_expiry_duration, t.attachment_bandwidth_limit, t.stripe_monthly_price_id, t.stripe_yearly_price_id
		FROM "user" u
		LEFT JOIN tier t on t.id = u.tier_id
		WHERE u.id = $1
	`
	postgresSelectUserByName = `
		SELECT u.id, u.user_name, u.pass, u.role, u.prefs, u.sync_topic, u.provisioned, u.stats_messages, u.stats_emails, u.stats_calls, u.stripe_customer_id, u.stripe_subscription_id, u.stripe_subscription_status, u.stripe_subscription_interval, u.stripe_subscription_paid_until, u.stripe_subscription_cancel_at, u.deleted, t.id, t.code, t.name, t.messages_limit, t.messages_expiry_duration, t.emails_limit, t.calls_limit, t.reservations_limit, t.attachment_file_size_limit, t.attachment_total_size_limit, t.attachment_expiry_duration, t.attachment_bandwidth_limit, t.stripe_monthly_price_id, t.stripe_yearly_price_id
		FROM "user" u
		LEFT JOIN tier t on t.id = u.tier_id
		WHERE user_name = $1
	`
	postgresSelectUserByToken = `
		SELECT u.id, u.user_name, u.pass, u.role, u.prefs, u.sync_topic, u.provisioned, u.stats_messages, u.stats_emails, u.stats_calls, u.stripe_customer_id, u.stripe_subscription_id, u.stripe_subscription_status, u.stripe_subscription_interval, u.stripe_subscription_paid_until, u.stripe_subscription_cancel_at, u.deleted, t.id, t.code, t.name, t.messages_limit, t.messages_expiry_duration, t.emails_limit, t.calls_limit, t.reservations_limit, t.attachment_file_size_limit, t.attachment_total_size_limit, t.attachment_expiry_duration, t.attachment_bandwidth_limit, t.stripe_monthly_price_id, t.stripe_yearly_price_id
		FROM "user" u
		JOIN user_token tk on u.id = tk.user_id
		LEFT JOIN tier t on t.id = u.tier_id
		WHERE tk.token = $1 AND (tk.expires = 0 OR tk.expires >= $2)
	`
	postgresSelectUserByStripeID = `
		SELECT u.id, u.user_name, u.pass, u.role, u.prefs, u.sync_topic, u.provisioned, u.stats_messages, u.stats_emails, u.stats_calls, u.stripe_customer_id, u.stripe_subscription_id, u.stripe_subscription_status, u.stripe_subscription_interval, u.stripe_subscription_paid_until, u.stripe_subscription_cancel_at, u.deleted, t.id, t.code, t.name, t.messages_limit, t.messages_expiry_duration, t.emails_limit, t.calls_limit, t.reservations_limit, t.attachment_file_size_limit, t.attachment_total_size_limit, t.attachment_expiry_duration, t.attachment_bandwidth_limit, t.stripe_monthly_price_id, t.stripe_yearly_price_id
		FROM "user" u
		LEFT JOIN tier t on t.id = u.tier_id
		WHERE u.stripe_customer_id = $1
	`
	postgresSelectUsernames = `
		SELECT user_name
		FROM "user"
		ORDER BY
			CASE role
				WHEN 'admin' THEN 1
				WHEN 'anonymous' THEN 3
				ELSE 2
			END, user_name
	`
	postgresSelectUserCount          = `SELECT COUNT(*) FROM "user"`
	postgresSelectUserIDFromUsername = `SELECT id FROM "user" WHERE user_name = $1`
	postgresInsertUser               = `INSERT INTO "user" (id, user_name, pass, role, sync_topic, provisioned, created) VALUES ($1, $2, $3, $4, $5, $6, $7)`
	postgresUpdateUserPass           = `UPDATE "user" SET pass = $1 WHERE user_name = $2`
	postgresUpdateUserRole           = `UPDATE "user" SET role = $1 WHERE user_name = $2`
	postgresUpdateUserProvisioned    = `UPDATE "user" SET provisioned = $1 WHERE user_name = $2`
	postgresUpdateUserPrefs          = `UPDATE "user" SET prefs = $1 WHERE id = $2`
	postgresUpdateUserStats          = `UPDATE "user" SET stats_messages = $1, stats_emails = $2, stats_calls = $3 WHERE id = $4`
	postgresUpdateUserStatsResetAll  = `UPDATE "user" SET stats_messages = 0, stats_emails = 0, stats_calls = 0`
	postgresUpdateUserTier           = `UPDATE "user" SET tier_id = (SELECT id FROM tier WHERE code = $1) WHERE user_name = $2`
	postgresUpdateUserDeleted        = `UPDATE "user" SET deleted = $1 WHERE id = $2`
	postgresDeleteUser               = `DELETE FROM "user" WHERE user_name = $1`
	postgresDeleteUserTier           = `UPDATE "user" SET tier_id = null WHERE user_name = $1`
	postgresDeleteUsersMarked        = `DELETE FROM "user" WHERE deleted < $1`

	// Access queries
	postgresSelectTopicPerms = `
		SELECT read, write
		FROM user_access a
		JOIN "user" u ON u.id = a.user_id
		WHERE (u.user_name = $1 OR u.user_name = $2) AND $3 LIKE a.topic ESCAPE '\'
		ORDER BY u.user_name DESC, LENGTH(a.topic) DESC, CASE WHEN a.write THEN 1 ELSE 0 END DESC
	`
	postgresSelectUserAllAccess = `
		SELECT user_id, topic, read, write, provisioned
		FROM user_access
		ORDER BY LENGTH(topic) DESC, CASE WHEN write THEN 1 ELSE 0 END DESC, CASE WHEN read THEN 1 ELSE 0 END DESC, topic
	`
	postgresSelectUserAccess = `
		SELECT topic, read, write, provisioned
		FROM user_access
		WHERE user_id = (SELECT id FROM "user" WHERE user_name = $1)
		ORDER BY LENGTH(topic) DESC, CASE WHEN write THEN 1 ELSE 0 END DESC, CASE WHEN read THEN 1 ELSE 0 END DESC, topic
	`
	postgresSelectUserReservations = `
		SELECT a_user.topic, a_user.read, a_user.write, a_everyone.read AS everyone_read, a_everyone.write AS everyone_write
		FROM user_access a_user
		LEFT JOIN  user_access a_everyone ON a_user.topic = a_everyone.topic AND a_everyone.user_id = (SELECT id FROM "user" WHERE user_name = $1)
		WHERE a_user.user_id = a_user.owner_user_id
		  AND a_user.owner_user_id = (SELECT id FROM "user" WHERE user_name = $2)
		ORDER BY a_user.topic
	`
	postgresSelectUserReservationsCount = `
		SELECT COUNT(*)
		FROM user_access
		WHERE user_id = owner_user_id
		  AND owner_user_id = (SELECT id FROM "user" WHERE user_name = $1)
	`
	postgresSelectUserReservationsOwner = `
		SELECT owner_user_id
		FROM user_access
		WHERE topic = $1
		  AND user_id = owner_user_id
	`
	postgresSelectUserHasReservation = `
		SELECT COUNT(*)
		FROM user_access
		WHERE user_id = owner_user_id
		  AND owner_user_id = (SELECT id FROM "user" WHERE user_name = $1)
		  AND topic = $2
	`
	postgresSelectOtherAccessCount = `
		SELECT COUNT(*)
		FROM user_access
		WHERE (topic = $1 OR $2 LIKE topic ESCAPE '\')
		  AND (owner_user_id IS NULL OR owner_user_id != (SELECT id FROM "user" WHERE user_name = $3))
	`
	postgresUpsertUserAccess = `
		INSERT INTO user_access (user_id, topic, read, write, owner_user_id, provisioned)
		VALUES (
			(SELECT id FROM "user" WHERE user_name = $1),
			$2,
			$3,
			$4,
			CASE WHEN $5 = '' THEN NULL ELSE (SELECT id FROM "user" WHERE user_name = $6) END,
			$7
		)
		ON CONFLICT (user_id, topic)
		DO UPDATE SET read=EXCLUDED.read, write=EXCLUDED.write, owner_user_id=EXCLUDED.owner_user_id, provisioned=EXCLUDED.provisioned
	`
	postgresDeleteUserAccess = `
		DELETE FROM user_access
		WHERE user_id = (SELECT id FROM "user" WHERE user_name = $1)
		   OR owner_user_id = (SELECT id FROM "user" WHERE user_name = $2)
	`
	postgresDeleteUserAccessProvisioned = `DELETE FROM user_access WHERE provisioned = true`
	postgresDeleteTopicAccess           = `
		DELETE FROM user_access
	   	WHERE (user_id = (SELECT id FROM "user" WHERE user_name = $1) OR owner_user_id = (SELECT id FROM "user" WHERE user_name = $2))
	   	  AND topic = $3
  	`
	postgresDeleteAllAccess = `DELETE FROM user_access`

	// Token queries
	postgresSelectToken                = `SELECT token, label, last_access, last_origin, expires, provisioned FROM user_token WHERE user_id = $1 AND token = $2`
	postgresSelectTokens               = `SELECT token, label, last_access, last_origin, expires, provisioned FROM user_token WHERE user_id = $1`
	postgresSelectTokenCount           = `SELECT COUNT(*) FROM user_token WHERE user_id = $1`
	postgresSelectAllProvisionedTokens = `SELECT token, label, last_access, last_origin, expires, provisioned FROM user_token WHERE provisioned = true`
	postgresUpsertToken                = `
		INSERT INTO user_token (user_id, token, label, last_access, last_origin, expires, provisioned)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id, token)
		DO UPDATE SET label = EXCLUDED.label, expires = EXCLUDED.expires, provisioned = EXCLUDED.provisioned
	`
	postgresUpdateTokenLabel       = `UPDATE user_token SET label = $1 WHERE user_id = $2 AND token = $3`
	postgresUpdateTokenExpiry      = `UPDATE user_token SET expires = $1 WHERE user_id = $2 AND token = $3`
	postgresUpdateTokenLastAccess  = `UPDATE user_token SET last_access = $1, last_origin = $2 WHERE token = $3`
	postgresDeleteToken            = `DELETE FROM user_token WHERE user_id = $1 AND token = $2`
	postgresDeleteProvisionedToken = `DELETE FROM user_token WHERE token = $1`
	postgresDeleteAllToken         = `DELETE FROM user_token WHERE user_id = $1`
	postgresDeleteExpiredTokens    = `DELETE FROM user_token WHERE expires > 0 AND expires < $1`
	postgresDeleteExcessTokens     = `
		DELETE FROM user_token
		WHERE user_id = $1
		  AND (user_id, token) NOT IN (
			SELECT user_id, token
			FROM user_token
			WHERE user_id = $2
			ORDER BY expires DESC
			LIMIT $3
		)
	`

	// Tier queries
	postgresInsertTier = `
		INSERT INTO tier (id, code, name, messages_limit, messages_expiry_duration, emails_limit, calls_limit, reservations_limit, attachment_file_size_limit, attachment_total_size_limit, attachment_expiry_duration, attachment_bandwidth_limit, stripe_monthly_price_id, stripe_yearly_price_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	postgresUpdateTier = `
		UPDATE tier
		SET name = $1, messages_limit = $2, messages_expiry_duration = $3, emails_limit = $4, calls_limit = $5, reservations_limit = $6, attachment_file_size_limit = $7, attachment_total_size_limit = $8, attachment_expiry_duration = $9, attachment_bandwidth_limit = $10, stripe_monthly_price_id = $11, stripe_yearly_price_id = $12
		WHERE code = $13
	`
	postgresSelectTiers = `
		SELECT id, code, name, messages_limit, messages_expiry_duration, emails_limit, calls_limit, reservations_limit, attachment_file_size_limit, attachment_total_size_limit, attachment_expiry_duration, attachment_bandwidth_limit, stripe_monthly_price_id, stripe_yearly_price_id
		FROM tier
	`
	postgresSelectTierByCode = `
		SELECT id, code, name, messages_limit, messages_expiry_duration, emails_limit, calls_limit, reservations_limit, attachment_file_size_limit, attachment_total_size_limit, attachment_expiry_duration, attachment_bandwidth_limit, stripe_monthly_price_id, stripe_yearly_price_id
		FROM tier
		WHERE code = $1
	`
	postgresSelectTierByPriceID = `
		SELECT id, code, name, messages_limit, messages_expiry_duration, emails_limit, calls_limit, reservations_limit, attachment_file_size_limit, attachment_total_size_limit, attachment_expiry_duration, attachment_bandwidth_limit, stripe_monthly_price_id, stripe_yearly_price_id
		FROM tier
		WHERE (stripe_monthly_price_id = $1 OR stripe_yearly_price_id = $2)
	`
	postgresDeleteTier = `DELETE FROM tier WHERE code = $1`

	// Phone queries
	postgresSelectPhoneNumbers = `SELECT phone_number FROM user_phone WHERE user_id = $1`
	postgresInsertPhoneNumber  = `INSERT INTO user_phone (user_id, phone_number) VALUES ($1, $2)`
	postgresDeletePhoneNumber  = `DELETE FROM user_phone WHERE user_id = $1 AND phone_number = $2`

	// Billing queries
	postgresUpdateBilling = `
		UPDATE "user"
		SET stripe_customer_id = $1, stripe_subscription_id = $2, stripe_subscription_status = $3, stripe_subscription_interval = $4, stripe_subscription_paid_until = $5, stripe_subscription_cancel_at = $6
		WHERE user_name = $7
	`

	// Schema version queries
	postgresSelectSchemaVersionExists = `SELECT EXISTS (
		SELECT FROM information_schema.tables
		WHERE table_schema = current_schema()
		AND table_name = 'schema_version'
	)`
	postgresSelectSchemaVersion = `SELECT version FROM schema_version WHERE store = 'user'`
	postgresInsertSchemaVersion = `INSERT INTO schema_version (store, version) VALUES ('user', $1)`
)

// NewPostgresStore creates a new PostgreSQL-backed user store
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
		db:      db,
		queries: postgresQueries(),
	}, nil
}

func postgresQueries() storeQueries {
	return storeQueries{
		// User queries
		selectUserByID:           postgresSelectUserByID,
		selectUserByName:         postgresSelectUserByName,
		selectUserByToken:        postgresSelectUserByToken,
		selectUserByStripeID:     postgresSelectUserByStripeID,
		selectUsernames:          postgresSelectUsernames,
		selectUserCount:          postgresSelectUserCount,
		selectUserIDFromUsername: postgresSelectUserIDFromUsername,
		insertUser:               postgresInsertUser,
		updateUserPass:           postgresUpdateUserPass,
		updateUserRole:           postgresUpdateUserRole,
		updateUserProvisioned:    postgresUpdateUserProvisioned,
		updateUserPrefs:          postgresUpdateUserPrefs,
		updateUserStats:          postgresUpdateUserStats,
		updateUserStatsResetAll:  postgresUpdateUserStatsResetAll,
		updateUserTier:           postgresUpdateUserTier,
		updateUserDeleted:        postgresUpdateUserDeleted,
		deleteUser:               postgresDeleteUser,
		deleteUserTier:           postgresDeleteUserTier,
		deleteUsersMarked:        postgresDeleteUsersMarked,

		// Access queries
		selectTopicPerms:            postgresSelectTopicPerms,
		selectUserAllAccess:         postgresSelectUserAllAccess,
		selectUserAccess:            postgresSelectUserAccess,
		selectUserReservations:      postgresSelectUserReservations,
		selectUserReservationsCount: postgresSelectUserReservationsCount,
		selectUserReservationsOwner: postgresSelectUserReservationsOwner,
		selectUserHasReservation:    postgresSelectUserHasReservation,
		selectOtherAccessCount:      postgresSelectOtherAccessCount,
		upsertUserAccess:            postgresUpsertUserAccess,
		deleteUserAccess:            postgresDeleteUserAccess,
		deleteUserAccessProvisioned: postgresDeleteUserAccessProvisioned,
		deleteTopicAccess:           postgresDeleteTopicAccess,
		deleteAllAccess:             postgresDeleteAllAccess,

		// Token queries
		selectToken:                postgresSelectToken,
		selectTokens:               postgresSelectTokens,
		selectTokenCount:           postgresSelectTokenCount,
		selectAllProvisionedTokens: postgresSelectAllProvisionedTokens,
		upsertToken:                postgresUpsertToken,
		updateTokenLabel:           postgresUpdateTokenLabel,
		updateTokenExpiry:          postgresUpdateTokenExpiry,
		updateTokenLastAccess:      postgresUpdateTokenLastAccess,
		deleteToken:                postgresDeleteToken,
		deleteProvisionedToken:     postgresDeleteProvisionedToken,
		deleteAllToken:             postgresDeleteAllToken,
		deleteExpiredTokens:        postgresDeleteExpiredTokens,
		deleteExcessTokens:         postgresDeleteExcessTokens,

		// Tier queries
		insertTier:          postgresInsertTier,
		selectTiers:         postgresSelectTiers,
		selectTierByCode:    postgresSelectTierByCode,
		selectTierByPriceID: postgresSelectTierByPriceID,
		updateTier:          postgresUpdateTier,
		deleteTier:          postgresDeleteTier,

		// Phone queries
		selectPhoneNumbers: postgresSelectPhoneNumbers,
		insertPhoneNumber:  postgresInsertPhoneNumber,
		deletePhoneNumber:  postgresDeletePhoneNumber,

		// Billing queries
		updateBilling: postgresUpdateBilling,
	}
}

func setupPostgresDB(db *sql.DB) error {
	// Check if schema version table exists
	var exists bool
	if err := db.QueryRow(postgresSelectSchemaVersionExists).Scan(&exists); err != nil {
		return err
	}

	if !exists {
		// New database, create all tables
		if _, err := db.Exec(postgresCreateTablesQueries); err != nil {
			return err
		}
		if _, err := db.Exec(postgresInsertSchemaVersion, currentSchemaVersion); err != nil {
			return err
		}
		return nil
	}

	// Table exists, check if user store has a row
	var schemaVersion int
	err := db.QueryRow(postgresSelectSchemaVersion).Scan(&schemaVersion)
	if err == sql.ErrNoRows {
		// schema_version table exists (e.g. created by webpush) but no user row yet
		if _, err := db.Exec(postgresCreateTablesQueries); err != nil {
			return err
		}
		if _, err := db.Exec(postgresInsertSchemaVersion, currentSchemaVersion); err != nil {
			return err
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("cannot determine schema version: %v", err)
	}

	if schemaVersion > currentSchemaVersion {
		return fmt.Errorf("unexpected schema version: version %d is higher than current version %d", schemaVersion, currentSchemaVersion)
	}

	// Note: PostgreSQL migrations will be added when needed
	// For now, we only support new installations at the latest schema version

	return nil
}
