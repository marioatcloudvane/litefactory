package messaging

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
)

func TestConversationCauseReplyNamesTypedCause(t *testing.T) {
	at := time.Date(2026, 10, 21, 16, 45, 0, 0, time.UTC)
	reset := at.Add(20 * time.Minute)
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"access", agent.ConversationError(&ConversationAccessError{RetryAfter: 30 * time.Second}, "preparation", true), "30 seconds"},
		{"access lost", &ConversationAccessError{Permanent: true}, "Discord"},
		{"format", &ConversationEnvelopeError{Reason: "x"}, "format check"},
		{"github", &github.RateLimitError{RetryAfter: time.Minute, Reset: reset}, reset.Local().Format("15:04")},
		{"provider", agent.ConversationError(errors.New("stream closed"), "stream", false), "provider failed"},
		{"provider not started", agent.ConversationError(errors.New("spawn failed"), "launch", true), "could not be started"},
	}
	for _, tc := range cases {
		reply := conversationCauseReply(tc.err, at)
		if !strings.Contains(reply, tc.want) || strings.Count(reply, ".") > 3 {
			t.Errorf("%s: %q", tc.name, reply)
		}
	}
	if reply := conversationCauseReply(errors.New("untyped"), at); reply != "" {
		t.Errorf("untyped cause named: %q", reply)
	}
}
