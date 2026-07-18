package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"text/template"
	"time"

	"heckel.io/ntfy/v2/user"
)

// Defines default config settings (excluding limits, see below)
const (
	DefaultListenHTTP                           = ":80"
	DefaultCacheDuration                        = 12 * time.Hour
	DefaultCacheBatchTimeout                    = time.Duration(0)
	DefaultKeepaliveInterval                    = 45 * time.Second // Not too frequently to save battery (Android read timeout used to be 77s!)
	DefaultManagerInterval                      = time.Minute
	DefaultManagerBatchSize                     = 30000
	DefaultDelayedSenderInterval                = 10 * time.Second
	DefaultMessageDelayMin                      = 10 * time.Second
	DefaultMessageDelayMax                      = 3 * 24 * time.Hour
	DefaultFirebaseKeepaliveInterval            = 3 * time.Hour    // ~control topic (Android), not too frequently to save battery
	DefaultFirebasePollInterval                 = 20 * time.Minute // ~poll topic (iOS), max. 2-3 times per hour (see docs)
	DefaultFirebaseQuotaExceededPenaltyDuration = 10 * time.Minute // Time that over-users are locked out of Firebase if it returns "quota exceeded"
	DefaultStripePriceCacheDuration             = 3 * time.Hour    // Time to keep Stripe prices cached in memory before a refresh is needed
)

// Platform-specific default paths (set in config_unix.go or config_windows.go)
var (
	DefaultConfigFile  string
	DefaultTemplateDir string
)

// Defines default Web Push settings
const (
	DefaultWebPushExpiryWarningDuration = 55 * 24 * time.Hour
	DefaultWebPushExpiryDuration        = 60 * 24 * time.Hour
)

// Defines default abuse ban-feed settings (see BanFile, BanWindow, BanThreshold, BanWeights)
const (
	DefaultBanWindow    = time.Minute
	DefaultBanThreshold = 100 // Weighted strikes per BanWindow before a visitor is banned
)

// DefaultBanWeights defines the per-code strike weights used when the ban-feed is enabled but no
// explicit ban-weights are configured. Format is "KEY:WEIGHT" (see ParseBanWeights). The auth-failure
// flood is weighted heavily so it bans fast, the legit quota 429s are exempt (weight 0), and every
// other rejection costs one strike via the "*" fallback.
var DefaultBanWeights = []string{"42909:10", "42908:0", "42903:0", "42905:0", "42910:0", "*:1"}

// Defines all global and per-visitor limits
// - message size limit: the max number of bytes for a message
// - total topic limit: max number of topics overall
// - various attachment limits
const (
	DefaultMessageSizeLimit            = 4096 // Bytes; note that FCM/APNS have a limit of ~4 KB for the entire message
	DefaultTotalTopicLimit             = 15000
	DefaultAttachmentTotalSizeLimit    = int64(5 * 1024 * 1024 * 1024) // 5 GB
	DefaultAttachmentFileSizeLimit     = int64(15 * 1024 * 1024)       // 15 MB
	DefaultAttachmentExpiryDuration    = 3 * time.Hour
	DefaultAttachmentOrphanGracePeriod = time.Hour // Don't delete orphaned objects younger than this to avoid races with in-flight uploads

)

// Defines all per-visitor limits
// - per visitor subscription limit: max number of subscriptions (active HTTP connections) per per-visitor/IP
// - per visitor request limit: max number of PUT/GET/.. requests (here: 60 requests bucket, replenished at a rate of one per 5 seconds)
// - per visitor email limit: max number of emails (here: 16 email bucket, replenished at a rate of one per hour)
// - per visitor attachment size limit: total per-visitor attachment size in bytes to be stored on the server
// - per visitor attachment daily bandwidth limit: number of bytes that can be transferred to/from the server
const (
	DefaultVisitorSubscriptionLimit             = 30
	DefaultVisitorRequestLimitBurst             = 60
	DefaultVisitorRequestLimitReplenish         = 5 * time.Second
	DefaultVisitorMessageDailyLimit             = 0
	DefaultVisitorEmailLimitBurst               = 16
	DefaultVisitorEmailLimitReplenish           = time.Hour
	DefaultVisitorTopicCreationLimitBurst       = 100
	DefaultVisitorTopicCreationLimitReplenish   = time.Minute
	DefaultVisitorAccountCreationLimitBurst     = 6 // Shared by signup and password-reset requests (same per-visitor bucket)
	DefaultVisitorAccountCreationLimitReplenish = 24 * time.Hour
	DefaultVisitorAuthFailureLimitBurst         = 30
	DefaultVisitorAuthFailureLimitReplenish     = time.Minute
	DefaultVisitorAttachmentTotalSizeLimit      = 100 * 1024 * 1024 // 100 MB
	DefaultVisitorAttachmentDailyBandwidthLimit = 500 * 1024 * 1024 // 500 MB
	DefaultVisitorPrefixBitsIPv4                = 32                // Use the entire IPv4 address for rate limiting
	DefaultVisitorPrefixBitsIPv6                = 64                // Use /64 for IPv6 rate limiting
)

var (
	// DefaultVisitorStatsResetTime defines the time at which visitor stats are reset (wall clock only)
	DefaultVisitorStatsResetTime = time.Date(0, 0, 0, 0, 0, 0, 0, time.UTC)

	// DefaultDisallowedTopics defines the topics that are forbidden, because they are used elsewhere. This array can be
	// extended using the server.yml config. If updated, also update in Android and web app.
	DefaultDisallowedTopics = []string{"docs", "static", "file", "app", "metrics", "account", "settings", "signup", "login", "v1"}
)

// Config is the main config struct for the application. Use New to instantiate a default config struct.
type Config struct {
	File                                 string // Config file, only used for testing
	BaseURL                              string
	ListenHTTP                           string
	ListenHTTPS                          string
	ListenUnix                           string
	ListenUnixMode                       fs.FileMode
	KeyFile                              string
	CertFile                             string
	DatabaseURL                          string   // PostgreSQL connection string (e.g. "postgres://user:pass@host:5432/ntfy")
	DatabaseReplicaURLs                  []string // PostgreSQL read replica connection strings
	FirebaseKeyFile                      string
	CacheFile                            string
	CacheDuration                        time.Duration
	CacheStartupQueries                  string
	CacheBatchSize                       int
	CacheBatchTimeout                    time.Duration
	AuthFile                             string
	AuthStartupQueries                   string
	AuthDefault                          user.Permission
	AuthUsers                            []*user.User
	AuthAccess                           map[string][]*user.Grant
	AuthTokens                           map[string][]*user.Token
	AuthBcryptCost                       int
	AuthStatsQueueWriterInterval         time.Duration
	AuthAccessCacheEnabled               bool          // Enables the in-memory ACL cache (high volume servers only)
	AuthAccessCacheReloadInterval        time.Duration // Reload interval for access cache, relevant for ACL writes from CLI
	AttachmentCacheDir                   string
	AttachmentTotalSizeLimit             int64
	AttachmentFileSizeLimit              int64
	AttachmentExpiryDuration             time.Duration
	AttachmentOrphanGracePeriod          time.Duration
	TemplateDir                          string // Directory to load named templates from
	KeepaliveInterval                    time.Duration
	ManagerInterval                      time.Duration
	ManagerBatchSize                     int
	DisallowedTopics                     []string
	WebRoot                              string // empty to disable
	DelayedSenderInterval                time.Duration
	FirebaseKeepaliveInterval            time.Duration
	FirebasePollInterval                 time.Duration
	FirebaseQuotaExceededPenaltyDuration time.Duration
	UpstreamBaseURL                      string
	UpstreamAccessToken                  string
	SMTPSenderAddr                       string
	SMTPSenderUser                       string
	SMTPSenderPass                       string
	SMTPSenderFrom                       string
	SMTPSenderVerify                     bool
	SMTPServerListen                     string
	SMTPServerDomain                     string
	SMTPServerAddrPrefix                 string
	TwilioAccount                        string
	TwilioAuthToken                      string
	TwilioPhoneNumber                    string
	TwilioCallsBaseURL                   string
	TwilioVerifyBaseURL                  string
	TwilioVerifyService                  string
	TwilioCallFormat                     *template.Template
	MetricsEnable                        bool
	MetricsListenHTTP                    string
	ProfileListenHTTP                    string
	MessageDelayMin                      time.Duration
	MessageDelayMax                      time.Duration
	MessageSizeLimit                     int
	TotalTopicLimit                      int
	TotalAttachmentSizeLimit             int64
	VisitorSubscriptionLimit             int
	VisitorAttachmentTotalSizeLimit      int64
	VisitorAttachmentDailyBandwidthLimit int64
	VisitorRequestLimitBurst             int
	VisitorRequestLimitReplenish         time.Duration
	VisitorRequestExemptPrefixes         []netip.Prefix
	VisitorMessageDailyLimit             int
	VisitorEmailLimitBurst               int
	VisitorEmailLimitReplenish           time.Duration
	VisitorTopicCreationLimitBurst       int           // Burst of new topic creations per visitor
	VisitorTopicCreationLimitReplenish   time.Duration // Interval at which topic-creation tokens are refilled
	VisitorAccountCreationLimitBurst     int
	VisitorAccountCreationLimitReplenish time.Duration
	VisitorAuthFailureLimitBurst         int
	VisitorAuthFailureLimitReplenish     time.Duration
	VisitorStatsResetTime                time.Time      // Time of the day at which to reset visitor stats
	VisitorSubscriberRateLimiting        bool           // Enable subscriber-based rate limiting for UnifiedPush topics
	VisitorPrefixBitsIPv4                int            // Number of bits for IPv4 rate limiting (default: 32)
	VisitorPrefixBitsIPv6                int            // Number of bits for IPv6 rate limiting (default: 64)
	BehindProxy                          bool           // If true, the server will trust the proxy client IP header to determine the client IP address (IPv4 and IPv6 supported)
	ProxyForwardedHeader                 string         // The header field to read the real/client IP address from, if BehindProxy is true, defaults to "X-Forwarded-For" (IPv4 and IPv6 supported)
	ProxyTrustedPrefixes                 []netip.Prefix // List of trusted proxy networks (IPv4 or IPv6) that will be stripped from the Forwarded header if BehindProxy is true
	StripeSecretKey                      string
	StripeWebhookKey                     string
	StripePriceCacheDuration             time.Duration
	BillingContact                       string
	EnableSignup                         bool // Enable creation of accounts via API and UI
	EnableLogin                          bool
	RequireLogin                         bool
	EnableReservations                   bool // Allow users with role "user" to own/reserve topics
	EnableMetrics                        bool
	AccessControlAllowOrigin             string // CORS header field to restrict access from web clients
	WebPushPrivateKey                    string
	WebPushPublicKey                     string
	WebPushFile                          string
	WebPushEmailAddress                  string
	WebPushStartupQueries                string
	WebPushExpiryDuration                time.Duration
	WebPushExpiryWarningDuration         time.Duration
	BanFile                              string         // Abuse ban-feed: file that fail2ban tails; empty string disables the feature
	BanWindow                            time.Duration  // Abuse ban-feed: rolling window over which weighted strikes are counted
	BanThreshold                         int            // Abuse ban-feed: weighted strikes per window before a visitor is banned
	BanWeights                           map[string]int // Abuse ban-feed: normalized code matcher -> strike weight (see ParseBanWeights, weightFor)
	BuildVersion                         string         // Injected by App
	BuildDate                            string         // Injected by App
	BuildCommit                          string         // Injected by App
}

// NewConfig instantiates a default new server config
func NewConfig() *Config {
	return &Config{
		File:                                 DefaultConfigFile, // Only used for testing
		BaseURL:                              "",
		ListenHTTP:                           DefaultListenHTTP,
		ListenHTTPS:                          "",
		ListenUnix:                           "",
		ListenUnixMode:                       0,
		KeyFile:                              "",
		CertFile:                             "",
		DatabaseURL:                          "",
		FirebaseKeyFile:                      "",
		CacheFile:                            "",
		CacheDuration:                        DefaultCacheDuration,
		CacheStartupQueries:                  "",
		CacheBatchSize:                       0,
		CacheBatchTimeout:                    0,
		AuthFile:                             "",
		AuthStartupQueries:                   "",
		AuthDefault:                          user.PermissionReadWrite,
		AuthBcryptCost:                       user.DefaultUserPasswordBcryptCost,
		AuthStatsQueueWriterInterval:         user.DefaultUserStatsQueueWriterInterval,
		AuthAccessCacheEnabled:               user.DefaultAccessCacheEnabled,
		AuthAccessCacheReloadInterval:        user.DefaultAccessCacheReloadInterval,
		AttachmentCacheDir:                   "",
		AttachmentTotalSizeLimit:             DefaultAttachmentTotalSizeLimit,
		AttachmentFileSizeLimit:              DefaultAttachmentFileSizeLimit,
		AttachmentExpiryDuration:             DefaultAttachmentExpiryDuration,
		AttachmentOrphanGracePeriod:          DefaultAttachmentOrphanGracePeriod,
		TemplateDir:                          DefaultTemplateDir,
		KeepaliveInterval:                    DefaultKeepaliveInterval,
		ManagerInterval:                      DefaultManagerInterval,
		ManagerBatchSize:                     DefaultManagerBatchSize,
		DisallowedTopics:                     DefaultDisallowedTopics,
		WebRoot:                              "/",
		DelayedSenderInterval:                DefaultDelayedSenderInterval,
		FirebaseKeepaliveInterval:            DefaultFirebaseKeepaliveInterval,
		FirebasePollInterval:                 DefaultFirebasePollInterval,
		FirebaseQuotaExceededPenaltyDuration: DefaultFirebaseQuotaExceededPenaltyDuration,
		UpstreamBaseURL:                      "",
		UpstreamAccessToken:                  "",
		SMTPSenderAddr:                       "",
		SMTPSenderUser:                       "",
		SMTPSenderPass:                       "",
		SMTPSenderFrom:                       "",
		SMTPSenderVerify:                     false,
		SMTPServerListen:                     "",
		SMTPServerDomain:                     "",
		SMTPServerAddrPrefix:                 "",
		TwilioCallsBaseURL:                   "https://api.twilio.com", // Override for tests
		TwilioAccount:                        "",
		TwilioAuthToken:                      "",
		TwilioPhoneNumber:                    "",
		TwilioVerifyBaseURL:                  "https://verify.twilio.com", // Override for tests
		TwilioVerifyService:                  "",
		TwilioCallFormat:                     nil,
		MessageSizeLimit:                     DefaultMessageSizeLimit,
		MessageDelayMin:                      DefaultMessageDelayMin,
		MessageDelayMax:                      DefaultMessageDelayMax,
		TotalTopicLimit:                      DefaultTotalTopicLimit,
		TotalAttachmentSizeLimit:             0,
		VisitorSubscriptionLimit:             DefaultVisitorSubscriptionLimit,
		VisitorSubscriberRateLimiting:        false,
		VisitorAttachmentTotalSizeLimit:      DefaultVisitorAttachmentTotalSizeLimit,
		VisitorAttachmentDailyBandwidthLimit: DefaultVisitorAttachmentDailyBandwidthLimit,
		VisitorRequestLimitBurst:             DefaultVisitorRequestLimitBurst,
		VisitorRequestLimitReplenish:         DefaultVisitorRequestLimitReplenish,
		VisitorRequestExemptPrefixes:         make([]netip.Prefix, 0),
		VisitorMessageDailyLimit:             DefaultVisitorMessageDailyLimit,
		VisitorEmailLimitBurst:               DefaultVisitorEmailLimitBurst,
		VisitorEmailLimitReplenish:           DefaultVisitorEmailLimitReplenish,
		VisitorTopicCreationLimitBurst:       DefaultVisitorTopicCreationLimitBurst,
		VisitorTopicCreationLimitReplenish:   DefaultVisitorTopicCreationLimitReplenish,
		VisitorAccountCreationLimitBurst:     DefaultVisitorAccountCreationLimitBurst,
		VisitorAccountCreationLimitReplenish: DefaultVisitorAccountCreationLimitReplenish,
		VisitorAuthFailureLimitBurst:         DefaultVisitorAuthFailureLimitBurst,
		VisitorAuthFailureLimitReplenish:     DefaultVisitorAuthFailureLimitReplenish,
		VisitorStatsResetTime:                DefaultVisitorStatsResetTime,
		VisitorPrefixBitsIPv4:                DefaultVisitorPrefixBitsIPv4, // Default: use full IPv4 address
		VisitorPrefixBitsIPv6:                DefaultVisitorPrefixBitsIPv6, // Default: use /64 for IPv6
		BehindProxy:                          false,                        // If true, the server will trust the proxy client IP header to determine the client IP address
		ProxyForwardedHeader:                 "X-Forwarded-For",            // Default header for reverse proxy client IPs
		StripeSecretKey:                      "",
		StripeWebhookKey:                     "",
		StripePriceCacheDuration:             DefaultStripePriceCacheDuration,
		BillingContact:                       "",
		EnableSignup:                         false,
		EnableLogin:                          false,
		EnableReservations:                   false,
		RequireLogin:                         false,
		AccessControlAllowOrigin:             "*",
		WebPushPrivateKey:                    "",
		WebPushPublicKey:                     "",
		WebPushFile:                          "",
		WebPushEmailAddress:                  "",
		WebPushExpiryDuration:                DefaultWebPushExpiryDuration,
		WebPushExpiryWarningDuration:         DefaultWebPushExpiryWarningDuration,
		BanFile:                              "",
		BanWindow:                            DefaultBanWindow,
		BanThreshold:                         DefaultBanThreshold,
		BanWeights:                           nil,
		BuildVersion:                         "",
		BuildDate:                            "",
		BuildCommit:                          "",
	}
}

// Hash computes an SHA-256 hash of the configuration. This is used to detect
// configuration changes for the web app version check feature. It uses reflection
// to include all JSON-serializable fields automatically.
func (c *Config) Hash() string {
	v := reflect.ValueOf(*c)
	t := v.Type()
	var result string
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldName := t.Field(i).Name
		// Try to marshal the field and skip if it fails (e.g. *template.Template, netip.Prefix)
		if b, err := json.Marshal(field.Interface()); err == nil {
			result += fmt.Sprintf("%s:%s|", fieldName, string(b))
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(result)))
}

// ParseBanWeights turns a list like ["42909:10","403:2","42908:0","*:1"] into a map of normalized
// matcher key -> strike weight for the abuse ban-feed's single weighted bucket. A key may be an exact
// ntfy code ("42909"), a prefix family ("429*"), or "*". A bare 3-digit HTTP status ("403") is a
// shorthand normalized to its family ("403*"), so operators can weight a whole status at once. Weights
// must be integers >= 0; a weight of 0 is valid and means the code is exempt (never contributes to a
// ban), which lets a "*" catch-all coexist with carved-out legit-quota codes. A malformed entry is
// rejected so misconfiguration surfaces at startup rather than silently disabling bans.
func ParseBanWeights(entries []string) (map[string]int, error) {
	out := make(map[string]int, len(entries))
	for _, entry := range entries {
		key, weightStr, ok := strings.Cut(entry, ":")
		if !ok {
			return nil, fmt.Errorf("invalid ban-weight %q, want KEY:WEIGHT", entry)
		}
		weight, err := strconv.Atoi(strings.TrimSpace(weightStr))
		if err != nil || weight < 0 {
			return nil, fmt.Errorf("invalid ban-weight value in %q, want a non-negative integer", entry)
		}
		key = strings.TrimSpace(key)
		if !validBanWeightKey(key) {
			return nil, fmt.Errorf("invalid ban-weight key in %q, want %q, an ntfy code, an HTTP status, or a PREFIX*", entry, "*")
		}
		// A bare 3-digit HTTP status is shorthand for the whole family (e.g. "403" -> "403*").
		if len(key) == 3 && isAllDigits(key) {
			key += "*"
		}
		out[key] = weight
	}
	return out, nil
}

// validBanWeightKey reports whether key is a legal ban-weight matcher: "*", an all-digits code,
// or an all-digits prefix followed by "*".
func validBanWeightKey(key string) bool {
	if key == "*" {
		return true
	}
	digits := strings.TrimSuffix(key, "*")
	return digits != "" && isAllDigits(digits)
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// weightFor returns the strike weight for a rejection's 5-digit ntfy code using the ban-weights
// matcher, longest-match-wins. An exact key ("42909") matches with length len(code); a family key
// ("429*") matches a code with that prefix, with length equal to the prefix; "*" matches everything
// with length 0. The weight of the longest match is returned, or 0 if no key matches (which the
// caller treats as "no strike").
func (c *Config) weightFor(ntfyCode int) int {
	code := strconv.Itoa(ntfyCode)
	weight, bestLen, matched := 0, -1, false
	for key, w := range c.BanWeights {
		matchLen := -1
		switch {
		case key == "*":
			matchLen = 0
		case strings.HasSuffix(key, "*"):
			if prefix := strings.TrimSuffix(key, "*"); strings.HasPrefix(code, prefix) {
				matchLen = len(prefix)
			}
		case key == code:
			matchLen = len(code)
		}
		if matchLen > bestLen {
			weight, bestLen, matched = w, matchLen, true
		}
	}
	if !matched {
		return 0
	}
	return weight
}
