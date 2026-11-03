package factory

import (
	"context"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/factory/owneractions"
	"github.com/cloudvane/litefactory/internal/messagingstate"
	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/store"
	"time"
)

// Messaging owns factory projections and compound owner-inbox operations used by
// a conversation. The private adapter retains existing serializers and transactions.
// Owned views preserve retained conversation payloads without exposing SQL,
// arbitrary transactions or factory row writes.
type Messaging struct {
	db      *store.DB
	Targets *owneractions.Targets
	Status  *TaskStatus
}

// NewMessaging binds the existing factory services at trusted composition.
func NewMessaging(db *store.DB) *Messaging {
	return &Messaging{db: db, Targets: owneractions.NewTargets(db), Status: NewTaskStatus(db)}
}

// Project preserves the existing owned operation and its atomic fences.
func (m *Messaging) Project(ctx context.Context, id int64) (contracts.Project, error) {
	r, err := m.db.GetRepo(ctx, id)
	return projectView(r), err
}

// Projects preserves the existing owned operation and its atomic fences.
func (m *Messaging) Projects(ctx context.Context) ([]contracts.Project, error) {
	repos, err := m.db.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]contracts.Project, len(repos))
	for i, r := range repos {
		out[i] = projectView(r)
	}
	return out, nil
}

// ListRepos supplies the bounded registered-project view used by Ask and drafts.
func (m *Messaging) ListRepos(ctx context.Context) ([]contracts.Project, error) {
	return m.Projects(ctx)
}

func projectView(r store.Repo) contracts.Project {
	return contracts.Project{ID: r.ID, Forge: r.Forge, Owner: r.Owner, Name: r.Name, BaseBranch: r.BaseBranch}
}

// AuthorizeProject preserves the existing owned operation and its atomic fences.
func (m *Messaging) AuthorizeProject(ctx context.Context, repositoryID int64, repositoryRef string) error {
	return m.db.CheckOwnerActionResolution(ctx, repositoryID, repositoryRef)
}

// StandardsOperation preserves the existing owned operation and its atomic fences.
func (m *Messaging) StandardsOperation(ctx context.Context, key string) (contracts.StandardsConversationOperation, error) {
	return m.db.GetStandardsConversationOperation(ctx, key)
}

// OpenInbox preserves the existing owned operation and its atomic fences.
func (m *Messaging) OpenInbox(ctx context.Context, scope contracts.InboxScope, filter contracts.InboxFilter, now time.Time) (contracts.InboxSnapshot, error) {
	return m.db.CreateInboxSnapshot(ctx, scope, filter, now)
}

// OpenProtectedInbox preserves the existing owned operation and its atomic fences.
func (m *Messaging) OpenProtectedInbox(ctx context.Context, scope contracts.InboxScope, filter contracts.InboxFilter, now time.Time, protect func(string) (string, error)) (contracts.InboxSnapshot, error) {
	return m.db.CreateProtectedInboxSnapshot(ctx, scope, filter, now, protect)
}

// InboxPage preserves the existing owned operation and its atomic fences.
func (m *Messaging) InboxPage(ctx context.Context, scope contracts.InboxScope, id string, offset, limit int, now time.Time) (contracts.InboxPage, error) {
	return m.db.ReadInboxSnapshot(ctx, scope, id, offset, limit, now)
}

// InboxOrganizationInput preserves the existing owned operation and its atomic fences.
func (m *Messaging) InboxOrganizationInput(ctx context.Context, scope contracts.InboxScope, id string, now time.Time) (contracts.InboxPage, error) {
	return m.db.ReadCompleteInboxSnapshot(ctx, scope, id, now)
}

// CurrentOwnerWait preserves the existing owned operation and its atomic fences.
func (m *Messaging) CurrentOwnerWait(ctx context.Context, key string) (contracts.InboxDecision, error) {
	return m.db.CurrentInboxDecision(ctx, key)
}

// AuthorizeInbox preserves the existing owned operation and its atomic fences.
func (m *Messaging) AuthorizeInbox(ctx context.Context, scope contracts.InboxScope, id string, now time.Time) error {
	return m.db.InboxSnapshotAuthorized(ctx, scope, id, now)
}

// AuthorizeInboxSources preserves the existing owned operation and its atomic fences.
func (m *Messaging) AuthorizeInboxSources(ctx context.Context, scope contracts.InboxScope, id string, sourceIDs []int64, now time.Time) error {
	return m.db.InboxSourcesAuthorized(ctx, scope, id, sourceIDs, now)
}

// ExpireInboxes preserves the existing owned operation and its atomic fences.
func (m *Messaging) ExpireInboxes(ctx context.Context, now time.Time) error {
	return m.db.PurgeInboxSnapshots(ctx, now)
}

// InboxOrganization preserves the existing owned operation and its atomic fences.
func (m *Messaging) InboxOrganization(ctx context.Context, scope contracts.InboxScope, id string, version int, now time.Time) (contracts.InboxOrganization, error) {
	return m.db.ReadInboxOrganization(ctx, scope, id, version, now)
}

// CommitInboxOrganization preserves the existing owned operation and its atomic fences.
func (m *Messaging) CommitInboxOrganization(ctx context.Context, scope contracts.InboxScope, id string, in contracts.InboxOrganization, now time.Time) (contracts.InboxOrganization, error) {
	return m.db.FinalizeInboxOrganization(ctx, scope, id, in, now)
}

// InboxNavigation preserves the existing owned operation and its atomic fences.
func (m *Messaging) InboxNavigation(ctx context.Context, scope contracts.InboxScope, id string, version int, now time.Time) (contracts.InboxPage, contracts.InboxOrganization, error) {
	return m.db.ReadInboxNavigation(ctx, scope, id, version, now)
}

// RetainWithheldInboxReply preserves the existing owned operation and its atomic fences.
func (m *Messaging) RetainWithheldInboxReply(ctx context.Context, r messagingstate.ChatReply, at time.Time) (string, error) {
	return m.db.SaveInboxWithheldReply(ctx, r, at)
}

// WithheldInboxReply preserves the existing owned operation and its atomic fences.
func (m *Messaging) WithheldInboxReply(ctx context.Context, r messagingstate.ChatReply) (string, error) {
	return m.db.ReadInboxWithheldReply(ctx, r)
}

// PendingOwnerActionNotices preserves the existing owned operation and its atomic fences.
func (m *Messaging) PendingOwnerActionNotices(ctx context.Context) ([]contracts.OwnerActionNotice, error) {
	records, err := m.db.OwnerActionNoticesDue(ctx)
	if err != nil {
		return nil, err
	}
	notices := make([]contracts.OwnerActionNotice, len(records))
	for i, record := range records {
		notices[i] = contracts.OwnerActionNotice{ID: record.ID, ApprovalChannel: record.ApprovalChannel}
	}
	return notices, nil
}

// CompleteOwnerActionNotice preserves the existing owned operation and its atomic fences.
func (m *Messaging) CompleteOwnerActionNotice(ctx context.Context, id, state string, notices []messagingstate.ChatNotice) error {
	return m.db.QueueOwnerActionOutcome(ctx, id, state, notices)
}

// InvalidateLegacyOwnerAction preserves the existing owned operation and its atomic fences.
func (m *Messaging) InvalidateLegacyOwnerAction(ctx context.Context, discordOwner, channel string) (bool, error) {
	return m.db.LegacyOwnerActionInvalidation(ctx, discordOwner, channel)
}

// Meter reads a bounded usage view, closing its snapshot inside the factory.
func (m *Messaging) Meter(ctx context.Context, r meterview.Request) (meterview.View, error) {
	return meterview.Read(ctx, m.db, r)
}

// MeterBreakdown reads a bounded grouped usage view.
func (m *Messaging) MeterBreakdown(ctx context.Context, r meterview.Request, by []string) (meterview.Breakdown, error) {
	return meterview.ReadBreakdown(ctx, m.db, r, by)
}

// MeterProject resolves the registered usage target.
func (m *Messaging) MeterProject(ctx context.Context, project string) (int64, error) {
	return meterview.ResolveProject(ctx, m.db, project)
}

// MeterPriceLines renders the existing recorded price catalog.
func (m *Messaging) MeterPriceLines(ctx context.Context, agent, model string) ([]string, error) {
	catalog, err := meterview.Prices(ctx, m.db, agent, model)
	if err != nil {
		return nil, err
	}
	return meterview.PriceLines(catalog), nil
}

// InitializeNotices keeps source watermarks and pending notice admission atomic.
func (m *Messaging) InitializeNotices(ctx context.Context, transport string, at time.Time) error {
	return m.db.InitNoticePositions(ctx, transport, at)
}

// NoticeEvents keeps source watermarks and pending notice admission atomic.
func (m *Messaging) NoticeEvents(ctx context.Context, transport, source string, at time.Time) (contracts.NoticePosition, []contracts.NoticeRecord, error) {
	position, records, err := m.db.ListNoticeRecords(ctx, transport, source, at)
	if err != nil {
		return position, nil, err
	}
	views := make([]contracts.NoticeRecord, len(records))
	for i, r := range records {
		_, stopped := r.Run.OwnerStop()
		views[i] = contracts.NoticeRecord{ID: r.ID, TaskRef: r.TaskRef, Workflow: r.Workflow, From: r.From, To: r.To, Key: r.Key, Kind: r.Kind, StopOrigin: r.StopOrigin, StopCause: r.StopCause, StopReason: r.StopReason, PreviousMode: r.PreviousMode, State: r.State, Published: r.Published, RunID: r.Run.ID, RunError: r.Run.Error, RunOwnerStopped: stopped, RunSucceeded: r.Run.Succeeded(), At: r.At, Candidate: r.Candidate, CommentID: r.CommentID, Ready: r.Ready, Ignore: r.Ignore}
	}
	return position, views, nil
}

// AdmitNoticeBatch keeps source watermarks and pending notice admission atomic.
func (m *Messaging) AdmitNoticeBatch(ctx context.Context, transport string, before, after contracts.NoticePosition, notices []messagingstate.ChatNotice) error {
	return m.db.AdmitNotices(ctx, transport, before, after, notices)
}
