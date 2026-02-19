package message_test

import (
	"net/netip"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/message"
	"heckel.io/ntfy/v2/model"
)

func testCacheMessages(t *testing.T, s message.Store) {
	m1 := model.NewDefaultMessage("mytopic", "my message")
	m1.Time = 1

	m2 := model.NewDefaultMessage("mytopic", "my other message")
	m2.Time = 2

	require.Nil(t, s.AddMessage(m1))
	require.Nil(t, s.AddMessage(model.NewDefaultMessage("example", "my example message")))
	require.Nil(t, s.AddMessage(m2))

	// Adding invalid
	require.Equal(t, model.ErrUnexpectedMessageType, s.AddMessage(model.NewKeepaliveMessage("mytopic"))) // These should not be added!
	require.Equal(t, model.ErrUnexpectedMessageType, s.AddMessage(model.NewOpenMessage("example")))      // These should not be added!

	// mytopic: count
	counts, err := s.MessageCounts()
	require.Nil(t, err)
	require.Equal(t, 2, counts["mytopic"])

	// mytopic: since all
	messages, _ := s.Messages("mytopic", model.SinceAllMessages, false)
	require.Equal(t, 2, len(messages))
	require.Equal(t, "my message", messages[0].Message)
	require.Equal(t, "mytopic", messages[0].Topic)
	require.Equal(t, model.MessageEvent, messages[0].Event)
	require.Equal(t, "", messages[0].Title)
	require.Equal(t, 0, messages[0].Priority)
	require.Nil(t, messages[0].Tags)
	require.Equal(t, "my other message", messages[1].Message)

	// mytopic: since none
	messages, _ = s.Messages("mytopic", model.SinceNoMessages, false)
	require.Empty(t, messages)

	// mytopic: since m1 (by ID)
	messages, _ = s.Messages("mytopic", model.NewSinceID(m1.ID), false)
	require.Equal(t, 1, len(messages))
	require.Equal(t, m2.ID, messages[0].ID)
	require.Equal(t, "my other message", messages[0].Message)
	require.Equal(t, "mytopic", messages[0].Topic)

	// mytopic: since 2
	messages, _ = s.Messages("mytopic", model.NewSinceTime(2), false)
	require.Equal(t, 1, len(messages))
	require.Equal(t, "my other message", messages[0].Message)

	// mytopic: latest
	messages, _ = s.Messages("mytopic", model.SinceLatestMessage, false)
	require.Equal(t, 1, len(messages))
	require.Equal(t, "my other message", messages[0].Message)

	// example: count
	counts, err = s.MessageCounts()
	require.Nil(t, err)
	require.Equal(t, 1, counts["example"])

	// example: since all
	messages, _ = s.Messages("example", model.SinceAllMessages, false)
	require.Equal(t, "my example message", messages[0].Message)

	// non-existing: count
	counts, err = s.MessageCounts()
	require.Nil(t, err)
	require.Equal(t, 0, counts["doesnotexist"])

	// non-existing: since all
	messages, _ = s.Messages("doesnotexist", model.SinceAllMessages, false)
	require.Empty(t, messages)
}

func testCacheMessagesLock(t *testing.T, s message.Store) {
	var wg sync.WaitGroup
	for i := 0; i < 5000; i++ {
		wg.Add(1)
		go func() {
			assert.Nil(t, s.AddMessage(model.NewDefaultMessage("mytopic", "test message")))
			wg.Done()
		}()
	}
	wg.Wait()
}

func testCacheMessagesScheduled(t *testing.T, s message.Store) {
	m1 := model.NewDefaultMessage("mytopic", "message 1")
	m2 := model.NewDefaultMessage("mytopic", "message 2")
	m2.Time = time.Now().Add(time.Hour).Unix()
	m3 := model.NewDefaultMessage("mytopic", "message 3")
	m3.Time = time.Now().Add(time.Minute).Unix() // earlier than m2!
	m4 := model.NewDefaultMessage("mytopic2", "message 4")
	m4.Time = time.Now().Add(time.Minute).Unix()
	require.Nil(t, s.AddMessage(m1))
	require.Nil(t, s.AddMessage(m2))
	require.Nil(t, s.AddMessage(m3))

	messages, _ := s.Messages("mytopic", model.SinceAllMessages, false) // exclude scheduled
	require.Equal(t, 1, len(messages))
	require.Equal(t, "message 1", messages[0].Message)

	messages, _ = s.Messages("mytopic", model.SinceAllMessages, true) // include scheduled
	require.Equal(t, 3, len(messages))
	require.Equal(t, "message 1", messages[0].Message)
	require.Equal(t, "message 3", messages[1].Message) // Order!
	require.Equal(t, "message 2", messages[2].Message)

	messages, _ = s.MessagesDue()
	require.Empty(t, messages)
}

func testCacheTopics(t *testing.T, s message.Store) {
	require.Nil(t, s.AddMessage(model.NewDefaultMessage("topic1", "my example message")))
	require.Nil(t, s.AddMessage(model.NewDefaultMessage("topic2", "message 1")))
	require.Nil(t, s.AddMessage(model.NewDefaultMessage("topic2", "message 2")))
	require.Nil(t, s.AddMessage(model.NewDefaultMessage("topic2", "message 3")))

	topics, err := s.Topics()
	if err != nil {
		t.Fatal(err)
	}
	require.Equal(t, 2, len(topics))
	require.Contains(t, topics, "topic1")
	require.Contains(t, topics, "topic2")
}

func testCacheMessagesTagsPrioAndTitle(t *testing.T, s message.Store) {
	m := model.NewDefaultMessage("mytopic", "some message")
	m.Tags = []string{"tag1", "tag2"}
	m.Priority = 5
	m.Title = "some title"
	require.Nil(t, s.AddMessage(m))

	messages, _ := s.Messages("mytopic", model.SinceAllMessages, false)
	require.Equal(t, []string{"tag1", "tag2"}, messages[0].Tags)
	require.Equal(t, 5, messages[0].Priority)
	require.Equal(t, "some title", messages[0].Title)
}

func testCacheMessagesSinceID(t *testing.T, s message.Store) {
	m1 := model.NewDefaultMessage("mytopic", "message 1")
	m1.Time = 100
	m2 := model.NewDefaultMessage("mytopic", "message 2")
	m2.Time = 200
	m3 := model.NewDefaultMessage("mytopic", "message 3")
	m3.Time = time.Now().Add(time.Hour).Unix() // Scheduled, in the future, later than m7 and m5
	m4 := model.NewDefaultMessage("mytopic", "message 4")
	m4.Time = 400
	m5 := model.NewDefaultMessage("mytopic", "message 5")
	m5.Time = time.Now().Add(time.Minute).Unix() // Scheduled, in the future, later than m7
	m6 := model.NewDefaultMessage("mytopic", "message 6")
	m6.Time = 600
	m7 := model.NewDefaultMessage("mytopic", "message 7")
	m7.Time = 700

	require.Nil(t, s.AddMessage(m1))
	require.Nil(t, s.AddMessage(m2))
	require.Nil(t, s.AddMessage(m3))
	require.Nil(t, s.AddMessage(m4))
	require.Nil(t, s.AddMessage(m5))
	require.Nil(t, s.AddMessage(m6))
	require.Nil(t, s.AddMessage(m7))

	// Case 1: Since ID exists, exclude scheduled
	messages, _ := s.Messages("mytopic", model.NewSinceID(m2.ID), false)
	require.Equal(t, 3, len(messages))
	require.Equal(t, "message 4", messages[0].Message)
	require.Equal(t, "message 6", messages[1].Message) // Not scheduled m3/m5!
	require.Equal(t, "message 7", messages[2].Message)

	// Case 2: Since ID exists, include scheduled
	messages, _ = s.Messages("mytopic", model.NewSinceID(m2.ID), true)
	require.Equal(t, 5, len(messages))
	require.Equal(t, "message 4", messages[0].Message)
	require.Equal(t, "message 6", messages[1].Message)
	require.Equal(t, "message 7", messages[2].Message)
	require.Equal(t, "message 5", messages[3].Message) // Order!
	require.Equal(t, "message 3", messages[4].Message) // Order!

	// Case 3: Since ID does not exist (-> Return all messages), include scheduled
	messages, _ = s.Messages("mytopic", model.NewSinceID("doesntexist"), true)
	require.Equal(t, 7, len(messages))
	require.Equal(t, "message 1", messages[0].Message)
	require.Equal(t, "message 2", messages[1].Message)
	require.Equal(t, "message 4", messages[2].Message)
	require.Equal(t, "message 6", messages[3].Message)
	require.Equal(t, "message 7", messages[4].Message)
	require.Equal(t, "message 5", messages[5].Message) // Order!
	require.Equal(t, "message 3", messages[6].Message) // Order!

	// Case 4: Since ID exists and is last message (-> Return no messages), exclude scheduled
	messages, _ = s.Messages("mytopic", model.NewSinceID(m7.ID), false)
	require.Equal(t, 0, len(messages))

	// Case 5: Since ID exists and is last message (-> Return no messages), include scheduled
	messages, _ = s.Messages("mytopic", model.NewSinceID(m7.ID), true)
	require.Equal(t, 2, len(messages))
	require.Equal(t, "message 5", messages[0].Message)
	require.Equal(t, "message 3", messages[1].Message)
}

func testCachePrune(t *testing.T, s message.Store) {
	now := time.Now().Unix()

	m1 := model.NewDefaultMessage("mytopic", "my message")
	m1.Time = now - 10
	m1.Expires = now - 5

	m2 := model.NewDefaultMessage("mytopic", "my other message")
	m2.Time = now - 5
	m2.Expires = now + 5 // In the future

	m3 := model.NewDefaultMessage("another_topic", "and another one")
	m3.Time = now - 12
	m3.Expires = now - 2

	require.Nil(t, s.AddMessage(m1))
	require.Nil(t, s.AddMessage(m2))
	require.Nil(t, s.AddMessage(m3))

	counts, err := s.MessageCounts()
	require.Nil(t, err)
	require.Equal(t, 2, counts["mytopic"])
	require.Equal(t, 1, counts["another_topic"])

	expiredMessageIDs, err := s.MessagesExpired()
	require.Nil(t, err)
	require.Nil(t, s.DeleteMessages(expiredMessageIDs...))

	counts, err = s.MessageCounts()
	require.Nil(t, err)
	require.Equal(t, 1, counts["mytopic"])
	require.Equal(t, 0, counts["another_topic"])

	messages, err := s.Messages("mytopic", model.SinceAllMessages, false)
	require.Nil(t, err)
	require.Equal(t, 1, len(messages))
	require.Equal(t, "my other message", messages[0].Message)
}

func testCacheAttachments(t *testing.T, s message.Store) {
	expires1 := time.Now().Add(-4 * time.Hour).Unix() // Expired
	m := model.NewDefaultMessage("mytopic", "flower for you")
	m.ID = "m1"
	m.SequenceID = "m1"
	m.Sender = netip.MustParseAddr("1.2.3.4")
	m.Attachment = &model.Attachment{
		Name:    "flower.jpg",
		Type:    "image/jpeg",
		Size:    5000,
		Expires: expires1,
		URL:     "https://ntfy.sh/file/AbDeFgJhal.jpg",
	}
	require.Nil(t, s.AddMessage(m))

	expires2 := time.Now().Add(2 * time.Hour).Unix() // Future
	m = model.NewDefaultMessage("mytopic", "sending you a car")
	m.ID = "m2"
	m.SequenceID = "m2"
	m.Sender = netip.MustParseAddr("1.2.3.4")
	m.Attachment = &model.Attachment{
		Name:    "car.jpg",
		Type:    "image/jpeg",
		Size:    10000,
		Expires: expires2,
		URL:     "https://ntfy.sh/file/aCaRURL.jpg",
	}
	require.Nil(t, s.AddMessage(m))

	expires3 := time.Now().Add(1 * time.Hour).Unix() // Future
	m = model.NewDefaultMessage("another-topic", "sending you another car")
	m.ID = "m3"
	m.SequenceID = "m3"
	m.User = "u_BAsbaAa"
	m.Sender = netip.MustParseAddr("5.6.7.8")
	m.Attachment = &model.Attachment{
		Name:    "another-car.jpg",
		Type:    "image/jpeg",
		Size:    20000,
		Expires: expires3,
		URL:     "https://ntfy.sh/file/zakaDHFW.jpg",
	}
	require.Nil(t, s.AddMessage(m))

	messages, err := s.Messages("mytopic", model.SinceAllMessages, false)
	require.Nil(t, err)
	require.Equal(t, 2, len(messages))

	require.Equal(t, "flower for you", messages[0].Message)
	require.Equal(t, "flower.jpg", messages[0].Attachment.Name)
	require.Equal(t, "image/jpeg", messages[0].Attachment.Type)
	require.Equal(t, int64(5000), messages[0].Attachment.Size)
	require.Equal(t, expires1, messages[0].Attachment.Expires)
	require.Equal(t, "https://ntfy.sh/file/AbDeFgJhal.jpg", messages[0].Attachment.URL)
	require.Equal(t, "1.2.3.4", messages[0].Sender.String())

	require.Equal(t, "sending you a car", messages[1].Message)
	require.Equal(t, "car.jpg", messages[1].Attachment.Name)
	require.Equal(t, "image/jpeg", messages[1].Attachment.Type)
	require.Equal(t, int64(10000), messages[1].Attachment.Size)
	require.Equal(t, expires2, messages[1].Attachment.Expires)
	require.Equal(t, "https://ntfy.sh/file/aCaRURL.jpg", messages[1].Attachment.URL)
	require.Equal(t, "1.2.3.4", messages[1].Sender.String())

	size, err := s.AttachmentBytesUsedBySender("1.2.3.4")
	require.Nil(t, err)
	require.Equal(t, int64(10000), size)

	size, err = s.AttachmentBytesUsedBySender("5.6.7.8")
	require.Nil(t, err)
	require.Equal(t, int64(0), size) // Accounted to the user, not the IP!

	size, err = s.AttachmentBytesUsedByUser("u_BAsbaAa")
	require.Nil(t, err)
	require.Equal(t, int64(20000), size)
}

func testCacheAttachmentsExpired(t *testing.T, s message.Store) {
	m := model.NewDefaultMessage("mytopic", "flower for you")
	m.ID = "m1"
	m.SequenceID = "m1"
	m.Expires = time.Now().Add(time.Hour).Unix()
	require.Nil(t, s.AddMessage(m))

	m = model.NewDefaultMessage("mytopic", "message with attachment")
	m.ID = "m2"
	m.SequenceID = "m2"
	m.Expires = time.Now().Add(2 * time.Hour).Unix()
	m.Attachment = &model.Attachment{
		Name:    "car.jpg",
		Type:    "image/jpeg",
		Size:    10000,
		Expires: time.Now().Add(2 * time.Hour).Unix(),
		URL:     "https://ntfy.sh/file/aCaRURL.jpg",
	}
	require.Nil(t, s.AddMessage(m))

	m = model.NewDefaultMessage("mytopic", "message with external attachment")
	m.ID = "m3"
	m.SequenceID = "m3"
	m.Expires = time.Now().Add(2 * time.Hour).Unix()
	m.Attachment = &model.Attachment{
		Name:    "car.jpg",
		Type:    "image/jpeg",
		Expires: 0, // Unknown!
		URL:     "https://somedomain.com/car.jpg",
	}
	require.Nil(t, s.AddMessage(m))

	m = model.NewDefaultMessage("mytopic2", "message with expired attachment")
	m.ID = "m4"
	m.SequenceID = "m4"
	m.Expires = time.Now().Add(2 * time.Hour).Unix()
	m.Attachment = &model.Attachment{
		Name:    "expired-car.jpg",
		Type:    "image/jpeg",
		Size:    20000,
		Expires: time.Now().Add(-1 * time.Hour).Unix(),
		URL:     "https://ntfy.sh/file/aCaRURL.jpg",
	}
	require.Nil(t, s.AddMessage(m))

	ids, err := s.AttachmentsExpired()
	require.Nil(t, err)
	require.Equal(t, 1, len(ids))
	require.Equal(t, "m4", ids[0])
}

func testSender(t *testing.T, s message.Store) {
	m1 := model.NewDefaultMessage("mytopic", "mymessage")
	m1.Sender = netip.MustParseAddr("1.2.3.4")
	require.Nil(t, s.AddMessage(m1))

	m2 := model.NewDefaultMessage("mytopic", "mymessage without sender")
	require.Nil(t, s.AddMessage(m2))

	messages, err := s.Messages("mytopic", model.SinceAllMessages, false)
	require.Nil(t, err)
	require.Equal(t, 2, len(messages))
	require.Equal(t, messages[0].Sender, netip.MustParseAddr("1.2.3.4"))
	require.Equal(t, messages[1].Sender, netip.Addr{})
}

func testDeleteScheduledBySequenceID(t *testing.T, s message.Store) {
	// Create a scheduled (unpublished) message
	scheduledMsg := model.NewDefaultMessage("mytopic", "scheduled message")
	scheduledMsg.ID = "scheduled1"
	scheduledMsg.SequenceID = "seq123"
	scheduledMsg.Time = time.Now().Add(time.Hour).Unix() // Future time makes it scheduled
	require.Nil(t, s.AddMessage(scheduledMsg))

	// Create a published message with different sequence ID
	publishedMsg := model.NewDefaultMessage("mytopic", "published message")
	publishedMsg.ID = "published1"
	publishedMsg.SequenceID = "seq456"
	publishedMsg.Time = time.Now().Add(-time.Hour).Unix() // Past time makes it published
	require.Nil(t, s.AddMessage(publishedMsg))

	// Create a scheduled message in a different topic
	otherTopicMsg := model.NewDefaultMessage("othertopic", "other scheduled")
	otherTopicMsg.ID = "other1"
	otherTopicMsg.SequenceID = "seq123" // Same sequence ID as scheduledMsg
	otherTopicMsg.Time = time.Now().Add(time.Hour).Unix()
	require.Nil(t, s.AddMessage(otherTopicMsg))

	// Verify all messages exist (including scheduled)
	messages, err := s.Messages("mytopic", model.SinceAllMessages, true)
	require.Nil(t, err)
	require.Equal(t, 2, len(messages))

	messages, err = s.Messages("othertopic", model.SinceAllMessages, true)
	require.Nil(t, err)
	require.Equal(t, 1, len(messages))

	// Delete scheduled message by sequence ID and verify returned IDs
	deletedIDs, err := s.DeleteScheduledBySequenceID("mytopic", "seq123")
	require.Nil(t, err)
	require.Equal(t, 1, len(deletedIDs))
	require.Equal(t, "scheduled1", deletedIDs[0])

	// Verify scheduled message is deleted
	messages, err = s.Messages("mytopic", model.SinceAllMessages, true)
	require.Nil(t, err)
	require.Equal(t, 1, len(messages))
	require.Equal(t, "published message", messages[0].Message)

	// Verify other topic's message still exists (topic-scoped deletion)
	messages, err = s.Messages("othertopic", model.SinceAllMessages, true)
	require.Nil(t, err)
	require.Equal(t, 1, len(messages))
	require.Equal(t, "other scheduled", messages[0].Message)

	// Deleting non-existent sequence ID should return empty list
	deletedIDs, err = s.DeleteScheduledBySequenceID("mytopic", "nonexistent")
	require.Nil(t, err)
	require.Empty(t, deletedIDs)

	// Deleting published message should not affect it (only deletes unpublished)
	deletedIDs, err = s.DeleteScheduledBySequenceID("mytopic", "seq456")
	require.Nil(t, err)
	require.Empty(t, deletedIDs)

	messages, err = s.Messages("mytopic", model.SinceAllMessages, true)
	require.Nil(t, err)
	require.Equal(t, 1, len(messages))
	require.Equal(t, "published message", messages[0].Message)
}

func testMessageByID(t *testing.T, s message.Store) {
	// Add a message
	m := model.NewDefaultMessage("mytopic", "some message")
	m.Title = "some title"
	m.Priority = 4
	m.Tags = []string{"tag1", "tag2"}
	require.Nil(t, s.AddMessage(m))

	// Retrieve by ID
	retrieved, err := s.Message(m.ID)
	require.Nil(t, err)
	require.Equal(t, m.ID, retrieved.ID)
	require.Equal(t, "mytopic", retrieved.Topic)
	require.Equal(t, "some message", retrieved.Message)
	require.Equal(t, "some title", retrieved.Title)
	require.Equal(t, 4, retrieved.Priority)
	require.Equal(t, []string{"tag1", "tag2"}, retrieved.Tags)

	// Non-existent ID returns ErrMessageNotFound
	_, err = s.Message("doesnotexist")
	require.Equal(t, model.ErrMessageNotFound, err)
}

func testMarkPublished(t *testing.T, s message.Store) {
	// Add a scheduled message (future time → unpublished)
	m := model.NewDefaultMessage("mytopic", "scheduled message")
	m.Time = time.Now().Add(time.Hour).Unix()
	require.Nil(t, s.AddMessage(m))

	// Verify it does not appear in non-scheduled queries
	messages, err := s.Messages("mytopic", model.SinceAllMessages, false)
	require.Nil(t, err)
	require.Equal(t, 0, len(messages))

	// Verify it does appear in scheduled queries
	messages, err = s.Messages("mytopic", model.SinceAllMessages, true)
	require.Nil(t, err)
	require.Equal(t, 1, len(messages))

	// Mark as published
	require.Nil(t, s.MarkPublished(m))

	// Now it should appear in non-scheduled queries too
	messages, err = s.Messages("mytopic", model.SinceAllMessages, false)
	require.Nil(t, err)
	require.Equal(t, 1, len(messages))
	require.Equal(t, "scheduled message", messages[0].Message)
}

func testExpireMessages(t *testing.T, s message.Store) {
	// Add messages to two topics
	m1 := model.NewDefaultMessage("topic1", "message 1")
	m1.Expires = time.Now().Add(time.Hour).Unix()
	m2 := model.NewDefaultMessage("topic1", "message 2")
	m2.Expires = time.Now().Add(time.Hour).Unix()
	m3 := model.NewDefaultMessage("topic2", "message 3")
	m3.Expires = time.Now().Add(time.Hour).Unix()
	require.Nil(t, s.AddMessage(m1))
	require.Nil(t, s.AddMessage(m2))
	require.Nil(t, s.AddMessage(m3))

	// Verify all messages exist
	messages, err := s.Messages("topic1", model.SinceAllMessages, false)
	require.Nil(t, err)
	require.Equal(t, 2, len(messages))
	messages, err = s.Messages("topic2", model.SinceAllMessages, false)
	require.Nil(t, err)
	require.Equal(t, 1, len(messages))

	// Expire topic1 messages
	require.Nil(t, s.ExpireMessages("topic1"))

	// topic1 messages should now be expired (expires set to past)
	expiredIDs, err := s.MessagesExpired()
	require.Nil(t, err)
	require.Equal(t, 2, len(expiredIDs))
	sort.Strings(expiredIDs)
	expectedIDs := []string{m1.ID, m2.ID}
	sort.Strings(expectedIDs)
	require.Equal(t, expectedIDs, expiredIDs)

	// topic2 should be unaffected
	messages, err = s.Messages("topic2", model.SinceAllMessages, false)
	require.Nil(t, err)
	require.Equal(t, 1, len(messages))
	require.Equal(t, "message 3", messages[0].Message)
}

func testMarkAttachmentsDeleted(t *testing.T, s message.Store) {
	// Add a message with an expired attachment (file needs cleanup)
	m1 := model.NewDefaultMessage("mytopic", "old file")
	m1.ID = "msg1"
	m1.SequenceID = "msg1"
	m1.Expires = time.Now().Add(time.Hour).Unix()
	m1.Attachment = &model.Attachment{
		Name:    "old.pdf",
		Type:    "application/pdf",
		Size:    50000,
		Expires: time.Now().Add(-time.Hour).Unix(), // Expired
		URL:     "https://ntfy.sh/file/old.pdf",
	}
	require.Nil(t, s.AddMessage(m1))

	// Add a message with another expired attachment
	m2 := model.NewDefaultMessage("mytopic", "another old file")
	m2.ID = "msg2"
	m2.SequenceID = "msg2"
	m2.Expires = time.Now().Add(time.Hour).Unix()
	m2.Attachment = &model.Attachment{
		Name:    "another.pdf",
		Type:    "application/pdf",
		Size:    30000,
		Expires: time.Now().Add(-time.Hour).Unix(), // Expired
		URL:     "https://ntfy.sh/file/another.pdf",
	}
	require.Nil(t, s.AddMessage(m2))

	// Both should show as expired attachments needing cleanup
	ids, err := s.AttachmentsExpired()
	require.Nil(t, err)
	require.Equal(t, 2, len(ids))

	// Mark msg1's attachment as deleted (file cleaned up)
	require.Nil(t, s.MarkAttachmentsDeleted("msg1"))

	// Now only msg2 should show as needing cleanup
	ids, err = s.AttachmentsExpired()
	require.Nil(t, err)
	require.Equal(t, 1, len(ids))
	require.Equal(t, "msg2", ids[0])

	// Mark msg2 too
	require.Nil(t, s.MarkAttachmentsDeleted("msg2"))

	// No more expired attachments to clean up
	ids, err = s.AttachmentsExpired()
	require.Nil(t, err)
	require.Equal(t, 0, len(ids))

	// Messages themselves still exist
	messages, err := s.Messages("mytopic", model.SinceAllMessages, false)
	require.Nil(t, err)
	require.Equal(t, 2, len(messages))
}

func testStats(t *testing.T, s message.Store) {
	// Initial stats should be zero
	messages, err := s.Stats()
	require.Nil(t, err)
	require.Equal(t, int64(0), messages)

	// Update stats
	require.Nil(t, s.UpdateStats(42))
	messages, err = s.Stats()
	require.Nil(t, err)
	require.Equal(t, int64(42), messages)

	// Update again (overwrites)
	require.Nil(t, s.UpdateStats(100))
	messages, err = s.Stats()
	require.Nil(t, err)
	require.Equal(t, int64(100), messages)
}

func testAddMessages(t *testing.T, s message.Store) {
	// Batch add multiple messages
	msgs := []*model.Message{
		model.NewDefaultMessage("mytopic", "batch 1"),
		model.NewDefaultMessage("mytopic", "batch 2"),
		model.NewDefaultMessage("othertopic", "batch 3"),
	}
	require.Nil(t, s.AddMessages(msgs))

	// Verify all were inserted
	messages, err := s.Messages("mytopic", model.SinceAllMessages, false)
	require.Nil(t, err)
	require.Equal(t, 2, len(messages))

	messages, err = s.Messages("othertopic", model.SinceAllMessages, false)
	require.Nil(t, err)
	require.Equal(t, 1, len(messages))
	require.Equal(t, "batch 3", messages[0].Message)

	// Empty batch should succeed
	require.Nil(t, s.AddMessages([]*model.Message{}))

	// Batch with invalid event type should fail
	badMsgs := []*model.Message{
		model.NewKeepaliveMessage("mytopic"),
	}
	require.NotNil(t, s.AddMessages(badMsgs))
}

func testMessagesDue(t *testing.T, s message.Store) {
	// Add a message scheduled in the past (i.e. it's due now)
	m1 := model.NewDefaultMessage("mytopic", "due message")
	m1.Time = time.Now().Add(-time.Second).Unix()
	// Set expires in the future so it doesn't get pruned
	m1.Expires = time.Now().Add(time.Hour).Unix()
	require.Nil(t, s.AddMessage(m1))

	// Add a message scheduled in the future (not due)
	m2 := model.NewDefaultMessage("mytopic", "future message")
	m2.Time = time.Now().Add(time.Hour).Unix()
	require.Nil(t, s.AddMessage(m2))

	// Mark m1 as published so it won't be "due"
	// (MessagesDue returns unpublished messages whose time <= now)
	// m1 is auto-published (time <= now), so it should not be due
	// m2 is unpublished (time in future), not due yet
	due, err := s.MessagesDue()
	require.Nil(t, err)
	require.Equal(t, 0, len(due))

	// Add a message that was explicitly scheduled in the past but time has "arrived"
	// We need to manipulate the database to create a truly "due" message:
	// a message with published=false and time <= now
	m3 := model.NewDefaultMessage("mytopic", "truly due message")
	m3.Time = time.Now().Add(2 * time.Second).Unix() // 2 seconds from now
	require.Nil(t, s.AddMessage(m3))

	// Not due yet
	due, err = s.MessagesDue()
	require.Nil(t, err)
	require.Equal(t, 0, len(due))

	// Wait for it to become due
	time.Sleep(3 * time.Second)

	due, err = s.MessagesDue()
	require.Nil(t, err)
	require.Equal(t, 1, len(due))
	require.Equal(t, "truly due message", due[0].Message)
}

func testMessageFieldRoundTrip(t *testing.T, s message.Store) {
	// Create a message with all fields populated
	m := model.NewDefaultMessage("mytopic", "hello world")
	m.SequenceID = "custom_seq_id"
	m.Title = "A Title"
	m.Priority = 4
	m.Tags = []string{"warning", "srv01"}
	m.Click = "https://example.com/click"
	m.Icon = "https://example.com/icon.png"
	m.Actions = []*model.Action{
		{
			ID:     "action1",
			Action: "view",
			Label:  "Open Site",
			URL:    "https://example.com",
			Clear:  true,
		},
		{
			ID:      "action2",
			Action:  "http",
			Label:   "Call Webhook",
			URL:     "https://example.com/hook",
			Method:  "PUT",
			Headers: map[string]string{"X-Token": "secret"},
			Body:    `{"key":"value"}`,
		},
	}
	m.ContentType = "text/markdown"
	m.Encoding = "base64"
	m.Sender = netip.MustParseAddr("9.8.7.6")
	m.User = "u_TestUser123"
	require.Nil(t, s.AddMessage(m))

	// Retrieve and verify every field
	retrieved, err := s.Message(m.ID)
	require.Nil(t, err)

	require.Equal(t, m.ID, retrieved.ID)
	require.Equal(t, "custom_seq_id", retrieved.SequenceID)
	require.Equal(t, m.Time, retrieved.Time)
	require.Equal(t, m.Expires, retrieved.Expires)
	require.Equal(t, model.MessageEvent, retrieved.Event)
	require.Equal(t, "mytopic", retrieved.Topic)
	require.Equal(t, "hello world", retrieved.Message)
	require.Equal(t, "A Title", retrieved.Title)
	require.Equal(t, 4, retrieved.Priority)
	require.Equal(t, []string{"warning", "srv01"}, retrieved.Tags)
	require.Equal(t, "https://example.com/click", retrieved.Click)
	require.Equal(t, "https://example.com/icon.png", retrieved.Icon)
	require.Equal(t, "text/markdown", retrieved.ContentType)
	require.Equal(t, "base64", retrieved.Encoding)
	require.Equal(t, netip.MustParseAddr("9.8.7.6"), retrieved.Sender)
	require.Equal(t, "u_TestUser123", retrieved.User)

	// Verify actions round-trip
	require.Equal(t, 2, len(retrieved.Actions))

	require.Equal(t, "action1", retrieved.Actions[0].ID)
	require.Equal(t, "view", retrieved.Actions[0].Action)
	require.Equal(t, "Open Site", retrieved.Actions[0].Label)
	require.Equal(t, "https://example.com", retrieved.Actions[0].URL)
	require.Equal(t, true, retrieved.Actions[0].Clear)

	require.Equal(t, "action2", retrieved.Actions[1].ID)
	require.Equal(t, "http", retrieved.Actions[1].Action)
	require.Equal(t, "Call Webhook", retrieved.Actions[1].Label)
	require.Equal(t, "https://example.com/hook", retrieved.Actions[1].URL)
	require.Equal(t, "PUT", retrieved.Actions[1].Method)
	require.Equal(t, "secret", retrieved.Actions[1].Headers["X-Token"])
	require.Equal(t, `{"key":"value"}`, retrieved.Actions[1].Body)
}
