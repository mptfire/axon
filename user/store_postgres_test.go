package user_test

import (
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/postgres"
	"heckel.io/ntfy/v2/user"
	"heckel.io/ntfy/v2/util"
)

func newTestPostgresStore(t *testing.T) user.Store {
	dsn := os.Getenv("NTFY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("NTFY_TEST_DATABASE_URL not set, skipping PostgreSQL tests")
	}
	schema := fmt.Sprintf("test_%s", util.RandomString(10))
	u, err := url.Parse(dsn)
	require.Nil(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	schemaDSN := u.String()
	setupDB, err := postgres.OpenDB(dsn)
	require.Nil(t, err)
	_, err = setupDB.Exec(fmt.Sprintf("CREATE SCHEMA %s", schema))
	require.Nil(t, err)
	require.Nil(t, setupDB.Close())
	db, err := postgres.OpenDB(schemaDSN)
	require.Nil(t, err)
	store, err := user.NewPostgresStore(db)
	require.Nil(t, err)
	t.Cleanup(func() {
		store.Close()
		cleanDB, err := postgres.OpenDB(dsn)
		if err == nil {
			cleanDB.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE", schema))
			cleanDB.Close()
		}
	})
	return store
}

func TestPostgresStoreAddUser(t *testing.T) {
	testStoreAddUser(t, newTestPostgresStore(t))
}

func TestPostgresStoreAddUserAlreadyExists(t *testing.T) {
	testStoreAddUserAlreadyExists(t, newTestPostgresStore(t))
}

func TestPostgresStoreRemoveUser(t *testing.T) {
	testStoreRemoveUser(t, newTestPostgresStore(t))
}

func TestPostgresStoreUserByID(t *testing.T) {
	testStoreUserByID(t, newTestPostgresStore(t))
}

func TestPostgresStoreUserByToken(t *testing.T) {
	testStoreUserByToken(t, newTestPostgresStore(t))
}

func TestPostgresStoreUserByStripeCustomer(t *testing.T) {
	testStoreUserByStripeCustomer(t, newTestPostgresStore(t))
}

func TestPostgresStoreUsers(t *testing.T) {
	testStoreUsers(t, newTestPostgresStore(t))
}

func TestPostgresStoreUsersCount(t *testing.T) {
	testStoreUsersCount(t, newTestPostgresStore(t))
}

func TestPostgresStoreChangePassword(t *testing.T) {
	testStoreChangePassword(t, newTestPostgresStore(t))
}

func TestPostgresStoreChangeRole(t *testing.T) {
	testStoreChangeRole(t, newTestPostgresStore(t))
}

func TestPostgresStoreTokens(t *testing.T) {
	testStoreTokens(t, newTestPostgresStore(t))
}

func TestPostgresStoreTokenChangeLabel(t *testing.T) {
	testStoreTokenChangeLabel(t, newTestPostgresStore(t))
}

func TestPostgresStoreTokenRemove(t *testing.T) {
	testStoreTokenRemove(t, newTestPostgresStore(t))
}

func TestPostgresStoreTokenRemoveExpired(t *testing.T) {
	testStoreTokenRemoveExpired(t, newTestPostgresStore(t))
}

func TestPostgresStoreTokenRemoveExcess(t *testing.T) {
	testStoreTokenRemoveExcess(t, newTestPostgresStore(t))
}

func TestPostgresStoreTokenUpdateLastAccess(t *testing.T) {
	testStoreTokenUpdateLastAccess(t, newTestPostgresStore(t))
}

func TestPostgresStoreAllowAccess(t *testing.T) {
	testStoreAllowAccess(t, newTestPostgresStore(t))
}

func TestPostgresStoreAllowAccessReadOnly(t *testing.T) {
	testStoreAllowAccessReadOnly(t, newTestPostgresStore(t))
}

func TestPostgresStoreResetAccess(t *testing.T) {
	testStoreResetAccess(t, newTestPostgresStore(t))
}

func TestPostgresStoreResetAccessAll(t *testing.T) {
	testStoreResetAccessAll(t, newTestPostgresStore(t))
}

func TestPostgresStoreAuthorizeTopicAccess(t *testing.T) {
	testStoreAuthorizeTopicAccess(t, newTestPostgresStore(t))
}

func TestPostgresStoreAuthorizeTopicAccessNotFound(t *testing.T) {
	testStoreAuthorizeTopicAccessNotFound(t, newTestPostgresStore(t))
}

func TestPostgresStoreAuthorizeTopicAccessDenyAll(t *testing.T) {
	testStoreAuthorizeTopicAccessDenyAll(t, newTestPostgresStore(t))
}

func TestPostgresStoreReservations(t *testing.T) {
	testStoreReservations(t, newTestPostgresStore(t))
}

func TestPostgresStoreReservationsCount(t *testing.T) {
	testStoreReservationsCount(t, newTestPostgresStore(t))
}

func TestPostgresStoreHasReservation(t *testing.T) {
	testStoreHasReservation(t, newTestPostgresStore(t))
}

func TestPostgresStoreReservationOwner(t *testing.T) {
	testStoreReservationOwner(t, newTestPostgresStore(t))
}

func TestPostgresStoreTiers(t *testing.T) {
	testStoreTiers(t, newTestPostgresStore(t))
}

func TestPostgresStoreTierUpdate(t *testing.T) {
	testStoreTierUpdate(t, newTestPostgresStore(t))
}

func TestPostgresStoreTierRemove(t *testing.T) {
	testStoreTierRemove(t, newTestPostgresStore(t))
}

func TestPostgresStoreTierByStripePrice(t *testing.T) {
	testStoreTierByStripePrice(t, newTestPostgresStore(t))
}

func TestPostgresStoreChangeTier(t *testing.T) {
	testStoreChangeTier(t, newTestPostgresStore(t))
}

func TestPostgresStorePhoneNumbers(t *testing.T) {
	testStorePhoneNumbers(t, newTestPostgresStore(t))
}

func TestPostgresStoreChangeSettings(t *testing.T) {
	testStoreChangeSettings(t, newTestPostgresStore(t))
}

func TestPostgresStoreChangeBilling(t *testing.T) {
	testStoreChangeBilling(t, newTestPostgresStore(t))
}

func TestPostgresStoreUpdateStats(t *testing.T) {
	testStoreUpdateStats(t, newTestPostgresStore(t))
}

func TestPostgresStoreResetStats(t *testing.T) {
	testStoreResetStats(t, newTestPostgresStore(t))
}

func TestPostgresStoreMarkUserRemoved(t *testing.T) {
	testStoreMarkUserRemoved(t, newTestPostgresStore(t))
}

func TestPostgresStoreRemoveDeletedUsers(t *testing.T) {
	testStoreRemoveDeletedUsers(t, newTestPostgresStore(t))
}

func TestPostgresStoreAllGrants(t *testing.T) {
	testStoreAllGrants(t, newTestPostgresStore(t))
}

func TestPostgresStoreOtherAccessCount(t *testing.T) {
	testStoreOtherAccessCount(t, newTestPostgresStore(t))
}
