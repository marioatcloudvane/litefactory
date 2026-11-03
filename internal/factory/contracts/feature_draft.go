package contracts

import "time"

// ChatIssuePreview retains the saved preview and creation fences. Ownership
// belongs to the factory; transports only present its confirmed content.
type ChatIssuePreview struct {
	ID, Transport, ChannelRef, SourceMessageID, LastConfirmMessageID, Code, ContentHash, Title, Body, State string
	RepoID                                                                                                  int64
	CreatedAt, ExpiresAt, LastAttemptAt, LeaseUntil, FinishedAt                                             time.Time
	ClaimVersion, IssueNumber                                                                               int
	IssueLink                                                                                               string
}
