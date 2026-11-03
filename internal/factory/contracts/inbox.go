package contracts

import (
	"github.com/cloudvane/litefactory/internal/ticket"
	"time"
)

// InboxScope binds saved browsing content to its authenticated conversation.
type InboxScope struct{ Owner, Transport, Conversation string }

// InboxFilter selects recorded facts before paging.
type InboxFilter struct{ Source, Project, Kind string }

// InboxEvidence binds a display-only promotion claim to recorded request facts.
type InboxEvidence struct {
	ID, Kind, DecisionKey, Source, Revision, Excerpt, Target string
}

// InboxDecision is a read projection, never workflow authority.
type InboxDecision struct {
	WaitingAt                                                                                             time.Time
	WaitingProvenance                                                                                     string
	Evidence                                                                                              []InboxEvidence
	Key, Source, Project, Ticket, WaitID, Workflow, Kind, Summary, Destination, Action, ActionUnavailable string
	Ordinal                                                                                               int
	RepoID, ConnectionID, SessionID                                                                       int64
	Locator                                                                                               ticket.Locator
	Ref                                                                                                   ticket.Ref
}

// InboxCoverage describes configured ticket coverage without credential fields.
type InboxCoverage struct {
	RepoID, ConnectionID    int64
	Source, Project, Status string
	ObservedAt              time.Time
}

// InboxSnapshot is immutable captured recorded state, not an atomic provider view.
type InboxSnapshot struct {
	ID                    string
	Scope                 InboxScope
	Filter                InboxFilter
	CapturedAt, ExpiresAt time.Time
	Coverage              []InboxCoverage
	Total                 int
}

// InboxPage uses saved ordinals; Next is -1 at the end.
type InboxPage struct {
	Snapshot     InboxSnapshot
	Entries      []InboxDecision
	Offset, Next int
}

// InboxOrganization is an immutable display projection of a complete snapshot.
type InboxOrganization struct {
	Version        int                    `json:"version"`
	Layout         string                 `json:"layout"`
	Mode           string                 `json:"mode"`
	FallbackReason string                 `json:"fallback_reason,omitempty"`
	Tickets        []InboxOrganizedTicket `json:"tickets"`
}

// InboxOrganizedTicket retains all request identities for one qualified ticket.
type InboxOrganizedTicket struct {
	Key             string   `json:"key"`
	DecisionKeys    []string `json:"decision_keys"`
	Summary         string   `json:"summary"`
	PromotionReason string   `json:"promotion_reason,omitempty"`
	EvidenceIDs     []string `json:"evidence_ids,omitempty"`
}
