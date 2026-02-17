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

func newTestPostgresStore(t *testing.T) webpush.Store {
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

func TestPostgresStoreUpsertSubscriptionSubscriptionsForTopic(t *testing.T) {
	testStoreUpsertSubscriptionSubscriptionsForTopic(t, newTestPostgresStore(t))
}

func TestPostgresStoreUpsertSubscriptionSubscriberIPLimitReached(t *testing.T) {
	testStoreUpsertSubscriptionSubscriberIPLimitReached(t, newTestPostgresStore(t))
}

func TestPostgresStoreUpsertSubscriptionUpdateTopics(t *testing.T) {
	testStoreUpsertSubscriptionUpdateTopics(t, newTestPostgresStore(t))
}

func TestPostgresStoreUpsertSubscriptionUpdateFields(t *testing.T) {
	testStoreUpsertSubscriptionUpdateFields(t, newTestPostgresStore(t))
}

func TestPostgresStoreRemoveByUserIDMultiple(t *testing.T) {
	testStoreRemoveByUserIDMultiple(t, newTestPostgresStore(t))
}

func TestPostgresStoreRemoveByEndpoint(t *testing.T) {
	testStoreRemoveByEndpoint(t, newTestPostgresStore(t))
}

func TestPostgresStoreRemoveByUserID(t *testing.T) {
	testStoreRemoveByUserID(t, newTestPostgresStore(t))
}

func TestPostgresStoreRemoveByUserIDEmpty(t *testing.T) {
	testStoreRemoveByUserIDEmpty(t, newTestPostgresStore(t))
}

func TestPostgresStoreExpiryWarningSent(t *testing.T) {
	store := newTestPostgresStore(t)
	testStoreExpiryWarningSent(t, store, store.SetSubscriptionUpdatedAt)
}

func TestPostgresStoreExpiring(t *testing.T) {
	store := newTestPostgresStore(t)
	testStoreExpiring(t, store, store.SetSubscriptionUpdatedAt)
}

func TestPostgresStoreRemoveExpired(t *testing.T) {
	store := newTestPostgresStore(t)
	testStoreRemoveExpired(t, store, store.SetSubscriptionUpdatedAt)
}
