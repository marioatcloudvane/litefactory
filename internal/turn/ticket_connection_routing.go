package turn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// ErrRequiredTicketPublisher identifies a definitively absent or invalid publisher.
// Transport failures and cancellation retain their original retry semantics.
var ErrRequiredTicketPublisher = errors.New("required ticket publisher unavailable")

func ticketPublisherID(ctx context.Context, db *store.DB, forge any, ref naming.Ref) (int64, error) {
	if contextual, ok := forge.(interface {
		BotIdentityFor(context.Context, github.Repo, int) (ticket.Identity, error)
	}); ok {
		actor, err := contextual.BotIdentityFor(ctx, github.RepoFromRef(ref), ref.Issue)
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && (errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrTicketIdentityConflict)) {
				return 0, fmt.Errorf("%w: %w", ErrRequiredTicketPublisher, err)
			}
			return 0, err
		}
		if err := actor.Validate(); err != nil || string(actor.Provider) != ref.Forge {
			return 0, ErrRequiredTicketPublisher
		}
		slot, err := compatibilitySlot(ctx, db, "actor", actor)
		if errors.Is(err, store.ErrTicketIdentityConflict) {
			return 0, fmt.Errorf("%w: %w", ErrRequiredTicketPublisher, err)
		}
		return slot, err
	}
	// Only historical GitHub callers can use the provider-global identity table.
	if ref.Forge != naming.ForgeGitHub {
		return 0, ErrRequiredTicketPublisher
	}
	bot, err := db.GetIdentity(ctx, ref.Forge, store.RoleBot)
	if errors.Is(err, store.ErrNotFound) || (err == nil && bot.UserID <= 0) {
		return 0, ErrRequiredTicketPublisher
	}
	return bot.UserID, err
}

// TicketConnectionSelector resolves a trusted project version or a retained
// session snapshot. A zero number allows container and reply operations; the
// factory uses retained context or rejects ambiguity rather than guessing.
type TicketConnectionSelector func(context.Context, github.Repo, int, int64) (WorkflowTickets, int64, error)

// NewRoutedGitHubTicketWorkflow selects a separate immutable bridge per operation.
// The default adapter supplies the contextless compatibility metadata. Every
// remote operation requires selection; failures never use replacement settings.
func NewRoutedGitHubTicketWorkflow(db *store.DB, defaults WorkflowTickets, botID int64, selector TicketConnectionSelector) (*TicketWorkflowBridge, error) {
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
		return nil, ErrRequiredTicketPublisher
	}
	selected, err := NewTicketWorkflow(ctx, b.DB, adapter, actorID)
	if err != nil {
		return nil, err
	}
	return selected, nil
}

// ActorID exposes the verified actor as the existing numeric compatibility value.
func (b *TicketWorkflowBridge) ActorID() int64 {
	return b.actorID
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
		return b.Publication.verifiedCommentDestination(ctx, c, ref)
	}
	selected, err := b.selected(ctx, github.RepoFromRef(ref), ref.Issue, 0)
	if err != nil || c.ID < 1 {
		return false
	}
	retained, err := selected.Publication.retainedReply(ctx, github.RepoFromRef(ref), c.ID)
	if err != nil {
		return false
	}
	locator, err := selected.Publication.locator(ctx, github.RepoFromRef(ref), ref.Issue)
	if err != nil || retained.Ticket.Locator != locator {
		return false
	}
	actor, err := compatibilityIdentity(ctx, b.DB, selected.Publication.provider, selected.Publication.origin, "actor", c.Author.ID)
	if err != nil || !retained.Author.Identity.Equal(actor) {
		return false
	}
	return selected.Publication.verifiedCommentDestination(ctx, c, ref)
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
