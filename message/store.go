package message

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"

	"heckel.io/ntfy/v2/log"
	"heckel.io/ntfy/v2/model"
	"heckel.io/ntfy/v2/util"
)

const (
	tagMessageCache = "message_cache"
)

var errNoRows = errors.New("no rows found")

// Store is the interface for a message cache store
type Store interface {
	AddMessage(m *model.Message) error
	AddMessages(ms []*model.Message) error
	Message(id string) (*model.Message, error)
	MessageCounts() (map[string]int, error)
	Messages(topic string, since model.SinceMarker, scheduled bool) ([]*model.Message, error)
	MessagesDue() ([]*model.Message, error)
	MessagesExpired() ([]string, error)
	MarkPublished(m *model.Message) error
	UpdateMessageTime(messageID string, timestamp int64) error
	Topics() ([]string, error)
	DeleteMessages(ids ...string) error
	DeleteScheduledBySequenceID(topic, sequenceID string) ([]string, error)
	ExpireMessages(topics ...string) error
	AttachmentsExpired() ([]string, error)
	MarkAttachmentsDeleted(ids ...string) error
	AttachmentBytesUsedBySender(sender string) (int64, error)
	AttachmentBytesUsedByUser(userID string) (int64, error)
	UpdateStats(messages int64) error
	Stats() (int64, error)
	Close() error
}

// storeQueries holds the database-specific SQL queries
type storeQueries struct {
	insertMessage                    string
	deleteMessage                    string
	selectScheduledMessageIDsBySeqID string
	deleteScheduledBySequenceID      string
	updateMessagesForTopicExpiry     string
	selectRowIDFromMessageID         string
	selectMessagesByID               string
	selectMessagesSinceTime          string
	selectMessagesSinceTimeScheduled string
	selectMessagesSinceID            string
	selectMessagesSinceIDScheduled   string
	selectMessagesLatest             string
	selectMessagesDue                string
	selectMessagesExpired            string
	updateMessagePublished           string
	selectMessagesCount              string
	selectMessageCountPerTopic       string
	selectTopics                     string
	updateAttachmentDeleted          string
	selectAttachmentsExpired         string
	selectAttachmentsSizeBySender    string
	selectAttachmentsSizeByUserID    string
	selectStats                      string
	updateStats                      string
	updateMessageTime                string
}

// commonStore implements store operations that are identical across database backends
type commonStore struct {
	db      *sql.DB
	queue   *util.BatchingQueue[*model.Message]
	nop     bool
	mu      sync.Mutex
	queries storeQueries
}

func newCommonStore(db *sql.DB, queries storeQueries, batchSize int, batchTimeout time.Duration, nop bool) *commonStore {
	var queue *util.BatchingQueue[*model.Message]
	if batchSize > 0 || batchTimeout > 0 {
		queue = util.NewBatchingQueue[*model.Message](batchSize, batchTimeout)
	}
	c := &commonStore{
		db:      db,
		queue:   queue,
		nop:     nop,
		queries: queries,
	}
	go c.processMessageBatches()
	return c
}

// AddMessage stores a message to the message cache synchronously, or queues it to be stored at a later date asynchronously.
func (c *commonStore) AddMessage(m *model.Message) error {
	if c.queue != nil {
		c.queue.Enqueue(m)
		return nil
	}
	return c.addMessages([]*model.Message{m})
}

// AddMessages synchronously stores a batch of messages to the message cache
func (c *commonStore) AddMessages(ms []*model.Message) error {
	return c.addMessages(ms)
}

func (c *commonStore) addMessages(ms []*model.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.nop {
		return nil
	}
	if len(ms) == 0 {
		return nil
	}
	start := time.Now()
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(c.queries.insertMessage)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, m := range ms {
		if m.Event != model.MessageEvent && m.Event != model.MessageDeleteEvent && m.Event != model.MessageClearEvent {
			return model.ErrUnexpectedMessageType
		}
		published := m.Time <= time.Now().Unix()
		tags := strings.Join(m.Tags, ",")
		var attachmentName, attachmentType, attachmentURL string
		var attachmentSize, attachmentExpires int64
		var attachmentDeleted bool
		if m.Attachment != nil {
			attachmentName = m.Attachment.Name
			attachmentType = m.Attachment.Type
			attachmentSize = m.Attachment.Size
			attachmentExpires = m.Attachment.Expires
			attachmentURL = m.Attachment.URL
		}
		var actionsStr string
		if len(m.Actions) > 0 {
			actionsBytes, err := json.Marshal(m.Actions)
			if err != nil {
				return err
			}
			actionsStr = string(actionsBytes)
		}
		var sender string
		if m.Sender.IsValid() {
			sender = m.Sender.String()
		}
		_, err := stmt.Exec(
			m.ID,
			m.SequenceID,
			m.Time,
			m.Event,
			m.Expires,
			m.Topic,
			m.Message,
			m.Title,
			m.Priority,
			tags,
			m.Click,
			m.Icon,
			actionsStr,
			attachmentName,
			attachmentType,
			attachmentSize,
			attachmentExpires,
			attachmentURL,
			attachmentDeleted, // Always zero
			sender,
			m.User,
			m.ContentType,
			m.Encoding,
			published,
		)
		if err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		log.Tag(tagMessageCache).Err(err).Error("Writing %d message(s) failed (took %v)", len(ms), time.Since(start))
		return err
	}
	log.Tag(tagMessageCache).Debug("Wrote %d message(s) in %v", len(ms), time.Since(start))
	return nil
}

func (c *commonStore) Messages(topic string, since model.SinceMarker, scheduled bool) ([]*model.Message, error) {
	if since.IsNone() {
		return make([]*model.Message, 0), nil
	} else if since.IsLatest() {
		return c.messagesLatest(topic)
	} else if since.IsID() {
		return c.messagesSinceID(topic, since, scheduled)
	}
	return c.messagesSinceTime(topic, since, scheduled)
}

func (c *commonStore) messagesSinceTime(topic string, since model.SinceMarker, scheduled bool) ([]*model.Message, error) {
	var rows *sql.Rows
	var err error
	if scheduled {
		rows, err = c.db.Query(c.queries.selectMessagesSinceTimeScheduled, topic, since.Time().Unix())
	} else {
		rows, err = c.db.Query(c.queries.selectMessagesSinceTime, topic, since.Time().Unix())
	}
	if err != nil {
		return nil, err
	}
	return readMessages(rows)
}

func (c *commonStore) messagesSinceID(topic string, since model.SinceMarker, scheduled bool) ([]*model.Message, error) {
	idrows, err := c.db.Query(c.queries.selectRowIDFromMessageID, since.ID())
	if err != nil {
		return nil, err
	}
	defer idrows.Close()
	if !idrows.Next() {
		return c.messagesSinceTime(topic, model.SinceAllMessages, scheduled)
	}
	var rowID int64
	if err := idrows.Scan(&rowID); err != nil {
		return nil, err
	}
	idrows.Close()
	var rows *sql.Rows
	if scheduled {
		rows, err = c.db.Query(c.queries.selectMessagesSinceIDScheduled, topic, rowID)
	} else {
		rows, err = c.db.Query(c.queries.selectMessagesSinceID, topic, rowID)
	}
	if err != nil {
		return nil, err
	}
	return readMessages(rows)
}

func (c *commonStore) messagesLatest(topic string) ([]*model.Message, error) {
	rows, err := c.db.Query(c.queries.selectMessagesLatest, topic)
	if err != nil {
		return nil, err
	}
	return readMessages(rows)
}

func (c *commonStore) MessagesDue() ([]*model.Message, error) {
	rows, err := c.db.Query(c.queries.selectMessagesDue, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	return readMessages(rows)
}

// MessagesExpired returns a list of IDs for messages that have expired (should be deleted)
func (c *commonStore) MessagesExpired() ([]string, error) {
	rows, err := c.db.Query(c.queries.selectMessagesExpired, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (c *commonStore) Message(id string) (*model.Message, error) {
	rows, err := c.db.Query(c.queries.selectMessagesByID, id)
	if err != nil {
		return nil, err
	}
	if !rows.Next() {
		return nil, model.ErrMessageNotFound
	}
	defer rows.Close()
	return readMessage(rows)
}

// UpdateMessageTime updates the time column for a message by ID. This is only used for testing.
func (c *commonStore) UpdateMessageTime(messageID string, timestamp int64) error {
	_, err := c.db.Exec(c.queries.updateMessageTime, timestamp, messageID)
	return err
}

func (c *commonStore) MarkPublished(m *model.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.db.Exec(c.queries.updateMessagePublished, m.ID)
	return err
}

func (c *commonStore) MessageCounts() (map[string]int, error) {
	rows, err := c.db.Query(c.queries.selectMessageCountPerTopic)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var topic string
	var count int
	counts := make(map[string]int)
	for rows.Next() {
		if err := rows.Scan(&topic, &count); err != nil {
			return nil, err
		} else if err := rows.Err(); err != nil {
			return nil, err
		}
		counts[topic] = count
	}
	return counts, nil
}

func (c *commonStore) Topics() ([]string, error) {
	rows, err := c.db.Query(c.queries.selectTopics)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	topics := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		topics = append(topics, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return topics, nil
}

func (c *commonStore) DeleteMessages(ids ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.Exec(c.queries.deleteMessage, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteScheduledBySequenceID deletes unpublished (scheduled) messages with the given topic and sequence ID.
// It returns the message IDs of the deleted messages, which can be used to clean up attachment files.
func (c *commonStore) DeleteScheduledBySequenceID(topic, sequenceID string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tx, err := c.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(c.queries.selectScheduledMessageIDsBySeqID, topic, sequenceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if _, err := tx.Exec(c.queries.deleteScheduledBySequenceID, topic, sequenceID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (c *commonStore) ExpireMessages(topics ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range topics {
		if _, err := tx.Exec(c.queries.updateMessagesForTopicExpiry, time.Now().Unix()-1, t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (c *commonStore) AttachmentsExpired() ([]string, error) {
	rows, err := c.db.Query(c.queries.selectAttachmentsExpired, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (c *commonStore) MarkAttachmentsDeleted(ids ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.Exec(c.queries.updateAttachmentDeleted, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (c *commonStore) AttachmentBytesUsedBySender(sender string) (int64, error) {
	rows, err := c.db.Query(c.queries.selectAttachmentsSizeBySender, sender, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return c.readAttachmentBytesUsed(rows)
}

func (c *commonStore) AttachmentBytesUsedByUser(userID string) (int64, error) {
	rows, err := c.db.Query(c.queries.selectAttachmentsSizeByUserID, userID, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return c.readAttachmentBytesUsed(rows)
}

func (c *commonStore) readAttachmentBytesUsed(rows *sql.Rows) (int64, error) {
	defer rows.Close()
	var size int64
	if !rows.Next() {
		return 0, errors.New("no rows found")
	}
	if err := rows.Scan(&size); err != nil {
		return 0, err
	} else if err := rows.Err(); err != nil {
		return 0, err
	}
	return size, nil
}

func (c *commonStore) UpdateStats(messages int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.db.Exec(c.queries.updateStats, messages)
	return err
}

func (c *commonStore) Stats() (messages int64, err error) {
	rows, err := c.db.Query(c.queries.selectStats)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, errNoRows
	}
	if err := rows.Scan(&messages); err != nil {
		return 0, err
	}
	return messages, nil
}

func (c *commonStore) Close() error {
	return c.db.Close()
}

func (c *commonStore) processMessageBatches() {
	if c.queue == nil {
		return
	}
	for messages := range c.queue.Dequeue() {
		if err := c.addMessages(messages); err != nil {
			log.Tag(tagMessageCache).Err(err).Error("Cannot write message batch")
		}
	}
}

func readMessages(rows *sql.Rows) ([]*model.Message, error) {
	defer rows.Close()
	messages := make([]*model.Message, 0)
	for rows.Next() {
		m, err := readMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func readMessage(rows *sql.Rows) (*model.Message, error) {
	var timestamp, expires, attachmentSize, attachmentExpires int64
	var priority int
	var id, sequenceID, event, topic, msg, title, tagsStr, click, icon, actionsStr, attachmentName, attachmentType, attachmentURL, sender, user, contentType, encoding string
	err := rows.Scan(
		&id,
		&sequenceID,
		&timestamp,
		&event,
		&expires,
		&topic,
		&msg,
		&title,
		&priority,
		&tagsStr,
		&click,
		&icon,
		&actionsStr,
		&attachmentName,
		&attachmentType,
		&attachmentSize,
		&attachmentExpires,
		&attachmentURL,
		&sender,
		&user,
		&contentType,
		&encoding,
	)
	if err != nil {
		return nil, err
	}
	var tags []string
	if tagsStr != "" {
		tags = strings.Split(tagsStr, ",")
	}
	var actions []*model.Action
	if actionsStr != "" {
		if err := json.Unmarshal([]byte(actionsStr), &actions); err != nil {
			return nil, err
		}
	}
	senderIP, err := netip.ParseAddr(sender)
	if err != nil {
		senderIP = netip.Addr{} // if no IP stored in database, return invalid address
	}
	var att *model.Attachment
	if attachmentName != "" && attachmentURL != "" {
		att = &model.Attachment{
			Name:    attachmentName,
			Type:    attachmentType,
			Size:    attachmentSize,
			Expires: attachmentExpires,
			URL:     attachmentURL,
		}
	}
	return &model.Message{
		ID:          id,
		SequenceID:  sequenceID,
		Time:        timestamp,
		Expires:     expires,
		Event:       event,
		Topic:       topic,
		Message:     msg,
		Title:       title,
		Priority:    priority,
		Tags:        tags,
		Click:       click,
		Icon:        icon,
		Actions:     actions,
		Attachment:  att,
		Sender:      senderIP,
		User:        user,
		ContentType: contentType,
		Encoding:    encoding,
	}, nil
}

// Ensure commonStore implements Store
var _ Store = (*commonStore)(nil)

// Needed by store.go but not part of Store interface; unused import guard
var _ = fmt.Sprintf
