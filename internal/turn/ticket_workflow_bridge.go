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

// TicketWorkflowBridge translates retained GitHub workflow values at the
// compatibility boundary. It contains no code repository or merge capability.
// Consumers continue to declare only their own narrow operation interfaces.
type TicketWorkflowBridge struct {
	selector TicketConnectionSelector
	*TicketPlanBridge
	Publication *TicketPublicationBridge
	Mutations   interface {
		AddLabel(context.Context, ticket.Ref, string) error
		RemoveLabel(context.Context, ticket.Ref, string) error
		DiscoverCommented(context.Context, string, time.Time) ([]ticket.Locator, error)
	}
}

// BaseURL delegates the retained workflow operation through qualified tickets.
func (b *TicketWorkflowBridge) BaseURL() string { return string(b.Tickets.Origin()) }

// GetIssue delegates the retained workflow operation through qualified tickets.
func (b *TicketWorkflowBridge) GetIssue(ctx context.Context, r github.Repo, n int) (github.Issue, error) {
	selected, err := b.selected(ctx, r, n, 0)
	if err != nil {
		return github.Issue{}, err
	}
	return selected.Publication.GetIssue(ctx, r, n)
}

// PostComment delegates the retained workflow operation through qualified tickets.
func (b *TicketWorkflowBridge) PostComment(ctx context.Context, r github.Repo, n int, body redact.Body) (github.Comment, error) {
	selected, err := b.selected(ctx, r, n, 0)
	if err != nil {
		return github.Comment{}, err
	}
	return selected.Publication.PostComment(ctx, r, n, body)
}

// ListCommentsAfter delegates the retained workflow operation through qualified tickets.
func (b *TicketWorkflowBridge) ListCommentsAfter(ctx context.Context, r github.Repo, n int, id int64) ([]github.Comment, error) {
	selected, err := b.selected(ctx, r, n, 0)
	if err != nil {
		return nil, err
	}
	return selected.Publication.ListCommentsAfter(ctx, r, n, id)
}

// ListCommentsPage delegates the retained workflow operation through qualified tickets.
func (b *TicketWorkflowBridge) ListCommentsPage(ctx context.Context, r github.Repo, n, p, l int) (github.CommentPage, error) {
	selected, err := b.selected(ctx, r, n, 0)
	if err != nil {
		return github.CommentPage{}, err
	}
	return selected.Publication.ListCommentsPage(ctx, r, n, p, l)
}

// EditComment delegates the retained workflow operation through qualified tickets.
func (b *TicketWorkflowBridge) EditComment(ctx context.Context, r github.Repo, id int64, body redact.Body) error {
	selected, err := b.selected(ctx, r, 0, 0)
	if err != nil {
		return err
	}
	return selected.Publication.EditComment(ctx, r, id, body)
}

// AddCommentReaction delegates the retained workflow operation through qualified tickets.
func (b *TicketWorkflowBridge) AddCommentReaction(ctx context.Context, r github.Repo, id int64, c github.ReactionContent) error {
	selected, err := b.selected(ctx, r, 0, 0)
	if err != nil {
		return err
	}
	return selected.Publication.AddCommentReaction(ctx, r, id, string(c))
}

// BindPublication delegates the retained workflow operation through qualified tickets.
func (b *TicketWorkflowBridge) BindPublication(ctx context.Context, key string, session int64, ref naming.Ref, bot int64) error {
	selected, err := b.selected(ctx, github.RepoFromRef(ref), ref.Issue, session)
	if err != nil {
		return err
	}
	if bot != selected.ActorID() {
		return errors.New("publication actor differs from retained connection")
	}
	return selected.Publication.BindPublication(ctx, key, session, ref, bot)
}

// VerifiedCommentDestination delegates the retained workflow operation through qualified tickets.
func (b *TicketWorkflowBridge) VerifiedCommentDestination(c github.Comment, ref naming.Ref) bool {
	if b.selector == nil {
		return b.Publication.VerifiedCommentDestination(c, ref)
	}
	return false // Routed receipts require the caller's retained context.
}

// AddIssueLabel delegates the retained workflow operation through qualified tickets.
func (b *TicketWorkflowBridge) AddIssueLabel(ctx context.Context, r github.Repo, n int, label string) error {
	selected, err := b.selected(ctx, r, n, 0)
	if err != nil {
		return err
	}
	if selected != b {
		return selected.AddIssueLabel(ctx, r, n, label)
	}
	s, err := b.read(ctx, r, n)
	if err != nil {
		return err
	}
	return b.Mutations.AddLabel(ctx, s.Ref, label)
}

// RemoveIssueLabel delegates the retained workflow operation through qualified tickets.
func (b *TicketWorkflowBridge) RemoveIssueLabel(ctx context.Context, r github.Repo, n int, label string) error {
	selected, err := b.selected(ctx, r, n, 0)
	if err != nil {
		return err
	}
	if selected != b {
		return selected.RemoveIssueLabel(ctx, r, n, label)
	}
	s, err := b.read(ctx, r, n)
	if err != nil {
		return err
	}
	return b.Mutations.RemoveLabel(ctx, s.Ref, label)
}

// ListCommentedIssues delegates the retained workflow operation through qualified tickets.
func (b *TicketWorkflowBridge) ListCommentedIssues(ctx context.Context, r github.Repo, since time.Time) ([]int, error) {
	selected, err := b.selected(ctx, r, 0, 0)
	if err != nil {
		return nil, err
	}
	if selected != b {
		return selected.ListCommentedIssues(ctx, r, since)
	}
	locators, err := b.Mutations.DiscoverCommented(ctx, r.String(), since)
	if err != nil {
		return nil, err
	}
	out := make([]int, 0, len(locators))
	for _, l := range locators {
		if l.Provider != ticket.GitHub || l.Origin != b.Tickets.Origin() || l.Container != r.String() {
			return nil, errors.New("comment discovery destination differs")
		}
		n, err := strconv.Atoi(l.Key)
		if err != nil || n < 1 {
			return nil, errors.New("invalid legacy ticket locator")
		}
		out = append(out, n)
	}
	return out, nil
}

// NewGitHubTicketWorkflow builds the only production compatibility boundary
// from the existing authenticated ticket adapter, never from code delivery.
func NewGitHubTicketWorkflow(db *store.DB, a *github.Tickets, botID int64) (*TicketWorkflowBridge, error) {
	if db == nil || a == nil || botID < 1 {
		return nil, errors.New("ticket workflow dependencies missing")
	}
	publication, err := NewTicketPublicationBridge(db, a, a.Origin())
	if err != nil {
		return nil, err
	}
	return &TicketWorkflowBridge{TicketPlanBridge: &TicketPlanBridge{DB: db, Tickets: a, Bot: ticket.Identity{Provider: ticket.GitHub, Origin: a.Origin(), RemoteID: strconv.FormatInt(botID, 10)}}, Publication: publication, Mutations: a}, nil
}
