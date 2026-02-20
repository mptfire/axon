package message_test

import (
	"testing"

	dbtest "heckel.io/ntfy/v2/db/test"
	"heckel.io/ntfy/v2/message"

	"github.com/stretchr/testify/require"
)

func newTestPostgresStore(t *testing.T) message.Store {
	testDB := dbtest.CreateTestDB(t)
	store, err := message.NewPostgresStore(testDB, 0, 0)
	require.Nil(t, err)
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
