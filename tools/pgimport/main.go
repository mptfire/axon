package main

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
	"gopkg.in/yaml.v2"
	"heckel.io/ntfy/v2/db"
)

const (
	batchSize = 1000

	expectedMessageSchemaVersion = 14
	expectedUserSchemaVersion    = 6
	expectedWebPushSchemaVersion = 1
)

var flags = []cli.Flag{
	&cli.StringFlag{Name: "config", Aliases: []string{"c"}, Usage: "path to server.yml config file"},
	altsrc.NewStringFlag(&cli.StringFlag{Name: "database-url", Aliases: []string{"database_url"}, Usage: "PostgreSQL connection string"}),
	altsrc.NewStringFlag(&cli.StringFlag{Name: "cache-file", Aliases: []string{"cache_file"}, Usage: "SQLite message cache file path"}),
	altsrc.NewStringFlag(&cli.StringFlag{Name: "auth-file", Aliases: []string{"auth_file"}, Usage: "SQLite user/auth database file path"}),
	altsrc.NewStringFlag(&cli.StringFlag{Name: "web-push-file", Aliases: []string{"web_push_file"}, Usage: "SQLite web push database file path"}),
}

func main() {
	app := &cli.App{
		Name:      "pgimport",
		Usage:     "SQLite to PostgreSQL migration tool for ntfy",
		UsageText: "pgimport [OPTIONS]",
		Flags:     flags,
		Before:    loadConfigFile("config", flags),
		Action:    execImport,
	}
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func execImport(c *cli.Context) error {
	databaseURL := c.String("database-url")
	cacheFile := c.String("cache-file")
	authFile := c.String("auth-file")
	webPushFile := c.String("web-push-file")

	if databaseURL == "" {
		return fmt.Errorf("database-url must be set (via --database-url or config file)")
	}
	if cacheFile == "" && authFile == "" && webPushFile == "" {
		return fmt.Errorf("at least one of --cache-file, --auth-file, or --web-push-file must be set")
	}

	fmt.Println("pgimport - SQLite to PostgreSQL migration tool for ntfy")
	fmt.Println()
	fmt.Println("Sources:")
	printSource("  Cache file:    ", cacheFile)
	printSource("  Auth file:     ", authFile)
	printSource("  Web push file: ", webPushFile)
	fmt.Println()
	fmt.Println("Target:")
	fmt.Printf("  Database URL:  %s\n", maskPassword(databaseURL))
	fmt.Println()
	fmt.Println("This will import data from the SQLite databases into PostgreSQL.")
	fmt.Print("Make sure ntfy is not running. Continue? (y/n): ")

	var answer string
	fmt.Scanln(&answer)
	if strings.TrimSpace(strings.ToLower(answer)) != "y" {
		fmt.Println("Aborted.")
		return nil
	}
	fmt.Println()

	pgDB, err := db.OpenPostgres(databaseURL)
	if err != nil {
		return fmt.Errorf("cannot connect to PostgreSQL: %w", err)
	}
	defer pgDB.Close()

	if authFile != "" {
		if err := verifySchemaVersion(pgDB, "user", expectedUserSchemaVersion); err != nil {
			return err
		}
		if err := importUsers(authFile, pgDB); err != nil {
			return fmt.Errorf("cannot import users: %w", err)
		}
	}
	if cacheFile != "" {
		if err := verifySchemaVersion(pgDB, "message", expectedMessageSchemaVersion); err != nil {
			return err
		}
		if err := importMessages(cacheFile, pgDB); err != nil {
			return fmt.Errorf("cannot import messages: %w", err)
		}
	}
	if webPushFile != "" {
		if err := verifySchemaVersion(pgDB, "webpush", expectedWebPushSchemaVersion); err != nil {
			return err
		}
		if err := importWebPush(webPushFile, pgDB); err != nil {
			return fmt.Errorf("cannot import web push subscriptions: %w", err)
		}
	}

	fmt.Println()
	fmt.Println("Verifying migration ...")
	failed := false
	if authFile != "" {
		if err := verifyUsers(authFile, pgDB, &failed); err != nil {
			return fmt.Errorf("cannot verify users: %w", err)
		}
	}
	if cacheFile != "" {
		if err := verifyMessages(cacheFile, pgDB, &failed); err != nil {
			return fmt.Errorf("cannot verify messages: %w", err)
		}
	}
	if webPushFile != "" {
		if err := verifyWebPush(webPushFile, pgDB, &failed); err != nil {
			return fmt.Errorf("cannot verify web push: %w", err)
		}
	}
	fmt.Println()
	if failed {
		return fmt.Errorf("verification FAILED, see above for details")
	}
	fmt.Println("Verification successful. Migration complete.")
	return nil
}

func loadConfigFile(configFlag string, flags []cli.Flag) cli.BeforeFunc {
	return func(c *cli.Context) error {
		configFile := c.String(configFlag)
		if configFile == "" {
			return nil
		}
		if _, err := os.Stat(configFile); os.IsNotExist(err) {
			return fmt.Errorf("config file %s does not exist", configFile)
		}
		inputSource, err := newYamlSourceFromFile(configFile, flags)
		if err != nil {
			return err
		}
		return altsrc.ApplyInputSourceValues(c, inputSource, flags)
	}
}

func newYamlSourceFromFile(file string, flags []cli.Flag) (altsrc.InputSourceContext, error) {
	var rawConfig map[any]any
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, &rawConfig); err != nil {
		return nil, err
	}
	for _, f := range flags {
		flagName := f.Names()[0]
		for _, flagAlias := range f.Names()[1:] {
			if _, ok := rawConfig[flagAlias]; ok {
				rawConfig[flagName] = rawConfig[flagAlias]
			}
		}
	}
	return altsrc.NewMapInputSource(file, rawConfig), nil
}

func verifySchemaVersion(pgDB *sql.DB, store string, expected int) error {
	var version int
	err := pgDB.QueryRow(`SELECT version FROM schema_version WHERE store = $1`, store).Scan(&version)
	if err != nil {
		return fmt.Errorf("cannot read %s schema version from PostgreSQL (is the schema set up?): %w", store, err)
	}
	if version != expected {
		return fmt.Errorf("%s schema version mismatch: expected %d, got %d", store, expected, version)
	}
	return nil
}

func printSource(label, path string) {
	if path == "" {
		fmt.Printf("%s(not set, skipping)\n", label)
	} else if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("%s%s (NOT FOUND, skipping)\n", label, path)
	} else {
		fmt.Printf("%s%s\n", label, path)
	}
}

func maskPassword(databaseURL string) string {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return databaseURL
	}
	if u.User != nil {
		if _, hasPass := u.User.Password(); hasPass {
			masked := u.Scheme + "://" + u.User.Username() + ":****@" + u.Host + u.Path
			if u.RawQuery != "" {
				masked += "?" + u.RawQuery
			}
			return masked
		}
	}
	return u.String()
}

func openSQLite(filename string) (*sql.DB, error) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return nil, fmt.Errorf("file %s does not exist", filename)
	}
	return sql.Open("sqlite3", filename+"?mode=ro")
}

// User import

func importUsers(sqliteFile string, pgDB *sql.DB) error {
	sqlDB, err := openSQLite(sqliteFile)
	if err != nil {
		fmt.Printf("Skipping user import: %s\n", err)
		return nil
	}
	defer sqlDB.Close()
	fmt.Printf("Importing users from %s ...\n", sqliteFile)

	count, err := importTiers(sqlDB, pgDB)
	if err != nil {
		return fmt.Errorf("importing tiers: %w", err)
	}
	fmt.Printf("  Imported %d tiers\n", count)

	count, err = importUserRows(sqlDB, pgDB)
	if err != nil {
		return fmt.Errorf("importing users: %w", err)
	}
	fmt.Printf("  Imported %d users\n", count)

	count, err = importUserAccess(sqlDB, pgDB)
	if err != nil {
		return fmt.Errorf("importing user access: %w", err)
	}
	fmt.Printf("  Imported %d access entries\n", count)

	count, err = importUserTokens(sqlDB, pgDB)
	if err != nil {
		return fmt.Errorf("importing user tokens: %w", err)
	}
	fmt.Printf("  Imported %d tokens\n", count)

	count, err = importUserPhones(sqlDB, pgDB)
	if err != nil {
		return fmt.Errorf("importing user phones: %w", err)
	}
	fmt.Printf("  Imported %d phone numbers\n", count)

	return nil
}

func importTiers(sqlDB, pgDB *sql.DB) (int, error) {
	rows, err := sqlDB.Query(`SELECT id, code, name, messages_limit, messages_expiry_duration, emails_limit, calls_limit, reservations_limit, attachment_file_size_limit, attachment_total_size_limit, attachment_expiry_duration, attachment_bandwidth_limit, stripe_monthly_price_id, stripe_yearly_price_id FROM tier`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	tx, err := pgDB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO tier (id, code, name, messages_limit, messages_expiry_duration, emails_limit, calls_limit, reservations_limit, attachment_file_size_limit, attachment_total_size_limit, attachment_expiry_duration, attachment_bandwidth_limit, stripe_monthly_price_id, stripe_yearly_price_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) ON CONFLICT (id) DO NOTHING`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	count := 0
	for rows.Next() {
		var id, code, name string
		var messagesLimit, messagesExpiryDuration, emailsLimit, callsLimit, reservationsLimit int64
		var attachmentFileSizeLimit, attachmentTotalSizeLimit, attachmentExpiryDuration, attachmentBandwidthLimit int64
		var stripeMonthlyPriceID, stripeYearlyPriceID sql.NullString
		if err := rows.Scan(&id, &code, &name, &messagesLimit, &messagesExpiryDuration, &emailsLimit, &callsLimit, &reservationsLimit, &attachmentFileSizeLimit, &attachmentTotalSizeLimit, &attachmentExpiryDuration, &attachmentBandwidthLimit, &stripeMonthlyPriceID, &stripeYearlyPriceID); err != nil {
			return 0, err
		}
		if _, err := stmt.Exec(id, code, name, messagesLimit, messagesExpiryDuration, emailsLimit, callsLimit, reservationsLimit, attachmentFileSizeLimit, attachmentTotalSizeLimit, attachmentExpiryDuration, attachmentBandwidthLimit, stripeMonthlyPriceID, stripeYearlyPriceID); err != nil {
			return 0, err
		}
		count++
	}
	return count, tx.Commit()
}

func importUserRows(sqlDB, pgDB *sql.DB) (int, error) {
	rows, err := sqlDB.Query(`SELECT id, user, pass, role, prefs, sync_topic, provisioned, stats_messages, stats_emails, stats_calls, stripe_customer_id, stripe_subscription_id, stripe_subscription_status, stripe_subscription_interval, stripe_subscription_paid_until, stripe_subscription_cancel_at, created, deleted, tier_id FROM user`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	tx, err := pgDB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO "user" (id, user_name, pass, role, prefs, sync_topic, provisioned, stats_messages, stats_emails, stats_calls, stripe_customer_id, stripe_subscription_id, stripe_subscription_status, stripe_subscription_interval, stripe_subscription_paid_until, stripe_subscription_cancel_at, created, deleted, tier_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		ON CONFLICT (id) DO NOTHING
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	count := 0
	for rows.Next() {
		var id, userName, pass, role, prefs, syncTopic string
		var provisioned int
		var statsMessages, statsEmails, statsCalls int64
		var stripeCustomerID, stripeSubscriptionID, stripeSubscriptionStatus, stripeSubscriptionInterval sql.NullString
		var stripeSubscriptionPaidUntil, stripeSubscriptionCancelAt sql.NullInt64
		var created int64
		var deleted sql.NullInt64
		var tierID sql.NullString
		if err := rows.Scan(&id, &userName, &pass, &role, &prefs, &syncTopic, &provisioned, &statsMessages, &statsEmails, &statsCalls, &stripeCustomerID, &stripeSubscriptionID, &stripeSubscriptionStatus, &stripeSubscriptionInterval, &stripeSubscriptionPaidUntil, &stripeSubscriptionCancelAt, &created, &deleted, &tierID); err != nil {
			return 0, err
		}
		provisionedBool := provisioned != 0
		if _, err := stmt.Exec(id, userName, pass, role, prefs, syncTopic, provisionedBool, statsMessages, statsEmails, statsCalls, stripeCustomerID, stripeSubscriptionID, stripeSubscriptionStatus, stripeSubscriptionInterval, stripeSubscriptionPaidUntil, stripeSubscriptionCancelAt, created, deleted, tierID); err != nil {
			return 0, err
		}
		count++
	}
	return count, tx.Commit()
}

func importUserAccess(sqlDB, pgDB *sql.DB) (int, error) {
	rows, err := sqlDB.Query(`SELECT a.user_id, a.topic, a.read, a.write, a.owner_user_id, a.provisioned FROM user_access a JOIN user u ON u.id = a.user_id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	tx, err := pgDB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO user_access (user_id, topic, read, write, owner_user_id, provisioned) VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (user_id, topic) DO NOTHING`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	count := 0
	for rows.Next() {
		var userID, topic string
		var read, write, provisioned int
		var ownerUserID sql.NullString
		if err := rows.Scan(&userID, &topic, &read, &write, &ownerUserID, &provisioned); err != nil {
			return 0, err
		}
		readBool := read != 0
		writeBool := write != 0
		provisionedBool := provisioned != 0
		if _, err := stmt.Exec(userID, topic, readBool, writeBool, ownerUserID, provisionedBool); err != nil {
			return 0, err
		}
		count++
	}
	return count, tx.Commit()
}

func importUserTokens(sqlDB, pgDB *sql.DB) (int, error) {
	rows, err := sqlDB.Query(`SELECT t.user_id, t.token, t.label, t.last_access, t.last_origin, t.expires, t.provisioned FROM user_token t JOIN user u ON u.id = t.user_id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	tx, err := pgDB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO user_token (user_id, token, label, last_access, last_origin, expires, provisioned) VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (user_id, token) DO NOTHING`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	count := 0
	for rows.Next() {
		var userID, token, label, lastOrigin string
		var lastAccess, expires int64
		var provisioned int
		if err := rows.Scan(&userID, &token, &label, &lastAccess, &lastOrigin, &expires, &provisioned); err != nil {
			return 0, err
		}
		provisionedBool := provisioned != 0
		if _, err := stmt.Exec(userID, token, label, lastAccess, lastOrigin, expires, provisionedBool); err != nil {
			return 0, err
		}
		count++
	}
	return count, tx.Commit()
}

func importUserPhones(sqlDB, pgDB *sql.DB) (int, error) {
	rows, err := sqlDB.Query(`SELECT p.user_id, p.phone_number FROM user_phone p JOIN user u ON u.id = p.user_id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	tx, err := pgDB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO user_phone (user_id, phone_number) VALUES ($1, $2) ON CONFLICT (user_id, phone_number) DO NOTHING`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	count := 0
	for rows.Next() {
		var userID, phoneNumber string
		if err := rows.Scan(&userID, &phoneNumber); err != nil {
			return 0, err
		}
		if _, err := stmt.Exec(userID, phoneNumber); err != nil {
			return 0, err
		}
		count++
	}
	return count, tx.Commit()
}

// Message import

func importMessages(sqliteFile string, pgDB *sql.DB) error {
	sqlDB, err := openSQLite(sqliteFile)
	if err != nil {
		fmt.Printf("Skipping message import: %s\n", err)
		return nil
	}
	defer sqlDB.Close()
	fmt.Printf("Importing messages from %s ...\n", sqliteFile)

	rows, err := sqlDB.Query(`SELECT mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, attachment_deleted, sender, user, content_type, encoding, published FROM messages`)
	if err != nil {
		return fmt.Errorf("querying messages: %w", err)
	}
	defer rows.Close()

	if _, err := pgDB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_message_mid_unique ON message (mid)`); err != nil {
		return fmt.Errorf("creating unique index on mid: %w", err)
	}

	insertQuery := `INSERT INTO message (mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, attachment_deleted, sender, user_id, content_type, encoding, published) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24) ON CONFLICT (mid) DO NOTHING`

	count := 0
	batchCount := 0
	tx, err := pgDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(insertQuery)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for rows.Next() {
		var mid, sequenceID, event, topic, message, title, tags, click, icon, actions string
		var attachmentName, attachmentType, attachmentURL, sender, userID, contentType, encoding string
		var msgTime, expires, attachmentExpires int64
		var priority int
		var attachmentSize int64
		var attachmentDeleted, published int
		if err := rows.Scan(&mid, &sequenceID, &msgTime, &event, &expires, &topic, &message, &title, &priority, &tags, &click, &icon, &actions, &attachmentName, &attachmentType, &attachmentSize, &attachmentExpires, &attachmentURL, &attachmentDeleted, &sender, &userID, &contentType, &encoding, &published); err != nil {
			return fmt.Errorf("scanning message: %w", err)
		}
		mid = toUTF8(mid)
		sequenceID = toUTF8(sequenceID)
		event = toUTF8(event)
		topic = toUTF8(topic)
		message = toUTF8(message)
		title = toUTF8(title)
		tags = toUTF8(tags)
		click = toUTF8(click)
		icon = toUTF8(icon)
		actions = toUTF8(actions)
		attachmentName = toUTF8(attachmentName)
		attachmentType = toUTF8(attachmentType)
		attachmentURL = toUTF8(attachmentURL)
		sender = toUTF8(sender)
		userID = toUTF8(userID)
		contentType = toUTF8(contentType)
		encoding = toUTF8(encoding)
		attachmentDeletedBool := attachmentDeleted != 0
		publishedBool := published != 0
		if _, err := stmt.Exec(mid, sequenceID, msgTime, event, expires, topic, message, title, priority, tags, click, icon, actions, attachmentName, attachmentType, attachmentSize, attachmentExpires, attachmentURL, attachmentDeletedBool, sender, userID, contentType, encoding, publishedBool); err != nil {
			return fmt.Errorf("inserting message: %w", err)
		}
		count++
		batchCount++
		if batchCount >= batchSize {
			stmt.Close()
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("committing message batch: %w", err)
			}
			fmt.Printf("  ... %d messages\n", count)
			tx, err = pgDB.Begin()
			if err != nil {
				return err
			}
			stmt, err = tx.Prepare(insertQuery)
			if err != nil {
				return err
			}
			batchCount = 0
		}
	}
	if batchCount > 0 {
		stmt.Close()
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing final message batch: %w", err)
		}
	}
	fmt.Printf("  Imported %d messages\n", count)

	var statsValue int64
	err = sqlDB.QueryRow(`SELECT value FROM stats WHERE key = 'messages'`).Scan(&statsValue)
	if err == nil {
		if _, err := pgDB.Exec(`UPDATE message_stats SET value = $1 WHERE key = 'messages'`, statsValue); err != nil {
			return fmt.Errorf("updating message stats: %w", err)
		}
		fmt.Printf("  Updated message stats (count: %d)\n", statsValue)
	}

	return nil
}

// Web push import

func importWebPush(sqliteFile string, pgDB *sql.DB) error {
	sqlDB, err := openSQLite(sqliteFile)
	if err != nil {
		fmt.Printf("Skipping web push import: %s\n", err)
		return nil
	}
	defer sqlDB.Close()
	fmt.Printf("Importing web push subscriptions from %s ...\n", sqliteFile)

	rows, err := sqlDB.Query(`SELECT id, endpoint, key_auth, key_p256dh, user_id, subscriber_ip, updated_at, warned_at FROM subscription`)
	if err != nil {
		return fmt.Errorf("querying subscriptions: %w", err)
	}
	defer rows.Close()

	tx, err := pgDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO webpush_subscription (id, endpoint, key_auth, key_p256dh, user_id, subscriber_ip, updated_at, warned_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (id) DO NOTHING`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	count := 0
	for rows.Next() {
		var id, endpoint, keyAuth, keyP256dh, userID, subscriberIP string
		var updatedAt, warnedAt int64
		if err := rows.Scan(&id, &endpoint, &keyAuth, &keyP256dh, &userID, &subscriberIP, &updatedAt, &warnedAt); err != nil {
			return fmt.Errorf("scanning subscription: %w", err)
		}
		if _, err := stmt.Exec(id, endpoint, keyAuth, keyP256dh, userID, subscriberIP, updatedAt, warnedAt); err != nil {
			return fmt.Errorf("inserting subscription: %w", err)
		}
		count++
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing subscriptions: %w", err)
	}
	fmt.Printf("  Imported %d subscriptions\n", count)

	topicRows, err := sqlDB.Query(`SELECT subscription_id, topic FROM subscription_topic`)
	if err != nil {
		return fmt.Errorf("querying subscription topics: %w", err)
	}
	defer topicRows.Close()

	tx, err = pgDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err = tx.Prepare(`INSERT INTO webpush_subscription_topic (subscription_id, topic) VALUES ($1, $2) ON CONFLICT (subscription_id, topic) DO NOTHING`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	topicCount := 0
	for topicRows.Next() {
		var subscriptionID, topic string
		if err := topicRows.Scan(&subscriptionID, &topic); err != nil {
			return fmt.Errorf("scanning subscription topic: %w", err)
		}
		if _, err := stmt.Exec(subscriptionID, topic); err != nil {
			return fmt.Errorf("inserting subscription topic: %w", err)
		}
		topicCount++
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing subscription topics: %w", err)
	}
	fmt.Printf("  Imported %d subscription topics\n", topicCount)

	return nil
}

func toUTF8(s string) string {
	return strings.ToValidUTF8(s, "\uFFFD")
}

// Verification

func verifyUsers(sqliteFile string, pgDB *sql.DB, failed *bool) error {
	sqlDB, err := openSQLite(sqliteFile)
	if err != nil {
		return nil
	}
	defer sqlDB.Close()

	verifyCount(sqlDB, pgDB, "tier", `SELECT COUNT(*) FROM tier`, `SELECT COUNT(*) FROM tier`, failed)
	verifyCount(sqlDB, pgDB, "user", `SELECT COUNT(*) FROM user`, `SELECT COUNT(*) FROM "user"`, failed)
	verifyCount(sqlDB, pgDB, "user_access", `SELECT COUNT(*) FROM user_access a JOIN user u ON u.id = a.user_id`, `SELECT COUNT(*) FROM user_access`, failed)
	verifyCount(sqlDB, pgDB, "user_token", `SELECT COUNT(*) FROM user_token t JOIN user u ON u.id = t.user_id`, `SELECT COUNT(*) FROM user_token`, failed)
	verifyCount(sqlDB, pgDB, "user_phone", `SELECT COUNT(*) FROM user_phone p JOIN user u ON u.id = p.user_id`, `SELECT COUNT(*) FROM user_phone`, failed)
	return nil
}

func verifyMessages(sqliteFile string, pgDB *sql.DB, failed *bool) error {
	sqlDB, err := openSQLite(sqliteFile)
	if err != nil {
		return nil
	}
	defer sqlDB.Close()

	verifyCount(sqlDB, pgDB, "messages", `SELECT COUNT(*) FROM messages`, `SELECT COUNT(*) FROM message`, failed)
	return nil
}

func verifyWebPush(sqliteFile string, pgDB *sql.DB, failed *bool) error {
	sqlDB, err := openSQLite(sqliteFile)
	if err != nil {
		return nil
	}
	defer sqlDB.Close()

	verifyCount(sqlDB, pgDB, "subscription", `SELECT COUNT(*) FROM subscription`, `SELECT COUNT(*) FROM webpush_subscription`, failed)
	verifyCount(sqlDB, pgDB, "subscription_topic", `SELECT COUNT(*) FROM subscription_topic`, `SELECT COUNT(*) FROM webpush_subscription_topic`, failed)
	return nil
}

func verifyCount(sqlDB, pgDB *sql.DB, table, sqliteQuery, pgQuery string, failed *bool) {
	var sqliteCount, pgCount int64
	if err := sqlDB.QueryRow(sqliteQuery).Scan(&sqliteCount); err != nil {
		fmt.Printf("  %-20s ERROR reading SQLite: %s\n", table, err)
		*failed = true
		return
	}
	if err := pgDB.QueryRow(pgQuery).Scan(&pgCount); err != nil {
		fmt.Printf("  %-20s ERROR reading PostgreSQL: %s\n", table, err)
		*failed = true
		return
	}
	if sqliteCount == pgCount {
		fmt.Printf("  %-20s OK (%d rows)\n", table, pgCount)
	} else {
		fmt.Printf("  %-20s MISMATCH: SQLite=%d, PostgreSQL=%d\n", table, sqliteCount, pgCount)
		*failed = true
	}
}
