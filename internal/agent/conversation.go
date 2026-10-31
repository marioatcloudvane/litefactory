package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
)

// ErrConversationIsolation refuses a provider without established conversation controls.
var ErrConversationIsolation = errors.New("conversation isolation is unavailable or unverified")

// ErrConversationSessionMissing reports unavailable exact native continuity.
var ErrConversationSessionMissing = errors.New("saved conversation session is missing")

// ErrConversationBusy prevents overlap with a previously owned process.
var ErrConversationBusy = errors.New("conversation provider is still running")

// ErrConversationRefused marks a turn the provider's safety filter refused.
var ErrConversationRefused = errors.New("conversation provider refused the message")

// ErrConversationAuthentication marks a turn the provider could not sign in for.
var ErrConversationAuthentication = errors.New("conversation provider authentication failed")

// ConversationRequest contains only prompt data and one private native session identity.
type ConversationRequest struct {
	ConversationID, TurnID, Model, StateDirectory, Prompt string
	NativeInstructions                                    string
	// Effort is a native reasoning effort (for example "high"), validated by
	// the caller for this model. Empty leaves the native default.
	Effort  string
	Session SessionRef
	// OnSession must durably record the exact identity before an answer is accepted.
	OnSession func(context.Context, SessionRef) error
	// BeforeLaunch rechecks durable worker fencing while native process ownership is held.
	BeforeLaunch func(context.Context) error
}

// Validate checks the prompt boundary and exact identity before launch.
func (r ConversationRequest) Validate() error {
	if strings.TrimSpace(r.NativeInstructions) == "" || len(r.NativeInstructions) > 256<<10 || r.ConversationID == "" || r.TurnID == "" || strings.TrimSpace(r.Model) == "" || strings.TrimSpace(r.Prompt) == "" || len(r.Prompt) > 256<<10 || !filepath.IsAbs(r.StateDirectory) || filepath.Clean(r.StateDirectory) != r.StateDirectory || r.OnSession == nil || r.BeforeLaunch == nil || !ConversationIdentity(r.Session.Ref) || (r.Session.Resume != "" && !ConversationIdentity(r.Session.Resume)) || !conversationEffort(r.Effort) {
		return errors.New("invalid conversation provider context")
	}
	return nil
}

func conversationEffort(effort string) bool {
	switch effort {
	case "", "low", "medium", "high", "xhigh", "max":
		return true
	}
	return false
}

// ConversationIdentity excludes selectors, paths and command options from native handles.
func ConversationIdentity(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-':
		default:
			return false
		}
	}
	return s[0] != '-'
}

// ConversationResult carries an answer and its verified native identity.
type ConversationResult struct {
	Answer  []byte
	Session SessionRef
}

// ConversationPrompt serializes separate fields; callers redact each field before persistence.
func ConversationPrompt(nativeInstructions, owner, prepared, recovery string) (string, error) {
	if strings.TrimSpace(nativeInstructions) == "" || len(nativeInstructions) > 256<<10 || strings.TrimSpace(owner) == "" || len(owner) > 128<<10 || len(prepared) > 64<<10 || len(recovery) > 8<<10 {
		return "", errors.New("conversation prompt byte budget exceeded or empty owner input")
	}
	body, err := json.Marshal(struct{ Instructions, OwnerInput, PreparedContext, RecoveryNotice string }{nativeInstructions, owner, prepared, recovery})
	if err != nil {
		return "", err
	}
	if len(body) > 256<<10 {
		return "", errors.New("conversation prompt byte budget exceeded")
	}
	return string(body), nil
}

// ConversationFailure records the failure boundary. NoProcessStarted is affirmative
// evidence, never inferred from a missing identity or an exited process.
type ConversationFailure struct {
	Phase            string
	NoProcessStarted bool
	Cause            error
}

func (e *ConversationFailure) Error() string { return e.Cause.Error() }
func (e *ConversationFailure) Unwrap() error { return e.Cause }

// ConversationError preserves more precise metadata supplied by a lower boundary.
func ConversationError(err error, phase string, noProcessStarted bool) error {
	if err == nil {
		return nil
	}
	var failure *ConversationFailure
	if errors.As(err, &failure) {
		return err
	}
	return &ConversationFailure{Phase: phase, NoProcessStarted: noProcessStarted, Cause: err}
}
