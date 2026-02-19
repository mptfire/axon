package message_test

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/message"
	"heckel.io/ntfy/v2/util"
)

func newTestPostgresStore(t *testing.T) message.Store {
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
	store, err := message.NewPostgresStore(u.String(), 0, 0)
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

func TestPostgresStore_Messages(t *testing.T) {
	testCacheMessages(t, newTestPostgresStore(t))
}

func TestPostgresStore_MessagesLock(t *testing.T) {
	testCacheMessagesLock(t, newTestPostgresStore(t))
}

func TestPostgresStore_MessagesScheduled(t *testing.T) {
	testCacheMessagesScheduled(t, newTestPostgresStore(t))
}

func TestPostgresStore_Topics(t *testing.T) {
	testCacheTopics(t, newTestPostgresStore(t))
}

func TestPostgresStore_MessagesTagsPrioAndTitle(t *testing.T) {
	testCacheMessagesTagsPrioAndTitle(t, newTestPostgresStore(t))
}

func TestPostgresStore_MessagesSinceID(t *testing.T) {
	testCacheMessagesSinceID(t, newTestPostgresStore(t))
}

func TestPostgresStore_Prune(t *testing.T) {
	testCachePrune(t, newTestPostgresStore(t))
}

func TestPostgresStore_Attachments(t *testing.T) {
	testCacheAttachments(t, newTestPostgresStore(t))
}

func TestPostgresStore_AttachmentsExpired(t *testing.T) {
	testCacheAttachmentsExpired(t, newTestPostgresStore(t))
}

func TestPostgresStore_Sender(t *testing.T) {
	testSender(t, newTestPostgresStore(t))
}

func TestPostgresStore_DeleteScheduledBySequenceID(t *testing.T) {
	testDeleteScheduledBySequenceID(t, newTestPostgresStore(t))
}

func TestPostgresStore_MessageByID(t *testing.T) {
	testMessageByID(t, newTestPostgresStore(t))
}

func TestPostgresStore_MarkPublished(t *testing.T) {
	testMarkPublished(t, newTestPostgresStore(t))
}

func TestPostgresStore_ExpireMessages(t *testing.T) {
	testExpireMessages(t, newTestPostgresStore(t))
}

func TestPostgresStore_MarkAttachmentsDeleted(t *testing.T) {
	testMarkAttachmentsDeleted(t, newTestPostgresStore(t))
}

func TestPostgresStore_Stats(t *testing.T) {
	testStats(t, newTestPostgresStore(t))
}

func TestPostgresStore_AddMessages(t *testing.T) {
	testAddMessages(t, newTestPostgresStore(t))
}

func TestPostgresStore_MessagesDue(t *testing.T) {
	testMessagesDue(t, newTestPostgresStore(t))
}

func TestPostgresStore_MessageFieldRoundTrip(t *testing.T) {
	testMessageFieldRoundTrip(t, newTestPostgresStore(t))
}
