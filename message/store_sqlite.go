package message

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
	"heckel.io/ntfy/v2/util"
)

// SQLite runtime query constants
const (
	sqliteInsertMessageQuery = `
		INSERT INTO messages (mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, attachment_deleted, sender, user, content_type, encoding, published)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	sqliteDeleteMessageQuery                    = `DELETE FROM messages WHERE mid = ?`
	sqliteSelectScheduledMessageIDsBySeqIDQuery = `SELECT mid FROM messages WHERE topic = ? AND sequence_id = ? AND published = 0`
	sqliteDeleteScheduledBySequenceIDQuery      = `DELETE FROM messages WHERE topic = ? AND sequence_id = ? AND published = 0`
	sqliteUpdateMessagesForTopicExpiryQuery     = `UPDATE messages SET expires = ? WHERE topic = ?`
	sqliteSelectRowIDFromMessageID              = `SELECT id FROM messages WHERE mid = ?`
	sqliteSelectMessagesByIDQuery               = `
		SELECT mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, sender, user, content_type, encoding
		FROM messages
		WHERE mid = ?
	`
	sqliteSelectMessagesSinceTimeQuery = `
		SELECT mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, sender, user, content_type, encoding
		FROM messages
		WHERE topic = ? AND time >= ? AND published = 1
		ORDER BY time, id
	`
	sqliteSelectMessagesSinceTimeIncludeScheduledQuery = `
		SELECT mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, sender, user, content_type, encoding
		FROM messages
		WHERE topic = ? AND time >= ?
		ORDER BY time, id
	`
	sqliteSelectMessagesSinceIDQuery = `
		SELECT mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, sender, user, content_type, encoding
		FROM messages
		WHERE topic = ? AND id > ? AND published = 1
		ORDER BY time, id
	`
	sqliteSelectMessagesSinceIDIncludeScheduledQuery = `
		SELECT mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, sender, user, content_type, encoding
		FROM messages
		WHERE topic = ? AND (id > ? OR published = 0)
		ORDER BY time, id
	`
	sqliteSelectMessagesLatestQuery = `
		SELECT mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, sender, user, content_type, encoding
		FROM messages
		WHERE topic = ? AND published = 1
		ORDER BY time DESC, id DESC
		LIMIT 1
	`
	sqliteSelectMessagesDueQuery = `
		SELECT mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, sender, user, content_type, encoding
		FROM messages
		WHERE time <= ? AND published = 0
		ORDER BY time, id
	`
	sqliteSelectMessagesExpiredQuery      = `SELECT mid FROM messages WHERE expires <= ? AND published = 1`
	sqliteUpdateMessagePublishedQuery     = `UPDATE messages SET published = 1 WHERE mid = ?`
	sqliteSelectMessagesCountQuery        = `SELECT COUNT(*) FROM messages`
	sqliteSelectMessageCountPerTopicQuery = `SELECT topic, COUNT(*) FROM messages GROUP BY topic`
	sqliteSelectTopicsQuery               = `SELECT topic FROM messages GROUP BY topic`

	sqliteUpdateAttachmentDeleted            = `UPDATE messages SET attachment_deleted = 1 WHERE mid = ?`
	sqliteSelectAttachmentsExpiredQuery      = `SELECT mid FROM messages WHERE attachment_expires > 0 AND attachment_expires <= ? AND attachment_deleted = 0`
	sqliteSelectAttachmentsSizeBySenderQuery = `SELECT IFNULL(SUM(attachment_size), 0) FROM messages WHERE user = '' AND sender = ? AND attachment_expires >= ?`
	sqliteSelectAttachmentsSizeByUserIDQuery = `SELECT IFNULL(SUM(attachment_size), 0) FROM messages WHERE user = ? AND attachment_expires >= ?`

	sqliteSelectStatsQuery       = `SELECT value FROM stats WHERE key = 'messages'`
	sqliteUpdateStatsQuery       = `UPDATE stats SET value = ? WHERE key = 'messages'`
	sqliteUpdateMessageTimeQuery = `UPDATE messages SET time = ? WHERE mid = ?`
)

var sqliteQueries = storeQueries{
	insertMessage:                    sqliteInsertMessageQuery,
	deleteMessage:                    sqliteDeleteMessageQuery,
	selectScheduledMessageIDsBySeqID: sqliteSelectScheduledMessageIDsBySeqIDQuery,
	deleteScheduledBySequenceID:      sqliteDeleteScheduledBySequenceIDQuery,
	updateMessagesForTopicExpiry:     sqliteUpdateMessagesForTopicExpiryQuery,
	selectRowIDFromMessageID:         sqliteSelectRowIDFromMessageID,
	selectMessagesByID:               sqliteSelectMessagesByIDQuery,
	selectMessagesSinceTime:          sqliteSelectMessagesSinceTimeQuery,
	selectMessagesSinceTimeScheduled: sqliteSelectMessagesSinceTimeIncludeScheduledQuery,
	selectMessagesSinceID:            sqliteSelectMessagesSinceIDQuery,
	selectMessagesSinceIDScheduled:   sqliteSelectMessagesSinceIDIncludeScheduledQuery,
	selectMessagesLatest:             sqliteSelectMessagesLatestQuery,
	selectMessagesDue:                sqliteSelectMessagesDueQuery,
	selectMessagesExpired:            sqliteSelectMessagesExpiredQuery,
	updateMessagePublished:           sqliteUpdateMessagePublishedQuery,
	selectMessagesCount:              sqliteSelectMessagesCountQuery,
	selectMessageCountPerTopic:       sqliteSelectMessageCountPerTopicQuery,
	selectTopics:                     sqliteSelectTopicsQuery,
	updateAttachmentDeleted:          sqliteUpdateAttachmentDeleted,
	selectAttachmentsExpired:         sqliteSelectAttachmentsExpiredQuery,
	selectAttachmentsSizeBySender:    sqliteSelectAttachmentsSizeBySenderQuery,
	selectAttachmentsSizeByUserID:    sqliteSelectAttachmentsSizeByUserIDQuery,
	selectStats:                      sqliteSelectStatsQuery,
	updateStats:                      sqliteUpdateStatsQuery,
	updateMessageTime:                sqliteUpdateMessageTimeQuery,
}

// NewSQLiteStore creates a SQLite file-backed cache
func NewSQLiteStore(filename, startupQueries string, cacheDuration time.Duration, batchSize int, batchTimeout time.Duration, nop bool) (Store, error) {
	parentDir := filepath.Dir(filename)
	if !util.FileExists(parentDir) {
		return nil, fmt.Errorf("cache database directory %s does not exist or is not accessible", parentDir)
	}
	db, err := sql.Open("sqlite3", filename)
	if err != nil {
		return nil, err
	}
	if err := setupSQLite(db, startupQueries, cacheDuration); err != nil {
		return nil, err
	}
	return newCommonStore(db, sqliteQueries, batchSize, batchTimeout, nop), nil
}

// NewMemStore creates an in-memory cache
func NewMemStore() (Store, error) {
	return NewSQLiteStore(createMemoryFilename(), "", 0, 0, 0, false)
}

// NewNopStore creates an in-memory cache that discards all messages;
// it is always empty and can be used if caching is entirely disabled
func NewNopStore() (Store, error) {
	return NewSQLiteStore(createMemoryFilename(), "", 0, 0, 0, true)
}

// createMemoryFilename creates a unique memory filename to use for the SQLite backend.
func createMemoryFilename() string {
	return fmt.Sprintf("file:%s?mode=memory&cache=shared", util.RandomString(10))
}
