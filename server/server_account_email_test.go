package server

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/user"
	"heckel.io/ntfy/v2/util"
)

// captureMailer is a fake emailVerifier that records the magic links it is asked to send, so
// tests can "click" them without a real SMTP server.
type captureMailer struct {
	verifyLinks map[string]string // email -> verification link
	resetLinks  map[string]string // email -> reset link
}

func newCaptureMailer() *captureMailer {
	return &captureMailer{verifyLinks: map[string]string{}, resetLinks: map[string]string{}}
}

func (c *captureMailer) SendEmailVerification(to, link string) error {
	c.verifyLinks[to] = link
	return nil
}

func (c *captureMailer) SendPasswordReset(to, link string) error {
	c.resetLinks[to] = link
	return nil
}

func (c *captureMailer) Close() {}

// newEmailTestServer creates a server with email sending "enabled" (SMTP + base-url configured)
// and a capturing mailer injected, plus a tier-less user "ben" logged in via basic auth.
func newEmailTestServer(t *testing.T, databaseURL string) (*Server, *captureMailer, map[string]string) {
	conf := newTestConfigWithAuthFile(t, databaseURL)
	conf.SMTPSenderAddr = "localhost:25"
	conf.SMTPSenderFrom = "noreply@example.com"
	conf.BaseURL = "https://ntfy.example.com"
	s := newTestServer(t, conf)
	mailer := newCaptureMailer()
	s.mailSender = mailer
	require.Nil(t, s.userManager.AddUser("ben", "ben", user.RoleUser, false))
	auth := map[string]string{"Authorization": util.BasicAuth("ben", "ben")}
	return s, mailer, auth
}

func getAccount(t *testing.T, s *Server, auth map[string]string) *apiAccountResponse {
	rr := request(t, s, "GET", "/v1/account", "", auth)
	require.Equal(t, 200, rr.Code)
	account, err := util.UnmarshalJSON[apiAccountResponse](io.NopCloser(rr.Body))
	require.Nil(t, err)
	return account
}

func tokenFromLink(t *testing.T, link, prefix string) string {
	require.True(t, strings.HasPrefix(link, prefix), "link %q missing prefix %q", link, prefix)
	return strings.TrimPrefix(link, prefix)
}

func TestAccount_Email_AddVerifySetsPrimary(t *testing.T) {
	forEachBackend(t, func(t *testing.T, databaseURL string) {
		s, mailer, auth := newEmailTestServer(t, databaseURL)
		defer s.closeDatabases()

		// Start verification
		rr := request(t, s, "PUT", "/v1/account/email", `{"email":"ben@example.com"}`, auth)
		require.Equal(t, 200, rr.Code)

		// Pending, not yet verified, no primary
		account := getAccount(t, s, auth)
		require.Equal(t, []string{"ben@example.com"}, account.PendingEmails)
		require.Empty(t, account.Emails)
		require.Equal(t, "", account.PrimaryEmail)

		// "Click" the captured link (unauthenticated POST)
		token := tokenFromLink(t, mailer.verifyLinks["ben@example.com"], "https://ntfy.example.com/account/email/verify/")
		rr = request(t, s, "POST", "/v1/account/email/verify", fmt.Sprintf(`{"token":"%s"}`, token), nil)
		require.Equal(t, 200, rr.Code)

		// Now verified + primary, no longer pending
		account = getAccount(t, s, auth)
		require.Equal(t, []string{"ben@example.com"}, account.Emails)
		require.Equal(t, "ben@example.com", account.PrimaryEmail)
		require.Empty(t, account.PendingEmails)
	})
}

func TestAccount_Email_VerifyInvalidToken(t *testing.T) {
	forEachBackend(t, func(t *testing.T, databaseURL string) {
		s, _, _ := newEmailTestServer(t, databaseURL)
		defer s.closeDatabases()

		rr := request(t, s, "POST", "/v1/account/email/verify", `{"token":"doesnotexist"}`, nil)
		require.Equal(t, 400, rr.Code)
		require.Equal(t, 40051, toHTTPError(t, rr.Body.String()).Code)

		// Empty token also rejected
		rr = request(t, s, "POST", "/v1/account/email/verify", `{"token":""}`, nil)
		require.Equal(t, 400, rr.Code)
	})
}

func TestAccount_Email_DeletePending(t *testing.T) {
	forEachBackend(t, func(t *testing.T, databaseURL string) {
		s, _, auth := newEmailTestServer(t, databaseURL)
		defer s.closeDatabases()

		require.Equal(t, 200, request(t, s, "PUT", "/v1/account/email", `{"email":"ben@example.com"}`, auth).Code)
		require.Equal(t, []string{"ben@example.com"}, getAccount(t, s, auth).PendingEmails)

		// Deleting the pending address clears it (no verification ever happened)
		require.Equal(t, 200, request(t, s, "DELETE", "/v1/account/email", `{"email":"ben@example.com"}`, auth).Code)
		account := getAccount(t, s, auth)
		require.Empty(t, account.PendingEmails)
		require.Empty(t, account.Emails)
	})
}

func TestAccount_Email_Resend(t *testing.T) {
	forEachBackend(t, func(t *testing.T, databaseURL string) {
		s, mailer, auth := newEmailTestServer(t, databaseURL)
		defer s.closeDatabases()

		require.Equal(t, 200, request(t, s, "PUT", "/v1/account/email", `{"email":"ben@example.com"}`, auth).Code)
		firstLink := mailer.verifyLinks["ben@example.com"]
		require.NotEmpty(t, firstLink)

		// Resend issues a fresh link (the old one is replaced)
		require.Equal(t, 200, request(t, s, "POST", "/v1/account/email/resend", `{"email":"ben@example.com"}`, auth).Code)
		require.NotEqual(t, firstLink, mailer.verifyLinks["ben@example.com"])

		// The old token no longer verifies; the new one does
		oldToken := tokenFromLink(t, firstLink, "https://ntfy.example.com/account/email/verify/")
		require.Equal(t, 400, request(t, s, "POST", "/v1/account/email/verify", fmt.Sprintf(`{"token":"%s"}`, oldToken), nil).Code)
		newToken := tokenFromLink(t, mailer.verifyLinks["ben@example.com"], "https://ntfy.example.com/account/email/verify/")
		require.Equal(t, 200, request(t, s, "POST", "/v1/account/email/verify", fmt.Sprintf(`{"token":"%s"}`, newToken), nil).Code)

		// Resending for a non-pending address is rejected
		require.Equal(t, 400, request(t, s, "POST", "/v1/account/email/resend", `{"email":"never@example.com"}`, auth).Code)
	})
}

func TestAccount_Email_SetPrimaryCollision(t *testing.T) {
	forEachBackend(t, func(t *testing.T, databaseURL string) {
		s, mailer, auth := newEmailTestServer(t, databaseURL)
		defer s.closeDatabases()

		// ben verifies shared@ -> becomes his primary
		require.Equal(t, 200, request(t, s, "PUT", "/v1/account/email", `{"email":"shared@example.com"}`, auth).Code)
		benToken := tokenFromLink(t, mailer.verifyLinks["shared@example.com"], "https://ntfy.example.com/account/email/verify/")
		require.Equal(t, 200, request(t, s, "POST", "/v1/account/email/verify", fmt.Sprintf(`{"token":"%s"}`, benToken), nil).Code)
		require.Equal(t, "shared@example.com", getAccount(t, s, auth).PrimaryEmail)

		// alice verifies the same address -> allowed as secondary, but it is not her primary
		require.Nil(t, s.userManager.AddUser("alice", "alice", user.RoleUser, false))
		aliceAuth := map[string]string{"Authorization": util.BasicAuth("alice", "alice")}
		require.Equal(t, 200, request(t, s, "PUT", "/v1/account/email", `{"email":"shared@example.com"}`, aliceAuth).Code)
		aliceToken := tokenFromLink(t, mailer.verifyLinks["shared@example.com"], "https://ntfy.example.com/account/email/verify/")
		require.Equal(t, 200, request(t, s, "POST", "/v1/account/email/verify", fmt.Sprintf(`{"token":"%s"}`, aliceToken), nil).Code)
		aliceAccount := getAccount(t, s, aliceAuth)
		require.Equal(t, []string{"shared@example.com"}, aliceAccount.Emails)
		require.Equal(t, "", aliceAccount.PrimaryEmail)

		// alice trying to promote it to primary collides with ben's
		rr := request(t, s, "POST", "/v1/account/email/primary", `{"email":"shared@example.com"}`, aliceAuth)
		require.Equal(t, 409, rr.Code)
		require.Equal(t, 40908, toHTTPError(t, rr.Body.String()).Code)
	})
}

func TestAccount_Email_AddDuplicateVerified(t *testing.T) {
	forEachBackend(t, func(t *testing.T, databaseURL string) {
		s, mailer, auth := newEmailTestServer(t, databaseURL)
		defer s.closeDatabases()

		require.Equal(t, 200, request(t, s, "PUT", "/v1/account/email", `{"email":"ben@example.com"}`, auth).Code)
		token := tokenFromLink(t, mailer.verifyLinks["ben@example.com"], "https://ntfy.example.com/account/email/verify/")
		require.Equal(t, 200, request(t, s, "POST", "/v1/account/email/verify", fmt.Sprintf(`{"token":"%s"}`, token), nil).Code)

		// Adding the same already-verified address is a conflict
		rr := request(t, s, "PUT", "/v1/account/email", `{"email":"ben@example.com"}`, auth)
		require.Equal(t, 409, rr.Code)
		require.Equal(t, 40907, toHTTPError(t, rr.Body.String()).Code)
	})
}
