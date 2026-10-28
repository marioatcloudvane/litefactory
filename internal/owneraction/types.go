// Package owneraction owns confirmation policy independently of its transports.
package owneraction

import (
	"context"
	"time"
)

// Invocation is a request to resolve an enabled action, never authorization.
type Invocation struct {
	Command   string            `json:"command"`
	Arguments map[string]string `json:"arguments"`
}

// Principal is supplied by authenticated transport composition, never tool input.
type Principal struct {
	OwnerID         string `json:"owner_id"`
	OwnerGeneration int64  `json:"owner_generation"`
	Transport       string `json:"transport"`
	ClientID        string `json:"client_id,omitempty"`
	GrantID         string `json:"grant_id,omitempty"`
	ChannelID       string `json:"channel_id,omitempty"`
	DiscordOwnerID  string `json:"discord_owner_id,omitempty"`
}

// Facts are frozen before availability; Payload is the domain's protected input.
type Facts struct {
	Invocation     Invocation `json:"invocation"`
	RepositoryID   int64      `json:"repository_id"`
	RepositoryRef  string     `json:"repository_ref"`
	TargetKind     string     `json:"target_kind"`
	TargetID       string     `json:"target_id"`
	TargetRevision string     `json:"target_revision"`
	Summary        string     `json:"summary"`
	Estimate       string     `json:"estimate"`
	Payload        string     `json:"-"`
}

// Result separates accepted background work from confirmed external completion.
type Result struct {
	State     string `json:"state"`
	Summary   string `json:"summary"`
	Reference string `json:"reference,omitempty"`
}

// Domain reuses existing target resolvers, handlers and operation receipts.
type Domain interface {
	Resolve(context.Context, Principal, Invocation, time.Time) (Facts, error)
	Recheck(context.Context, Principal, Facts, time.Time) error
	Execute(context.Context, Principal, Facts, string, time.Time) (Result, error)
	Recover(context.Context, Principal, Facts, string, time.Time) (Result, bool, error)
}

// Proposal is the exact owner review plus its recorded lifecycle.
type Proposal struct {
	ID               string    `json:"proposal_id"`
	Principal        Principal `json:"principal"`
	Facts            Facts     `json:"facts"`
	Tier             string    `json:"tier"`
	State            string    `json:"state"`
	Reason           string    `json:"reason,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	AvailableAt      time.Time `json:"available_at"`
	ConfirmNotBefore time.Time `json:"confirm_not_before"`
	ExpiresAt        time.Time `json:"expires_at"`
	CompletedAt      time.Time `json:"completed_at,omitempty"`
	ExecutionID      string    `json:"execution_id,omitempty"`
	ProposalMessage  string    `json:"proposal_message,omitempty"`
	ApprovalChannel  string    `json:"approval_channel,omitempty"`
	ApprovalMessage  string    `json:"approval_message,omitempty"`
	DeliveryRetryAt  time.Time `json:"delivery_retry_at,omitempty"`
	DeliveryAttempts int       `json:"delivery_attempts"`
	DeliveryState    string    `json:"delivery_state,omitempty"`
	Result           Result    `json:"result"`
	Attempts         []Attempt `json:"attempts,omitempty"`
}

// Attempt is durable timed confirmation or cancellation evidence.
type Attempt struct {
	ID         int64     `json:"id"`
	ReceivedAt time.Time `json:"received_at"`
	Method     string    `json:"method"`
	Principal  string    `json:"principal"`
	Decision   string    `json:"decision"`
	State      string    `json:"state"`
	Evidence   string    `json:"evidence,omitempty"`
}

// Page is a stable authorized history page; the client carries its cursor.
type Page struct {
	Proposals  []Proposal `json:"proposals"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

// Reaction contains authenticated gateway fields, never MCP arguments.
type Reaction struct {
	ReceivedAt      time.Time
	OwnerID         string
	OwnerGeneration int64
	ChannelID       string
	MessageID       string
	UserID          string
	Bot             bool
	Emoji           string
	EmojiID         string
}

// Delivery sends the exact approval facts and returns a confirmed message identity.
type Delivery interface {
	Send(context.Context, Proposal) (string, error)
}

// Filters restrict history without granting any additional visibility.
type Filters struct {
	Action     string `json:"action,omitempty"`
	Repository string `json:"repository,omitempty"`
	State      string `json:"state,omitempty"`
}

// DeliveryRetryError represents a provider-confirmed refusal before sending.
// Only this error permits retry; ambiguous transport failures never do.
type DeliveryRetryError struct{ RetryAfter time.Duration }

func (e *DeliveryRetryError) Error() string {
	return "Discord refused delivery temporarily; waiting for the provider retry time"
}

// ActorID is the authenticated owner identity on the originating transport.
// Domain adapters use it without importing transport-specific identity types.
func (p Principal) ActorID() string {
	if p.Transport == "discord" {
		return p.DiscordOwnerID
	}
	return p.OwnerID
}
