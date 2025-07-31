package user

import (
	"golang.org/x/crypto/bcrypt"
	"heckel.io/ntfy/v2/util"
	"regexp"
	"strings"
)

var (
	allowedUsernameRegex     = regexp.MustCompile(`^[-_.+@a-zA-Z0-9]+$`)    // Does not include Everyone (*)
	allowedTopicRegex        = regexp.MustCompile(`^[-_A-Za-z0-9]{1,64}$`)  // No '*'
	allowedTopicPatternRegex = regexp.MustCompile(`^[-_*A-Za-z0-9]{1,64}$`) // Adds '*' for wildcards!
	allowedTierRegex         = regexp.MustCompile(`^[-_A-Za-z0-9]{1,64}$`)
	allowedTokenRegex        = regexp.MustCompile(`^tk_[-_A-Za-z0-9]{29}$`) // Must be tokenLength-len(tokenPrefix)
)

// AllowedRole returns true if the given role can be used for new users
func AllowedRole(role Role) bool {
	return role == RoleUser || role == RoleAdmin
}

// AllowedUsername returns true if the given username is valid
func AllowedUsername(username string) bool {
	return allowedUsernameRegex.MatchString(username)
}

// AllowedTopic returns true if the given topic name is valid
func AllowedTopic(topic string) bool {
	return allowedTopicRegex.MatchString(topic)
}

// AllowedTopicPattern returns true if the given topic pattern is valid; this includes the wildcard character (*)
func AllowedTopicPattern(topic string) bool {
	return allowedTopicPatternRegex.MatchString(topic)
}

// AllowedTier returns true if the given tier name is valid
func AllowedTier(tier string) bool {
	return allowedTierRegex.MatchString(tier)
}

// ValidPasswordHash checks if the given password hash is a valid bcrypt hash
func ValidPasswordHash(hash string) error {
	if !strings.HasPrefix(hash, "$2a$") && !strings.HasPrefix(hash, "$2b$") && !strings.HasPrefix(hash, "$2y$") {
		return ErrPasswordHashInvalid
	}
	return nil
}

// ValidToken returns true if the given token matches the naming convention
func ValidToken(token string) bool {
	return allowedTokenRegex.MatchString(token)
}

// GenerateToken generates a new token with a prefix and a fixed length
// Lowercase only to support "<topic>+<token>@<domain>" email addresses
func GenerateToken() string {
	return util.RandomLowerStringPrefix(tokenPrefix, tokenLength)
}

// HashPassword hashes the given password using bcrypt with the configured cost
func HashPassword(password string) (string, error) {
	return hashPassword(password, DefaultUserPasswordBcryptCost)
}

func hashPassword(password string, cost int) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
