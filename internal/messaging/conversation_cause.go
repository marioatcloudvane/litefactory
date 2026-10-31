package messaging

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
		return "The model provider failed before a completed answer was saved; its completion is unknown."
	}
	return ""
}

// conversationProviderReply answers a provider safety refusal or login failure
// in plain words; "" for every other cause.
func conversationProviderReply(err error) string {
	switch {
	case errors.Is(err, agent.ErrConversationRefused):
		return "Claude's safety filter blocked this message, probably a false alarm. Please rephrase it. If it keeps happening, send \"new conversation\" to start a fresh conversation."
	case errors.Is(err, agent.ErrConversationAuthentication):
		return "Claude could not sign in to its account, so I didn't answer. Ask the factory operator to sign Claude in again on the factory host, then send your message again."
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
		if reply := conversationArgumentCause(err); reply != "" {
			return reply
		}
		return "My answer was rejected by the format check, after the available corrections, so I didn't send it. Please ask again."
	case errors.Is(err, context.DeadlineExceeded):
		return "The model took too long to answer; completion is unknown."
	}
	return ""
}

// conversationArgumentCause tells the owner what to say when one argument of a
// rejected answer could not be grounded or was too long; "" otherwise.
func conversationArgumentCause(err error) string {
	var rejected *ConversationEnvelopeError
	if !errors.As(err, &rejected) || rejected.Argument == nil || rejected.Argument.Name == "" {
		return ""
	}
	name := rejected.Argument.Name
	label := strings.ReplaceAll(name, "_", " ")
	switch {
	case rejected.Argument.Limit > 0:
		return fmt.Sprintf("The %s is longer than the %d bytes I can pass on, so I didn't act. Please shorten it.", label, rejected.Argument.Limit)
	case !rejected.Ungrounded:
		return ""
	case name == "issue" || name == "task":
		return `I couldn't tell which issue you meant, so I didn't act. Name it in your message, for example "diagnose #1100".`
	case name == "project":
		return `I couldn't tell which project you meant, so I didn't act. Name it in your message, for example "in litefactory".`
	case conversationProtectedContent(name) || standardsProtectedContent(name):
		// Only standards commands declare the standards prose fields.
		return fmt.Sprintf("I couldn't find the %s word for word in your messages, so I didn't act. Please send it in your message itself.", label)
	}
	return fmt.Sprintf("I couldn't tell which %s you meant, so I didn't act. Please name it in your message.", label)
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
