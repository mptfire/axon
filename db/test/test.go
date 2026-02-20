package dbtest

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/db"
	"heckel.io/ntfy/v2/util"
)

const testPoolMaxConns = "2"

// CreateTestSchema creates a temporary PostgreSQL schema and returns the DSN pointing to it.
// It registers a cleanup function to drop the schema when the test finishes.
// If NTFY_TEST_DATABASE_URL is not set, the test is skipped.
func CreateTestSchema(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("NTFY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("NTFY_TEST_DATABASE_URL not set")
	}
	schema := fmt.Sprintf("test_%s", util.RandomString(10))
	u, err := url.Parse(dsn)
	require.Nil(t, err)
	q := u.Query()
	q.Set("pool_max_conns", testPoolMaxConns)
	u.RawQuery = q.Encode()
	dsn = u.String()
	setupDB, err := db.Open(dsn)
	require.Nil(t, err)
	_, err = setupDB.Exec(fmt.Sprintf("CREATE SCHEMA %s", schema))
	require.Nil(t, err)
	require.Nil(t, setupDB.Close())
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	schemaDSN := u.String()
	t.Cleanup(func() {
		cleanDB, err := db.Open(dsn)
		if err == nil {
			cleanDB.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE", schema))
			cleanDB.Close()
		}
	})
	return schemaDSN
}

// CreateTestDB creates a temporary PostgreSQL schema and returns an open *sql.DB connection to it.
// It registers cleanup functions to close the DB and drop the schema when the test finishes.
// If NTFY_TEST_DATABASE_URL is not set, the test is skipped.
func CreateTestDB(t *testing.T) *sql.DB {
	t.Helper()
	schemaDSN := CreateTestSchema(t)
	testDB, err := db.Open(schemaDSN)
	require.Nil(t, err)
	t.Cleanup(func() {
		testDB.Close()
	})
	return testDB
}
