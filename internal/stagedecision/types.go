// Package stagedecision proposes exact current-stage decisions through owneraction.
package stagedecision

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
)

// ErrProtectedChange refuses revision guidance whose protected meaning changed.
var ErrProtectedChange = errors.New("protection changed revision guidance; submit corrected guidance")

// ErrPreviewTooLarge refuses a proposal that cannot be displayed completely.
var ErrPreviewTooLarge = errors.New("the complete decision preview is too large; shorten the proposed summary or revision guidance")

// Request contains intent and a proposed public summary, never authority or revisions.
type Request struct {
	TaskRef          string                 `json:"task_ref"`
	Stage            string                 `json:"stage"`
	Change           string                 `json:"change,omitempty"`
	ClientRequestKey string                 `json:"client_request_key"`
	ConversationRef  string                 `json:"conversation_ref,omitempty"`
	ConclusionRef    string                 `json:"conclusion_ref,omitempty"`
	DiscordChannelID string                 `json:"discord_channel_id,omitempty"`
	Summary          taskdiscussion.Summary `json:"summary"`
}

// Publisher reuses the conservative discussion publication journal.
type Publisher interface {
	Publish(context.Context, store.DiscussionConclusion) (string, error)
	Recover(context.Context, store.DiscussionConclusion) (string, bool, error)
}

// Boundary resolves and applies only existing owner transitions.
type Boundary interface {
	ResolveStageDecisionTarget(context.Context, string, string, string) (store.StageDecisionTarget, error)
	ApplyStageDecision(context.Context, store.StageDecisionTarget, string, string, time.Time) (store.StageDecisionReceipt, error)
	RecoverStageDecision(context.Context, string, time.Time) (store.StageDecisionReceipt, bool, error)
}

// Options supplies trusted dependencies; no client can replace these boundaries.
type Options struct {
	DB        *store.DB
	Now       func() time.Time
	Protect   func(context.Context, string) (string, error)
	Publisher Publisher
	Boundary  Boundary
}

// View separates confirmation, transition, replacement progress and delivery.
type View struct {
	OmittedAttempts  int                         `json:"omitted_attempts,omitempty"`
	DecisionRef      string                      `json:"decision_ref"`
	Proposal         owneraction.Proposal        `json:"proposal"`
	Transition       *store.StageDecisionReceipt `json:"transition,omitempty"`
	RevisionProgress string                      `json:"revision_progress"`
	Summary          string                      `json:"summary"`
	DeliveryState    string                      `json:"delivery_state"`
	DeliveryReason   string                      `json:"delivery_reason,omitempty"`
	CommentURL       string                      `json:"comment_url,omitempty"`
	Cancellation     string                      `json:"cancellation,omitempty"`
	NextActions      []string                    `json:"next_actions"`
}

type frozen struct {
	Target   store.StageDecisionTarget `json:"target"`
	Request  Request                   `json:"request"`
	Rendered string                    `json:"rendered"`
}

// PublicationRejected proves no POST was attempted for invalid frozen content.
type PublicationRejected struct{ Reason string }

func (e *PublicationRejected) Error() string { return e.Reason }
