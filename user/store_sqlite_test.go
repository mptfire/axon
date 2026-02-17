package user_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/user"
)

func newTestSQLiteStore(t *testing.T) user.Store {
	store, err := user.NewSQLiteStore(filepath.Join(t.TempDir(), "user.db"), "")
	require.Nil(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSQLiteStoreAddUser(t *testing.T) {
	testStoreAddUser(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreAddUserAlreadyExists(t *testing.T) {
	testStoreAddUserAlreadyExists(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreRemoveUser(t *testing.T) {
	testStoreRemoveUser(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreUserByID(t *testing.T) {
	testStoreUserByID(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreUserByToken(t *testing.T) {
	testStoreUserByToken(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreUserByStripeCustomer(t *testing.T) {
	testStoreUserByStripeCustomer(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreUsers(t *testing.T) {
	testStoreUsers(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreUsersCount(t *testing.T) {
	testStoreUsersCount(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreChangePassword(t *testing.T) {
	testStoreChangePassword(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreChangeRole(t *testing.T) {
	testStoreChangeRole(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreTokens(t *testing.T) {
	testStoreTokens(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreTokenChangeLabel(t *testing.T) {
	testStoreTokenChangeLabel(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreTokenRemove(t *testing.T) {
	testStoreTokenRemove(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreTokenRemoveExpired(t *testing.T) {
	testStoreTokenRemoveExpired(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreTokenRemoveExcess(t *testing.T) {
	testStoreTokenRemoveExcess(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreTokenUpdateLastAccess(t *testing.T) {
	testStoreTokenUpdateLastAccess(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreAllowAccess(t *testing.T) {
	testStoreAllowAccess(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreAllowAccessReadOnly(t *testing.T) {
	testStoreAllowAccessReadOnly(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreResetAccess(t *testing.T) {
	testStoreResetAccess(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreResetAccessAll(t *testing.T) {
	testStoreResetAccessAll(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreAuthorizeTopicAccess(t *testing.T) {
	testStoreAuthorizeTopicAccess(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreAuthorizeTopicAccessNotFound(t *testing.T) {
	testStoreAuthorizeTopicAccessNotFound(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreAuthorizeTopicAccessDenyAll(t *testing.T) {
	testStoreAuthorizeTopicAccessDenyAll(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreReservations(t *testing.T) {
	testStoreReservations(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreReservationsCount(t *testing.T) {
	testStoreReservationsCount(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreHasReservation(t *testing.T) {
	testStoreHasReservation(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreReservationOwner(t *testing.T) {
	testStoreReservationOwner(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreTiers(t *testing.T) {
	testStoreTiers(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreTierUpdate(t *testing.T) {
	testStoreTierUpdate(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreTierRemove(t *testing.T) {
	testStoreTierRemove(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreTierByStripePrice(t *testing.T) {
	testStoreTierByStripePrice(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreChangeTier(t *testing.T) {
	testStoreChangeTier(t, newTestSQLiteStore(t))
}

func TestSQLiteStorePhoneNumbers(t *testing.T) {
	testStorePhoneNumbers(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreChangeSettings(t *testing.T) {
	testStoreChangeSettings(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreChangeBilling(t *testing.T) {
	testStoreChangeBilling(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreUpdateStats(t *testing.T) {
	testStoreUpdateStats(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreResetStats(t *testing.T) {
	testStoreResetStats(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreMarkUserRemoved(t *testing.T) {
	testStoreMarkUserRemoved(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreRemoveDeletedUsers(t *testing.T) {
	testStoreRemoveDeletedUsers(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreAllGrants(t *testing.T) {
	testStoreAllGrants(t, newTestSQLiteStore(t))
}

func TestSQLiteStoreOtherAccessCount(t *testing.T) {
	testStoreOtherAccessCount(t, newTestSQLiteStore(t))
}
