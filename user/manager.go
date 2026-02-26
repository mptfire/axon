// Package user deals with authentication and authorization against topics
package user

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"heckel.io/ntfy/v2/log"
	"heckel.io/ntfy/v2/util"
)

const (
	tierIDPrefix                    = "ti_"
	tierIDLength                    = 8
	syncTopicPrefix                 = "st_"
	syncTopicLength                 = 16
	userIDPrefix                    = "u_"
	userIDLength                    = 12
	userAuthIntentionalSlowDownHash = "$2a$10$YFCQvqQDwIIwnJM1xkAYOeih0dg17UVGanaTStnrSzC8NCWxcLDwy" // Cost should match DefaultUserPasswordBcryptCost
	userHardDeleteAfterDuration     = 7 * 24 * time.Hour
	tokenPrefix                     = "tk_"
	tokenLength                     = 32
	tokenMaxCount                   = 60 // Only keep this many tokens in the table per user
	tag                             = "user_manager"
)

// Default constants that may be overridden by configs
const (
	DefaultUserStatsQueueWriterInterval = 33 * time.Second
	DefaultUserPasswordBcryptCost       = 10
)

var (
	errNoTokenProvided    = errors.New("no token provided")
	errTopicOwnedByOthers = errors.New("topic owned by others")
	errNoRows             = errors.New("no rows found")
)

// Manager handles user authentication, authorization, and management
type Manager struct {
	config     *Config
	store      Store                   // Database store
	statsQueue map[string]*Stats       // "Queue" to asynchronously write user stats to the database (UserID -> Stats)
	tokenQueue map[string]*TokenUpdate // "Queue" to asynchronously write token access stats to the database (Token ID -> TokenUpdate)
	mu         sync.Mutex
}

var _ Auther = (*Manager)(nil)

// NewManager creates a new Manager instance
func NewManager(store Store, config *Config) (*Manager, error) {
	// Set defaults
	if config.BcryptCost <= 0 {
		config.BcryptCost = DefaultUserPasswordBcryptCost
	}
	if config.QueueWriterInterval.Seconds() <= 0 {
		config.QueueWriterInterval = DefaultUserStatsQueueWriterInterval
	}
	manager := &Manager{
		store:      store,
		config:     config,
		statsQueue: make(map[string]*Stats),
		tokenQueue: make(map[string]*TokenUpdate),
	}
	if err := manager.maybeProvisionUsersAccessAndTokens(); err != nil {
		return nil, err
	}
	go manager.asyncQueueWriter(config.QueueWriterInterval)
	return manager, nil
}

// Authenticate checks username and password and returns a User if correct, and the user has not been
// marked as deleted. The method returns in constant-ish time, regardless of whether the user exists or
// the password is correct or incorrect.
func (a *Manager) Authenticate(username, password string) (*User, error) {
	if username == Everyone {
		return nil, ErrUnauthenticated
	}
	user, err := a.store.User(username)
	if err != nil {
		log.Tag(tag).Field("user_name", username).Err(err).Trace("Authentication of user failed (1)")
		bcrypt.CompareHashAndPassword([]byte(userAuthIntentionalSlowDownHash), []byte("intentional slow-down to avoid timing attacks"))
		return nil, ErrUnauthenticated
	} else if user.Deleted {
		log.Tag(tag).Field("user_name", username).Trace("Authentication of user failed (2): user marked deleted")
		bcrypt.CompareHashAndPassword([]byte(userAuthIntentionalSlowDownHash), []byte("intentional slow-down to avoid timing attacks"))
		return nil, ErrUnauthenticated
	} else if err := bcrypt.CompareHashAndPassword([]byte(user.Hash), []byte(password)); err != nil {
		log.Tag(tag).Field("user_name", username).Err(err).Trace("Authentication of user failed (3)")
		return nil, ErrUnauthenticated
	}
	return user, nil
}

// AuthenticateToken checks if the token exists and returns the associated User if it does.
// The method sets the User.Token value to the token that was used for authentication.
func (a *Manager) AuthenticateToken(token string) (*User, error) {
	if len(token) != tokenLength {
		return nil, ErrUnauthenticated
	}
	user, err := a.store.UserByToken(token)
	if err != nil {
		log.Tag(tag).Field("token", token).Err(err).Trace("Authentication of token failed")
		return nil, ErrUnauthenticated
	}
	user.Token = token
	return user, nil
}

// CreateToken generates a random token for the given user and returns it. The token expires
// after a fixed duration unless ChangeToken is called. This function also prunes tokens for the
// given user, if there are too many of them.
func (a *Manager) CreateToken(userID, label string, expires time.Time, origin netip.Addr, provisioned bool) (*Token, error) {
	return a.store.CreateToken(userID, GenerateToken(), label, time.Now(), origin, expires, tokenMaxCount, provisioned)
}

// Tokens returns all existing tokens for the user with the given user ID
func (a *Manager) Tokens(userID string) ([]*Token, error) {
	return a.store.Tokens(userID)
}

// Token returns a specific token for a user
func (a *Manager) Token(userID, token string) (*Token, error) {
	return a.store.Token(userID, token)
}

// ChangeToken updates a token's label and/or expiry date
func (a *Manager) ChangeToken(userID, token string, label *string, expires *time.Time) (*Token, error) {
	if token == "" {
		return nil, errNoTokenProvided
	}
	if err := a.canChangeToken(userID, token); err != nil {
		return nil, err
	}
	t, err := a.store.Token(userID, token)
	if err != nil {
		return nil, err
	}
	if label != nil {
		t.Label = *label
	}
	if expires != nil {
		t.Expires = *expires
	}
	if err := a.store.ChangeToken(userID, token, t.Label, t.Expires); err != nil {
		return nil, err
	}
	return t, nil
}

// RemoveToken deletes the token defined in User.Token
func (a *Manager) RemoveToken(userID, token string) error {
	if err := a.canChangeToken(userID, token); err != nil {
		return err
	}
	return a.store.RemoveToken(userID, token)
}

// canChangeToken checks if the token can be changed. If the token is provisioned, it cannot be changed.
func (a *Manager) canChangeToken(userID, token string) error {
	t, err := a.Token(userID, token)
	if err != nil {
		return err
	} else if t.Provisioned {
		return ErrProvisionedTokenChange
	}
	return nil
}

// RemoveExpiredTokens deletes all expired tokens from the database
func (a *Manager) RemoveExpiredTokens() error {
	return a.store.RemoveExpiredTokens()
}

// PhoneNumbers returns all phone numbers for the user with the given user ID
func (a *Manager) PhoneNumbers(userID string) ([]string, error) {
	return a.store.PhoneNumbers(userID)
}

// AddPhoneNumber adds a phone number to the user with the given user ID
func (a *Manager) AddPhoneNumber(userID string, phoneNumber string) error {
	return a.store.AddPhoneNumber(userID, phoneNumber)
}

// RemovePhoneNumber deletes a phone number from the user with the given user ID
func (a *Manager) RemovePhoneNumber(userID string, phoneNumber string) error {
	return a.store.RemovePhoneNumber(userID, phoneNumber)
}

// RemoveDeletedUsers deletes all users that have been marked deleted
func (a *Manager) RemoveDeletedUsers() error {
	return a.store.RemoveDeletedUsers()
}

// ChangeSettings persists the user settings
func (a *Manager) ChangeSettings(userID string, prefs *Prefs) error {
	return a.store.ChangeSettings(userID, prefs)
}

// ResetStats resets all user stats in the user database. This touches all users.
func (a *Manager) ResetStats() error {
	a.mu.Lock() // Includes database query to avoid races!
	defer a.mu.Unlock()
	if err := a.store.ResetStats(); err != nil {
		return err
	}
	a.statsQueue = make(map[string]*Stats)
	return nil
}

// EnqueueUserStats adds the user to a queue which writes out user stats (messages, emails, ..) in
// batches at a regular interval
func (a *Manager) EnqueueUserStats(userID string, stats *Stats) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.statsQueue[userID] = stats
}

// EnqueueTokenUpdate adds the token update to a queue which writes out token access times
// in batches at a regular interval
func (a *Manager) EnqueueTokenUpdate(tokenID string, update *TokenUpdate) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tokenQueue[tokenID] = update
}

func (a *Manager) asyncQueueWriter(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		if err := a.writeUserStatsQueue(); err != nil {
			log.Tag(tag).Err(err).Warn("Writing user stats queue failed")
		}
		if err := a.writeTokenUpdateQueue(); err != nil {
			log.Tag(tag).Err(err).Warn("Writing token update queue failed")
		}
	}
}

func (a *Manager) writeUserStatsQueue() error {
	a.mu.Lock()
	if len(a.statsQueue) == 0 {
		a.mu.Unlock()
		log.Tag(tag).Trace("No user stats updates to commit")
		return nil
	}
	statsQueue := a.statsQueue
	a.statsQueue = make(map[string]*Stats)
	a.mu.Unlock()

	log.Tag(tag).Debug("Writing user stats queue for %d user(s)", len(statsQueue))
	for userID, update := range statsQueue {
		log.
			Tag(tag).
			Fields(log.Context{
				"user_id":        userID,
				"messages_count": update.Messages,
				"emails_count":   update.Emails,
				"calls_count":    update.Calls,
			}).
			Trace("Updating stats for user %s", userID)
	}
	return a.store.UpdateStats(statsQueue)
}

func (a *Manager) writeTokenUpdateQueue() error {
	a.mu.Lock()
	if len(a.tokenQueue) == 0 {
		a.mu.Unlock()
		log.Tag(tag).Trace("No token updates to commit")
		return nil
	}
	tokenQueue := a.tokenQueue
	a.tokenQueue = make(map[string]*TokenUpdate)
	a.mu.Unlock()

	log.Tag(tag).Debug("Writing token update queue for %d token(s)", len(tokenQueue))
	for tokenID, update := range tokenQueue {
		log.Tag(tag).Trace("Updating token %s with last access time %v", tokenID, update.LastAccess.Unix())
	}
	return a.store.UpdateTokenLastAccess(tokenQueue)
}

// Authorize returns nil if the given user has access to the given topic using the desired
// permission. The user param may be nil to signal an anonymous user.
func (a *Manager) Authorize(user *User, topic string, perm Permission) error {
	if user != nil && user.Role == RoleAdmin {
		return nil // Admin can do everything
	}
	username := Everyone
	if user != nil {
		username = user.Name
	}
	// Select the read/write permissions for this user/topic combo.
	read, write, found, err := a.store.AuthorizeTopicAccess(username, topic)
	if err != nil {
		return err
	}
	if !found {
		return a.resolvePerms(a.config.DefaultAccess, perm)
	}
	return a.resolvePerms(NewPermission(read, write), perm)
}

func (a *Manager) resolvePerms(base, perm Permission) error {
	if perm == PermissionRead && base.IsRead() {
		return nil
	} else if perm == PermissionWrite && base.IsWrite() {
		return nil
	}
	return ErrUnauthorized
}

// AddUser adds a user with the given username, password and role
func (a *Manager) AddUser(username, password string, role Role, hashed bool) error {
	return a.addUser(username, password, role, hashed, false)
}

func (a *Manager) addUser(username, password string, role Role, hashed, provisioned bool) error {
	if !AllowedUsername(username) || !AllowedRole(role) {
		return ErrInvalidArgument
	}
	var hash string
	var err error
	if hashed {
		hash = password
		if err := ValidPasswordHash(hash, a.config.BcryptCost); err != nil {
			return err
		}
	} else {
		hash, err = hashPassword(password, a.config.BcryptCost)
		if err != nil {
			return err
		}
	}
	return a.store.AddUser(username, hash, role, provisioned)
}

// RemoveUser deletes the user with the given username. The function returns nil on success, even
// if the user did not exist in the first place.
func (a *Manager) RemoveUser(username string) error {
	if err := a.CanChangeUser(username); err != nil {
		return err
	}
	return a.store.RemoveUser(username)
}

// MarkUserRemoved sets the deleted flag on the user, and deletes all access tokens. This prevents
// successful auth via Authenticate. A background process will delete the user at a later date.
func (a *Manager) MarkUserRemoved(user *User) error {
	if !AllowedUsername(user.Name) {
		return ErrInvalidArgument
	}
	return a.store.MarkUserRemoved(user.ID, user.Name)
}

// Users returns a list of users. It always also returns the Everyone user ("*").
func (a *Manager) Users() ([]*User, error) {
	return a.store.Users()
}

// UsersCount returns the number of users in the database
func (a *Manager) UsersCount() (int64, error) {
	return a.store.UsersCount()
}

// User returns the user with the given username if it exists, or ErrUserNotFound otherwise.
// You may also pass Everyone to retrieve the anonymous user and its Grant list.
func (a *Manager) User(username string) (*User, error) {
	return a.store.User(username)
}

// UserByID returns the user with the given ID if it exists, or ErrUserNotFound otherwise
func (a *Manager) UserByID(id string) (*User, error) {
	return a.store.UserByID(id)
}

// UserByStripeCustomer returns the user with the given Stripe customer ID if it exists, or ErrUserNotFound otherwise.
func (a *Manager) UserByStripeCustomer(stripeCustomerID string) (*User, error) {
	return a.store.UserByStripeCustomer(stripeCustomerID)
}

// AllGrants returns all user-specific access control entries, mapped to their respective user IDs
func (a *Manager) AllGrants() (map[string][]Grant, error) {
	return a.store.AllGrants()
}

// Grants returns all user-specific access control entries
func (a *Manager) Grants(username string) ([]Grant, error) {
	return a.store.Grants(username)
}

// Reservations returns all user-owned topics, and the associated everyone-access
func (a *Manager) Reservations(username string) ([]Reservation, error) {
	return a.store.Reservations(username)
}

// HasReservation returns true if the given topic access is owned by the user
func (a *Manager) HasReservation(username, topic string) (bool, error) {
	return a.store.HasReservation(username, topic)
}

// ReservationsCount returns the number of reservations owned by this user
func (a *Manager) ReservationsCount(username string) (int64, error) {
	return a.store.ReservationsCount(username)
}

// ReservationOwner returns user ID of the user that owns this topic, or an
// empty string if it's not owned by anyone
func (a *Manager) ReservationOwner(topic string) (string, error) {
	return a.store.ReservationOwner(topic)
}

// ChangePassword changes a user's password
func (a *Manager) ChangePassword(username, password string, hashed bool) error {
	if err := a.CanChangeUser(username); err != nil {
		return err
	}
	var hash string
	var err error
	if hashed {
		hash = password
		if err := ValidPasswordHash(hash, a.config.BcryptCost); err != nil {
			return err
		}
	} else {
		hash, err = hashPassword(password, a.config.BcryptCost)
		if err != nil {
			return err
		}
	}
	return a.store.ChangePassword(username, hash)
}

// CanChangeUser checks if the user with the given username can be changed.
// This is used to prevent changes to provisioned users, which are defined in the config file.
func (a *Manager) CanChangeUser(username string) error {
	user, err := a.User(username)
	if err != nil {
		return err
	} else if user.Provisioned {
		return ErrProvisionedUserChange
	}
	return nil
}

// ChangeRole changes a user's role. When a role is changed from RoleUser to RoleAdmin,
// all existing access control entries (Grant) are removed, since they are no longer needed.
func (a *Manager) ChangeRole(username string, role Role) error {
	if err := a.CanChangeUser(username); err != nil {
		return err
	}
	return a.store.ChangeRole(username, role)
}

// ChangeTier changes a user's tier using the tier code. This function does not delete reservations, messages,
// or attachments, even if the new tier has lower limits in this regard. That has to be done elsewhere.
func (a *Manager) ChangeTier(username, tier string) error {
	if !AllowedUsername(username) {
		return ErrInvalidArgument
	}
	t, err := a.Tier(tier)
	if err != nil {
		return err
	} else if err := a.checkReservationsLimit(username, t.ReservationLimit); err != nil {
		return err
	}
	return a.store.ChangeTier(username, tier)
}

// ResetTier removes the tier from the given user
func (a *Manager) ResetTier(username string) error {
	if !AllowedUsername(username) && username != Everyone && username != "" {
		return ErrInvalidArgument
	} else if err := a.checkReservationsLimit(username, 0); err != nil {
		return err
	}
	return a.store.ResetTier(username)
}

func (a *Manager) checkReservationsLimit(username string, reservationsLimit int64) error {
	u, err := a.User(username)
	if err != nil {
		return err
	}
	if u.Tier != nil && reservationsLimit < u.Tier.ReservationLimit {
		reservations, err := a.Reservations(username)
		if err != nil {
			return err
		} else if int64(len(reservations)) > reservationsLimit {
			return ErrTooManyReservations
		}
	}
	return nil
}

// AllowReservation tests if a user may create an access control entry for the given topic.
// If there are any ACL entries that are not owned by the user, an error is returned.
func (a *Manager) AllowReservation(username string, topic string) error {
	if (!AllowedUsername(username) && username != Everyone) || !AllowedTopic(topic) {
		return ErrInvalidArgument
	}
	otherCount, err := a.store.OtherAccessCount(username, topic)
	if err != nil {
		return err
	}
	if otherCount > 0 {
		return errTopicOwnedByOthers
	}
	return nil
}

// AllowAccess adds or updates an entry in the access control list for a specific user. It controls
// read/write access to a topic. The parameter topicPattern may include wildcards (*). The ACL entry
// owner may either be a user (username), or the system (empty).
func (a *Manager) AllowAccess(username string, topicPattern string, permission Permission) error {
	return a.allowAccess(username, topicPattern, permission, false)
}

func (a *Manager) allowAccess(username string, topicPattern string, permission Permission, provisioned bool) error {
	if !AllowedUsername(username) && username != Everyone {
		return ErrInvalidArgument
	} else if !AllowedTopicPattern(topicPattern) {
		return ErrInvalidArgument
	}
	return a.store.AllowAccess(username, topicPattern, permission.IsRead(), permission.IsWrite(), "", provisioned)
}

// ResetAccess removes an access control list entry for a specific username/topic, or (if topic is
// empty) for an entire user. The parameter topicPattern may include wildcards (*).
func (a *Manager) ResetAccess(username string, topicPattern string) error {
	return a.resetAccess(username, topicPattern)
}

func (a *Manager) resetAccess(username string, topicPattern string) error {
	if !AllowedUsername(username) && username != Everyone && username != "" {
		return ErrInvalidArgument
	} else if !AllowedTopicPattern(topicPattern) && topicPattern != "" {
		return ErrInvalidArgument
	}
	return a.store.ResetAccess(username, topicPattern)
}

// AddReservation creates two access control entries for the given topic: one with full read/write access for the
// given user, and one for Everyone with the permission passed as everyone. The user also owns the entries, and
// can modify or delete them.
func (a *Manager) AddReservation(username string, topic string, everyone Permission) error {
	if !AllowedUsername(username) || username == Everyone || !AllowedTopic(topic) {
		return ErrInvalidArgument
	}
	return a.store.AddReservation(username, topic, everyone)
}

// RemoveReservations deletes the access control entries associated with the given username/topic, as
// well as all entries with Everyone/topic. This is the counterpart for AddReservation.
func (a *Manager) RemoveReservations(username string, topics ...string) error {
	if !AllowedUsername(username) || username == Everyone || len(topics) == 0 {
		return ErrInvalidArgument
	}
	for _, topic := range topics {
		if !AllowedTopic(topic) {
			return ErrInvalidArgument
		}
	}
	return a.store.RemoveReservations(username, topics...)
}

// DefaultAccess returns the default read/write access if no access control entry matches
func (a *Manager) DefaultAccess() Permission {
	return a.config.DefaultAccess
}

// AddTier creates a new tier in the database
func (a *Manager) AddTier(tier *Tier) error {
	return a.store.AddTier(tier)
}

// UpdateTier updates a tier's properties in the database
func (a *Manager) UpdateTier(tier *Tier) error {
	return a.store.UpdateTier(tier)
}

// RemoveTier deletes the tier with the given code
func (a *Manager) RemoveTier(code string) error {
	return a.store.RemoveTier(code)
}

// ChangeBilling updates a user's billing fields, namely the Stripe customer ID, and subscription information
func (a *Manager) ChangeBilling(username string, billing *Billing) error {
	return a.store.ChangeBilling(username, billing)
}

// Tiers returns a list of all Tier structs
func (a *Manager) Tiers() ([]*Tier, error) {
	return a.store.Tiers()
}

// Tier returns a Tier based on the code, or ErrTierNotFound if it does not exist
func (a *Manager) Tier(code string) (*Tier, error) {
	return a.store.Tier(code)
}

// TierByStripePrice returns a Tier based on the Stripe price ID, or ErrTierNotFound if it does not exist
func (a *Manager) TierByStripePrice(priceID string) (*Tier, error) {
	return a.store.TierByStripePrice(priceID)
}

// Close closes the underlying database
func (a *Manager) Close() error {
	return a.store.Close()
}

// maybeProvisionUsersAccessAndTokens provisions users, access control entries, and tokens based on the config.
func (a *Manager) maybeProvisionUsersAccessAndTokens() error {
	if !a.config.ProvisionEnabled {
		return nil
	}
	existingUsers, err := a.Users()
	if err != nil {
		return err
	}
	provisionUsernames := util.Map(a.config.Users, func(u *User) string {
		return u.Name
	})
	if err := a.maybeProvisionUsers(provisionUsernames, existingUsers); err != nil {
		return fmt.Errorf("failed to provision users: %v", err)
	}
	if err := a.maybeProvisionGrants(); err != nil {
		return fmt.Errorf("failed to provision grants: %v", err)
	}
	if err := a.maybeProvisionTokens(provisionUsernames); err != nil {
		return fmt.Errorf("failed to provision tokens: %v", err)
	}
	return nil
}

// maybeProvisionUsers checks if the users in the config are provisioned, and adds or updates them.
// It also removes users that are provisioned, but not in the config anymore.
func (a *Manager) maybeProvisionUsers(provisionUsernames []string, existingUsers []*User) error {
	// Remove users that are provisioned, but not in the config anymore
	for _, user := range existingUsers {
		if user.Name == Everyone {
			continue
		} else if user.Provisioned && !util.Contains(provisionUsernames, user.Name) {
			if err := a.store.RemoveUser(user.Name); err != nil {
				return fmt.Errorf("failed to remove provisioned user %s: %v", user.Name, err)
			}
		}
	}
	// Add or update provisioned users
	for _, user := range a.config.Users {
		if user.Name == Everyone {
			continue
		}
		existingUser, exists := util.Find(existingUsers, func(u *User) bool {
			return u.Name == user.Name
		})
		if !exists {
			if err := a.addUser(user.Name, user.Hash, user.Role, true, true); err != nil && !errors.Is(err, ErrUserExists) {
				return fmt.Errorf("failed to add provisioned user %s: %v", user.Name, err)
			}
		} else {
			if !existingUser.Provisioned {
				if err := a.store.ChangeProvisioned(user.Name, true); err != nil {
					return fmt.Errorf("failed to change provisioned status for user %s: %v", user.Name, err)
				}
			}
			if existingUser.Hash != user.Hash {
				if err := a.store.ChangePassword(user.Name, user.Hash); err != nil {
					return fmt.Errorf("failed to change password for provisioned user %s: %v", user.Name, err)
				}
			}
			if existingUser.Role != user.Role {
				if err := a.store.ChangeRole(user.Name, user.Role); err != nil {
					return fmt.Errorf("failed to change role for provisioned user %s: %v", user.Name, err)
				}
			}
		}
	}
	return nil
}

// maybeProvisionGrants removes all provisioned grants, and (re-)adds the grants from the config.
//
// Unlike users and tokens, grants can be just re-added, because they do not carry any state (such as last
// access time) or do not have dependent resources (such as grants or tokens).
func (a *Manager) maybeProvisionGrants() error {
	// Remove all provisioned grants
	if err := a.store.ResetAllProvisionedAccess(); err != nil {
		return err
	}
	// (Re-)add provisioned grants
	for username, grants := range a.config.Access {
		user, exists := util.Find(a.config.Users, func(u *User) bool {
			return u.Name == username
		})
		if !exists && username != Everyone {
			return fmt.Errorf("user %s is not a provisioned user, refusing to add ACL entry", username)
		} else if user != nil && user.Role == RoleAdmin {
			return fmt.Errorf("adding access control entries is not allowed for admin roles for user %s", username)
		}
		for _, grant := range grants {
			if err := a.resetAccess(username, grant.TopicPattern); err != nil {
				return fmt.Errorf("failed to reset access for user %s and topic %s: %v", username, grant.TopicPattern, err)
			}
			if err := a.allowAccess(username, grant.TopicPattern, grant.Permission, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *Manager) maybeProvisionTokens(provisionUsernames []string) error {
	// Remove tokens that are provisioned, but not in the config anymore
	existingTokens, err := a.store.AllProvisionedTokens()
	if err != nil {
		return fmt.Errorf("failed to retrieve existing provisioned tokens: %v", err)
	}
	var provisionTokens []string
	for _, userTokens := range a.config.Tokens {
		for _, token := range userTokens {
			provisionTokens = append(provisionTokens, token.Value)
		}
	}
	for _, existingToken := range existingTokens {
		if !slices.Contains(provisionTokens, existingToken.Value) {
			if err := a.store.RemoveProvisionedToken(existingToken.Value); err != nil {
				return fmt.Errorf("failed to remove provisioned token %s: %v", existingToken.Value, err)
			}
		}
	}
	// (Re-)add provisioned tokens
	for username, tokens := range a.config.Tokens {
		if !slices.Contains(provisionUsernames, username) && username != Everyone {
			return fmt.Errorf("user %s is not a provisioned user, refusing to add tokens", username)
		}
		userID, err := a.store.UserIDByUsername(username)
		if err != nil {
			return fmt.Errorf("failed to find provisioned user %s for provisioned tokens: %v", username, err)
		}
		for _, token := range tokens {
			if _, err := a.store.CreateToken(userID, token.Value, token.Label, time.Unix(0, 0), netip.IPv4Unspecified(), time.Unix(0, 0), 0, true); err != nil {
				return err
			}
		}
	}
	return nil
}
