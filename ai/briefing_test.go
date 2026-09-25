package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBriefinger_Briefing(t *testing.T) {
	var seenPrompt string
	client := newTestClient(t, &Config{Provider: "mock"})
	client.Mock().SetHandler(func(req *Request) (*Response, error) {
		require.Equal(t, FeatureDigest, req.Feature)
		seenPrompt = req.Prompt
		return &Response{Text: `{"headline": "Two incidents", "sections": [
			{"topic": "backups", "title": "Backup", "points": ["exit 2"]},
			{"topic": "_", "title": "Cross", "points": ["pattern"]}
		]}`}, nil
	})
	result, err := NewBriefinger(client).Briefing(context.Background(), "user:phil", &BriefingInput{
		Topics: []BriefingTopicMessages{
			{Topic: "backups", Messages: []DigestMessage{{ID: "m1", Message: "backup failed", Time: 1790300000}}},
			{Topic: "home", Messages: []DigestMessage{{ID: "m2", Message: "garage open", Time: 1790300100}}},
		},
	})
	require.Nil(t, err)
	require.Equal(t, 2, result.TopicCount)
	require.Equal(t, 2, result.MessageCount)
	require.Equal(t, BriefingDisclaimer, result.Disclaimer)
	require.Len(t, result.Sections, 2)
	require.Contains(t, seenPrompt, "## Topic: backups")
	require.Contains(t, seenPrompt, "## Topic: home")
}

func TestBriefinger_EmptyRejected(t *testing.T) {
	client := newTestClient(t, &Config{Provider: "mock"})
	_, err := NewBriefinger(client).Briefing(context.Background(), "", &BriefingInput{})
	require.Error(t, err)
}

func TestBriefinger_MessageBudgetShared(t *testing.T) {
	client := newTestClient(t, &Config{Provider: "mock"})
	client.Mock().SetHandler(func(req *Request) (*Response, error) {
		// First topic fills the entire budget; the second topic contributes nothing
		require.NotContains(t, req.Prompt, "second-topic-message")
		return &Response{Text: `{"headline": "h", "sections": []}`}, nil
	})
	first := make([]DigestMessage, DigestMaxMessages)
	for i := range first {
		first[i] = DigestMessage{ID: "a", Message: "filler-a"}
	}
	_, err := NewBriefinger(client).Briefing(context.Background(), "", &BriefingInput{
		Topics: []BriefingTopicMessages{
			{Topic: "first", Messages: first},
			{Topic: "second", Messages: []DigestMessage{{ID: "b", Message: "second-topic-message"}}},
		},
	})
	require.Nil(t, err)
	_ = strings.TrimSpace
}
