package user

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// SQLite schema and queries
const (
	sqliteCreateTablesQueries = `
		BEGIN;
		CREATE TABLE IF NOT EXISTS tier (
			id TEXT PRIMARY KEY,
			code TEXT NOT NULL,
			name TEXT NOT NULL,
			messages_limit INT NOT NULL,
			messages_expiry_duration INT NOT NULL,
			emails_limit INT NOT NULL,
			calls_limit INT NOT NULL,
			reservations_limit INT NOT NULL,
			attachment_file_size_limit INT NOT NULL,
			attachment_total_size_limit INT NOT NULL,
			attachment_expiry_duration INT NOT NULL,
			attachment_bandwidth_limit INT NOT NULL,
			stripe_monthly_price_id TEXT,
			stripe_yearly_price_id TEXT
		);
		CREATE UNIQUE INDEX idx_tier_code ON tier (code);
		CREATE UNIQUE INDEX idx_tier_stripe_monthly_price_id ON tier (stripe_monthly_price_id);
		CREATE UNIQUE INDEX idx_tier_stripe_yearly_price_id ON tier (stripe_yearly_price_id);
		CREATE TABLE IF NOT EXISTS user (
		    id TEXT PRIMARY KEY,
			tier_id TEXT,
			user TEXT NOT NULL,
			pass TEXT NOT NULL,
			role TEXT CHECK (role IN ('anonymous', 'admin', 'user')) NOT NULL,
			prefs JSON NOT NULL DEFAULT '{}',
			sync_topic TEXT NOT NULL,
			provisioned INT NOT NULL,
			stats_messages INT NOT NULL DEFAULT (0),
			stats_emails INT NOT NULL DEFAULT (0),
			stats_calls INT NOT NULL DEFAULT (0),
			stripe_customer_id TEXT,
			stripe_subscription_id TEXT,
			stripe_subscription_status TEXT,
			stripe_subscription_interval TEXT,
			stripe_subscription_paid_until INT,
			stripe_subscription_cancel_at INT,
			created INT NOT NULL,
			deleted INT,
		    FOREIGN KEY (tier_id) REFERENCES tier (id)
		);
		CREATE UNIQUE INDEX idx_user ON user (user);
		CREATE UNIQUE INDEX idx_user_stripe_customer_id ON user (stripe_customer_id);
		CREATE UNIQUE INDEX idx_user_stripe_subscription_id ON user (stripe_subscription_id);
		CREATE TABLE IF NOT EXISTS user_access (
			user_id TEXT NOT NULL,
			topic TEXT NOT NULL,
			read INT NOT NULL,
			write INT NOT NULL,
			owner_user_id INT,
			provisioned INT NOT NULL,
			PRIMARY KEY (user_id, topic),
			FOREIGN KEY (user_id) REFERENCES user (id) ON DELETE CASCADE,
		    FOREIGN KEY (owner_user_id) REFERENCES user (id) ON DELETE CASCADE
		);
		CREATE TABLE IF NOT EXISTS user_token (
			user_id TEXT NOT NULL,
			token TEXT NOT NULL,
			label TEXT NOT NULL,
			last_access INT NOT NULL,
			last_origin TEXT NOT NULL,
			expires INT NOT NULL,
			provisioned INT NOT NULL,
			PRIMARY KEY (user_id, token),
			FOREIGN KEY (user_id) REFERENCES user (id) ON DELETE CASCADE
		);
		CREATE UNIQUE INDEX idx_user_token ON user_token (token);
		CREATE TABLE IF NOT EXISTS user_phone (
			user_id TEXT NOT NULL,
			phone_number TEXT NOT NULL,
			PRIMARY KEY (user_id, phone_number),
			FOREIGN KEY (user_id) REFERENCES user (id) ON DELETE CASCADE
		);
		CREATE TABLE IF NOT EXISTS schemaVersion (
			id INT PRIMARY KEY,
			version INT NOT NULL
		);
		INSERT INTO user (id, user, pass, role, sync_topic, provisioned, created)
		VALUES ('` + everyoneID + `', '*', '', 'anonymous', '', false, UNIXEPOCH())
		ON CONFLICT (id) DO NOTHING;
		COMMIT;
	`

	sqliteBuiltinStartupQueries = `
		PRAGMA foreign_keys = ON;
	`

	// User queries
	sqliteSelectUserByID = `
		SELECT u.id, u.user, u.pass, u.role, u.prefs, u.sync_topic, u.provisioned, u.stats_messages, u.stats_emails, u.stats_calls, u.stripe_customer_id, u.stripe_subscription_id, u.stripe_subscription_status, u.stripe_subscription_interval, u.stripe_subscription_paid_until, u.stripe_subscription_cancel_at, deleted, t.id, t.code, t.name, t.messages_limit, t.messages_expiry_duration, t.emails_limit, t.calls_limit, t.reservations_limit, t.attachment_file_size_limit, t.attachment_total_size_limit, t.attachment_expiry_duration, t.attachment_bandwidth_limit, t.stripe_monthly_price_id, t.stripe_yearly_price_id
		FROM user u
		LEFT JOIN tier t on t.id = u.tier_id
		WHERE u.id = ?
	`
	sqliteSelectUserByName = `
		SELECT u.id, u.user, u.pass, u.role, u.prefs, u.sync_topic, u.provisioned, u.stats_messages, u.stats_emails, u.stats_calls, u.stripe_customer_id, u.stripe_subscription_id, u.stripe_subscription_status, u.stripe_subscription_interval, u.stripe_subscription_paid_until, u.stripe_subscription_cancel_at, deleted, t.id, t.code, t.name, t.messages_limit, t.messages_expiry_duration, t.emails_limit, t.calls_limit, t.reservations_limit, t.attachment_file_size_limit, t.attachment_total_size_limit, t.attachment_expiry_duration, t.attachment_bandwidth_limit, t.stripe_monthly_price_id, t.stripe_yearly_price_id
		FROM user u
		LEFT JOIN tier t on t.id = u.tier_id
		WHERE user = ?
	`
	sqliteSelectUserByToken = `
		SELECT u.id, u.user, u.pass, u.role, u.prefs, u.sync_topic, u.provisioned, u.stats_messages, u.stats_emails, u.stats_calls, u.stripe_customer_id, u.stripe_subscription_id, u.stripe_subscription_status, u.stripe_subscription_interval, u.stripe_subscription_paid_until, u.stripe_subscription_cancel_at, deleted, t.id, t.code, t.name, t.messages_limit, t.messages_expiry_duration, t.emails_limit, t.calls_limit, t.reservations_limit, t.attachment_file_size_limit, t.attachment_total_size_limit, t.attachment_expiry_duration, t.attachment_bandwidth_limit, t.stripe_monthly_price_id, t.stripe_yearly_price_id
		FROM user u
		JOIN user_token tk on u.id = tk.user_id
		LEFT JOIN tier t on t.id = u.tier_id
		WHERE tk.token = ? AND (tk.expires = 0 OR tk.expires >= ?)
	`
	sqliteSelectUserByStripeID = `
		SELECT u.id, u.user, u.pass, u.role, u.prefs, u.sync_topic, u.provisioned, u.stats_messages, u.stats_emails, u.stats_calls, u.stripe_customer_id, u.stripe_subscription_id, u.stripe_subscription_status, u.stripe_subscription_interval, u.stripe_subscription_paid_until, u.stripe_subscription_cancel_at, deleted, t.id, t.code, t.name, t.messages_limit, t.messages_expiry_duration, t.emails_limit, t.calls_limit, t.reservations_limit, t.attachment_file_size_limit, t.attachment_total_size_limit, t.attachment_expiry_duration, t.attachment_bandwidth_limit, t.stripe_monthly_price_id, t.stripe_yearly_price_id
		FROM user u
		LEFT JOIN tier t on t.id = u.tier_id
		WHERE u.stripe_customer_id = ?
	`
	sqliteSelectUsernames = `
		SELECT user
		FROM user
		ORDER BY
			CASE role
				WHEN 'admin' THEN 1
				WHEN 'anonymous' THEN 3
				ELSE 2
			END, user
	`
	sqliteSelectUserCount          = `SELECT COUNT(*) FROM user`
	sqliteSelectUserIDFromUsername = `SELECT id FROM user WHERE user = ?`
	sqliteInsertUser               = `INSERT INTO user (id, user, pass, role, sync_topic, provisioned, created) VALUES (?, ?, ?, ?, ?, ?, ?)`
	sqliteUpdateUserPass           = `UPDATE user SET pass = ? WHERE user = ?`
	sqliteUpdateUserRole           = `UPDATE user SET role = ? WHERE user = ?`
	sqliteUpdateUserProvisioned    = `UPDATE user SET provisioned = ? WHERE user = ?`
	sqliteUpdateUserPrefs          = `UPDATE user SET prefs = ? WHERE id = ?`
	sqliteUpdateUserStats          = `UPDATE user SET stats_messages = ?, stats_emails = ?, stats_calls = ? WHERE id = ?`
	sqliteUpdateUserStatsResetAll  = `UPDATE user SET stats_messages = 0, stats_emails = 0, stats_calls = 0`
	sqliteUpdateUserTier           = `UPDATE user SET tier_id = (SELECT id FROM tier WHERE code = ?) WHERE user = ?`
	sqliteUpdateUserDeleted        = `UPDATE user SET deleted = ? WHERE id = ?`
	sqliteDeleteUser               = `DELETE FROM user WHERE user = ?`
	sqliteDeleteUserTier           = `UPDATE user SET tier_id = null WHERE user = ?`
	sqliteDeleteUsersMarked        = `DELETE FROM user WHERE deleted < ?`

	// Access queries
	sqliteSelectTopicPerms = `
		SELECT read, write
		FROM user_access a
		JOIN user u ON u.id = a.user_id
		WHERE (u.user = ? OR u.user = ?) AND ? LIKE a.topic ESCAPE '\'
		ORDER BY u.user DESC, LENGTH(a.topic) DESC, a.write DESC
	`
	sqliteSelectUserAllAccess = `
		SELECT user_id, topic, read, write, provisioned
		FROM user_access
		ORDER BY LENGTH(topic) DESC, write DESC, read DESC, topic
	`
	sqliteSelectUserAccess = `
		SELECT topic, read, write, provisioned
		FROM user_access
		WHERE user_id = (SELECT id FROM user WHERE user = ?)
		ORDER BY LENGTH(topic) DESC, write DESC, read DESC, topic
	`
	sqliteSelectUserReservations = `
		SELECT a_user.topic, a_user.read, a_user.write, a_everyone.read AS everyone_read, a_everyone.write AS everyone_write
		FROM user_access a_user
		LEFT JOIN  user_access a_everyone ON a_user.topic = a_everyone.topic AND a_everyone.user_id = (SELECT id FROM user WHERE user = ?)
		WHERE a_user.user_id = a_user.owner_user_id
		  AND a_user.owner_user_id = (SELECT id FROM user WHERE user = ?)
		ORDER BY a_user.topic
	`
	sqliteSelectUserReservationsCount = `
		SELECT COUNT(*)
		FROM user_access
		WHERE user_id = owner_user_id
		  AND owner_user_id = (SELECT id FROM user WHERE user = ?)
	`
	sqliteSelectUserReservationsOwner = `
		SELECT owner_user_id
		FROM user_access
		WHERE topic = ?
		  AND user_id = owner_user_id
	`
	sqliteSelectUserHasReservation = `
		SELECT COUNT(*)
		FROM user_access
		WHERE user_id = owner_user_id
		  AND owner_user_id = (SELECT id FROM user WHERE user = ?)
		  AND topic = ?
	`
	sqliteSelectOtherAccessCount = `
		SELECT COUNT(*)
		FROM user_access
		WHERE (topic = ? OR ? LIKE topic ESCAPE '\')
		  AND (owner_user_id IS NULL OR owner_user_id != (SELECT id FROM user WHERE user = ?))
	`
	sqliteUpsertUserAccess = `
		INSERT INTO user_access (user_id, topic, read, write, owner_user_id, provisioned)
		VALUES ((SELECT id FROM user WHERE user = ?), ?, ?, ?, (SELECT IIF(?='',NULL,(SELECT id FROM user WHERE user=?))), ?)
		ON CONFLICT (user_id, topic)
		DO UPDATE SET read=excluded.read, write=excluded.write, owner_user_id=excluded.owner_user_id, provisioned=excluded.provisioned
	`
	sqliteDeleteUserAccess = `
		DELETE FROM user_access
		WHERE user_id = (SELECT id FROM user WHERE user = ?)
		   OR owner_user_id = (SELECT id FROM user WHERE user = ?)
	`
	sqliteDeleteUserAccessProvisioned = `DELETE FROM user_access WHERE provisioned = 1`
	sqliteDeleteTopicAccess           = `
		DELETE FROM user_access
	   	WHERE (user_id = (SELECT id FROM user WHERE user = ?) OR owner_user_id = (SELECT id FROM user WHERE user = ?))
	   	  AND topic = ?
  	`
	sqliteDeleteAllAccess = `DELETE FROM user_access`

	// Token queries
	sqliteSelectToken                = `SELECT token, label, last_access, last_origin, expires, provisioned FROM user_token WHERE user_id = ? AND token = ?`
	sqliteSelectTokens               = `SELECT token, label, last_access, last_origin, expires, provisioned FROM user_token WHERE user_id = ?`
	sqliteSelectTokenCount           = `SELECT COUNT(*) FROM user_token WHERE user_id = ?`
	sqliteSelectAllProvisionedTokens = `SELECT token, label, last_access, last_origin, expires, provisioned FROM user_token WHERE provisioned = 1`
	sqliteUpsertToken                = `
		INSERT INTO user_token (user_id, token, label, last_access, last_origin, expires, provisioned)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, token)
		DO UPDATE SET label = excluded.label, expires = excluded.expires, provisioned = excluded.provisioned;
	`
	sqliteUpdateTokenLabel       = `UPDATE user_token SET label = ? WHERE user_id = ? AND token = ?`
	sqliteUpdateTokenExpiry      = `UPDATE user_token SET expires = ? WHERE user_id = ? AND token = ?`
	sqliteUpdateTokenLastAccess  = `UPDATE user_token SET last_access = ?, last_origin = ? WHERE token = ?`
	sqliteDeleteToken            = `DELETE FROM user_token WHERE user_id = ? AND token = ?`
	sqliteDeleteProvisionedToken = `DELETE FROM user_token WHERE token = ?`
	sqliteDeleteAllToken         = `DELETE FROM user_token WHERE user_id = ?`
	sqliteDeleteExpiredTokens    = `DELETE FROM user_token WHERE expires > 0 AND expires < ?`
	sqliteDeleteExcessTokens     = `
		DELETE FROM user_token
		WHERE user_id = ?
		  AND (user_id, token) NOT IN (
			SELECT user_id, token
			FROM user_token
			WHERE user_id = ?
			ORDER BY expires DESC
			LIMIT ?
		)
	`

	// Tier queries
	sqliteInsertTier = `
		INSERT INTO tier (id, code, name, messages_limit, messages_expiry_duration, emails_limit, calls_limit, reservations_limit, attachment_file_size_limit, attachment_total_size_limit, attachment_expiry_duration, attachment_bandwidth_limit, stripe_monthly_price_id, stripe_yearly_price_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	sqliteUpdateTier = `
		UPDATE tier
		SET name = ?, messages_limit = ?, messages_expiry_duration = ?, emails_limit = ?, calls_limit = ?, reservations_limit = ?, attachment_file_size_limit = ?, attachment_total_size_limit = ?, attachment_expiry_duration = ?, attachment_bandwidth_limit = ?, stripe_monthly_price_id = ?, stripe_yearly_price_id = ?
		WHERE code = ?
	`
	sqliteSelectTiers = `
		SELECT id, code, name, messages_limit, messages_expiry_duration, emails_limit, calls_limit, reservations_limit, attachment_file_size_limit, attachment_total_size_limit, attachment_expiry_duration, attachment_bandwidth_limit, stripe_monthly_price_id, stripe_yearly_price_id
		FROM tier
	`
	sqliteSelectTierByCode = `
		SELECT id, code, name, messages_limit, messages_expiry_duration, emails_limit, calls_limit, reservations_limit, attachment_file_size_limit, attachment_total_size_limit, attachment_expiry_duration, attachment_bandwidth_limit, stripe_monthly_price_id, stripe_yearly_price_id
		FROM tier
		WHERE code = ?
	`
	sqliteSelectTierByPriceID = `
		SELECT id, code, name, messages_limit, messages_expiry_duration, emails_limit, calls_limit, reservations_limit, attachment_file_size_limit, attachment_total_size_limit, attachment_expiry_duration, attachment_bandwidth_limit, stripe_monthly_price_id, stripe_yearly_price_id
		FROM tier
		WHERE (stripe_monthly_price_id = ? OR stripe_yearly_price_id = ?)
	`
	sqliteDeleteTier = `DELETE FROM tier WHERE code = ?`

	// Phone queries
	sqliteSelectPhoneNumbers = `SELECT phone_number FROM user_phone WHERE user_id = ?`
	sqliteInsertPhoneNumber  = `INSERT INTO user_phone (user_id, phone_number) VALUES (?, ?)`
	sqliteDeletePhoneNumber  = `DELETE FROM user_phone WHERE user_id = ? AND phone_number = ?`

	// Billing queries
	sqliteUpdateBilling = `
		UPDATE user
		SET stripe_customer_id = ?, stripe_subscription_id = ?, stripe_subscription_status = ?, stripe_subscription_interval = ?, stripe_subscription_paid_until = ?, stripe_subscription_cancel_at = ?
		WHERE user = ?
	`
)

// NewSQLiteStore creates a new SQLite-backed user store
func NewSQLiteStore(filename, startupQueries string) (Store, error) {
	db, err := sql.Open("sqlite3", filename)
	if err != nil {
		return nil, err
	}
	if err := setupSQLiteDB(db); err != nil {
		return nil, err
	}
	if err := runSQLiteStartupQueries(db, startupQueries); err != nil {
		return nil, err
	}
	return &commonStore{
		db:      db,
		queries: sqliteQueries(),
	}, nil
}

func sqliteQueries() storeQueries {
	return storeQueries{
		selectUserByID:              sqliteSelectUserByID,
		selectUserByName:            sqliteSelectUserByName,
		selectUserByToken:           sqliteSelectUserByToken,
		selectUserByStripeID:        sqliteSelectUserByStripeID,
		selectUsernames:             sqliteSelectUsernames,
		selectUserCount:             sqliteSelectUserCount,
		selectUserIDFromUsername:    sqliteSelectUserIDFromUsername,
		insertUser:                  sqliteInsertUser,
		updateUserPass:              sqliteUpdateUserPass,
		updateUserRole:              sqliteUpdateUserRole,
		updateUserProvisioned:       sqliteUpdateUserProvisioned,
		updateUserPrefs:             sqliteUpdateUserPrefs,
		updateUserStats:             sqliteUpdateUserStats,
		updateUserStatsResetAll:     sqliteUpdateUserStatsResetAll,
		updateUserTier:              sqliteUpdateUserTier,
		updateUserDeleted:           sqliteUpdateUserDeleted,
		deleteUser:                  sqliteDeleteUser,
		deleteUserTier:              sqliteDeleteUserTier,
		deleteUsersMarked:           sqliteDeleteUsersMarked,
		selectTopicPerms:            sqliteSelectTopicPerms,
		selectUserAllAccess:         sqliteSelectUserAllAccess,
		selectUserAccess:            sqliteSelectUserAccess,
		selectUserReservations:      sqliteSelectUserReservations,
		selectUserReservationsCount: sqliteSelectUserReservationsCount,
		selectUserReservationsOwner: sqliteSelectUserReservationsOwner,
		selectUserHasReservation:    sqliteSelectUserHasReservation,
		selectOtherAccessCount:      sqliteSelectOtherAccessCount,
		upsertUserAccess:            sqliteUpsertUserAccess,
		deleteUserAccess:            sqliteDeleteUserAccess,
		deleteUserAccessProvisioned: sqliteDeleteUserAccessProvisioned,
		deleteTopicAccess:           sqliteDeleteTopicAccess,
		deleteAllAccess:             sqliteDeleteAllAccess,
		selectToken:                 sqliteSelectToken,
		selectTokens:                sqliteSelectTokens,
		selectTokenCount:            sqliteSelectTokenCount,
		selectAllProvisionedTokens:  sqliteSelectAllProvisionedTokens,
		upsertToken:                 sqliteUpsertToken,
		updateTokenLabel:            sqliteUpdateTokenLabel,
		updateTokenExpiry:           sqliteUpdateTokenExpiry,
		updateTokenLastAccess:       sqliteUpdateTokenLastAccess,
		deleteToken:                 sqliteDeleteToken,
		deleteProvisionedToken:      sqliteDeleteProvisionedToken,
		deleteAllToken:              sqliteDeleteAllToken,
		deleteExpiredTokens:         sqliteDeleteExpiredTokens,
		deleteExcessTokens:          sqliteDeleteExcessTokens,
		insertTier:                  sqliteInsertTier,
		selectTiers:                 sqliteSelectTiers,
		selectTierByCode:            sqliteSelectTierByCode,
		selectTierByPriceID:         sqliteSelectTierByPriceID,
		updateTier:                  sqliteUpdateTier,
		deleteTier:                  sqliteDeleteTier,
		selectPhoneNumbers:          sqliteSelectPhoneNumbers,
		insertPhoneNumber:           sqliteInsertPhoneNumber,
		deletePhoneNumber:           sqliteDeletePhoneNumber,
		updateBilling:               sqliteUpdateBilling,
	}
}

func setupSQLiteDB(db *sql.DB) error {
	rowsSV, err := db.Query(selectSchemaVersionQuery)
	if err != nil {
		return setupNewSQLiteDB(db)
	}
	defer rowsSV.Close()
	schemaVersion := 0
	if !rowsSV.Next() {
		return fmt.Errorf("cannot determine schema version: database file may be corrupt")
	}
	if err := rowsSV.Scan(&schemaVersion); err != nil {
		return err
	}
	rowsSV.Close()
	if schemaVersion == currentSchemaVersion {
		return nil
	} else if schemaVersion > currentSchemaVersion {
		return fmt.Errorf("unexpected schema version: version %d is higher than current version %d", schemaVersion, currentSchemaVersion)
	}
	for i := schemaVersion; i < currentSchemaVersion; i++ {
		fn, ok := migrations[i]
		if !ok {
			return fmt.Errorf("cannot find migration step from schema version %d to %d", i, i+1)
		} else if err := fn(db); err != nil {
			return err
		}
	}
	return nil
}

func setupNewSQLiteDB(db *sql.DB) error {
	if _, err := db.Exec(sqliteCreateTablesQueries); err != nil {
		return err
	}
	if _, err := db.Exec(insertSchemaVersion, currentSchemaVersion); err != nil {
		return err
	}
	return nil
}

func runSQLiteStartupQueries(db *sql.DB, startupQueries string) error {
	if _, err := db.Exec(sqliteBuiltinStartupQueries); err != nil {
		return err
	}
	if startupQueries != "" {
		if _, err := db.Exec(startupQueries); err != nil {
			return err
		}
	}
	return nil
}
