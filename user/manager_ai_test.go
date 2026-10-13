package user

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManager_AIConsent_AccountIsolationAndRevocation(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		require.NoError(t, a.AddUser("alice", "alice", RoleUser, false))
		require.NoError(t, a.AddUser("bob", "bob", RoleUser, false))
		alice, err := a.User("alice")
		require.NoError(t, err)
		bob, err := a.User("bob")
		require.NoError(t, err)
		ctx := context.Background()
		consent, err := a.AIConsent(ctx, alice.ID)
		require.NoError(t, err)
		require.Nil(t, consent)
		configuration := strings.Repeat("a", 64)
		require.NoError(t, a.GrantAIConsent(ctx, alice.ID, configuration))
		consent, err = a.AIConsent(ctx, alice.ID)
		require.NoError(t, err)
		require.Equal(t, configuration, consent.ProviderConfig)
		require.False(t, consent.GrantedAt.IsZero())
		consent, err = a.AIConsent(ctx, bob.ID)
		require.NoError(t, err)
		require.Nil(t, consent)
		require.NoError(t, a.RevokeAIConsent(ctx, alice.ID))
		language := "en"
		require.NoError(t, a.ChangeSettings(alice.ID, &Prefs{Language: &language}))
		consent, err = a.AIConsent(ctx, alice.ID)
		require.NoError(t, err)
		require.Nil(t, consent, "settings must not restore revoked consent")
		require.NoError(t, a.RevokeAIConsent(ctx, alice.ID))
	})
}

func TestManager_AIConsent_PersistsAcrossManagers(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		require.NoError(t, a.AddUser("alice", "alice", RoleUser, false))
		alice, err := a.User("alice")
		require.NoError(t, err)
		configuration := strings.Repeat("b", 64)
		require.NoError(t, a.GrantAIConsent(context.Background(), alice.ID, configuration))
		b := newTestManager(t, newManager, PermissionDenyAll)
		consent, err := b.AIConsent(context.Background(), alice.ID)
		require.NoError(t, err)
		require.Equal(t, configuration, consent.ProviderConfig)
	})
}

func TestManager_AIConsent_MigrationFrom12StartsWithoutConsent(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		require.NoError(t, a.AddUser("alice", "alice", RoleUser, false))
		alice, err := a.User("alice")
		require.NoError(t, err)
		_, err = a.db.Exec(`DROP TABLE user_ai_consent`)
		require.NoError(t, err)
		_, err = a.db.Exec(`DROP TABLE user_ai_consent_meta`)
		require.NoError(t, err)
		if strings.HasSuffix(t.Name(), "/postgres") {
			_, err = a.db.Exec(`UPDATE schema_version SET version = 12 WHERE store = 'user'`)
		} else {
			_, err = a.db.Exec(`UPDATE schemaVersion SET version = 12 WHERE id = 1`)
		}
		require.NoError(t, err)
		require.NoError(t, a.Close())
		b := newTestManager(t, newManager, PermissionDenyAll)
		consent, err := b.AIConsent(context.Background(), alice.ID)
		require.NoError(t, err)
		require.Nil(t, consent, "upgrading an existing account must not imply consent")
		secret, err := b.AIConsentSecret(context.Background())
		require.NoError(t, err)
		require.Len(t, secret, 32)
		require.NoError(t, b.GrantAIConsent(context.Background(), alice.ID, strings.Repeat("a", 64)))
		c := newTestManager(t, newManager, PermissionDenyAll)
		consent, err = c.AIConsent(context.Background(), alice.ID)
		require.NoError(t, err)
		require.NotNil(t, consent, "reopening must not rerun the migration")
	})
}

func TestManager_AIConsent_SecretStableUnderConcurrentInitialization(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		b := newTestManager(t, newManager, PermissionDenyAll)
		type result struct {
			secret []byte
			err    error
		}
		results := make(chan result, 8)
		var wg sync.WaitGroup
		for i := 0; i < cap(results); i++ {
			wg.Add(1)
			go func(manager *Manager) {
				defer wg.Done()
				secret, err := manager.AIConsentSecret(context.Background())
				results <- result{secret, err}
			}([]*Manager{a, b}[i%2])
		}
		wg.Wait()
		close(results)
		var first []byte
		for r := range results {
			require.NoError(t, r.err)
			require.Len(t, r.secret, 32)
			if first == nil {
				first = r.secret
			}
			require.Equal(t, first, r.secret)
		}
		c := newTestManager(t, newManager, PermissionDenyAll)
		secret, err := c.AIConsentSecret(context.Background())
		require.NoError(t, err)
		require.Equal(t, first, secret)
	})
}

func TestManager_AIConsent_CancellationAndInvalidIdentity(t *testing.T) {
	forEachBackend(t, func(t *testing.T, newManager newManagerFunc) {
		a := newTestManager(t, newManager, PermissionDenyAll)
		require.NoError(t, a.AddUser("alice", "alice", RoleUser, false))
		u, err := a.User("alice")
		require.NoError(t, err)
		require.Error(t, a.GrantAIConsent(context.Background(), u.ID, "not a configuration"))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = a.AIConsent(ctx, u.ID)
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorIs(t, a.GrantAIConsent(ctx, u.ID, strings.Repeat("a", 64)), context.Canceled)
		require.ErrorIs(t, a.RevokeAIConsent(ctx, u.ID), context.Canceled)
		consent, err := a.AIConsent(context.Background(), u.ID)
		require.NoError(t, err)
		require.Nil(t, consent)
	})
}
