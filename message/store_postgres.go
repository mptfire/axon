package message

import (
	"database/sql"
	"time"
)

// PostgreSQL runtime query constants
const (
	pgInsertMessageQuery = `
		INSERT INTO message (mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, attachment_deleted, sender, user_id, content_type, encoding, published)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)
	`
	pgDeleteMessageQuery                    = `DELETE FROM message WHERE mid = $1`
	pgSelectScheduledMessageIDsBySeqIDQuery = `SELECT mid FROM message WHERE topic = $1 AND sequence_id = $2 AND published = FALSE`
	pgDeleteScheduledBySequenceIDQuery      = `DELETE FROM message WHERE topic = $1 AND sequence_id = $2 AND published = FALSE`
	pgUpdateMessagesForTopicExpiryQuery     = `UPDATE message SET expires = $1 WHERE topic = $2`
	pgSelectRowIDFromMessageID              = `SELECT id FROM message WHERE mid = $1`
	pgSelectMessagesByIDQuery               = `
		SELECT mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, sender, user_id, content_type, encoding
		FROM message
		WHERE mid = $1
	`
	pgSelectMessagesSinceTimeQuery = `
		SELECT mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, sender, user_id, content_type, encoding
		FROM message
		WHERE topic = $1 AND time >= $2 AND published = TRUE
		ORDER BY time, id
	`
	pgSelectMessagesSinceTimeIncludeScheduledQuery = `
		SELECT mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, sender, user_id, content_type, encoding
		FROM message
		WHERE topic = $1 AND time >= $2
		ORDER BY time, id
	`
	pgSelectMessagesSinceIDQuery = `
		SELECT mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, sender, user_id, content_type, encoding
		FROM message
		WHERE topic = $1 AND id > $2 AND published = TRUE
		ORDER BY time, id
	`
	pgSelectMessagesSinceIDIncludeScheduledQuery = `
		SELECT mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, sender, user_id, content_type, encoding
		FROM message
		WHERE topic = $1 AND (id > $2 OR published = FALSE)
		ORDER BY time, id
	`
	pgSelectMessagesLatestQuery = `
		SELECT mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, sender, user_id, content_type, encoding
		FROM message
		WHERE topic = $1 AND published = TRUE
		ORDER BY time DESC, id DESC
		LIMIT 1
	`
	pgSelectMessagesDueQuery = `
		SELECT mid, sequence_id, time, event, expires, topic, message, title, priority, tags, click, icon, actions, attachment_name, attachment_type, attachment_size, attachment_expires, attachment_url, sender, user_id, content_type, encoding
		FROM message
		WHERE time <= $1 AND published = FALSE
		ORDER BY time, id
	`
	pgSelectMessagesExpiredQuery      = `SELECT mid FROM message WHERE expires <= $1 AND published = TRUE`
	pgUpdateMessagePublishedQuery     = `UPDATE message SET published = TRUE WHERE mid = $1`
	pgSelectMessagesCountQuery        = `SELECT COUNT(*) FROM message`
	pgSelectMessageCountPerTopicQuery = `SELECT topic, COUNT(*) FROM message GROUP BY topic`
	pgSelectTopicsQuery               = `SELECT topic FROM message GROUP BY topic`

	pgUpdateAttachmentDeleted            = `UPDATE message SET attachment_deleted = TRUE WHERE mid = $1`
	pgSelectAttachmentsExpiredQuery      = `SELECT mid FROM message WHERE attachment_expires > 0 AND attachment_expires <= $1 AND attachment_deleted = FALSE`
	pgSelectAttachmentsSizeBySenderQuery = `SELECT COALESCE(SUM(attachment_size), 0) FROM message WHERE user_id = '' AND sender = $1 AND attachment_expires >= $2`
	pgSelectAttachmentsSizeByUserIDQuery = `SELECT COALESCE(SUM(attachment_size), 0) FROM message WHERE user_id = $1 AND attachment_expires >= $2`

	pgSelectStatsQuery        = `SELECT value FROM message_stats WHERE key = 'messages'`
	pgUpdateStatsQuery        = `UPDATE message_stats SET value = $1 WHERE key = 'messages'`
	pgUpdateMessageTimesQuery = `UPDATE message SET time = $1 WHERE mid = $2`
)

var pgQueries = storeQueries{
	insertMessage:                    pgInsertMessageQuery,
	deleteMessage:                    pgDeleteMessageQuery,
	selectScheduledMessageIDsBySeqID: pgSelectScheduledMessageIDsBySeqIDQuery,
	deleteScheduledBySequenceID:      pgDeleteScheduledBySequenceIDQuery,
	updateMessagesForTopicExpiry:     pgUpdateMessagesForTopicExpiryQuery,
	selectRowIDFromMessageID:         pgSelectRowIDFromMessageID,
	selectMessagesByID:               pgSelectMessagesByIDQuery,
	selectMessagesSinceTime:          pgSelectMessagesSinceTimeQuery,
	selectMessagesSinceTimeScheduled: pgSelectMessagesSinceTimeIncludeScheduledQuery,
	selectMessagesSinceID:            pgSelectMessagesSinceIDQuery,
	selectMessagesSinceIDScheduled:   pgSelectMessagesSinceIDIncludeScheduledQuery,
	selectMessagesLatest:             pgSelectMessagesLatestQuery,
	selectMessagesDue:                pgSelectMessagesDueQuery,
	selectMessagesExpired:            pgSelectMessagesExpiredQuery,
	updateMessagePublished:           pgUpdateMessagePublishedQuery,
	selectMessagesCount:              pgSelectMessagesCountQuery,
	selectMessageCountPerTopic:       pgSelectMessageCountPerTopicQuery,
	selectTopics:                     pgSelectTopicsQuery,
	updateAttachmentDeleted:          pgUpdateAttachmentDeleted,
	selectAttachmentsExpired:         pgSelectAttachmentsExpiredQuery,
	selectAttachmentsSizeBySender:    pgSelectAttachmentsSizeBySenderQuery,
	selectAttachmentsSizeByUserID:    pgSelectAttachmentsSizeByUserIDQuery,
	selectStats:                      pgSelectStatsQuery,
	updateStats:                      pgUpdateStatsQuery,
	updateMessageTime:                pgUpdateMessageTimesQuery,
}

// NewPostgresStore creates a new PostgreSQL-backed message cache store using an existing database connection pool.
func NewPostgresStore(db *sql.DB, batchSize int, batchTimeout time.Duration) (Store, error) {
	if err := setupPostgresDB(db); err != nil {
		return nil, err
	}
	return newCommonStore(db, pgQueries, batchSize, batchTimeout, false), nil
}
