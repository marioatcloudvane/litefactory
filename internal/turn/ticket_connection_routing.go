package turn

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// TicketConnectionSelector resolves a trusted project version or a retained
// session snapshot. A zero number allows container and reply operations; the
// factory uses retained context or rejects ambiguity rather than guessing.
type TicketConnectionSelector func(context.Context, github.Repo, int, int64) (*github.Tickets, int64, error)

// NewRoutedGitHubTicketWorkflow selects a separate immutable bridge per operation.
// The default adapter supplies the contextless compatibility metadata. Every
// remote operation requires selection; failures never use replacement settings.
func NewRoutedGitHubTicketWorkflow(db *store.DB, defaults *github.Tickets, botID int64, selector TicketConnectionSelector) (*TicketWorkflowBridge, error) {
	if selector == nil {
		return nil, errors.New("ticket connection selector required")
	}
	bridge, err := NewGitHubTicketWorkflow(db, defaults, botID)
	if err != nil {
		return nil, err
	}
	bridge.selector = selector
	return bridge, nil
}

func (b *TicketWorkflowBridge) selected(ctx context.Context, repo github.Repo, number int, sessionID int64) (*TicketWorkflowBridge, error) {
	if b.selector == nil {
		return b, nil
	}
	adapter, actorID, err := b.selector(ctx, repo, number, sessionID)
	if err != nil {
		return nil, err
	}
	if adapter == nil || actorID < 1 {
		return nil, errors.New("retained ticket connection unavailable")
	}
	selected, err := NewGitHubTicketWorkflow(b.DB, adapter, actorID)
	if err != nil {
		return nil, err
	}
	return selected, nil
}

// ActorID exposes the verified actor as the existing numeric compatibility value.
func (b *TicketWorkflowBridge) ActorID() int64 {
	id, _ := strconv.ParseInt(b.Bot.RemoteID, 10, 64)
	return id
}

// BotIdentityFor qualifies the verified retained actor for contextual callers.
func (b *TicketWorkflowBridge) BotIdentityFor(ctx context.Context, repo github.Repo, number int) (ticket.Identity, error) {
	selected, err := b.selected(ctx, repo, number, 0)
	if err != nil {
		return ticket.Identity{}, err
	}
	return selected.Bot, nil
}

// GetParentIssue uses the selected connection without changing shared clients.
func (b *TicketWorkflowBridge) GetParentIssue(ctx context.Context, r github.Repo, n int) (github.Issue, error) {
	selected, err := b.selected(ctx, r, n, 0)
	if err != nil {
		return github.Issue{}, err
	}
	return selected.TicketPlanBridge.GetParentIssue(ctx, r, n)
}

// ListIssues uses the selected connection without changing shared clients.
func (b *TicketWorkflowBridge) ListIssues(ctx context.Context, r github.Repo, at time.Time) ([]github.Issue, error) {
	selected, err := b.selected(ctx, r, 0, 0)
	if err != nil {
		return nil, err
	}
	return selected.TicketPlanBridge.ListIssues(ctx, r, at)
}

// ListFollowupIssues uses the selected connection without changing shared clients.
func (b *TicketWorkflowBridge) ListFollowupIssues(ctx context.Context, r github.Repo) ([]github.Issue, error) {
	selected, err := b.selected(ctx, r, 0, 0)
	if err != nil {
		return nil, err
	}
	return selected.TicketPlanBridge.ListFollowupIssues(ctx, r)
}

// ListSubIssues uses the selected connection without changing shared clients.
func (b *TicketWorkflowBridge) ListSubIssues(ctx context.Context, r github.Repo, n int) ([]github.Issue, error) {
	selected, err := b.selected(ctx, r, n, 0)
	if err != nil {
		return nil, err
	}
	return selected.TicketPlanBridge.ListSubIssues(ctx, r, n)
}

// CreateIssue uses the selected connection without changing shared clients.
func (b *TicketWorkflowBridge) CreateIssue(ctx context.Context, r github.Repo, title, body redact.Body) (github.Issue, error) {
	selected, err := b.selected(ctx, r, 0, 0)
	if err != nil {
		return github.Issue{}, err
	}
	return selected.TicketPlanBridge.CreateIssue(ctx, r, title, body)
}

// UpdateIssue uses the selected connection without changing shared clients.
func (b *TicketWorkflowBridge) UpdateIssue(ctx context.Context, r github.Repo, n int, title, body redact.Body) (github.Issue, error) {
	selected, err := b.selected(ctx, r, n, 0)
	if err != nil {
		return github.Issue{}, err
	}
	return selected.TicketPlanBridge.UpdateIssue(ctx, r, n, title, body)
}

// AddSubIssue uses the selected connection without changing shared clients.
func (b *TicketWorkflowBridge) AddSubIssue(ctx context.Context, r github.Repo, n int, id int64) error {
	selected, err := b.selected(ctx, r, n, 0)
	if err != nil {
		return err
	}
	return selected.TicketPlanBridge.AddSubIssue(ctx, r, n, id)
}

// SetStoryState uses the selected connection without changing shared clients.
func (b *TicketWorkflowBridge) SetStoryState(ctx context.Context, r github.Repo, pn int, pid int64, cn int, cid int64, done bool) error {
	selected, err := b.selected(ctx, r, pn, 0)
	if err != nil {
		return err
	}
	return selected.TicketPlanBridge.SetStoryState(ctx, r, pn, pid, cn, cid, done)
}

// RetirePlanStory uses the selected connection without changing shared clients.
func (b *TicketWorkflowBridge) RetirePlanStory(ctx context.Context, r github.Repo, pn int, pid int64, cn int, cid int64) error {
	selected, err := b.selected(ctx, r, pn, 0)
	if err != nil {
		return err
	}
	return selected.TicketPlanBridge.RetirePlanStory(ctx, r, pn, pid, cn, cid)
}

// UpdateIssueExpected uses the selected connection without changing shared clients.
func (b *TicketWorkflowBridge) UpdateIssueExpected(ctx context.Context, r github.Repo, n int, expected string, title, body redact.Body) (github.Issue, error) {
	selected, err := b.selected(ctx, r, n, 0)
	if err != nil {
		return github.Issue{}, err
	}
	return selected.TicketPlanBridge.UpdateIssueExpected(ctx, r, n, expected, title, body)
}

// CloseDeliveredParent uses the selected connection without changing shared clients.
func (b *TicketWorkflowBridge) CloseDeliveredParent(ctx context.Context, r github.Repo, n int, id int64) error {
	selected, err := b.selected(ctx, r, n, 0)
	if err != nil {
		return err
	}
	return selected.TicketPlanBridge.CloseDeliveredParent(ctx, r, n, id)
}

// ListIssuesPage uses the selected connection without changing shared clients.
func (b *TicketWorkflowBridge) ListIssuesPage(ctx context.Context, r github.Repo, state string, page, limit int) (github.IssuePage, error) {
	selected, err := b.selected(ctx, r, 0, 0)
	if err != nil {
		return github.IssuePage{}, err
	}
	return selected.TicketPlanBridge.ListIssuesPage(ctx, r, state, page, limit)
}

// ReadOpenIssues uses the selected connection without changing shared clients.
func (b *TicketWorkflowBridge) ReadOpenIssues(ctx context.Context, r github.Repo, limit int) ([]github.Issue, bool, error) {
	selected, err := b.selected(ctx, r, 0, 0)
	if err != nil {
		return nil, false, err
	}
	return selected.TicketPlanBridge.ReadOpenIssues(ctx, r, limit)
}

// VerifiedCommentDestinationFor checks the exact recorded receipt in the
// caller's retained execution context before accepting its legacy URL fields.
func (b *TicketWorkflowBridge) VerifiedCommentDestinationFor(ctx context.Context, c github.Comment, ref naming.Ref) bool {
	if b.selector == nil {
		return b.Publication.VerifiedCommentDestination(c, ref)
	}
	selected, err := b.selected(ctx, github.RepoFromRef(ref), ref.Issue, 0)
	if err != nil || c.ID < 1 {
		return false
	}
	retained, err := selected.Publication.retainedReply(ctx, github.RepoFromRef(ref), c.ID)
	if err != nil || retained.Ticket.Locator.Key != strconv.Itoa(ref.Issue) || retained.Author.Identity.RemoteID != strconv.FormatInt(c.Author.ID, 10) {
		return false
	}
	return selected.Publication.VerifiedCommentDestination(c, ref)
}

func verifiedCommentDestination(ctx context.Context, forge any, c github.Comment, ref naming.Ref) (bool, bool) {
	if verifier, ok := forge.(interface {
		VerifiedCommentDestinationFor(context.Context, github.Comment, naming.Ref) bool
	}); ok {
		return verifier.VerifiedCommentDestinationFor(ctx, c, ref), true
	}
	if verifier, ok := forge.(interface {
		VerifiedCommentDestination(github.Comment, naming.Ref) bool
	}); ok {
		return verifier.VerifiedCommentDestination(c, ref), true
	}
	return false, false
}

// ticketMetadataKey namespaces additive locator metadata when a historical
// numeric alias belongs to another origin. Owning ledger keys stay unchanged.
func ticketMetadataKey(ctx context.Context, db *store.DB, legacy string, locator ticket.Locator) (string, error) {
	existing, err := db.GetTicketEffect(ctx, legacy)
	if errors.Is(err, store.ErrNotFound) {
		return legacy, nil
	}
	if err != nil {
		return "", err
	}
	if existing.Locator.Origin == locator.Origin {
		return legacy, nil
	}
	return naming.QualifiedTaskRef(legacy, ticket.Identity{Provider: locator.Provider, Origin: locator.Origin, RemoteID: locator.Key}), nil
}
