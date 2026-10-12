package turn

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// PlanTickets contains only ticket operations. Code repository and merge
// capabilities cannot be acquired through this dependency.
type PlanTickets interface {
	Origin() ticket.Origin
	IssuesPage(context.Context, string, string, int, int) (ticket.SnapshotPage, error)
	OpenContext(context.Context, string, int) (ticket.SnapshotPage, error)
	Resolve(context.Context, ticket.Locator) (ticket.Snapshot, error)
	Read(context.Context, ticket.Ref) (ticket.Snapshot, error)
	Discover(context.Context, string, time.Time) ([]ticket.Snapshot, error)
	Followups(context.Context, string) ([]ticket.Snapshot, error)
	Children(context.Context, ticket.Ref) ([]ticket.Snapshot, error)
	Parent(context.Context, ticket.Ref) (ticket.Snapshot, error)
	Create(context.Context, string, redact.Body, redact.Body) (ticket.Snapshot, error)
	Attach(context.Context, ticket.Ref, ticket.Ref) error
	UpdateContent(context.Context, ticket.Ref, string, redact.Body, redact.Body) (ticket.Snapshot, error)
	RepliesAfter(context.Context, ticket.Ref, ticket.Cursor) (ticket.ReplyPage, error)
	PublishReply(context.Context, ticket.Ref, redact.Body) (ticket.Reply, error)
	SetStoryState(context.Context, ticket.Ref, ticket.Ref, bool) error
	RetireStory(context.Context, ticket.Ref, ticket.Ref) error
	Close(context.Context, ticket.Ref) error
}

// TicketPlanBridge is the explicit reader for retained numeric GitHub Plan
// records. It freezes origin before remote resolution and binds immutable IDs
// additively; historical request keys, revisions and prepared bytes stay owned
// by their existing ledgers. Every remote operation uses the neutral contract.
type TicketPlanBridge struct {
	DB      *store.DB
	Tickets PlanTickets
	Bot     ticket.Identity
}

func (b *TicketPlanBridge) locator(repo github.Repo, number int) ticket.Locator {
	return ticket.Locator{Provider: ticket.GitHub, Origin: b.Tickets.Origin(), Container: repo.String(), Key: strconv.Itoa(number)}
}
func planDestination(l ticket.Locator) string {
	return "plan-ticket:" + string(l.Provider) + ":" + l.Container + "#" + l.Key
}
func (b *TicketPlanBridge) bind(ctx context.Context, s ticket.Snapshot) (ticket.Snapshot, error) {
	if b.DB == nil || b.Tickets == nil {
		return ticket.Snapshot{}, errors.New("plan ticket dependencies required")
	}
	if err := s.Ref.Validate(); err != nil {
		return ticket.Snapshot{}, err
	}
	if s.Ref.Identity.Provider != workflowProvider(b.Tickets) || s.Ref.Identity.Origin != b.Tickets.Origin() {
		return ticket.Snapshot{}, store.ErrTicketIdentityConflict
	}
	if err := bindKnownTicketSession(ctx, b.DB, s); err != nil {
		return ticket.Snapshot{}, err
	}
	key, err := ticketMetadataKey(ctx, b.DB, planDestination(s.Ref.Locator), s.Ref.Locator)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	if _, err := b.DB.BindTicketEffect(ctx, key, s.Ref.Locator, ticket.Identity{}); err != nil {
		return ticket.Snapshot{}, err
	}
	if _, err := b.DB.ResolveTicketEffect(ctx, key, s.Ref); err != nil {
		return ticket.Snapshot{}, err
	}
	return s, nil
}
func (b *TicketPlanBridge) read(ctx context.Context, repo github.Repo, number int) (ticket.Snapshot, error) {
	if b.DB == nil || b.Tickets == nil {
		return ticket.Snapshot{}, errors.New("plan ticket dependencies required")
	}
	locator, err := compatibilityLocator(ctx, b.DB, workflowProvider(b.Tickets), b.Tickets.Origin(), repo, number)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	if _, err := retainedTicketSession(ctx, b.DB, locator); err != nil {
		return ticket.Snapshot{}, err
	}
	key, err := ticketMetadataKey(ctx, b.DB, planDestination(locator), locator)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	binding, err := b.DB.BindTicketEffect(ctx, key, locator, ticket.Identity{})
	if err != nil {
		return ticket.Snapshot{}, err
	}
	var s ticket.Snapshot
	if binding.RemoteID != "" {
		ref, refErr := binding.Ref()
		if refErr != nil {
			return ticket.Snapshot{}, refErr
		}
		s, err = b.Tickets.Read(ctx, ref)
	} else {
		s, err = b.Tickets.Resolve(ctx, binding.Locator)
	}
	if err != nil {
		return ticket.Snapshot{}, err
	}
	if s.Ref.Locator != locator || (binding.RemoteID != "" && s.Ref.Identity.RemoteID != binding.RemoteID) {
		return ticket.Snapshot{}, store.ErrTicketIdentityConflict
	}
	return b.bind(ctx, s)
}
func planLegacyIssue(s ticket.Snapshot) (github.Issue, error) {
	if s.Ref.Identity.Provider != ticket.GitHub {
		return github.Issue{}, store.ErrTicketIdentityConflict
	}
	if err := s.Ref.Validate(); err != nil {
		return github.Issue{}, err
	}
	for _, actor := range append([]ticket.Actor{s.Author}, s.Assignees...) {
		if actor.Identity.Provider != s.Ref.Identity.Provider || actor.Identity.Origin != s.Ref.Identity.Origin {
			return github.Issue{}, store.ErrTicketIdentityConflict
		}
	}
	id, err := strconv.ParseInt(s.Ref.Identity.RemoteID, 10, 64)
	if err != nil || id < 1 {
		return github.Issue{}, errors.New("invalid retained GitHub ticket id")
	}
	number, err := strconv.Atoi(s.Ref.Locator.Key)
	if err != nil || number < 1 {
		return github.Issue{}, errors.New("invalid retained GitHub ticket number")
	}
	authorID, err := planLegacyActor(s.Author)
	if err != nil {
		return github.Issue{}, err
	}
	out := github.Issue{APIURL: string(s.Ref.Identity.Origin) + "/repos/" + s.Ref.Locator.Container + "/issues/" + s.Ref.Locator.Key, ID: id, Number: number, Title: s.Title, Body: s.Body, State: s.State, StateReason: s.StateReason, HTMLURL: s.BrowserURL, Author: authorID, Labels: s.Labels, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
	for _, a := range s.Assignees {
		u, err := planLegacyActor(a)
		if err != nil {
			return github.Issue{}, err
		}
		out.Assignees = append(out.Assignees, u)
	}
	return out, nil
}
func planLegacyActor(a ticket.Actor) (github.User, error) {
	id := int64(0)
	if a.Identity.RemoteID != "" {
		var err error
		id, err = strconv.ParseInt(a.Identity.RemoteID, 10, 64)
		if err != nil || id < 1 {
			return github.User{}, errors.New("invalid retained GitHub actor id")
		}
	}
	return github.User{ID: id, Login: a.Handle, IsBot: a.IsBot}, nil
}

// GetIssue reads a recorded ticket through its qualified binding.
func (b *TicketPlanBridge) GetIssue(ctx context.Context, r github.Repo, n int) (github.Issue, error) {
	s, e := b.read(ctx, r, n)
	if e != nil {
		return github.Issue{}, e
	}
	return compatibilityIssue(ctx, b.DB, s)
}

// GetParentIssue observes and binds the direct parent ticket.
func (b *TicketPlanBridge) GetParentIssue(ctx context.Context, r github.Repo, n int) (github.Issue, error) {
	s, e := b.read(ctx, r, n)
	if e != nil {
		return github.Issue{}, e
	}
	p, e := b.Tickets.Parent(ctx, s.Ref)
	if e != nil {
		return github.Issue{}, e
	}
	p, e = b.bind(ctx, p)
	if e != nil {
		return github.Issue{}, e
	}
	return compatibilityIssue(ctx, b.DB, p)
}
func (b *TicketPlanBridge) inventory(ctx context.Context, ss []ticket.Snapshot, e error) ([]github.Issue, error) {
	if e != nil {
		return nil, e
	}
	out := make([]github.Issue, 0, len(ss))
	for _, s := range ss {
		s, e = b.bind(ctx, s)
		if e != nil {
			return nil, e
		}
		v, e := compatibilityIssue(ctx, b.DB, s)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}

// ListIssues returns verified open-ticket observations.
func (b *TicketPlanBridge) ListIssues(ctx context.Context, r github.Repo, t time.Time) ([]github.Issue, error) {
	ss, e := b.Tickets.Discover(ctx, r.String(), t)
	return b.inventory(ctx, ss, e)
}

// ListFollowupIssues includes closed tickets for creation reconciliation.
func (b *TicketPlanBridge) ListFollowupIssues(ctx context.Context, r github.Repo) ([]github.Issue, error) {
	ss, e := b.Tickets.Followups(ctx, r.String())
	return b.inventory(ctx, ss, e)
}

// ListSubIssues returns verified direct child tickets.
func (b *TicketPlanBridge) ListSubIssues(ctx context.Context, r github.Repo, n int) ([]github.Issue, error) {
	p, e := b.read(ctx, r, n)
	if e != nil {
		return nil, e
	}
	ss, e := b.Tickets.Children(ctx, p.Ref)
	return b.inventory(ctx, ss, e)
}

// CreateIssue retains the created immutable identity before attachment.
func (b *TicketPlanBridge) CreateIssue(ctx context.Context, r github.Repo, t, body redact.Body) (github.Issue, error) {
	s, e := b.Tickets.Create(ctx, r.String(), t, body)
	if e != nil {
		return github.Issue{}, e
	}
	s, e = b.bind(ctx, s)
	if e != nil {
		return github.Issue{}, e
	}
	return compatibilityIssue(ctx, b.DB, s)
}

// UpdateIssue checks fresh content and verifies the update through readback.
func (b *TicketPlanBridge) UpdateIssue(ctx context.Context, r github.Repo, n int, t, body redact.Body) (github.Issue, error) {
	s, e := b.read(ctx, r, n)
	if e != nil {
		return github.Issue{}, e
	}
	s, e = b.Tickets.UpdateContent(ctx, s.Ref, s.Revision, t, body)
	if e != nil {
		return github.Issue{}, e
	}
	return compatibilityIssue(ctx, b.DB, s)
}
func (b *TicketPlanBridge) childID(ctx context.Context, r github.Repo, id int64) (ticket.Ref, error) {
	ss, e := b.Tickets.Followups(ctx, r.String())
	if e != nil {
		return ticket.Ref{}, e
	}
	var found *ticket.Snapshot
	for i := range ss {
		candidate, slotErr := compatibilitySlot(ctx, b.DB, "issue", ss[i].Ref.Identity)
		if slotErr != nil {
			return ticket.Ref{}, slotErr
		}
		if candidate == id {
			if found != nil {
				return ticket.Ref{}, errors.New("ambiguous child identity")
			}
			found = &ss[i]
		}
	}
	if found == nil {
		return ticket.Ref{}, fmt.Errorf("child identity %d not found: %w", id, github.ErrNotFound)
	}
	s, e := b.bind(ctx, *found)
	return s.Ref, e
}

// AddSubIssue attaches the recorded immutable child to the recorded parent.
func (b *TicketPlanBridge) AddSubIssue(ctx context.Context, r github.Repo, n int, id int64) error {
	p, e := b.read(ctx, r, n)
	if e != nil {
		return e
	}
	c, e := b.childID(ctx, r, id)
	if e != nil {
		return e
	}
	return b.Tickets.Attach(ctx, p.Ref, c)
}

// ListCommentsAfter translates a bounded complete qualified reply observation.
func (b *TicketPlanBridge) ListCommentsAfter(ctx context.Context, r github.Repo, n int, id int64) ([]github.Comment, error) {
	s, e := b.read(ctx, r, n)
	if e != nil {
		return nil, e
	}
	cursor, e := compatibilityReplyCursor(ctx, b.DB, s.Ref, id)
	if e != nil {
		return nil, e
	}
	page, e := completeCompatibilityReplies(ctx, b.Tickets, s.Ref, cursor)
	if e != nil {
		return nil, e
	}
	if !page.Complete {
		return nil, errors.New("incomplete Plan reply observation")
	}
	out := make([]github.Comment, 0, len(page.Replies))
	for _, reply := range page.Replies {
		v, e := compatibilityReply(ctx, b.DB, reply, s.Ref)
		if e != nil {
			return nil, e
		}
		if v.ID > id {
			out = append(out, v)
		}
	}
	return out, nil
}

// PostComment publishes only to the recorded immutable ticket.
func (b *TicketPlanBridge) PostComment(ctx context.Context, r github.Repo, n int, body redact.Body) (github.Comment, error) {
	s, e := b.read(ctx, r, n)
	if e != nil {
		return github.Comment{}, e
	}
	reply, e := b.Tickets.PublishReply(ctx, s.Ref, body)
	if e != nil {
		return github.Comment{}, e
	}
	return compatibilityReply(ctx, b.DB, reply, s.Ref)
}
func (b *TicketPlanBridge) relationship(ctx context.Context, r github.Repo, pn int, pid int64, cn int, cid int64) (ticket.Ref, ticket.Ref, error) {
	p, e := b.read(ctx, r, pn)
	if e != nil {
		return ticket.Ref{}, ticket.Ref{}, e
	}
	c, e := b.read(ctx, r, cn)
	if e != nil {
		return ticket.Ref{}, ticket.Ref{}, e
	}
	parentID, e := compatibilitySlot(ctx, b.DB, "issue", p.Ref.Identity)
	if e != nil {
		return ticket.Ref{}, ticket.Ref{}, e
	}
	childID, e := compatibilitySlot(ctx, b.DB, "issue", c.Ref.Identity)
	if e != nil {
		return ticket.Ref{}, ticket.Ref{}, e
	}
	if parentID != pid || childID != cid {
		return ticket.Ref{}, ticket.Ref{}, errors.New("retained Plan relationship identity differs")
	}
	return p.Ref, c.Ref, nil
}

// SetStoryState changes lifecycle through verified direct parentage.
func (b *TicketPlanBridge) SetStoryState(ctx context.Context, r github.Repo, pn int, pid int64, cn int, cid int64, done bool) error {
	p, c, e := b.relationship(ctx, r, pn, pid, cn, cid)
	if e != nil {
		return e
	}
	return b.Tickets.SetStoryState(ctx, p, c, done)
}

// RetirePlanStory preserves the not-planned closure and relationship guard.
func (b *TicketPlanBridge) RetirePlanStory(ctx context.Context, r github.Repo, pn int, pid int64, cn int, cid int64) error {
	p, c, e := b.relationship(ctx, r, pn, pid, cn, cid)
	if e != nil {
		return e
	}
	return b.Tickets.RetireStory(ctx, p, c)
}

var _ PlanForge = (*TicketPlanBridge)(nil)
var _ PlanStoryForge = (*TicketPlanBridge)(nil)
var _ StoryProgressForge = (*TicketPlanBridge)(nil)

// UpdateIssueExpected retains the factory's original Plan revision check while
// using the adapter revision for its fresh pre-dispatch check and readback.
func (b *TicketPlanBridge) UpdateIssueExpected(ctx context.Context, r github.Repo, n int, expected string, title, body redact.Body) (github.Issue, error) {
	s, err := b.read(ctx, r, n)
	if err != nil {
		return github.Issue{}, err
	}
	if expected == "" || PlanIssueRevision(s.Title, s.Body) != expected {
		return github.Issue{}, &ticket.Failure{Kind: ticket.Conflict, Operation: "plan content", Cause: errors.New("expected Plan content changed")}
	}
	s, err = b.Tickets.UpdateContent(ctx, s.Ref, s.Revision, title, body)
	if err != nil {
		return github.Issue{}, err
	}
	return compatibilityIssue(ctx, b.DB, s)
}

// CloseDeliveredParent closes the exact recorded parent after caller integration checks.
func (b *TicketPlanBridge) CloseDeliveredParent(ctx context.Context, r github.Repo, n int, id int64) error {
	s, err := b.read(ctx, r, n)
	if err != nil {
		return err
	}
	currentID, err := compatibilitySlot(ctx, b.DB, "issue", s.Ref.Identity)
	if err != nil {
		return err
	}
	if currentID != id {
		return errors.New("delivered parent identity changed")
	}
	return b.Tickets.Close(ctx, s.Ref)
}

// CreateSubIssues retains the compatibility capability marker without allowing
// a batch to bypass the durable managed Plan publication owner.
func (b *TicketPlanBridge) CreateSubIssues(context.Context, github.Repo, int, []github.SubIssueSpec) ([]github.SubIssueResult, error) {
	return nil, &ticket.Failure{Kind: ticket.Unsupported, Operation: "create subissues", Cause: errors.New("batch publication requires the durable Plan publication owner")}
}

var _ SubIssueCreator = (*TicketPlanBridge)(nil)

// BotIdentity exposes only the configured qualified publication identity.
func (b *TicketPlanBridge) BotIdentity() ticket.Identity { return b.Bot }

// ListIssuesPage retains provider pagination in a bounded observation.
func (b *TicketPlanBridge) ListIssuesPage(ctx context.Context, repo github.Repo, state string, page, limit int) (github.IssuePage, error) {
	observed, err := b.Tickets.IssuesPage(ctx, repo.String(), state, page, limit)
	if err != nil {
		return github.IssuePage{}, err
	}
	items, err := b.inventory(ctx, observed.Tickets, nil)
	return github.IssuePage{Items: items, NextPage: observed.NextPage}, err
}

// ReadOpenIssues preserves the bounded newest-ticket context read.
func (b *TicketPlanBridge) ReadOpenIssues(ctx context.Context, repo github.Repo, limit int) ([]github.Issue, bool, error) {
	observed, err := b.Tickets.OpenContext(ctx, repo.String(), limit)
	if err != nil {
		return nil, false, err
	}
	items, err := b.inventory(ctx, observed.Tickets, nil)
	return items, observed.More, err
}
