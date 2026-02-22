package user_test

import (
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	dbtest "heckel.io/ntfy/v2/db/test"
	"heckel.io/ntfy/v2/user"
)

func forEachStoreBackend(t *testing.T, f func(t *testing.T, store user.Store)) {
	t.Run("sqlite", func(t *testing.T) {
		store, err := user.NewSQLiteStore(filepath.Join(t.TempDir(), "user.db"), "")
		require.Nil(t, err)
		t.Cleanup(func() { store.Close() })
		f(t, store)
	})
	t.Run("postgres", func(t *testing.T) {
		testDB := dbtest.CreateTestPostgres(t)
		store, err := user.NewPostgresStore(testDB)
		require.Nil(t, err)
		f(t, store)
	})
}

func TestStoreAddUser(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)
		require.Equal(t, "phil", u.Name)
		require.Equal(t, user.RoleUser, u.Role)
		require.False(t, u.Provisioned)
		require.NotEmpty(t, u.ID)
		require.NotEmpty(t, u.SyncTopic)
	})
}

func TestStoreAddUserAlreadyExists(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		require.Equal(t, user.ErrUserExists, store.AddUser("phil", "philhash", user.RoleUser, false))
	})
}

func TestStoreRemoveUser(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)
		require.Equal(t, "phil", u.Name)

		require.Nil(t, store.RemoveUser("phil"))
		_, err = store.User("phil")
		require.Equal(t, user.ErrUserNotFound, err)
	})
}

func TestStoreUserByID(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleAdmin, false))
		u, err := store.User("phil")
		require.Nil(t, err)

		u2, err := store.UserByID(u.ID)
		require.Nil(t, err)
		require.Equal(t, u.Name, u2.Name)
		require.Equal(t, u.ID, u2.ID)
	})
}

func TestStoreUserByToken(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)

		tk, err := store.CreateToken(u.ID, "tk_test123", "test token", time.Now(), netip.MustParseAddr("1.2.3.4"), time.Now().Add(24*time.Hour), false)
		require.Nil(t, err)
		require.Equal(t, "tk_test123", tk.Value)

		u2, err := store.UserByToken(tk.Value)
		require.Nil(t, err)
		require.Equal(t, "phil", u2.Name)
	})
}

func TestStoreUserByStripeCustomer(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		require.Nil(t, store.ChangeBilling("phil", &user.Billing{
			StripeCustomerID:     "cus_test123",
			StripeSubscriptionID: "sub_test123",
		}))

		u, err := store.UserByStripeCustomer("cus_test123")
		require.Nil(t, err)
		require.Equal(t, "phil", u.Name)
		require.Equal(t, "cus_test123", u.Billing.StripeCustomerID)
	})
}

func TestStoreUsers(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		require.Nil(t, store.AddUser("ben", "benhash", user.RoleAdmin, false))

		users, err := store.Users()
		require.Nil(t, err)
		require.True(t, len(users) >= 3) // phil, ben, and the everyone user
	})
}

func TestStoreUsersCount(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		count, err := store.UsersCount()
		require.Nil(t, err)
		require.True(t, count >= 1) // At least the everyone user

		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		count2, err := store.UsersCount()
		require.Nil(t, err)
		require.Equal(t, count+1, count2)
	})
}

func TestStoreChangePassword(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)
		require.Equal(t, "philhash", u.Hash)

		require.Nil(t, store.ChangePassword("phil", "newhash"))
		u, err = store.User("phil")
		require.Nil(t, err)
		require.Equal(t, "newhash", u.Hash)
	})
}

func TestStoreChangeRole(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)
		require.Equal(t, user.RoleUser, u.Role)

		require.Nil(t, store.ChangeRole("phil", user.RoleAdmin))
		u, err = store.User("phil")
		require.Nil(t, err)
		require.Equal(t, user.RoleAdmin, u.Role)
	})
}

func TestStoreTokens(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)

		now := time.Now()
		expires := now.Add(24 * time.Hour)
		origin := netip.MustParseAddr("9.9.9.9")

		tk, err := store.CreateToken(u.ID, "tk_abc", "my token", now, origin, expires, false)
		require.Nil(t, err)
		require.Equal(t, "tk_abc", tk.Value)
		require.Equal(t, "my token", tk.Label)

		// Get single token
		tk2, err := store.Token(u.ID, "tk_abc")
		require.Nil(t, err)
		require.Equal(t, "tk_abc", tk2.Value)
		require.Equal(t, "my token", tk2.Label)

		// Get all tokens
		tokens, err := store.Tokens(u.ID)
		require.Nil(t, err)
		require.Len(t, tokens, 1)
		require.Equal(t, "tk_abc", tokens[0].Value)

		// Token count
		count, err := store.TokenCount(u.ID)
		require.Nil(t, err)
		require.Equal(t, 1, count)
	})
}

func TestStoreTokenChangeLabel(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)

		_, err = store.CreateToken(u.ID, "tk_abc", "old label", time.Now(), netip.MustParseAddr("1.2.3.4"), time.Now().Add(time.Hour), false)
		require.Nil(t, err)

		require.Nil(t, store.ChangeTokenLabel(u.ID, "tk_abc", "new label"))
		tk, err := store.Token(u.ID, "tk_abc")
		require.Nil(t, err)
		require.Equal(t, "new label", tk.Label)
	})
}

func TestStoreTokenRemove(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)

		_, err = store.CreateToken(u.ID, "tk_abc", "label", time.Now(), netip.MustParseAddr("1.2.3.4"), time.Now().Add(time.Hour), false)
		require.Nil(t, err)

		require.Nil(t, store.RemoveToken(u.ID, "tk_abc"))
		_, err = store.Token(u.ID, "tk_abc")
		require.Equal(t, user.ErrTokenNotFound, err)
	})
}

func TestStoreTokenRemoveExpired(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)

		// Create expired token and active token
		_, err = store.CreateToken(u.ID, "tk_expired", "expired", time.Now(), netip.MustParseAddr("1.2.3.4"), time.Now().Add(-time.Hour), false)
		require.Nil(t, err)
		_, err = store.CreateToken(u.ID, "tk_active", "active", time.Now(), netip.MustParseAddr("1.2.3.4"), time.Now().Add(time.Hour), false)
		require.Nil(t, err)

		require.Nil(t, store.RemoveExpiredTokens())

		// Expired token should be gone
		_, err = store.Token(u.ID, "tk_expired")
		require.Equal(t, user.ErrTokenNotFound, err)

		// Active token should still exist
		tk, err := store.Token(u.ID, "tk_active")
		require.Nil(t, err)
		require.Equal(t, "tk_active", tk.Value)
	})
}

func TestStoreTokenRemoveExcess(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)

		// Create 3 tokens with increasing expiry
		for i, name := range []string{"tk_a", "tk_b", "tk_c"} {
			_, err = store.CreateToken(u.ID, name, name, time.Now(), netip.MustParseAddr("1.2.3.4"), time.Now().Add(time.Duration(i+1)*time.Hour), false)
			require.Nil(t, err)
		}

		count, err := store.TokenCount(u.ID)
		require.Nil(t, err)
		require.Equal(t, 3, count)

		// Remove excess, keep only 2 (the ones with latest expiry: tk_b, tk_c)
		require.Nil(t, store.RemoveExcessTokens(u.ID, 2))

		count, err = store.TokenCount(u.ID)
		require.Nil(t, err)
		require.Equal(t, 2, count)

		// tk_a should be removed (earliest expiry)
		_, err = store.Token(u.ID, "tk_a")
		require.Equal(t, user.ErrTokenNotFound, err)

		// tk_b and tk_c should remain
		_, err = store.Token(u.ID, "tk_b")
		require.Nil(t, err)
		_, err = store.Token(u.ID, "tk_c")
		require.Nil(t, err)
	})
}

func TestStoreTokenUpdateLastAccess(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)

		_, err = store.CreateToken(u.ID, "tk_abc", "label", time.Now(), netip.MustParseAddr("1.2.3.4"), time.Now().Add(time.Hour), false)
		require.Nil(t, err)

		newTime := time.Now().Add(5 * time.Minute)
		newOrigin := netip.MustParseAddr("5.5.5.5")
		require.Nil(t, store.UpdateTokenLastAccess("tk_abc", newTime, newOrigin))

		tk, err := store.Token(u.ID, "tk_abc")
		require.Nil(t, err)
		require.Equal(t, newTime.Unix(), tk.LastAccess.Unix())
		require.Equal(t, newOrigin, tk.LastOrigin)
	})
}

func TestStoreAllowAccess(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))

		require.Nil(t, store.AllowAccess("phil", "mytopic", true, true, "", false))
		grants, err := store.Grants("phil")
		require.Nil(t, err)
		require.Len(t, grants, 1)
		require.Equal(t, "mytopic", grants[0].TopicPattern)
		require.True(t, grants[0].Permission.IsReadWrite())
	})
}

func TestStoreAllowAccessReadOnly(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))

		require.Nil(t, store.AllowAccess("phil", "announcements", true, false, "", false))
		grants, err := store.Grants("phil")
		require.Nil(t, err)
		require.Len(t, grants, 1)
		require.True(t, grants[0].Permission.IsRead())
		require.False(t, grants[0].Permission.IsWrite())
	})
}

func TestStoreResetAccess(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		require.Nil(t, store.AllowAccess("phil", "topic1", true, true, "", false))
		require.Nil(t, store.AllowAccess("phil", "topic2", true, false, "", false))

		grants, err := store.Grants("phil")
		require.Nil(t, err)
		require.Len(t, grants, 2)

		require.Nil(t, store.ResetAccess("phil", "topic1"))
		grants, err = store.Grants("phil")
		require.Nil(t, err)
		require.Len(t, grants, 1)
		require.Equal(t, "topic2", grants[0].TopicPattern)
	})
}

func TestStoreResetAccessAll(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		require.Nil(t, store.AllowAccess("phil", "topic1", true, true, "", false))
		require.Nil(t, store.AllowAccess("phil", "topic2", true, false, "", false))

		require.Nil(t, store.ResetAccess("phil", ""))
		grants, err := store.Grants("phil")
		require.Nil(t, err)
		require.Len(t, grants, 0)
	})
}

func TestStoreAuthorizeTopicAccess(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		require.Nil(t, store.AllowAccess("phil", "mytopic", true, true, "", false))

		read, write, found, err := store.AuthorizeTopicAccess("phil", "mytopic")
		require.Nil(t, err)
		require.True(t, found)
		require.True(t, read)
		require.True(t, write)
	})
}

func TestStoreAuthorizeTopicAccessNotFound(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))

		_, _, found, err := store.AuthorizeTopicAccess("phil", "other")
		require.Nil(t, err)
		require.False(t, found)
	})
}

func TestStoreAuthorizeTopicAccessDenyAll(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		require.Nil(t, store.AllowAccess("phil", "secret", false, false, "", false))

		read, write, found, err := store.AuthorizeTopicAccess("phil", "secret")
		require.Nil(t, err)
		require.True(t, found)
		require.False(t, read)
		require.False(t, write)
	})
}

func TestStoreReservations(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		require.Nil(t, store.AllowAccess("phil", "mytopic", true, true, "phil", false))
		require.Nil(t, store.AllowAccess(user.Everyone, "mytopic", true, false, "phil", false))

		reservations, err := store.Reservations("phil")
		require.Nil(t, err)
		require.Len(t, reservations, 1)
		require.Equal(t, "mytopic", reservations[0].Topic)
		require.True(t, reservations[0].Owner.IsReadWrite())
		require.True(t, reservations[0].Everyone.IsRead())
		require.False(t, reservations[0].Everyone.IsWrite())
	})
}

func TestStoreReservationsCount(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		require.Nil(t, store.AllowAccess("phil", "topic1", true, true, "phil", false))
		require.Nil(t, store.AllowAccess("phil", "topic2", true, true, "phil", false))

		count, err := store.ReservationsCount("phil")
		require.Nil(t, err)
		require.Equal(t, int64(2), count)
	})
}

func TestStoreHasReservation(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		require.Nil(t, store.AllowAccess("phil", "mytopic", true, true, "phil", false))

		has, err := store.HasReservation("phil", "mytopic")
		require.Nil(t, err)
		require.True(t, has)

		has, err = store.HasReservation("phil", "other")
		require.Nil(t, err)
		require.False(t, has)
	})
}

func TestStoreReservationOwner(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		require.Nil(t, store.AllowAccess("phil", "mytopic", true, true, "phil", false))

		owner, err := store.ReservationOwner("mytopic")
		require.Nil(t, err)
		require.NotEmpty(t, owner) // Returns the user ID

		owner, err = store.ReservationOwner("unowned")
		require.Nil(t, err)
		require.Empty(t, owner)
	})
}

func TestStoreTiers(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		tier := &user.Tier{
			ID:                       "ti_test",
			Code:                     "pro",
			Name:                     "Pro",
			MessageLimit:             5000,
			MessageExpiryDuration:    24 * time.Hour,
			EmailLimit:               100,
			CallLimit:                10,
			ReservationLimit:         20,
			AttachmentFileSizeLimit:  10 * 1024 * 1024,
			AttachmentTotalSizeLimit: 100 * 1024 * 1024,
			AttachmentExpiryDuration: 48 * time.Hour,
			AttachmentBandwidthLimit: 500 * 1024 * 1024,
		}
		require.Nil(t, store.AddTier(tier))

		// Get by code
		t2, err := store.Tier("pro")
		require.Nil(t, err)
		require.Equal(t, "ti_test", t2.ID)
		require.Equal(t, "pro", t2.Code)
		require.Equal(t, "Pro", t2.Name)
		require.Equal(t, int64(5000), t2.MessageLimit)
		require.Equal(t, int64(100), t2.EmailLimit)
		require.Equal(t, int64(10), t2.CallLimit)
		require.Equal(t, int64(20), t2.ReservationLimit)

		// List all tiers
		tiers, err := store.Tiers()
		require.Nil(t, err)
		require.Len(t, tiers, 1)
		require.Equal(t, "pro", tiers[0].Code)
	})
}

func TestStoreTierUpdate(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		tier := &user.Tier{
			ID:   "ti_test",
			Code: "pro",
			Name: "Pro",
		}
		require.Nil(t, store.AddTier(tier))

		tier.Name = "Professional"
		tier.MessageLimit = 9999
		require.Nil(t, store.UpdateTier(tier))

		t2, err := store.Tier("pro")
		require.Nil(t, err)
		require.Equal(t, "Professional", t2.Name)
		require.Equal(t, int64(9999), t2.MessageLimit)
	})
}

func TestStoreTierRemove(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		tier := &user.Tier{
			ID:   "ti_test",
			Code: "pro",
			Name: "Pro",
		}
		require.Nil(t, store.AddTier(tier))

		t2, err := store.Tier("pro")
		require.Nil(t, err)
		require.Equal(t, "pro", t2.Code)

		require.Nil(t, store.RemoveTier("pro"))
		_, err = store.Tier("pro")
		require.Equal(t, user.ErrTierNotFound, err)
	})
}

func TestStoreTierByStripePrice(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		tier := &user.Tier{
			ID:                   "ti_test",
			Code:                 "pro",
			Name:                 "Pro",
			StripeMonthlyPriceID: "price_monthly",
			StripeYearlyPriceID:  "price_yearly",
		}
		require.Nil(t, store.AddTier(tier))

		t2, err := store.TierByStripePrice("price_monthly")
		require.Nil(t, err)
		require.Equal(t, "pro", t2.Code)

		t3, err := store.TierByStripePrice("price_yearly")
		require.Nil(t, err)
		require.Equal(t, "pro", t3.Code)
	})
}

func TestStoreChangeTier(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		tier := &user.Tier{
			ID:   "ti_test",
			Code: "pro",
			Name: "Pro",
		}
		require.Nil(t, store.AddTier(tier))
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		require.Nil(t, store.ChangeTier("phil", "pro"))

		u, err := store.User("phil")
		require.Nil(t, err)
		require.NotNil(t, u.Tier)
		require.Equal(t, "pro", u.Tier.Code)
	})
}

func TestStorePhoneNumbers(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)

		require.Nil(t, store.AddPhoneNumber(u.ID, "+1234567890"))
		require.Nil(t, store.AddPhoneNumber(u.ID, "+0987654321"))

		numbers, err := store.PhoneNumbers(u.ID)
		require.Nil(t, err)
		require.Len(t, numbers, 2)

		require.Nil(t, store.RemovePhoneNumber(u.ID, "+1234567890"))
		numbers, err = store.PhoneNumbers(u.ID)
		require.Nil(t, err)
		require.Len(t, numbers, 1)
		require.Equal(t, "+0987654321", numbers[0])
	})
}

func TestStoreChangeSettings(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)

		lang := "de"
		prefs := &user.Prefs{Language: &lang}
		require.Nil(t, store.ChangeSettings(u.ID, prefs))

		u2, err := store.User("phil")
		require.Nil(t, err)
		require.NotNil(t, u2.Prefs)
		require.Equal(t, "de", *u2.Prefs.Language)
	})
}

func TestStoreChangeBilling(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))

		billing := &user.Billing{
			StripeCustomerID:     "cus_123",
			StripeSubscriptionID: "sub_456",
		}
		require.Nil(t, store.ChangeBilling("phil", billing))

		u, err := store.User("phil")
		require.Nil(t, err)
		require.Equal(t, "cus_123", u.Billing.StripeCustomerID)
		require.Equal(t, "sub_456", u.Billing.StripeSubscriptionID)
	})
}

func TestStoreUpdateStats(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)

		stats := &user.Stats{Messages: 42, Emails: 3, Calls: 1}
		require.Nil(t, store.UpdateStats(u.ID, stats))

		u2, err := store.User("phil")
		require.Nil(t, err)
		require.Equal(t, int64(42), u2.Stats.Messages)
		require.Equal(t, int64(3), u2.Stats.Emails)
		require.Equal(t, int64(1), u2.Stats.Calls)
	})
}

func TestStoreResetStats(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)

		require.Nil(t, store.UpdateStats(u.ID, &user.Stats{Messages: 42, Emails: 3, Calls: 1}))
		require.Nil(t, store.ResetStats())

		u2, err := store.User("phil")
		require.Nil(t, err)
		require.Equal(t, int64(0), u2.Stats.Messages)
		require.Equal(t, int64(0), u2.Stats.Emails)
		require.Equal(t, int64(0), u2.Stats.Calls)
	})
}

func TestStoreMarkUserRemoved(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)

		require.Nil(t, store.MarkUserRemoved(u.ID))

		u2, err := store.User("phil")
		require.Nil(t, err)
		require.True(t, u2.Deleted)
	})
}

func TestStoreRemoveDeletedUsers(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		u, err := store.User("phil")
		require.Nil(t, err)

		require.Nil(t, store.MarkUserRemoved(u.ID))

		// RemoveDeletedUsers only removes users past the hard-delete duration (7 days).
		// Immediately after marking, the user should still exist.
		require.Nil(t, store.RemoveDeletedUsers())
		u2, err := store.User("phil")
		require.Nil(t, err)
		require.True(t, u2.Deleted)
	})
}

func TestStoreAllGrants(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		require.Nil(t, store.AddUser("ben", "benhash", user.RoleUser, false))
		phil, err := store.User("phil")
		require.Nil(t, err)
		ben, err := store.User("ben")
		require.Nil(t, err)

		require.Nil(t, store.AllowAccess("phil", "topic1", true, true, "", false))
		require.Nil(t, store.AllowAccess("ben", "topic2", true, false, "", false))

		grants, err := store.AllGrants()
		require.Nil(t, err)
		require.Contains(t, grants, phil.ID)
		require.Contains(t, grants, ben.ID)
	})
}

func TestStoreOtherAccessCount(t *testing.T) {
	forEachStoreBackend(t, func(t *testing.T, store user.Store) {
		require.Nil(t, store.AddUser("phil", "philhash", user.RoleUser, false))
		require.Nil(t, store.AddUser("ben", "benhash", user.RoleUser, false))
		require.Nil(t, store.AllowAccess("ben", "mytopic", true, true, "ben", false))

		count, err := store.OtherAccessCount("phil", "mytopic")
		require.Nil(t, err)
		require.Equal(t, 1, count)
	})
}
