package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDigester_Digest(t *testing.T) {
	var seenPrompt string
	client := newTestClient(t, &Config{Provider: "mock"})
	client.Mock().SetHandler(func(req *Request) (*Response, error) {
		require.Equal(t, FeatureDigest, req.Feature)
		require.Equal(t, "user:phil", req.UserKey)
		seenPrompt = req.Prompt
		return &Response{Text: `{"headline": "Backups failed overnight", "sections": [{"title": "db-1 failures", "points": ["3 failures between 02:00 and 04:00", "Exit code 2"]}]}`}, nil
	})
	result, err := NewDigester(client).Digest(context.Background(), "user:phil", &DigestInput{
		Topic:  "backups",
		Locale: "en",
		Messages: []DigestMessage{
			{ID: "m1", Message: "backup failed", Priority: 4, Time: 1790300000},
			{ID: "m2", Title: "retry", Message: "backup failed again", Time: 1790300600},
		},
	})
	require.Nil(t, err)
	require.Equal(t, "backups", result.Topic)             // server-stamped
	require.Equal(t, 2, result.MessageCount)              // server-stamped
	require.Equal(t, DigestDisclaimer, result.Disclaimer) // server-stamped
	require.Equal(t, "Backups failed overnight", result.Headline)
	require.Len(t, result.Sections, 1)
	require.Len(t, result.Sections[0].Points, 2)
	require.Contains(t, seenPrompt, "Topic: backups")
	require.Contains(t, seenPrompt, "backup failed again")
	require.Contains(t, seenPrompt, "(4/5)")
}

func TestDigester_EmptyMessagesRejected(t *testing.T) {
	client := newTestClient(t, &Config{Provider: "mock"})
	_, err := NewDigester(client).Digest(context.Background(), "", &DigestInput{Topic: "t"})
	require.Error(t, err)
}

func TestDigester_MessageCapKeepsMostRecent(t *testing.T) {
	client := newTestClient(t, &Config{Provider: "mock"})
	var seenNewest bool
	client.Mock().SetHandler(func(req *Request) (*Response, error) {
		seenNewest = strings.Contains(req.Prompt, "newest message")
		require.NotContains(t, req.Prompt, "oldest message")
		return &Response{Text: `{"headline": "h", "sections": []}`}, nil
	})
	messages := make([]DigestMessage, DigestMaxMessages+10)
	for i := range messages {
		messages[i] = DigestMessage{ID: "m", Message: "filler"}
	}
	messages[len(messages)-1].Message = "newest message"
	messages[0].Message = "oldest message"
	_, err := NewDigester(client).Digest(context.Background(), "", &DigestInput{Topic: "t", Messages: messages})
	require.Nil(t, err)
	require.True(t, seenNewest)
}

func TestDigester_GarbageRejectedAndSanitized(t *testing.T) {
	client := newTestClient(t, &Config{Provider: "mock"})
	client.Mock().EnqueueText("prose, not json")
	_, err := NewDigester(client).Digest(context.Background(), "", &DigestInput{Topic: "t", Messages: []DigestMessage{{Message: "x"}}})
	require.ErrorIs(t, err, ErrInvalidResponse)

	client2 := newTestClient(t, &Config{Provider: "mock"})
	client2.Mock().EnqueueText(`{"headline": "", "sections": []}`)
	_, err = NewDigester(client2).Digest(context.Background(), "", &DigestInput{Topic: "t", Messages: []DigestMessage{{Message: "x"}}})
	require.ErrorIs(t, err, ErrInvalidResponse)

	// Oversized output capped: many sections/points, huge headline
	client3 := newTestClient(t, &Config{Provider: "mock"})
	client3.Mock().SetHandler(func(req *Request) (*Response, error) {
		sections := make([]string, 10)
		for i := range sections {
			sections[i] = `{"title": "s", "points": ["` + strings.Repeat("p", 400) + `","a","b","c","d","e"]}`
		}
		return &Response{Text: `{"headline": "` + strings.Repeat("h", 500) + `", "sections": [` + strings.Join(sections, ",") + `]}`}, nil
	})
	result, err := NewDigester(client3).Digest(context.Background(), "", &DigestInput{Topic: "t", Messages: []DigestMessage{{Message: "x"}}})
	require.Nil(t, err)
	require.LessOrEqual(t, len(result.Headline), DigestMaxHeadlineChars)
	require.Len(t, result.Sections, DigestMaxSections)
	require.LessOrEqual(t, len(result.Sections[0].Points), DigestMaxPoints)
	require.LessOrEqual(t, len(result.Sections[0].Points[0]), DigestMaxPointChars)
}

func TestNewDigesterWithEmbedder_PromptContainsClusterHints(t *testing.T) {
	var seenPrompt string
	client := newTestClient(t, &Config{Provider: "mock"})
	client.Mock().SetHandler(func(req *Request) (*Response, error) {
		seenPrompt = req.Prompt
		return &Response{Text: `{"headline": "h", "sections": []}`}, nil
	})
	client.Mock().SetEmbedHandler(func(text string) ([]float32, error) {
		if strings.Contains(text, "disk") {
			return []float32{1, 0.05}, nil
		}
		return []float32{0.05, 1}, nil
	})
	embedder := client.Embedder("embed-model")
	digester := NewDigesterWithEmbedder(client, embedder)
	_, err := digester.Digest(context.Background(), "", &DigestInput{
		Topic: "ops",
		Messages: []DigestMessage{
			{Message: "disk alert one"},
			{Message: "disk alert two"},
			{Message: "garden gate open"},
			{Message: "garden gate still open"},
		},
	})
	require.Nil(t, err)
	require.Contains(t, seenPrompt, "Semantic pre-clustering")
}
