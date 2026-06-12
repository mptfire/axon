package user

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// addVerifyLink stores an email-verification magic link and returns the raw token so the test
// can "click" it via VerifyEmail.
func addVerifyLink(t *testing.T, a *Manager, userID, email string, ttl time.Duration) string {
	raw, err := a.CreateMagicLink(MagicLinkKindEmailVerify, userID, email, ttl)
	require.Nil(t, err)
	return raw
}

func TestUser_MagicLink_VerifyEmail_SetsPrimary(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		require.Nil(t, a.AddUser("phil", "phil", RoleUser, false))
		phil, err := a.User("phil")
		require.Nil(t, err)

		raw := addVerifyLink(t, a, phil.ID, "phil@example.com", 24*time.Hour)

		// Before verifying: pending, not yet verified, no primary
		pending, err := a.PendingEmails(phil.ID)
		require.Nil(t, err)
		require.Equal(t, []string{"phil@example.com"}, pending)
		emails, err := a.Emails(phil.ID)
		require.Nil(t, err)
		require.Equal(t, 0, len(emails))
		primary, err := a.PrimaryEmail(phil.ID)
		require.Nil(t, err)
		require.Equal(t, "", primary)

		// Verify: the first verified email auto-becomes primary
		m, err := a.VerifyEmail(raw)
		require.Nil(t, err)
		require.Equal(t, "phil@example.com", m.Email)

		emails, err = a.Emails(phil.ID)
		require.Nil(t, err)
		require.Equal(t, []string{"phil@example.com"}, emails)
		primary, err = a.PrimaryEmail(phil.ID)
		require.Nil(t, err)
		require.Equal(t, "phil@example.com", primary)
		pending, err = a.PendingEmails(phil.ID)
		require.Nil(t, err)
		require.Equal(t, 0, len(pending))

		// Reset-by-email lookup resolves to the account
		userID, err := a.UserIDByPrimaryEmail("phil@example.com")
		require.Nil(t, err)
		require.Equal(t, phil.ID, userID)
	})
}

func TestUser_MagicLink_VerifyEmail_SecondStaysSecondary(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		require.Nil(t, a.AddUser("phil", "phil", RoleUser, false))
		phil, err := a.User("phil")
		require.Nil(t, err)

		raw1 := addVerifyLink(t, a, phil.ID, "first@example.com", 24*time.Hour)
		_, err = a.VerifyEmail(raw1)
		require.Nil(t, err)

		raw2 := addVerifyLink(t, a, phil.ID, "second@example.com", 24*time.Hour)
		_, err = a.VerifyEmail(raw2)
		require.Nil(t, err)

		// Both verified, but primary is still the first
		emails, err := a.Emails(phil.ID)
		require.Nil(t, err)
		require.Equal(t, []string{"first@example.com", "second@example.com"}, emails)
		primary, err := a.PrimaryEmail(phil.ID)
		require.Nil(t, err)
		require.Equal(t, "first@example.com", primary)
	})
}

func TestUser_MagicLink_PrimaryGlobalUniqueness(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		require.Nil(t, a.AddUser("phil", "phil", RoleUser, false))
		require.Nil(t, a.AddUser("ben", "ben", RoleUser, false))
		phil, err := a.User("phil")
		require.Nil(t, err)
		ben, err := a.User("ben")
		require.Nil(t, err)

		// phil verifies shared@ first -> becomes his primary
		_, err = a.VerifyEmail(addVerifyLink(t, a, phil.ID, "shared@example.com", 24*time.Hour))
		require.Nil(t, err)
		primary, err := a.PrimaryEmail(phil.ID)
		require.Nil(t, err)
		require.Equal(t, "shared@example.com", primary)

		// ben verifies the same address -> allowed as secondary, but NOT his primary
		_, err = a.VerifyEmail(addVerifyLink(t, a, ben.ID, "shared@example.com", 24*time.Hour))
		require.Nil(t, err)
		emails, err := a.Emails(ben.ID)
		require.Nil(t, err)
		require.Equal(t, []string{"shared@example.com"}, emails)
		primary, err = a.PrimaryEmail(ben.ID)
		require.Nil(t, err)
		require.Equal(t, "", primary)

		// Explicitly promoting ben's copy to primary collides with phil's
		require.ErrorIs(t, a.SetPrimaryEmail(ben.ID, "shared@example.com"), ErrEmailPrimaryElsewhere)
		// ...and phil keeps his primary (the failed promotion rolled back ben's clear)
		primary, err = a.PrimaryEmail(phil.ID)
		require.Nil(t, err)
		require.Equal(t, "shared@example.com", primary)
	})
}

func TestUser_MagicLink_SetPrimary_NotVerified(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		require.Nil(t, a.AddUser("phil", "phil", RoleUser, false))
		phil, err := a.User("phil")
		require.Nil(t, err)
		require.ErrorIs(t, a.SetPrimaryEmail(phil.ID, "nope@example.com"), ErrEmailNotFound)
	})
}

func TestUser_MagicLink_Expired(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		require.Nil(t, a.AddUser("phil", "phil", RoleUser, false))
		phil, err := a.User("phil")
		require.Nil(t, err)

		raw := addVerifyLink(t, a, phil.ID, "phil@example.com", -time.Minute)
		_, err = a.VerifyEmail(raw)
		require.ErrorIs(t, err, ErrMagicLinkNotFound)

		// Nothing got verified
		emails, err := a.Emails(phil.ID)
		require.Nil(t, err)
		require.Equal(t, 0, len(emails))
	})
}

func TestUser_MagicLink_SingleUse(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		require.Nil(t, a.AddUser("phil", "phil", RoleUser, false))
		phil, err := a.User("phil")
		require.Nil(t, err)

		raw := addVerifyLink(t, a, phil.ID, "phil@example.com", 24*time.Hour)
		_, err = a.VerifyEmail(raw)
		require.Nil(t, err)
		// Second click: token already consumed
		_, err = a.VerifyEmail(raw)
		require.ErrorIs(t, err, ErrMagicLinkNotFound)
	})
}

func TestUser_MagicLink_ReplaceOnReRequest(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		require.Nil(t, a.AddUser("phil", "phil", RoleUser, false))
		phil, err := a.User("phil")
		require.Nil(t, err)

		raw1 := addVerifyLink(t, a, phil.ID, "phil@example.com", 24*time.Hour)
		raw2 := addVerifyLink(t, a, phil.ID, "phil@example.com", 24*time.Hour)

		// Only one pending row remains; the old token no longer works
		pending, err := a.PendingEmails(phil.ID)
		require.Nil(t, err)
		require.Equal(t, []string{"phil@example.com"}, pending)
		_, err = a.MagicLinkByToken(raw1)
		require.ErrorIs(t, err, ErrMagicLinkNotFound)

		m, err := a.MagicLinkByToken(raw2)
		require.Nil(t, err)
		require.Equal(t, "phil@example.com", m.Email)
	})
}

func TestUser_MagicLink_PasswordReset_RoundTrip(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		require.Nil(t, a.AddUser("phil", "phil", RoleUser, false))
		phil, err := a.User("phil")
		require.Nil(t, err)

		raw, err := a.CreateMagicLink(MagicLinkKindPasswordReset, phil.ID, "", time.Hour)
		require.Nil(t, err)

		m, err := a.MagicLinkByToken(raw)
		require.Nil(t, err)
		require.Equal(t, MagicLinkKindPasswordReset, m.Kind)
		require.Equal(t, phil.ID, m.UserID)
		require.Equal(t, "", m.Email) // reset rows carry no email

		// Reset rows do not appear as pending emails
		pending, err := a.PendingEmails(phil.ID)
		require.Nil(t, err)
		require.Equal(t, 0, len(pending))

		// New request replaces the old token
		raw2, err := a.CreateMagicLink(MagicLinkKindPasswordReset, phil.ID, "", time.Hour)
		require.Nil(t, err)
		_, err = a.MagicLinkByToken(raw)
		require.ErrorIs(t, err, ErrMagicLinkNotFound)

		// Single use: deleting consumes it
		require.Nil(t, a.DeleteMagicLinkByToken(raw2))
		_, err = a.MagicLinkByToken(raw2)
		require.ErrorIs(t, err, ErrMagicLinkNotFound)
	})
}

func TestUser_MagicLink_Reaper(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		require.Nil(t, a.AddUser("phil", "phil", RoleUser, false))
		phil, err := a.User("phil")
		require.Nil(t, err)

		expired := addVerifyLink(t, a, phil.ID, "expired@example.com", -time.Hour)
		valid := addVerifyLink(t, a, phil.ID, "valid@example.com", time.Hour)

		require.Nil(t, a.deleteExpiredMagicLinks())

		_, err = a.MagicLinkByToken(expired)
		require.ErrorIs(t, err, ErrMagicLinkNotFound)
		m, err := a.MagicLinkByToken(valid)
		require.Nil(t, err)
		require.Equal(t, "valid@example.com", m.Email)
	})
}

func TestUser_MagicLink_ResetPassword(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		require.Nil(t, a.AddUser("phil", "oldpass", RoleUser, false))
		phil, err := a.User("phil")
		require.Nil(t, err)

		raw, err := a.CreateMagicLink(MagicLinkKindPasswordReset, phil.ID, "", time.Hour)
		require.Nil(t, err)

		// Old password works before reset
		_, err = a.Authenticate("phil", "oldpass")
		require.Nil(t, err)

		require.Nil(t, a.ResetPassword(raw, "newpass"))

		// New password works, old does not
		_, err = a.Authenticate("phil", "newpass")
		require.Nil(t, err)
		_, err = a.Authenticate("phil", "oldpass")
		require.ErrorIs(t, err, ErrUnauthenticated)

		// Token is single-use
		require.ErrorIs(t, a.ResetPassword(raw, "againpass"), ErrMagicLinkNotFound)
	})
}

func TestUser_MagicLink_ResetPassword_WrongKindRejected(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		require.Nil(t, a.AddUser("phil", "oldpass", RoleUser, false))
		phil, err := a.User("phil")
		require.Nil(t, err)

		// An email-verification token must not be usable for password reset...
		verifyToken := addVerifyLink(t, a, phil.ID, "phil@example.com", time.Hour)
		require.ErrorIs(t, a.ResetPassword(verifyToken, "newpass"), ErrMagicLinkNotFound)

		// ...and a reset token must not be usable for email verification
		resetToken, err := a.CreateMagicLink(MagicLinkKindPasswordReset, phil.ID, "", time.Hour)
		require.Nil(t, err)
		_, err = a.VerifyEmail(resetToken)
		require.ErrorIs(t, err, ErrMagicLinkNotFound)

		// Old password unchanged
		_, err = a.Authenticate("phil", "oldpass")
		require.Nil(t, err)
	})
}

func TestUser_MagicLink_ResetPassword_Expired(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		require.Nil(t, a.AddUser("phil", "oldpass", RoleUser, false))
		phil, err := a.User("phil")
		require.Nil(t, err)

		raw, err := a.CreateMagicLink(MagicLinkKindPasswordReset, phil.ID, "", -time.Minute)
		require.Nil(t, err)
		require.ErrorIs(t, a.ResetPassword(raw, "newpass"), ErrMagicLinkNotFound)
		_, err = a.Authenticate("phil", "oldpass")
		require.Nil(t, err)
	})
}

func TestUser_MagicLink_UserIDByPrimaryEmail_NotFound(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		_, err := a.UserIDByPrimaryEmail("ghost@example.com")
		require.ErrorIs(t, err, ErrUserNotFound)
	})
}
