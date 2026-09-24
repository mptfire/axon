package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatter_Chat(t *testing.T) {
	var seenPrompt string
	client := newTestClient(t, &Config{Provider: "mock"})
	client.Mock().SetHandler(func(req *Request) (*Response, error) {
		require.Equal(t, FeatureChat, req.Feature)
		require.Equal(t, "user:phil", req.UserKey)
		seenPrompt = req.Prompt
		return &Response{Text: `{"answer": "The last backup failed at 02:00 with exit code 2.", "citations": ["m1", "invented-id"]}`}, nil
	})
	messages := []DigestMessage{
		{ID: "m1", Message: "backup failed exit 2", Priority: 4, Time: 1790300000},
		{ID: "m2", Message: "unrelated chatter", Time: 1790300500},
	}
	answer, err := NewChatter(client).Chat(context.Background(), "user:phil", "backups", "when did the last backup fail?", messages, nil)
	require.Nil(t, err)
	require.Contains(t, answer.Answer, "02:00")
	require.Equal(t, []string{"m1"}, answer.Citations) // invented-id stripped
	require.Contains(t, seenPrompt, "id=m1")
	require.Contains(t, seenPrompt, "when did the last backup fail?")
}

func TestChatter_RejectsEmptyInput(t *testing.T) {
	client := newTestClient(t, &Config{Provider: "mock"})
	_, err := NewChatter(client).Chat(context.Background(), "", "t", "  ", []DigestMessage{{ID: "m", Message: "x"}}, nil)
	require.Error(t, err)
	_, err = NewChatter(client).Chat(context.Background(), "", "t", "question", nil, nil)
	require.Error(t, err)
}

func TestChatter_GarbageAndEmptyAnswer(t *testing.T) {
	client := newTestClient(t, &Config{Provider: "mock"})
	client.Mock().EnqueueText("prose not json")
	_, err := NewChatter(client).Chat(context.Background(), "", "t", "q", []DigestMessage{{ID: "m", Message: "x"}}, nil)
	require.ErrorIs(t, err, ErrInvalidResponse)

	client2 := newTestClient(t, &Config{Provider: "mock"})
	client2.Mock().EnqueueText(`{"answer": ""}`)
	_, err = NewChatter(client2).Chat(context.Background(), "", "t", "q", []DigestMessage{{ID: "m", Message: "x"}}, nil)
	require.ErrorIs(t, err, ErrInvalidResponse)
}

func TestChatter_AnswerSanitized(t *testing.T) {
	client := newTestClient(t, &Config{Provider: "mock"})
	client.Mock().SetHandler(func(req *Request) (*Response, error) {
		return &Response{Text: `{"answer": "line1\u0000line2 ` + strings.Repeat("x", 5000) + `", "citations": []}`}, nil
	})
	answer, err := NewChatter(client).Chat(context.Background(), "", "t", "q", []DigestMessage{{ID: "m", Message: "x"}}, nil)
	require.Nil(t, err)
	require.NotContains(t, answer.Answer, "\u0000")
	require.NotContains(t, answer.Answer, "\x00")
	require.LessOrEqual(t, len([]rune(answer.Answer)), ChatMaxAnswerChars)
}

func TestChatter_MultiTurn(t *testing.T) {
	var seenPrompt string
	client := newTestClient(t, &Config{Provider: "mock"})
	client.Mock().SetHandler(func(req *Request) (*Response, error) {
		seenPrompt = req.Prompt
		return &Response{Text: `{"answer": "It failed twice that night.", "citations": ["m1"]}`}, nil
	})
	prior := []ChatTurn{{Question: "when did the backup fail?", Answer: "At 02:00."}}
	answer, err := NewChatter(client).Chat(context.Background(), "user:phil", "backups", "how often?", []DigestMessage{{ID: "m1", Message: "failed"}}, prior)
	require.Nil(t, err)
	require.Equal(t, "It failed twice that night.", answer.Answer)
	require.Contains(t, seenPrompt, "Earlier in this conversation:")
	require.Contains(t, seenPrompt, "Q: when did the backup fail?")
	require.Contains(t, seenPrompt, "A: At 02:00.")
	require.Contains(t, seenPrompt, "Question: how often?")
}
