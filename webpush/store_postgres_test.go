package webpush_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	dbtest "heckel.io/ntfy/v2/db/test"
	"heckel.io/ntfy/v2/webpush"
)

func newTestPostgresStore(t *testing.T) webpush.Store {
	testDB := dbtest.CreateTestDB(t)
	store, err := webpush.NewPostgresStore(testDB)
	require.Nil(t, err)
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
