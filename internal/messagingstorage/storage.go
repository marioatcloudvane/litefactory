// Package messagingstorage provides the conversation and delivery persistence port.
// The adapter keeps compound capture and acknowledgment transactions indivisible.
package messagingstorage

import (
	"context"
	"github.com/cloudvane/litefactory/internal/messagingstate"
	"github.com/cloudvane/litefactory/internal/store"
	"time"
)

// Port owns conversation, transport intake, channel delivery and their recovery.
// Factory policy, repository records and usage snapshots are deliberately absent.
type Port interface {
	ActivateAlertControlScan(ctx context.Context, channel, boundary string) error
	ActivateNaturalScan(ctx context.Context, channel, boundary string) error
	AdmitChatReply(ctx context.Context, r messagingstate.ChatReply) (bool, error)
	AdmitConversationDetails(ctx context.Context, r messagingstate.ChatReply, owner, selector string, at time.Time) (bool, error)
	AdmitConversationTurn(ctx context.Context, id, source, prompt string, at time.Time) (messagingstate.ConversationTurn, error)
	BackfillLegacyConversationCapture(ctx context.Context, id string, raw []byte, expected messagingstate.ConversationLegacyInstructions, at time.Time) error
	BindNaturalPrompt(ctx context.Context, d messagingstate.NaturalDialogue, prompt string) error
	ClaimChatNotice(ctx context.Context, transport string, at time.Time) (messagingstate.ChatReply, error)
	ClaimChatReply(ctx context.Context, transport string, at time.Time) (messagingstate.ChatReply, error)
	ClaimConversationReply(ctx context.Context, transport string, at time.Time) (messagingstate.ChatReply, error)
	ClaimConversationTurn(ctx context.Context, id, worker string, at time.Time, lease time.Duration) (messagingstate.ConversationTurn, error)
	CompleteConversationAnswer(ctx context.Context, t messagingstate.ConversationTurn, state string, a messagingstate.ConversationAnswer, parts []messagingstate.ConversationAnswerPart, at time.Time) error
	CompleteConversationFreshAnswer(ctx context.Context, t messagingstate.ConversationTurn, a messagingstate.ConversationAnswer, parts []messagingstate.ConversationAnswerPart, at time.Time) error
	CompleteConversationFreshTurn(ctx context.Context, t messagingstate.ConversationTurn, reply string, at time.Time) error
	CompleteConversationHandledAnswer(ctx context.Context, t messagingstate.ConversationTurn, a messagingstate.ConversationAnswer, parts []messagingstate.ConversationAnswerPart, at time.Time) error
	CompleteConversationHandledTurn(ctx context.Context, t messagingstate.ConversationTurn, reply string, at time.Time) error
	CompleteConversationPreStartTurn(ctx context.Context, t messagingstate.ConversationTurn, reply string, at time.Time) error
	CompleteConversationTurn(ctx context.Context, t messagingstate.ConversationTurn, state, reply string, at time.Time) error
	ConversationDetailsReplySelection(ctx context.Context, r messagingstate.ChatReply) (string, error)
	ConversationDetailsSelection(ctx context.Context, r messagingstate.ChatReply) (string, error)
	ConversationReplyAnswer(ctx context.Context, r messagingstate.ChatReply) (messagingstate.ConversationAnswer, int, error)
	CurrentChatIssuePreview(ctx context.Context, transport, channel string) (messagingstate.ChatIssuePreview, error)
	DeferConversationTurn(ctx context.Context, turn messagingstate.ConversationTurn) error
	EnsureConversationWithCapture(ctx context.Context, c messagingstate.Conversation, capture []byte, at time.Time) (messagingstate.Conversation, error)
	FinishAlertControlMessage(ctx context.Context, channel, source string) error
	FinishChatNotice(ctx context.Context, r messagingstate.ChatReply, failed bool, at time.Time) error
	FinishChatReply(ctx context.Context, r messagingstate.ChatReply, failed bool, at time.Time) error
	FinishConversationActionReply(ctx context.Context, t messagingstate.ConversationTurn, r messagingstate.ChatReply, at time.Time) error
	FinishConversationReply(ctx context.Context, r messagingstate.ChatReply, discordID string, at time.Time) error
	FinishNaturalExecution(ctx context.Context, identity, outcome string, uncertain bool, at time.Time) error
	FinishNaturalMessage(ctx context.Context, channel, id string, at time.Time) error
	FinishWorkflowChatNotice(ctx context.Context, r messagingstate.ChatReply, state, message, reason string, next, at time.Time) error
	GetConversationActionLink(ctx context.Context, turnID string) (messagingstate.ConversationActionLink, error)
	LatestConversationTarget(ctx context.Context, conversationID string) (messagingstate.ConversationReference, error)
	LatestConversationTurn(ctx context.Context, conversationID string) (messagingstate.ConversationTurn, error)
	ListConversationEvidence(ctx context.Context, conversationID string, at time.Time, limit int) ([]messagingstate.ConversationEvidence, error)
	ListConversationExchanges(ctx context.Context, turnID string) ([]messagingstate.ConversationExchange, error)
	ListConversationReferences(ctx context.Context, conversationID string, at time.Time, limit int) ([]messagingstate.ConversationReference, error)
	ListConversationSources(ctx context.Context, conversationID string, at time.Time, limit int) ([]messagingstate.ConversationTurn, error)
	ListConversationTurnEvidence(ctx context.Context, turnID string, at time.Time) ([]messagingstate.ConversationEvidence, error)
	ListConversations(ctx context.Context) ([]messagingstate.Conversation, error)
	ListLegacyConversations(ctx context.Context) ([]messagingstate.Conversation, error)
	ListPendingConversationActionOutcomes(ctx context.Context, conversationID string) ([]messagingstate.ChatReply, error)
	PauseConversationReply(ctx context.Context, r messagingstate.ChatReply, reason string, at time.Time) error
	PendingNaturalAlertControls(ctx context.Context, channel string) ([]messagingstate.NaturalMessage, error)
	PendingNaturalMessages(ctx context.Context, channel string, limit int) ([]messagingstate.NaturalMessage, error)
	PurgeChatIssuePreviews(ctx context.Context, at time.Time) error
	PurgeChatNotices(ctx context.Context, at time.Time) error
	PurgeChatReplies(ctx context.Context, at time.Time) error
	PurgeConversationBodies(ctx context.Context, at time.Time) error
	PurgeConversationEvidence(ctx context.Context, at time.Time) error
	PurgeNaturalChat(ctx context.Context, at time.Time) error
	QueueChatNotices(ctx context.Context, transport string, notices []messagingstate.ChatNotice) error
	ReadAlertControlScan(ctx context.Context, channel string) (messagingstate.AlertControlScan, bool, error)
	ReadAskReplies(ctx context.Context, transport, channel string, limit int) ([]messagingstate.ChatReply, error)
	ReadChatReply(ctx context.Context, transport, id string) (messagingstate.ChatReply, error)
	ReadConversation(ctx context.Context, transport, channel string) (messagingstate.Conversation, error)
	ReadConversationAnswer(ctx context.Context, transport, owner, channel, selector string) (messagingstate.ConversationAnswer, error)
	ReadConversationAnswerParts(ctx context.Context, id string) ([]messagingstate.ConversationAnswerPart, error)
	ReadConversationByID(ctx context.Context, id string) (messagingstate.Conversation, error)
	ReadConversationCapture(ctx context.Context, id string) ([]byte, bool, error)
	ReadConversationDetailsParts(ctx context.Context, r messagingstate.ChatReply) ([]messagingstate.ConversationAnswerPart, error)
	ReadNaturalDialogue(ctx context.Context, transport, owner, channel string) (messagingstate.NaturalDialogue, error)
	ReadNaturalExecution(ctx context.Context, identity string) (messagingstate.NaturalExecution, error)
	ReadNaturalScan(ctx context.Context, channel string) (messagingstate.NaturalScan, error)
	RecoverConversationActionOutcome(ctx context.Context, t messagingstate.ConversationTurn, at time.Time) (string, error)
	RecoverConversationTurns(ctx context.Context, id string, at time.Time) ([]messagingstate.ConversationTurn, error)
	RefreshConversationCapture(ctx context.Context, id string, raw []byte, at time.Time) (bool, error)
	RenewConversationTurn(ctx context.Context, turn messagingstate.ConversationTurn, at time.Time, lease time.Duration) error
	ReplaceConversationDetailsParts(ctx context.Context, r messagingstate.ChatReply, parts []messagingstate.ConversationAnswerPart, at time.Time) error
	RetryChatNotice(ctx context.Context, r messagingstate.ChatReply, next, at time.Time) error
	RetryChatReply(ctx context.Context, r messagingstate.ChatReply, next, at time.Time) error
	SaveChatReply(ctx context.Context, r messagingstate.ChatReply, text string) error
	SaveConversationActionLink(ctx context.Context, t messagingstate.ConversationTurn, a messagingstate.ConversationActionLink) error
	SaveConversationDetails(ctx context.Context, r messagingstate.ChatReply, parts []messagingstate.ConversationAnswerPart, at time.Time) error
	SaveConversationEvidence(ctx context.Context, t messagingstate.ConversationTurn, e messagingstate.ConversationEvidence) error
	SaveConversationExchange(ctx context.Context, t messagingstate.ConversationTurn, x messagingstate.ConversationExchange) error
	SaveConversationReference(ctx context.Context, t messagingstate.ConversationTurn, r messagingstate.ConversationReference) error
	SaveConversationSession(ctx context.Context, t messagingstate.ConversationTurn, handle string) error
	SetConversationHealth(ctx context.Context, id, reason string, at time.Time) error
	StageAlertControlScan(ctx context.Context, channel string, expected, next messagingstate.AlertControlScan, messages []messagingstate.NaturalMessage, complete bool) error
	StageNaturalScanWithControls(ctx context.Context, expected, next messagingstate.NaturalScan, messages []messagingstate.NaturalMessage, complete bool, controls map[string]bool) error
	StartNaturalExecution(ctx context.Context, identity string, at time.Time) error
}

// adapter binds the messaging port to the shared physical database.
// Its database stays private so a consumer cannot use factory tables.
type adapter struct{ db *store.DB }

// New composes messaging persistence over existing unchanged transactions.
func New(db *store.DB) Port { return &adapter{db: db} }
func (persistence *adapter) ActivateAlertControlScan(ctx context.Context, channel, boundary string) error {
	return persistence.db.ActivateAlertControlScan(ctx, channel, boundary)
}
func (persistence *adapter) ActivateNaturalScan(ctx context.Context, channel, boundary string) error {
	return persistence.db.ActivateNaturalScan(ctx, channel, boundary)
}
func (persistence *adapter) AdmitChatReply(ctx context.Context, r messagingstate.ChatReply) (bool, error) {
	return persistence.db.AdmitChatReply(ctx, r)
}
func (persistence *adapter) AdmitConversationDetails(ctx context.Context, r messagingstate.ChatReply, owner, selector string, at time.Time) (bool, error) {
	return persistence.db.AdmitConversationDetails(ctx, r, owner, selector, at)
}
func (persistence *adapter) AdmitConversationTurn(ctx context.Context, id, source, prompt string, at time.Time) (messagingstate.ConversationTurn, error) {
	return persistence.db.AdmitConversationTurn(ctx, id, source, prompt, at)
}
func (persistence *adapter) BackfillLegacyConversationCapture(ctx context.Context, id string, raw []byte, expected messagingstate.ConversationLegacyInstructions, at time.Time) error {
	return persistence.db.BackfillLegacyConversationCapture(ctx, id, raw, expected, at)
}
func (persistence *adapter) BindNaturalPrompt(ctx context.Context, d messagingstate.NaturalDialogue, prompt string) error {
	return persistence.db.BindNaturalPrompt(ctx, d, prompt)
}
func (persistence *adapter) ClaimChatNotice(ctx context.Context, transport string, at time.Time) (messagingstate.ChatReply, error) {
	return persistence.db.ClaimChatNotice(ctx, transport, at)
}
func (persistence *adapter) ClaimChatReply(ctx context.Context, transport string, at time.Time) (messagingstate.ChatReply, error) {
	return persistence.db.ClaimChatReply(ctx, transport, at)
}
func (persistence *adapter) ClaimConversationReply(ctx context.Context, transport string, at time.Time) (messagingstate.ChatReply, error) {
	return persistence.db.ClaimConversationReply(ctx, transport, at)
}
func (persistence *adapter) ClaimConversationTurn(ctx context.Context, id, worker string, at time.Time, lease time.Duration) (messagingstate.ConversationTurn, error) {
	return persistence.db.ClaimConversationTurn(ctx, id, worker, at, lease)
}
func (persistence *adapter) CompleteConversationAnswer(ctx context.Context, t messagingstate.ConversationTurn, state string, a messagingstate.ConversationAnswer, parts []messagingstate.ConversationAnswerPart, at time.Time) error {
	return persistence.db.CompleteConversationAnswer(ctx, t, state, a, parts, at)
}
func (persistence *adapter) CompleteConversationFreshAnswer(ctx context.Context, t messagingstate.ConversationTurn, a messagingstate.ConversationAnswer, parts []messagingstate.ConversationAnswerPart, at time.Time) error {
	return persistence.db.CompleteConversationFreshAnswer(ctx, t, a, parts, at)
}
func (persistence *adapter) CompleteConversationFreshTurn(ctx context.Context, t messagingstate.ConversationTurn, reply string, at time.Time) error {
	return persistence.db.CompleteConversationFreshTurn(ctx, t, reply, at)
}
func (persistence *adapter) CompleteConversationHandledAnswer(ctx context.Context, t messagingstate.ConversationTurn, a messagingstate.ConversationAnswer, parts []messagingstate.ConversationAnswerPart, at time.Time) error {
	return persistence.db.CompleteConversationHandledAnswer(ctx, t, a, parts, at)
}
func (persistence *adapter) CompleteConversationHandledTurn(ctx context.Context, t messagingstate.ConversationTurn, reply string, at time.Time) error {
	return persistence.db.CompleteConversationHandledTurn(ctx, t, reply, at)
}
func (persistence *adapter) CompleteConversationPreStartTurn(ctx context.Context, t messagingstate.ConversationTurn, reply string, at time.Time) error {
	return persistence.db.CompleteConversationPreStartTurn(ctx, t, reply, at)
}
func (persistence *adapter) CompleteConversationTurn(ctx context.Context, t messagingstate.ConversationTurn, state, reply string, at time.Time) error {
	return persistence.db.CompleteConversationTurn(ctx, t, state, reply, at)
}
func (persistence *adapter) ConversationDetailsReplySelection(ctx context.Context, r messagingstate.ChatReply) (string, error) {
	return persistence.db.ConversationDetailsReplySelection(ctx, r)
}
func (persistence *adapter) ConversationDetailsSelection(ctx context.Context, r messagingstate.ChatReply) (string, error) {
	return persistence.db.ConversationDetailsSelection(ctx, r)
}
func (persistence *adapter) ConversationReplyAnswer(ctx context.Context, r messagingstate.ChatReply) (messagingstate.ConversationAnswer, int, error) {
	return persistence.db.ConversationReplyAnswer(ctx, r)
}
func (persistence *adapter) CurrentChatIssuePreview(ctx context.Context, transport, channel string) (messagingstate.ChatIssuePreview, error) {
	return persistence.db.CurrentChatIssuePreview(ctx, transport, channel)
}
func (persistence *adapter) DeferConversationTurn(ctx context.Context, turn messagingstate.ConversationTurn) error {
	return persistence.db.DeferConversationTurn(ctx, turn)
}
func (persistence *adapter) EnsureConversationWithCapture(ctx context.Context, c messagingstate.Conversation, capture []byte, at time.Time) (messagingstate.Conversation, error) {
	return persistence.db.EnsureConversationWithCapture(ctx, c, capture, at)
}
func (persistence *adapter) FinishAlertControlMessage(ctx context.Context, channel, source string) error {
	return persistence.db.FinishAlertControlMessage(ctx, channel, source)
}
func (persistence *adapter) FinishChatNotice(ctx context.Context, r messagingstate.ChatReply, failed bool, at time.Time) error {
	return persistence.db.FinishChatNotice(ctx, r, failed, at)
}
func (persistence *adapter) FinishChatReply(ctx context.Context, r messagingstate.ChatReply, failed bool, at time.Time) error {
	return persistence.db.FinishChatReply(ctx, r, failed, at)
}
func (persistence *adapter) FinishConversationActionReply(ctx context.Context, t messagingstate.ConversationTurn, r messagingstate.ChatReply, at time.Time) error {
	return persistence.db.FinishConversationActionReply(ctx, t, r, at)
}
func (persistence *adapter) FinishConversationReply(ctx context.Context, r messagingstate.ChatReply, discordID string, at time.Time) error {
	return persistence.db.FinishConversationReply(ctx, r, discordID, at)
}
func (persistence *adapter) FinishNaturalExecution(ctx context.Context, identity, outcome string, uncertain bool, at time.Time) error {
	return persistence.db.FinishNaturalExecution(ctx, identity, outcome, uncertain, at)
}
func (persistence *adapter) FinishNaturalMessage(ctx context.Context, channel, id string, at time.Time) error {
	return persistence.db.FinishNaturalMessage(ctx, channel, id, at)
}
func (persistence *adapter) FinishWorkflowChatNotice(ctx context.Context, r messagingstate.ChatReply, state, message, reason string, next, at time.Time) error {
	return persistence.db.FinishWorkflowChatNotice(ctx, r, state, message, reason, next, at)
}
func (persistence *adapter) GetConversationActionLink(ctx context.Context, turnID string) (messagingstate.ConversationActionLink, error) {
	return persistence.db.GetConversationActionLink(ctx, turnID)
}
func (persistence *adapter) LatestConversationTarget(ctx context.Context, conversationID string) (messagingstate.ConversationReference, error) {
	return persistence.db.LatestConversationTarget(ctx, conversationID)
}
func (persistence *adapter) LatestConversationTurn(ctx context.Context, conversationID string) (messagingstate.ConversationTurn, error) {
	return persistence.db.LatestConversationTurn(ctx, conversationID)
}
func (persistence *adapter) ListConversationEvidence(ctx context.Context, conversationID string, at time.Time, limit int) ([]messagingstate.ConversationEvidence, error) {
	return persistence.db.ListConversationEvidence(ctx, conversationID, at, limit)
}
func (persistence *adapter) ListConversationExchanges(ctx context.Context, turnID string) ([]messagingstate.ConversationExchange, error) {
	return persistence.db.ListConversationExchanges(ctx, turnID)
}
func (persistence *adapter) ListConversationReferences(ctx context.Context, conversationID string, at time.Time, limit int) ([]messagingstate.ConversationReference, error) {
	return persistence.db.ListConversationReferences(ctx, conversationID, at, limit)
}
func (persistence *adapter) ListConversationSources(ctx context.Context, conversationID string, at time.Time, limit int) ([]messagingstate.ConversationTurn, error) {
	return persistence.db.ListConversationSources(ctx, conversationID, at, limit)
}
func (persistence *adapter) ListConversationTurnEvidence(ctx context.Context, turnID string, at time.Time) ([]messagingstate.ConversationEvidence, error) {
	return persistence.db.ListConversationTurnEvidence(ctx, turnID, at)
}
func (persistence *adapter) ListConversations(ctx context.Context) ([]messagingstate.Conversation, error) {
	return persistence.db.ListConversations(ctx)
}
func (persistence *adapter) ListLegacyConversations(ctx context.Context) ([]messagingstate.Conversation, error) {
	return persistence.db.ListLegacyConversations(ctx)
}
func (persistence *adapter) ListPendingConversationActionOutcomes(ctx context.Context, conversationID string) ([]messagingstate.ChatReply, error) {
	return persistence.db.ListPendingConversationActionOutcomes(ctx, conversationID)
}
func (persistence *adapter) PauseConversationReply(ctx context.Context, r messagingstate.ChatReply, reason string, at time.Time) error {
	return persistence.db.PauseConversationReply(ctx, r, reason, at)
}
func (persistence *adapter) PendingNaturalAlertControls(ctx context.Context, channel string) ([]messagingstate.NaturalMessage, error) {
	return persistence.db.PendingNaturalAlertControls(ctx, channel)
}
func (persistence *adapter) PendingNaturalMessages(ctx context.Context, channel string, limit int) ([]messagingstate.NaturalMessage, error) {
	return persistence.db.PendingNaturalMessages(ctx, channel, limit)
}
func (persistence *adapter) PurgeChatIssuePreviews(ctx context.Context, at time.Time) error {
	return persistence.db.PurgeChatIssuePreviews(ctx, at)
}
func (persistence *adapter) PurgeChatNotices(ctx context.Context, at time.Time) error {
	return persistence.db.PurgeChatNotices(ctx, at)
}
func (persistence *adapter) PurgeChatReplies(ctx context.Context, at time.Time) error {
	return persistence.db.PurgeChatReplies(ctx, at)
}
func (persistence *adapter) PurgeConversationBodies(ctx context.Context, at time.Time) error {
	return persistence.db.PurgeConversationBodies(ctx, at)
}
func (persistence *adapter) PurgeConversationEvidence(ctx context.Context, at time.Time) error {
	return persistence.db.PurgeConversationEvidence(ctx, at)
}
func (persistence *adapter) PurgeNaturalChat(ctx context.Context, at time.Time) error {
	return persistence.db.PurgeNaturalChat(ctx, at)
}
func (persistence *adapter) QueueChatNotices(ctx context.Context, transport string, notices []messagingstate.ChatNotice) error {
	return persistence.db.QueueChatNotices(ctx, transport, notices)
}
func (persistence *adapter) ReadAlertControlScan(ctx context.Context, channel string) (messagingstate.AlertControlScan, bool, error) {
	return persistence.db.ReadAlertControlScan(ctx, channel)
}
func (persistence *adapter) ReadAskReplies(ctx context.Context, transport, channel string, limit int) ([]messagingstate.ChatReply, error) {
	return persistence.db.ReadAskReplies(ctx, transport, channel, limit)
}
func (persistence *adapter) ReadChatReply(ctx context.Context, transport, id string) (messagingstate.ChatReply, error) {
	return persistence.db.ReadChatReply(ctx, transport, id)
}
func (persistence *adapter) ReadConversation(ctx context.Context, transport, channel string) (messagingstate.Conversation, error) {
	return persistence.db.ReadConversation(ctx, transport, channel)
}
func (persistence *adapter) ReadConversationAnswer(ctx context.Context, transport, owner, channel, selector string) (messagingstate.ConversationAnswer, error) {
	return persistence.db.ReadConversationAnswer(ctx, transport, owner, channel, selector)
}
func (persistence *adapter) ReadConversationAnswerParts(ctx context.Context, id string) ([]messagingstate.ConversationAnswerPart, error) {
	return persistence.db.ReadConversationAnswerParts(ctx, id)
}
func (persistence *adapter) ReadConversationByID(ctx context.Context, id string) (messagingstate.Conversation, error) {
	return persistence.db.ReadConversationByID(ctx, id)
}
func (persistence *adapter) ReadConversationCapture(ctx context.Context, id string) ([]byte, bool, error) {
	return persistence.db.ReadConversationCapture(ctx, id)
}
func (persistence *adapter) ReadConversationDetailsParts(ctx context.Context, r messagingstate.ChatReply) ([]messagingstate.ConversationAnswerPart, error) {
	return persistence.db.ReadConversationDetailsParts(ctx, r)
}
func (persistence *adapter) ReadNaturalDialogue(ctx context.Context, transport, owner, channel string) (messagingstate.NaturalDialogue, error) {
	return persistence.db.ReadNaturalDialogue(ctx, transport, owner, channel)
}
func (persistence *adapter) ReadNaturalExecution(ctx context.Context, identity string) (messagingstate.NaturalExecution, error) {
	return persistence.db.ReadNaturalExecution(ctx, identity)
}
func (persistence *adapter) ReadNaturalScan(ctx context.Context, channel string) (messagingstate.NaturalScan, error) {
	return persistence.db.ReadNaturalScan(ctx, channel)
}
func (persistence *adapter) RecoverConversationActionOutcome(ctx context.Context, t messagingstate.ConversationTurn, at time.Time) (string, error) {
	return persistence.db.RecoverConversationActionOutcome(ctx, t, at)
}
func (persistence *adapter) RecoverConversationTurns(ctx context.Context, id string, at time.Time) ([]messagingstate.ConversationTurn, error) {
	return persistence.db.RecoverConversationTurns(ctx, id, at)
}
func (persistence *adapter) RefreshConversationCapture(ctx context.Context, id string, raw []byte, at time.Time) (bool, error) {
	return persistence.db.RefreshConversationCapture(ctx, id, raw, at)
}
func (persistence *adapter) RenewConversationTurn(ctx context.Context, turn messagingstate.ConversationTurn, at time.Time, lease time.Duration) error {
	return persistence.db.RenewConversationTurn(ctx, turn, at, lease)
}
func (persistence *adapter) ReplaceConversationDetailsParts(ctx context.Context, r messagingstate.ChatReply, parts []messagingstate.ConversationAnswerPart, at time.Time) error {
	return persistence.db.ReplaceConversationDetailsParts(ctx, r, parts, at)
}
func (persistence *adapter) RetryChatNotice(ctx context.Context, r messagingstate.ChatReply, next, at time.Time) error {
	return persistence.db.RetryChatNotice(ctx, r, next, at)
}
func (persistence *adapter) RetryChatReply(ctx context.Context, r messagingstate.ChatReply, next, at time.Time) error {
	return persistence.db.RetryChatReply(ctx, r, next, at)
}
func (persistence *adapter) SaveChatReply(ctx context.Context, r messagingstate.ChatReply, text string) error {
	return persistence.db.SaveChatReply(ctx, r, text)
}
func (persistence *adapter) SaveConversationActionLink(ctx context.Context, t messagingstate.ConversationTurn, a messagingstate.ConversationActionLink) error {
	return persistence.db.SaveConversationActionLink(ctx, t, a)
}
func (persistence *adapter) SaveConversationDetails(ctx context.Context, r messagingstate.ChatReply, parts []messagingstate.ConversationAnswerPart, at time.Time) error {
	return persistence.db.SaveConversationDetails(ctx, r, parts, at)
}
func (persistence *adapter) SaveConversationEvidence(ctx context.Context, t messagingstate.ConversationTurn, e messagingstate.ConversationEvidence) error {
	return persistence.db.SaveConversationEvidence(ctx, t, e)
}
func (persistence *adapter) SaveConversationExchange(ctx context.Context, t messagingstate.ConversationTurn, x messagingstate.ConversationExchange) error {
	return persistence.db.SaveConversationExchange(ctx, t, x)
}
func (persistence *adapter) SaveConversationReference(ctx context.Context, t messagingstate.ConversationTurn, r messagingstate.ConversationReference) error {
	return persistence.db.SaveConversationReference(ctx, t, r)
}
func (persistence *adapter) SaveConversationSession(ctx context.Context, t messagingstate.ConversationTurn, handle string) error {
	return persistence.db.SaveConversationSession(ctx, t, handle)
}
func (persistence *adapter) SetConversationHealth(ctx context.Context, id, reason string, at time.Time) error {
	return persistence.db.SetConversationHealth(ctx, id, reason, at)
}
func (persistence *adapter) StageAlertControlScan(ctx context.Context, channel string, expected, next messagingstate.AlertControlScan, messages []messagingstate.NaturalMessage, complete bool) error {
	return persistence.db.StageAlertControlScan(ctx, channel, expected, next, messages, complete)
}
func (persistence *adapter) StageNaturalScanWithControls(ctx context.Context, expected, next messagingstate.NaturalScan, messages []messagingstate.NaturalMessage, complete bool, controls map[string]bool) error {
	return persistence.db.StageNaturalScanWithControls(ctx, expected, next, messages, complete, controls)
}
func (persistence *adapter) StartNaturalExecution(ctx context.Context, identity string, at time.Time) error {
	return persistence.db.StartNaturalExecution(ctx, identity, at)
}
