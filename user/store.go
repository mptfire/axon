package user

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"time"

	"heckel.io/ntfy/v2/payments"
	"heckel.io/ntfy/v2/util"
)

// Store is the interface for a user database store
type Store interface {
	// User operations
	UserByID(id string) (*User, error)
	User(username string) (*User, error)
	UserByToken(token string) (*User, error)
	UserByStripeCustomer(customerID string) (*User, error)
	UserIDByUsername(username string) (string, error)
	Users() ([]*User, error)
	UsersCount() (int64, error)
	AddUser(username, hash string, role Role, provisioned bool) error
	RemoveUser(username string) error
	MarkUserRemoved(userID, username string) error
	RemoveDeletedUsers() error
	ChangePassword(username, hash string) error
	ChangeRole(username string, role Role) error
	ChangeProvisioned(username string, provisioned bool) error
	ChangeSettings(userID string, prefs *Prefs) error
	ChangeTier(username, tierCode string) error
	ResetTier(username string) error
	UpdateStats(stats map[string]*Stats) error
	ResetStats() error

	// Token operations
	CreateToken(userID, token, label string, lastAccess time.Time, lastOrigin netip.Addr, expires time.Time, maxTokenCount int, provisioned bool) (*Token, error)
	Token(userID, token string) (*Token, error)
	Tokens(userID string) ([]*Token, error)
	AllProvisionedTokens() ([]*Token, error)
	ChangeToken(userID, token, label string, expires time.Time) error
	UpdateTokenLastAccess(updates map[string]*TokenUpdate) error
	RemoveToken(userID, token string) error
	RemoveProvisionedToken(token string) error
	RemoveExpiredTokens() error

	// Access operations
	AuthorizeTopicAccess(usernameOrEveryone, topic string) (read, write, found bool, err error)
	AllGrants() (map[string][]Grant, error)
	Grants(username string) ([]Grant, error)
	AllowAccess(username, topicPattern string, read, write bool, ownerUsername string, provisioned bool) error
	ResetAccess(username, topicPattern string) error
	ResetAllProvisionedAccess() error
	AddReservation(username, topic string, everyone Permission) error
	RemoveReservations(username string, topics ...string) error
	Reservations(username string) ([]Reservation, error)
	HasReservation(username, topic string) (bool, error)
	ReservationsCount(username string) (int64, error)
	ReservationOwner(topic string) (string, error)
	OtherAccessCount(username, topic string) (int, error)

	// Tier operations
	AddTier(tier *Tier) error
	UpdateTier(tier *Tier) error
	RemoveTier(code string) error
	Tiers() ([]*Tier, error)
	Tier(code string) (*Tier, error)
	TierByStripePrice(priceID string) (*Tier, error)

	// Phone operations
	PhoneNumbers(userID string) ([]string, error)
	AddPhoneNumber(userID, phoneNumber string) error
	RemovePhoneNumber(userID, phoneNumber string) error

	// Other stuff
	ChangeBilling(username string, billing *Billing) error
	Close() error
}

// storeQueries holds the database-specific SQL queries
type storeQueries struct {
	// User queries
	selectUserByID               string
	selectUserByName             string
	selectUserByToken            string
	selectUserByStripeCustomerID string
	selectUsernames              string
	selectUserCount              string
	selectUserIDFromUsername     string
	insertUser                   string
	updateUserPass               string
	updateUserRole               string
	updateUserProvisioned        string
	updateUserPrefs              string
	updateUserStats              string
	updateUserStatsResetAll      string
	updateUserTier               string
	updateUserDeleted            string
	deleteUser                   string
	deleteUserTier               string
	deleteUsersMarked            string
	// Access queries
	selectTopicPerms            string
	selectUserAllAccess         string
	selectUserAccess            string
	selectUserReservations      string
	selectUserReservationsCount string
	selectUserReservationsOwner string
	selectUserHasReservation    string
	selectOtherAccessCount      string
	upsertUserAccess            string
	deleteUserAccess            string
	deleteUserAccessProvisioned string
	deleteTopicAccess           string
	deleteAllAccess             string
	// Token queries
	selectToken                string
	selectTokens               string
	selectTokenCount           string
	selectAllProvisionedTokens string
	upsertToken                string
	updateToken                string
	updateTokenLastAccess      string
	deleteToken                string
	deleteProvisionedToken     string
	deleteAllToken             string
	deleteExpiredTokens        string
	deleteExcessTokens         string
	// Tier queries
	insertTier          string
	selectTiers         string
	selectTierByCode    string
	selectTierByPriceID string
	updateTier          string
	deleteTier          string
	// Phone queries
	selectPhoneNumbers string
	insertPhoneNumber  string
	deletePhoneNumber  string
	// Billing queries
	updateBilling string
}

// execer is satisfied by both *sql.DB and *sql.Tx, allowing helper methods
// to be used both standalone and within a transaction.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// commonStore implements store operations that work across database backends
type commonStore struct {
	db      *sql.DB
	queries storeQueries
}

// User returns the user with the given username if it exists, or ErrUserNotFound otherwise
func (s *commonStore) User(username string) (*User, error) {
	rows, err := s.db.Query(s.queries.selectUserByName, username)
	if err != nil {
		return nil, err
	}
	return s.readUser(rows)
}

// UserByID returns the user with the given ID if it exists, or ErrUserNotFound otherwise
func (s *commonStore) UserByID(id string) (*User, error) {
	rows, err := s.db.Query(s.queries.selectUserByID, id)
	if err != nil {
		return nil, err
	}
	return s.readUser(rows)
}

// UserByToken returns the user with the given token if it exists and is not expired, or ErrUserNotFound otherwise
func (s *commonStore) UserByToken(token string) (*User, error) {
	rows, err := s.db.Query(s.queries.selectUserByToken, token, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	return s.readUser(rows)
}

// UserByStripeCustomer returns the user with the given Stripe customer ID if it exists, or ErrUserNotFound otherwise
func (s *commonStore) UserByStripeCustomer(customerID string) (*User, error) {
	rows, err := s.db.Query(s.queries.selectUserByStripeCustomerID, customerID)
	if err != nil {
		return nil, err
	}
	return s.readUser(rows)
}

// Users returns a list of users
func (s *commonStore) Users() ([]*User, error) {
	rows, err := s.db.Query(s.queries.selectUsernames)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	usernames := make([]string, 0)
	for rows.Next() {
		var username string
		if err := rows.Scan(&username); err != nil {
			return nil, err
		} else if err := rows.Err(); err != nil {
			return nil, err
		}
		usernames = append(usernames, username)
	}
	rows.Close()
	users := make([]*User, 0)
	for _, username := range usernames {
		user, err := s.User(username)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

// UsersCount returns the number of users in the database
func (s *commonStore) UsersCount() (int64, error) {
	rows, err := s.db.Query(s.queries.selectUserCount)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, errNoRows
	}
	var count int64
	if err := rows.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// AddUser adds a user with the given username, password hash and role
func (s *commonStore) AddUser(username, hash string, role Role, provisioned bool) error {
	if !AllowedUsername(username) || !AllowedRole(role) {
		return ErrInvalidArgument
	}
	userID := util.RandomStringPrefix(userIDPrefix, userIDLength)
	syncTopic := util.RandomStringPrefix(syncTopicPrefix, syncTopicLength)
	now := time.Now().Unix()
	if _, err := s.db.Exec(s.queries.insertUser, userID, username, hash, string(role), syncTopic, provisioned, now); err != nil {
		if isUniqueConstraintError(err) {
			return ErrUserExists
		}
		return err
	}
	return nil
}

// RemoveUser deletes the user with the given username
func (s *commonStore) RemoveUser(username string) error {
	if !AllowedUsername(username) {
		return ErrInvalidArgument
	}
	// Rows in user_access, user_token, etc. are deleted via foreign keys
	if _, err := s.db.Exec(s.queries.deleteUser, username); err != nil {
		return err
	}
	return nil
}

// MarkUserRemoved sets the deleted flag on the user, and deletes all access tokens
func (s *commonStore) MarkUserRemoved(userID, username string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.resetUserAccessTx(tx, username); err != nil {
		return err
	}
	if _, err := tx.Exec(s.queries.deleteAllToken, userID); err != nil {
		return err
	}
	deletedTime := time.Now().Add(userHardDeleteAfterDuration).Unix()
	if _, err := tx.Exec(s.queries.updateUserDeleted, deletedTime, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveDeletedUsers deletes all users that have been marked deleted
func (s *commonStore) RemoveDeletedUsers() error {
	if _, err := s.db.Exec(s.queries.deleteUsersMarked, time.Now().Unix()); err != nil {
		return err
	}
	return nil
}

// ChangePassword changes a user's password
func (s *commonStore) ChangePassword(username, hash string) error {
	if _, err := s.db.Exec(s.queries.updateUserPass, hash, username); err != nil {
		return err
	}
	return nil
}

// ChangeRole changes a user's role
func (s *commonStore) ChangeRole(username string, role Role) error {
	if !AllowedUsername(username) || !AllowedRole(role) {
		return ErrInvalidArgument
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(s.queries.updateUserRole, string(role), username); err != nil {
		return err
	}
	// If changing to admin, remove all access entries
	if role == RoleAdmin {
		if err := s.resetUserAccessTx(tx, username); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ChangeProvisioned changes the provisioned status of a user
func (s *commonStore) ChangeProvisioned(username string, provisioned bool) error {
	if _, err := s.db.Exec(s.queries.updateUserProvisioned, provisioned, username); err != nil {
		return err
	}
	return nil
}

// ChangeSettings persists the user settings
func (s *commonStore) ChangeSettings(userID string, prefs *Prefs) error {
	b, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(s.queries.updateUserPrefs, string(b), userID); err != nil {
		return err
	}
	return nil
}

// ChangeTier changes a user's tier using the tier code
func (s *commonStore) ChangeTier(username, tierCode string) error {
	if _, err := s.db.Exec(s.queries.updateUserTier, tierCode, username); err != nil {
		return err
	}
	return nil
}

// ResetTier removes the tier from the given user
func (s *commonStore) ResetTier(username string) error {
	if !AllowedUsername(username) && username != Everyone && username != "" {
		return ErrInvalidArgument
	}
	_, err := s.db.Exec(s.queries.deleteUserTier, username)
	return err
}

// UpdateStats updates statistics for one or more users in a single transaction
func (s *commonStore) UpdateStats(stats map[string]*Stats) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for userID, update := range stats {
		if _, err := tx.Exec(s.queries.updateUserStats, update.Messages, update.Emails, update.Calls, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ResetStats resets all user stats in the user database
func (s *commonStore) ResetStats() error {
	if _, err := s.db.Exec(s.queries.updateUserStatsResetAll); err != nil {
		return err
	}
	return nil
}

func (s *commonStore) readUser(rows *sql.Rows) (*User, error) {
	defer rows.Close()
	var id, username, hash, role, prefs, syncTopic string
	var provisioned bool
	var stripeCustomerID, stripeSubscriptionID, stripeSubscriptionStatus, stripeSubscriptionInterval, stripeMonthlyPriceID, stripeYearlyPriceID, tierID, tierCode, tierName sql.NullString
	var messages, emails, calls int64
	var messagesLimit, messagesExpiryDuration, emailsLimit, callsLimit, reservationsLimit, attachmentFileSizeLimit, attachmentTotalSizeLimit, attachmentExpiryDuration, attachmentBandwidthLimit, stripeSubscriptionPaidUntil, stripeSubscriptionCancelAt, deleted sql.NullInt64
	if !rows.Next() {
		return nil, ErrUserNotFound
	}
	if err := rows.Scan(&id, &username, &hash, &role, &prefs, &syncTopic, &provisioned, &messages, &emails, &calls, &stripeCustomerID, &stripeSubscriptionID, &stripeSubscriptionStatus, &stripeSubscriptionInterval, &stripeSubscriptionPaidUntil, &stripeSubscriptionCancelAt, &deleted, &tierID, &tierCode, &tierName, &messagesLimit, &messagesExpiryDuration, &emailsLimit, &callsLimit, &reservationsLimit, &attachmentFileSizeLimit, &attachmentTotalSizeLimit, &attachmentExpiryDuration, &attachmentBandwidthLimit, &stripeMonthlyPriceID, &stripeYearlyPriceID); err != nil {
		return nil, err
	} else if err := rows.Err(); err != nil {
		return nil, err
	}
	user := &User{
		ID:          id,
		Name:        username,
		Hash:        hash,
		Role:        Role(role),
		Prefs:       &Prefs{},
		SyncTopic:   syncTopic,
		Provisioned: provisioned,
		Stats: &Stats{
			Messages: messages,
			Emails:   emails,
			Calls:    calls,
		},
		Billing: &Billing{
			StripeCustomerID:            stripeCustomerID.String,                                            // May be empty
			StripeSubscriptionID:        stripeSubscriptionID.String,                                        // May be empty
			StripeSubscriptionStatus:    payments.SubscriptionStatus(stripeSubscriptionStatus.String),       // May be empty
			StripeSubscriptionInterval:  payments.PriceRecurringInterval(stripeSubscriptionInterval.String), // May be empty
			StripeSubscriptionPaidUntil: time.Unix(stripeSubscriptionPaidUntil.Int64, 0),                    // May be zero
			StripeSubscriptionCancelAt:  time.Unix(stripeSubscriptionCancelAt.Int64, 0),                     // May be zero
		},
		Deleted: deleted.Valid,
	}
	if err := json.Unmarshal([]byte(prefs), user.Prefs); err != nil {
		return nil, err
	}
	if tierCode.Valid {
		// See readTier() when this is changed!
		user.Tier = &Tier{
			ID:                       tierID.String,
			Code:                     tierCode.String,
			Name:                     tierName.String,
			MessageLimit:             messagesLimit.Int64,
			MessageExpiryDuration:    time.Duration(messagesExpiryDuration.Int64) * time.Second,
			EmailLimit:               emailsLimit.Int64,
			CallLimit:                callsLimit.Int64,
			ReservationLimit:         reservationsLimit.Int64,
			AttachmentFileSizeLimit:  attachmentFileSizeLimit.Int64,
			AttachmentTotalSizeLimit: attachmentTotalSizeLimit.Int64,
			AttachmentExpiryDuration: time.Duration(attachmentExpiryDuration.Int64) * time.Second,
			AttachmentBandwidthLimit: attachmentBandwidthLimit.Int64,
			StripeMonthlyPriceID:     stripeMonthlyPriceID.String, // May be empty
			StripeYearlyPriceID:      stripeYearlyPriceID.String,  // May be empty
		}
	}
	return user, nil
}

// CreateToken creates a new token and prunes excess tokens if the count exceeds maxTokenCount.
// If maxTokenCount is 0, no pruning is performed.
func (s *commonStore) CreateToken(userID, token, label string, lastAccess time.Time, lastOrigin netip.Addr, expires time.Time, maxTokenCount int, provisioned bool) (*Token, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(s.queries.upsertToken, userID, token, label, lastAccess.Unix(), lastOrigin.String(), expires.Unix(), provisioned); err != nil {
		return nil, err
	}
	if maxTokenCount > 0 {
		var tokenCount int
		if err := tx.QueryRow(s.queries.selectTokenCount, userID).Scan(&tokenCount); err != nil {
			return nil, err
		}
		if tokenCount > maxTokenCount {
			if _, err := tx.Exec(s.queries.deleteExcessTokens, userID, userID, maxTokenCount); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Token{
		Value:       token,
		Label:       label,
		LastAccess:  lastAccess,
		LastOrigin:  lastOrigin,
		Expires:     expires,
		Provisioned: provisioned,
	}, nil
}

// Token returns a specific token for a user
func (s *commonStore) Token(userID, token string) (*Token, error) {
	rows, err := s.db.Query(s.queries.selectToken, userID, token)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.readToken(rows)
}

// Tokens returns all existing tokens for the user with the given user ID
func (s *commonStore) Tokens(userID string) ([]*Token, error) {
	rows, err := s.db.Query(s.queries.selectTokens, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tokens := make([]*Token, 0)
	for {
		token, err := s.readToken(rows)
		if errors.Is(err, ErrTokenNotFound) {
			break
		} else if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

// AllProvisionedTokens returns all provisioned tokens
func (s *commonStore) AllProvisionedTokens() ([]*Token, error) {
	rows, err := s.db.Query(s.queries.selectAllProvisionedTokens)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tokens := make([]*Token, 0)
	for {
		token, err := s.readToken(rows)
		if errors.Is(err, ErrTokenNotFound) {
			break
		} else if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

// ChangeToken updates a token's label and expiry time
func (s *commonStore) ChangeToken(userID, token, label string, expires time.Time) error {
	if _, err := s.db.Exec(s.queries.updateToken, label, expires.Unix(), userID, token); err != nil {
		return err
	}
	return nil
}

// UpdateTokenLastAccess updates the last access time and origin for one or more tokens in a single transaction
func (s *commonStore) UpdateTokenLastAccess(updates map[string]*TokenUpdate) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for token, update := range updates {
		if _, err := tx.Exec(s.queries.updateTokenLastAccess, update.LastAccess.Unix(), update.LastOrigin.String(), token); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RemoveToken deletes the token
func (s *commonStore) RemoveToken(userID, token string) error {
	if token == "" {
		return errNoTokenProvided
	}
	if _, err := s.db.Exec(s.queries.deleteToken, userID, token); err != nil {
		return err
	}
	return nil
}

// RemoveProvisionedToken deletes a provisioned token by value, regardless of user
func (s *commonStore) RemoveProvisionedToken(token string) error {
	if token == "" {
		return errNoTokenProvided
	}
	if _, err := s.db.Exec(s.queries.deleteProvisionedToken, token); err != nil {
		return err
	}
	return nil
}

// RemoveExpiredTokens deletes all expired tokens from the database
func (s *commonStore) RemoveExpiredTokens() error {
	if _, err := s.db.Exec(s.queries.deleteExpiredTokens, time.Now().Unix()); err != nil {
		return err
	}
	return nil
}

func (s *commonStore) readToken(rows *sql.Rows) (*Token, error) {
	var token, label, lastOrigin string
	var lastAccess, expires int64
	var provisioned bool
	if !rows.Next() {
		return nil, ErrTokenNotFound
	}
	if err := rows.Scan(&token, &label, &lastAccess, &lastOrigin, &expires, &provisioned); err != nil {
		return nil, err
	} else if err := rows.Err(); err != nil {
		return nil, err
	}
	lastOriginIP, err := netip.ParseAddr(lastOrigin)
	if err != nil {
		lastOriginIP = netip.IPv4Unspecified()
	}
	return &Token{
		Value:       token,
		Label:       label,
		LastAccess:  time.Unix(lastAccess, 0),
		LastOrigin:  lastOriginIP,
		Expires:     time.Unix(expires, 0),
		Provisioned: provisioned,
	}, nil
}

// AuthorizeTopicAccess returns the read/write permissions for the given username and topic.
// The found return value indicates whether an ACL entry was found at all.
//
// - The query may return two rows (one for everyone, and one for the user), but prioritizes the user.
// - Furthermore, the query prioritizes more specific permissions (longer!) over more generic ones, e.g. "test*" > "*"
// - It also prioritizes write permissions over read permissions
func (s *commonStore) AuthorizeTopicAccess(usernameOrEveryone, topic string) (read, write, found bool, err error) {
	rows, err := s.db.Query(s.queries.selectTopicPerms, Everyone, usernameOrEveryone, topic)
	if err != nil {
		return false, false, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return false, false, false, nil
	}
	if err := rows.Scan(&read, &write); err != nil {
		return false, false, false, err
	} else if err := rows.Err(); err != nil {
		return false, false, false, err
	}
	return read, write, true, nil
}

// AllGrants returns all user-specific access control entries, mapped to their respective user IDs
func (s *commonStore) AllGrants() (map[string][]Grant, error) {
	rows, err := s.db.Query(s.queries.selectUserAllAccess)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grants := make(map[string][]Grant, 0)
	for rows.Next() {
		var userID, topic string
		var read, write, provisioned bool
		if err := rows.Scan(&userID, &topic, &read, &write, &provisioned); err != nil {
			return nil, err
		} else if err := rows.Err(); err != nil {
			return nil, err
		}
		if _, ok := grants[userID]; !ok {
			grants[userID] = make([]Grant, 0)
		}
		grants[userID] = append(grants[userID], Grant{
			TopicPattern: fromSQLWildcard(topic),
			Permission:   NewPermission(read, write),
			Provisioned:  provisioned,
		})
	}
	return grants, nil
}

// Grants returns all user-specific access control entries
func (s *commonStore) Grants(username string) ([]Grant, error) {
	rows, err := s.db.Query(s.queries.selectUserAccess, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grants := make([]Grant, 0)
	for rows.Next() {
		var topic string
		var read, write, provisioned bool
		if err := rows.Scan(&topic, &read, &write, &provisioned); err != nil {
			return nil, err
		} else if err := rows.Err(); err != nil {
			return nil, err
		}
		grants = append(grants, Grant{
			TopicPattern: fromSQLWildcard(topic),
			Permission:   NewPermission(read, write),
			Provisioned:  provisioned,
		})
	}
	return grants, nil
}

// AllowAccess adds or updates an entry in the access control list
func (s *commonStore) AllowAccess(username, topicPattern string, read, write bool, ownerUsername string, provisioned bool) error {
	return s.allowAccessTx(s.db, username, topicPattern, read, write, ownerUsername, provisioned)
}

func (s *commonStore) allowAccessTx(tx execer, username, topicPattern string, read, write bool, ownerUsername string, provisioned bool) error {
	if !AllowedUsername(username) && username != Everyone {
		return ErrInvalidArgument
	} else if !AllowedTopicPattern(topicPattern) {
		return ErrInvalidArgument
	}
	_, err := tx.Exec(s.queries.upsertUserAccess, username, toSQLWildcard(topicPattern), read, write, ownerUsername, ownerUsername, provisioned)
	return err
}

// ResetAccess removes an access control list entry
func (s *commonStore) ResetAccess(username, topicPattern string) error {
	if username == "" && topicPattern == "" {
		_, err := s.db.Exec(s.queries.deleteAllAccess)
		return err
	} else if topicPattern == "" {
		return s.resetUserAccessTx(s.db, username)
	}
	return s.resetTopicAccessTx(s.db, username, topicPattern)
}

func (s *commonStore) resetUserAccessTx(tx execer, username string) error {
	if !AllowedUsername(username) && username != Everyone {
		return ErrInvalidArgument
	}
	_, err := tx.Exec(s.queries.deleteUserAccess, username, username)
	return err
}

func (s *commonStore) resetTopicAccessTx(tx execer, username, topicPattern string) error {
	if !AllowedUsername(username) && username != Everyone && username != "" {
		return ErrInvalidArgument
	} else if !AllowedTopicPattern(topicPattern) && topicPattern != "" {
		return ErrInvalidArgument
	}
	_, err := tx.Exec(s.queries.deleteTopicAccess, username, username, toSQLWildcard(topicPattern))
	return err
}

// ResetAllProvisionedAccess removes all provisioned access control entries
func (s *commonStore) ResetAllProvisionedAccess() error {
	if _, err := s.db.Exec(s.queries.deleteUserAccessProvisioned); err != nil {
		return err
	}
	return nil
}

// AddReservation creates two access control entries for the given topic: one with full read/write
// access for the given user, and one for Everyone with the given permission. Both entries are
// created atomically in a single transaction.
func (s *commonStore) AddReservation(username, topic string, everyone Permission) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.allowAccessTx(tx, username, topic, true, true, username, false); err != nil {
		return err
	}
	if err := s.allowAccessTx(tx, Everyone, topic, everyone.IsRead(), everyone.IsWrite(), username, false); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveReservations deletes the access control entries associated with the given username/topic,
// as well as all entries with Everyone/topic. All deletions are performed atomically in a single
// transaction.
func (s *commonStore) RemoveReservations(username string, topics ...string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, topic := range topics {
		if err := s.resetTopicAccessTx(tx, username, topic); err != nil {
			return err
		}
		if err := s.resetTopicAccessTx(tx, Everyone, topic); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Reservations returns all user-owned topics, and the associated everyone-access
func (s *commonStore) Reservations(username string) ([]Reservation, error) {
	rows, err := s.db.Query(s.queries.selectUserReservations, Everyone, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reservations := make([]Reservation, 0)
	for rows.Next() {
		var topic string
		var ownerRead, ownerWrite bool
		var everyoneRead, everyoneWrite sql.NullBool
		if err := rows.Scan(&topic, &ownerRead, &ownerWrite, &everyoneRead, &everyoneWrite); err != nil {
			return nil, err
		} else if err := rows.Err(); err != nil {
			return nil, err
		}
		reservations = append(reservations, Reservation{
			Topic:    fromSQLWildcard(topic),
			Owner:    NewPermission(ownerRead, ownerWrite),
			Everyone: NewPermission(everyoneRead.Bool, everyoneWrite.Bool),
		})
	}
	return reservations, nil
}

// HasReservation returns true if the given topic access is owned by the user
func (s *commonStore) HasReservation(username, topic string) (bool, error) {
	rows, err := s.db.Query(s.queries.selectUserHasReservation, username, escapeUnderscore(topic))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return false, errNoRows
	}
	var count int64
	if err := rows.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// ReservationsCount returns the number of reservations owned by this user
func (s *commonStore) ReservationsCount(username string) (int64, error) {
	rows, err := s.db.Query(s.queries.selectUserReservationsCount, username)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, errNoRows
	}
	var count int64
	if err := rows.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// ReservationOwner returns user ID of the user that owns this topic, or an empty string if it's not owned by anyone
func (s *commonStore) ReservationOwner(topic string) (string, error) {
	rows, err := s.db.Query(s.queries.selectUserReservationsOwner, escapeUnderscore(topic))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", nil
	}
	var ownerUserID string
	if err := rows.Scan(&ownerUserID); err != nil {
		return "", err
	}
	return ownerUserID, nil
}

// OtherAccessCount returns the number of access entries for the given topic that are not owned by the user
func (s *commonStore) OtherAccessCount(username, topic string) (int, error) {
	rows, err := s.db.Query(s.queries.selectOtherAccessCount, escapeUnderscore(topic), escapeUnderscore(topic), username)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, errNoRows
	}
	var count int
	if err := rows.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// AddTier creates a new tier in the database
func (s *commonStore) AddTier(tier *Tier) error {
	if tier.ID == "" {
		tier.ID = util.RandomStringPrefix(tierIDPrefix, tierIDLength)
	}
	if _, err := s.db.Exec(s.queries.insertTier, tier.ID, tier.Code, tier.Name, tier.MessageLimit, int64(tier.MessageExpiryDuration.Seconds()), tier.EmailLimit, tier.CallLimit, tier.ReservationLimit, tier.AttachmentFileSizeLimit, tier.AttachmentTotalSizeLimit, int64(tier.AttachmentExpiryDuration.Seconds()), tier.AttachmentBandwidthLimit, nullString(tier.StripeMonthlyPriceID), nullString(tier.StripeYearlyPriceID)); err != nil {
		return err
	}
	return nil
}

// UpdateTier updates a tier's properties in the database
func (s *commonStore) UpdateTier(tier *Tier) error {
	if _, err := s.db.Exec(s.queries.updateTier, tier.Name, tier.MessageLimit, int64(tier.MessageExpiryDuration.Seconds()), tier.EmailLimit, tier.CallLimit, tier.ReservationLimit, tier.AttachmentFileSizeLimit, tier.AttachmentTotalSizeLimit, int64(tier.AttachmentExpiryDuration.Seconds()), tier.AttachmentBandwidthLimit, nullString(tier.StripeMonthlyPriceID), nullString(tier.StripeYearlyPriceID), tier.Code); err != nil {
		return err
	}
	return nil
}

// RemoveTier deletes the tier with the given code
func (s *commonStore) RemoveTier(code string) error {
	if !AllowedTier(code) {
		return ErrInvalidArgument
	}
	// This fails if any user has this tier
	if _, err := s.db.Exec(s.queries.deleteTier, code); err != nil {
		return err
	}
	return nil
}

// Tiers returns a list of all Tier structs
func (s *commonStore) Tiers() ([]*Tier, error) {
	rows, err := s.db.Query(s.queries.selectTiers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tiers := make([]*Tier, 0)
	for {
		tier, err := s.readTier(rows)
		if errors.Is(err, ErrTierNotFound) {
			break
		} else if err != nil {
			return nil, err
		}
		tiers = append(tiers, tier)
	}
	return tiers, nil
}

// Tier returns a Tier based on the code, or ErrTierNotFound if it does not exist
func (s *commonStore) Tier(code string) (*Tier, error) {
	rows, err := s.db.Query(s.queries.selectTierByCode, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.readTier(rows)
}

// TierByStripePrice returns a Tier based on the Stripe price ID, or ErrTierNotFound if it does not exist
func (s *commonStore) TierByStripePrice(priceID string) (*Tier, error) {
	rows, err := s.db.Query(s.queries.selectTierByPriceID, priceID, priceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.readTier(rows)
}

func (s *commonStore) readTier(rows *sql.Rows) (*Tier, error) {
	var id, code, name string
	var stripeMonthlyPriceID, stripeYearlyPriceID sql.NullString
	var messagesLimit, messagesExpiryDuration, emailsLimit, callsLimit, reservationsLimit, attachmentFileSizeLimit, attachmentTotalSizeLimit, attachmentExpiryDuration, attachmentBandwidthLimit sql.NullInt64
	if !rows.Next() {
		return nil, ErrTierNotFound
	}
	if err := rows.Scan(&id, &code, &name, &messagesLimit, &messagesExpiryDuration, &emailsLimit, &callsLimit, &reservationsLimit, &attachmentFileSizeLimit, &attachmentTotalSizeLimit, &attachmentExpiryDuration, &attachmentBandwidthLimit, &stripeMonthlyPriceID, &stripeYearlyPriceID); err != nil {
		return nil, err
	} else if err := rows.Err(); err != nil {
		return nil, err
	}
	// When changed, note readUser() as well
	return &Tier{
		ID:                       id,
		Code:                     code,
		Name:                     name,
		MessageLimit:             messagesLimit.Int64,
		MessageExpiryDuration:    time.Duration(messagesExpiryDuration.Int64) * time.Second,
		EmailLimit:               emailsLimit.Int64,
		CallLimit:                callsLimit.Int64,
		ReservationLimit:         reservationsLimit.Int64,
		AttachmentFileSizeLimit:  attachmentFileSizeLimit.Int64,
		AttachmentTotalSizeLimit: attachmentTotalSizeLimit.Int64,
		AttachmentExpiryDuration: time.Duration(attachmentExpiryDuration.Int64) * time.Second,
		AttachmentBandwidthLimit: attachmentBandwidthLimit.Int64,
		StripeMonthlyPriceID:     stripeMonthlyPriceID.String, // May be empty
		StripeYearlyPriceID:      stripeYearlyPriceID.String,  // May be empty
	}, nil
}

// PhoneNumbers returns all phone numbers for the user with the given user ID
func (s *commonStore) PhoneNumbers(userID string) ([]string, error) {
	rows, err := s.db.Query(s.queries.selectPhoneNumbers, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	phoneNumbers := make([]string, 0)
	for {
		phoneNumber, err := s.readPhoneNumber(rows)
		if errors.Is(err, ErrPhoneNumberNotFound) {
			break
		} else if err != nil {
			return nil, err
		}
		phoneNumbers = append(phoneNumbers, phoneNumber)
	}
	return phoneNumbers, nil
}

// AddPhoneNumber adds a phone number to the user with the given user ID
func (s *commonStore) AddPhoneNumber(userID, phoneNumber string) error {
	if _, err := s.db.Exec(s.queries.insertPhoneNumber, userID, phoneNumber); err != nil {
		if isUniqueConstraintError(err) {
			return ErrPhoneNumberExists
		}
		return err
	}
	return nil
}

// RemovePhoneNumber deletes a phone number from the user with the given user ID
func (s *commonStore) RemovePhoneNumber(userID, phoneNumber string) error {
	_, err := s.db.Exec(s.queries.deletePhoneNumber, userID, phoneNumber)
	return err
}
func (s *commonStore) readPhoneNumber(rows *sql.Rows) (string, error) {
	var phoneNumber string
	if !rows.Next() {
		return "", ErrPhoneNumberNotFound
	}
	if err := rows.Scan(&phoneNumber); err != nil {
		return "", err
	} else if err := rows.Err(); err != nil {
		return "", err
	}
	return phoneNumber, nil
}

// ChangeBilling updates a user's billing fields
func (s *commonStore) ChangeBilling(username string, billing *Billing) error {
	if _, err := s.db.Exec(s.queries.updateBilling, nullString(billing.StripeCustomerID), nullString(billing.StripeSubscriptionID), nullString(string(billing.StripeSubscriptionStatus)), nullString(string(billing.StripeSubscriptionInterval)), nullInt64(billing.StripeSubscriptionPaidUntil.Unix()), nullInt64(billing.StripeSubscriptionCancelAt.Unix()), username); err != nil {
		return err
	}
	return nil
}

// UserIDByUsername returns the user ID for the given username
func (s *commonStore) UserIDByUsername(username string) (string, error) {
	rows, err := s.db.Query(s.queries.selectUserIDFromUsername, username)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", ErrUserNotFound
	}
	var userID string
	if err := rows.Scan(&userID); err != nil {
		return "", err
	}
	return userID, nil
}

// Close closes the underlying database
func (s *commonStore) Close() error {
	return s.db.Close()
}

// isUniqueConstraintError checks if the error is a unique constraint violation for both SQLite and PostgreSQL
func isUniqueConstraintError(err error) bool {
	errStr := err.Error()
	return strings.Contains(errStr, "UNIQUE constraint failed") || strings.Contains(errStr, "23505")
}
