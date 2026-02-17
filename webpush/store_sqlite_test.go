package webpush_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/webpush"
)

func newTestSQLiteStore(t *testing.T) webpush.Store {
	store, err := webpush.NewSQLiteStore(filepath.Join(t.TempDir(), "webpush.db"), "")
	require.Nil(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSQLiteStoreUpsertSubscriptionSubscriptionsForTopic(t *testing.T) {
	testStoreUpsertSubscriptionSubscriptionsForTopic(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreUpsertSubscriptionSubscriberIPLimitReached(t *testing.T) {
	testStoreUpsertSubscriptionSubscriberIPLimitReached(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreUpsertSubscriptionUpdateTopics(t *testing.T) {
	testStoreUpsertSubscriptionUpdateTopics(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreUpsertSubscriptionUpdateFields(t *testing.T) {
	testStoreUpsertSubscriptionUpdateFields(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreRemoveByUserIDMultiple(t *testing.T) {
	testStoreRemoveByUserIDMultiple(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreRemoveByEndpoint(t *testing.T) {
	testStoreRemoveByEndpoint(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreRemoveByUserID(t *testing.T) {
	testStoreRemoveByUserID(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreRemoveByUserIDEmpty(t *testing.T) {
	testStoreRemoveByUserIDEmpty(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreExpiryWarningSent(t *testing.T) {
	store := newTestSQLiteStore(t)
	testStoreExpiryWarningSent(t, store, store.SetSubscriptionUpdatedAt)
}

func TestSQLiteStoreExpiring(t *testing.T) {
	store := newTestSQLiteStore(t)
	testStoreExpiring(t, store, store.SetSubscriptionUpdatedAt)
}

func TestSQLiteStoreRemoveExpired(t *testing.T) {
	store := newTestSQLiteStore(t)
	testStoreRemoveExpired(t, store, store.SetSubscriptionUpdatedAt)
}
