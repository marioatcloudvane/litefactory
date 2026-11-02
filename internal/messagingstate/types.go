// Package messagingstate defines conversation and delivery values without persistence access.
package messagingstate

import (
	"github.com/cloudvane/litefactory/internal/retainedevidence"
	"time"
)

// Conversation is a durable channel binding. Its name is a label, never a path.
type Conversation struct {
	ID, Transport, ChannelRef, OwnerID, Name, Provider, SessionHandle, StateDir, HealthReason string
	Revision, NextSequence                                                                    int64
	// FreshAfter is the sequence of the owner's last fresh-conversation turn; 0 means never.
	FreshAfter             int64
	Active, DeliveryPaused bool
}

// ConversationTurn journals the launch boundary and durable delivery identity.
type ConversationTurn struct {
	ID, ConversationID, SourceID, Source, ReplyTo, Prompt, State, Worker, ReplyID, DiscordMessageID string
	Sequence, Generation                                                                            int64
	LeaseUntil, CreatedAt, FinishedAt, DeliveredAt                                                  time.Time
}

// ConversationAnswer freezes presentation identity separately from retained evidence.
type ConversationAnswer struct {
	ID, TurnID, Transport, OwnerID, ChannelRef, Blocks string
	Version, ProposalPart                              int
	CreatedAt, CompletedAt, ExpiredAt                  time.Time
}

// ConversationAnswerPart holds exact saved delivery bytes and acknowledgment metadata.
type ConversationAnswerPart struct {
	ReplyID, Text, Attachment, DiscordMessageID string
	Ordinal                                     int
	AcknowledgedAt                              time.Time
}

// ConversationExchange is private evidence for one bounded provider step.
// Request and Result contain validated, redacted JSON owned by the factory.
type ConversationExchange struct {
	TurnID                                       string
	Ordinal                                      int
	Kind, Request, Result, ProviderAttempt, Gaps string
	CapturedAt                                   time.Time
	Complete                                     bool
}

// ConversationReference binds source bytes or a target revision to its scope.
type ConversationReference struct {
	ID, TurnID, Kind, Repository, Target, Revision, Content string
	ExpiresAt                                               time.Time
}

// ConversationActionLink preserves proposal and saved effect identities independently of narration.
type ConversationActionLink struct {
	TurnID, DialogueID, ExecutionID, PreviewID, PayloadDigest, Outcome string
	DialogueRevision                                                   int64
}

// ConversationEvidence is a protected, bounded view of an admitted source.
// Entry identifies captured bytes; ViewDigest identifies the derived view.
// SourceTurnID pins retention to its original turn, so a follow-up cannot extend it.
// Read methods provide no access grant: callers recheck owner/channel/repository.
type ConversationEvidence struct {
	TurnID, SourceTurnID  string
	Ordinal               int
	Repository, Issue     string
	CapturedAt, ExpiresAt time.Time
	Entry                 retainedevidence.Entry
	Content               string
	RangeStart, RangeEnd  int64
	ViewDigest            string
}

// ConversationLegacyInstructions are the exact pinned pre-migration values.
// They are used only to check available saved instruction evidence for conflicts.
type ConversationLegacyInstructions struct{ Native, Factory, Correction, ActionOutcome string }

// ChatReply contains only the durable command and delivery state, never tokens.
type ChatReply struct {
	OutcomeStatus                                                          string
	ConversationTurnID                                                     string
	Transport, MessageID, ChannelRef, Command, State, ReplyText, TargetRef string
	PresentationState, PresentationOutcome, ReplyAttachment                string
	PresentationGeneration, PresentationCheck                              bool
	Arguments                                                              map[string]string
	Question, ProjectOption                                                string
	ResolvedRepoID                                                         int64
	AnswerAttempts                                                         int
	Attempts                                                               int
	NextAttemptAt, CreatedAt, FinishedAt, LastErrorAt                      time.Time
}

// ChatNotice shares the reply delivery state without an interaction handle.
type ChatNotice struct {
	ChatReply
	TaskRef string
	// After names a confirmed conversation action (its execution identity):
	// the notice waits until the conversation has posted that confirmation,
	// for at most noticeHold.
	After string
}

// ChatIssuePreview holds only the protected preview and its short-lived outcome.
// ClaimVersion fences stale workers; no transaction is held during forge I/O.
type ChatIssuePreview struct {
	ID, Transport, ChannelRef, SourceMessageID, LastConfirmMessageID, Code, ContentHash, Title, Body, State string
	RepoID                                                                                                  int64
	CreatedAt, ExpiresAt, LastAttemptAt, LeaseUntil, FinishedAt                                             time.Time
	ClaimVersion, IssueNumber                                                                               int
	IssueLink                                                                                               string
}

// NaturalDialogue is the single current protected proposal or clarification.
type NaturalDialogue struct {
	Transport, OwnerID, ChannelRef, State, SourceID, Payload, Digest, Code, PromptID string
	Revision                                                                         int64
	ExpiresAt, UpdatedAt                                                             time.Time
}

// NaturalExecution owns the immutable invocation and recovery identity.
type NaturalExecution struct {
	Identity, Transport, OwnerID, ChannelRef, Payload, State, Outcome string
	Revision                                                          int64
	CreatedAt, FinishedAt                                             time.Time
}

// NaturalScan retains a closed interval's progress without admitting partial history.
type NaturalScan struct{ ChannelRef, Cursor, HighWater, Before string }

// NaturalMessage contains only prefiltered owner text from one allowed channel.
type NaturalMessage struct {
	Transport, ChannelRef, SourceID, OwnerID, ReplyTo, Text string
	CreatedAt                                               time.Time
}

// AlertControlScan keeps an authenticated history interval independent of the
// conversation backlog. Complete intervals retain only reserved control texts.
type AlertControlScan struct {
	Cursor, HighWater, Before string
	Messages                  []NaturalMessage
	Complete                  bool
}
