package webpush_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/webpush"
)

func newTestSQLiteStore(t *testing.T) *webpush.SQLiteStore {
	store, err := webpush.NewSQLiteStore(filepath.Join(t.TempDir(), "webpush.db"), "")
	require.Nil(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSQLiteStore_UpsertSubscription_SubscriptionsForTopic(t *testing.T) {
	testStoreUpsertSubscription_SubscriptionsForTopic(t, newTestSQLiteStore(t))
}

func TestSQLiteStore_UpsertSubscription_SubscriberIPLimitReached(t *testing.T) {
	testStoreUpsertSubscription_SubscriberIPLimitReached(t, newTestSQLiteStore(t))
}

func TestSQLiteStore_UpsertSubscription_UpdateTopics(t *testing.T) {
	testStoreUpsertSubscription_UpdateTopics(t, newTestSQLiteStore(t))
}

func TestSQLiteStore_RemoveSubscriptionsByEndpoint(t *testing.T) {
	testStoreRemoveSubscriptionsByEndpoint(t, newTestSQLiteStore(t))
}

func TestSQLiteStore_RemoveSubscriptionsByUserID(t *testing.T) {
	testStoreRemoveSubscriptionsByUserID(t, newTestSQLiteStore(t))
}

func TestSQLiteStore_RemoveSubscriptionsByUserID_Empty(t *testing.T) {
	testStoreRemoveSubscriptionsByUserID_Empty(t, newTestSQLiteStore(t))
}

func TestSQLiteStore_MarkExpiryWarningSent(t *testing.T) {
	store := newTestSQLiteStore(t)
	testStoreMarkExpiryWarningSent(t, store, store.SetSubscriptionUpdatedAt)
}

func TestSQLiteStore_SubscriptionsExpiring(t *testing.T) {
	store := newTestSQLiteStore(t)
	testStoreSubscriptionsExpiring(t, store, store.SetSubscriptionUpdatedAt)
}

func TestSQLiteStore_RemoveExpiredSubscriptions(t *testing.T) {
	store := newTestSQLiteStore(t)
	testStoreRemoveExpiredSubscriptions(t, store, store.SetSubscriptionUpdatedAt)
}
