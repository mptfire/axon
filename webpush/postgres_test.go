package webpush_test

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/util"
	"heckel.io/ntfy/v2/webpush"
)

func newTestPostgresStore(t *testing.T) *webpush.PostgresStore {
	dsn := os.Getenv("NTFY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("NTFY_TEST_DATABASE_URL not set, skipping PostgreSQL tests")
	}
	// Create a unique schema for this test
	schema := fmt.Sprintf("test_%s", util.RandomString(10))
	setupDB, err := sql.Open("pgx", dsn)
	require.Nil(t, err)
	_, err = setupDB.Exec(fmt.Sprintf("CREATE SCHEMA %s", schema))
	require.Nil(t, err)
	require.Nil(t, setupDB.Close())
	// Open store with search_path set to the new schema
	u, err := url.Parse(dsn)
	require.Nil(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	store, err := webpush.NewPostgresStore(u.String())
	require.Nil(t, err)
	t.Cleanup(func() {
		store.Close()
		cleanDB, err := sql.Open("pgx", dsn)
		if err == nil {
			cleanDB.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE", schema))
			cleanDB.Close()
		}
	})
	return store
}

func TestPostgresStore_UpsertSubscription_SubscriptionsForTopic(t *testing.T) {
	testStoreUpsertSubscription_SubscriptionsForTopic(t, newTestPostgresStore(t))
}

func TestPostgresStore_UpsertSubscription_SubscriberIPLimitReached(t *testing.T) {
	testStoreUpsertSubscription_SubscriberIPLimitReached(t, newTestPostgresStore(t))
}

func TestPostgresStore_UpsertSubscription_UpdateTopics(t *testing.T) {
	testStoreUpsertSubscription_UpdateTopics(t, newTestPostgresStore(t))
}

func TestPostgresStore_RemoveSubscriptionsByEndpoint(t *testing.T) {
	testStoreRemoveSubscriptionsByEndpoint(t, newTestPostgresStore(t))
}

func TestPostgresStore_RemoveSubscriptionsByUserID(t *testing.T) {
	testStoreRemoveSubscriptionsByUserID(t, newTestPostgresStore(t))
}

func TestPostgresStore_RemoveSubscriptionsByUserID_Empty(t *testing.T) {
	testStoreRemoveSubscriptionsByUserID_Empty(t, newTestPostgresStore(t))
}

func TestPostgresStore_MarkExpiryWarningSent(t *testing.T) {
	store := newTestPostgresStore(t)
	testStoreMarkExpiryWarningSent(t, store, store.SetSubscriptionUpdatedAt)
}

func TestPostgresStore_SubscriptionsExpiring(t *testing.T) {
	store := newTestPostgresStore(t)
	testStoreSubscriptionsExpiring(t, store, store.SetSubscriptionUpdatedAt)
}

func TestPostgresStore_RemoveExpiredSubscriptions(t *testing.T) {
	store := newTestPostgresStore(t)
	testStoreRemoveExpiredSubscriptions(t, store, store.SetSubscriptionUpdatedAt)
}
