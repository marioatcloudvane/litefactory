package messaging

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
)

// conversationCauseReply names a known failure cause in one or two plain
// sentences for the owner, without internal identifiers. It returns "" when the
// cause is not one of the typed ones, so callers keep their generic notice.
func conversationCauseReply(err error, at time.Time) string {
	if reply := conversationSpecificCause(err, at); reply != "" {
		return reply
	}
	var failure *agent.ConversationFailure
	switch {
	case errors.As(err, &failure) && failure.NoProcessStarted:
		return "The model provider could not be started for this message. Please ask again."
	case errors.As(err, &failure):
		return "The model provider failed before finishing an answer. Please ask again."
	}
	return ""
}

// conversationSpecificCause names only causes more specific than a generic
// provider failure: Discord access, GitHub budget, format rejection, timeout.
func conversationSpecificCause(err error, at time.Time) string {
	var rate *github.RateLimitError
	var access *ConversationAccessError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &rate):
		return fmt.Sprintf("GitHub's API budget is used up, so I couldn't look that up. Please ask again after %s.", githubBudgetUntil(rate, at))
	case errors.As(err, &access) && access.Permanent:
		return "I could not confirm access to this Discord channel, so I didn't answer."
	case errors.As(err, &access):
		return fmt.Sprintf("The Discord access check was unavailable, so I didn't answer. Please try again in about %s.", plainWait(accessRetry(err)))
	case errors.Is(err, ErrConversationEnvelope):
		return "My answer was rejected by the format check, even after one retry, so I didn't send it. Please ask again."
	case errors.Is(err, context.DeadlineExceeded):
		return "The model took too long to answer. Please ask again."
	}
	return ""
}

// githubBudgetGap adds the GitHub budget pause to a lookup gap when that was
// the reason a read failed.
func githubBudgetGap(err error, at time.Time) string {
	var rate *github.RateLimitError
	if !errors.As(err, &rate) {
		return ""
	}
	return " GitHub's API budget is used up until " + githubBudgetUntil(rate, at) + "."
}

func githubBudgetUntil(rate *github.RateLimitError, at time.Time) string {
	until := at.Add(rate.RetryAfter)
	if rate.Reset.After(until) {
		until = rate.Reset
	}
	return until.Local().Format("15:04 MST")
}

func plainWait(d time.Duration) string {
	switch {
	case d <= time.Minute:
		return fmt.Sprintf("%d seconds", int(max(time.Second, d.Round(time.Second))/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int((d+time.Minute-1)/time.Minute))
	default:
		return fmt.Sprintf("%d hours", int((d+time.Hour-1)/time.Hour))
	}
}
