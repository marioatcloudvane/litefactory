package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// Tickets adapts ticket operations using the existing authenticated client. It
// exposes no repository, branch, PR or merge operations. Consumer interfaces
// belong in the consuming package. The adapter never retries a remote effect.
type Tickets struct {
	client *Client
	origin ticket.Origin
}

// NewTickets binds the adapter to the client's trusted configured API root.
func NewTickets(client *Client) (*Tickets, error) {
	if client == nil || client.api == nil {
		return nil, ErrUninitialized
	}
	origin, err := ticket.CanonicalOrigin(client.BaseURL())
	if err != nil {
		return nil, err
	}
	return &Tickets{client: client, origin: origin}, nil
}

// Origin returns the trusted canonical API instance, never a ticket-supplied URL.
func (a *Tickets) Origin() ticket.Origin { return a.origin }

func (a *Tickets) address(locator ticket.Locator) (Repo, int, error) {
	if err := locator.Validate(); err != nil {
		return Repo{}, 0, ticketError("address", err, false)
	}
	if locator.Provider != ticket.GitHub || locator.Origin != a.origin {
		return Repo{}, 0, &ticket.Failure{Kind: ticket.Refused, Operation: "address", Cause: errors.New("ticket belongs to a different API instance")}
	}
	parts := strings.Split(locator.Container, "/")
	if len(parts) != 2 {
		return Repo{}, 0, ticketError("address", ErrInvalidArgument, false)
	}
	repo := Repo{Owner: parts[0], Name: parts[1]}
	if err := repo.Validate(); err != nil {
		return Repo{}, 0, ticketError("address", err, false)
	}
	number, err := strconv.Atoi(locator.Key)
	if err != nil || number < 1 || strconv.Itoa(number) != locator.Key {
		return Repo{}, 0, ticketError("address", ErrInvalidArgument, false)
	}
	return repo, number, nil
}

func (a *Tickets) actor(user User) ticket.Actor {
	id := ""
	if user.ID > 0 {
		id = strconv.FormatInt(user.ID, 10)
	}
	return ticket.Actor{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: a.origin, RemoteID: id}, Handle: user.Login, IsBot: user.IsBot}
}

// GitHub repository names are case-insensitive. Keep the trusted origin and
// endpoint exact while accepting the provider's canonical owner/name casing.
func (a *Tickets) matchesAPIURL(actual string, repo Repo, endpoint string) bool {
	rest, ok := strings.CutPrefix(actual, string(a.origin)+"/repos/")
	if !ok {
		return false
	}
	parts := strings.SplitN(rest, "/", 3)
	return len(parts) == 3 && strings.EqualFold(parts[0], repo.Owner) && strings.EqualFold(parts[1], repo.Name) && parts[2] == endpoint
}

func (a *Tickets) snapshot(repo Repo, issue Issue) (ticket.Snapshot, error) {
	if issue.ID < 1 || issue.Number < 1 {
		return ticket.Snapshot{}, &ticket.Failure{Kind: ticket.Conflict, Operation: "read", Cause: errors.New("response has no immutable ticket identity")}
	}
	if !a.matchesAPIURL(issue.APIURL, repo, "issues/"+strconv.Itoa(issue.Number)) {
		return ticket.Snapshot{}, &ticket.Failure{Kind: ticket.Conflict, Operation: "read", Cause: errors.New("response ticket origin or container differs")}
	}
	ref := ticket.Ref{
		Identity: ticket.Identity{Provider: ticket.GitHub, Origin: a.origin, RemoteID: strconv.FormatInt(issue.ID, 10)},
		Locator:  ticket.Locator{Provider: ticket.GitHub, Origin: a.origin, Container: repo.String(), Key: strconv.Itoa(issue.Number)},
	}
	assignees := make([]ticket.Actor, len(issue.Assignees))
	for i, user := range issue.Assignees {
		assignees[i] = a.actor(user)
	}
	// A length-delimited encoding avoids ambiguous concatenations. This token
	// covers editable content only; timestamps and labels do not grant authority.
	encoded, err := json.Marshal([2]string{issue.Title, issue.Body})
	if err != nil {
		return ticket.Snapshot{}, fmt.Errorf("ticket revision: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return ticket.Snapshot{Ref: ref, DisplayKey: ref.Locator.Key, BrowserURL: issue.HTMLURL,
		Title: issue.Title, Body: issue.Body, State: issue.State, StateReason: issue.StateReason,
		Author: a.actor(issue.Author), Assignees: assignees, Labels: slices.Clone(issue.Labels),
		CreatedAt: issue.CreatedAt, UpdatedAt: issue.UpdatedAt, Revision: hex.EncodeToString(digest[:])}, nil
}

// Resolve verifies a locator and returns the immutable binding.
func (a *Tickets) Resolve(ctx context.Context, locator ticket.Locator) (ticket.Snapshot, error) {
	repo, number, err := a.address(locator)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	issue, err := a.client.GetIssue(ctx, repo, number)
	if err != nil {
		return ticket.Snapshot{}, ticketError("read", err, false)
	}
	if issue.Number != number {
		return ticket.Snapshot{}, &ticket.Failure{Kind: ticket.Conflict, Operation: "read", Cause: errors.New("response ticket number differs")}
	}
	return a.snapshot(repo, issue)
}

// Read requires the locator still to resolve to the recorded immutable ticket.
func (a *Tickets) Read(ctx context.Context, ref ticket.Ref) (ticket.Snapshot, error) {
	if err := ref.Validate(); err != nil {
		return ticket.Snapshot{}, ticketError("read", err, false)
	}
	snapshot, err := a.Resolve(ctx, ref.Locator)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	if !snapshot.Ref.Equal(ref) {
		return ticket.Snapshot{}, &ticket.Failure{Kind: ticket.Conflict, Operation: "read", Cause: errors.New("recorded ticket identity changed")}
	}
	return snapshot, nil
}

// Discover preserves the client's complete bounded open-issue discovery.
func (a *Tickets) Discover(ctx context.Context, container string, since time.Time) ([]ticket.Snapshot, error) {
	locator := ticket.Locator{Provider: ticket.GitHub, Origin: a.origin, Container: container, Key: "1"}
	repo, _, err := a.address(locator)
	if err != nil {
		return nil, err
	}
	issues, err := a.client.ListIssues(ctx, repo, since)
	if err != nil {
		return nil, ticketError("discover", err, false)
	}
	out := make([]ticket.Snapshot, 0, len(issues))
	for _, issue := range issues {
		snapshot, err := a.snapshot(repo, issue)
		if err != nil {
			return nil, err
		}
		out = append(out, snapshot)
	}
	return out, nil
}

// ReplyCursor preserves the legacy numeric watermark without making
// opaque neutral reply IDs orderable. The cursor is bound to its exact ticket.
func ReplyCursor(ref ticket.Ref, afterID int64) (ticket.Cursor, error) {
	if err := ref.Validate(); err != nil {
		return ticket.Cursor{}, err
	}
	if ref.Identity.Provider != ticket.GitHub || afterID < 0 {
		return ticket.Cursor{}, ErrInvalidArgument
	}
	return ticket.Cursor{Ticket: ref.Identity, Kind: "github-comments-after/v1", Value: strconv.FormatInt(afterID, 10)}, nil
}

func (a *Tickets) reply(ref ticket.Ref, comment Comment) (ticket.Reply, error) {
	repo, _, err := a.address(ref.Locator)
	if err != nil {
		return ticket.Reply{}, err
	}
	if !a.matchesAPIURL(comment.IssueAPIURL, repo, "issues/"+ref.Locator.Key) || !a.matchesAPIURL(comment.APIURL, repo, "issues/comments/"+strconv.FormatInt(comment.ID, 10)) {
		return ticket.Reply{}, errors.New("response reply origin or ticket differs")
	}
	if comment.ID < 1 {
		return ticket.Reply{}, errors.New("response has no immutable reply identity")
	}
	return ticket.Reply{Ticket: ref, Identity: ticket.Identity{Provider: ticket.GitHub, Origin: a.origin, RemoteID: strconv.FormatInt(comment.ID, 10)},
		Author: a.actor(comment.Author), Body: comment.Body, BrowserURL: comment.HTMLURL, CreatedAt: comment.CreatedAt, UpdatedAt: comment.UpdatedAt}, nil
}

// RepliesAfter uses GitHub's existing bounded complete traversal and ascending
// numeric order. It retains inclusive bootstrap and legacy watermark semantics.
func (a *Tickets) RepliesAfter(ctx context.Context, ref ticket.Ref, cursor ticket.Cursor) (ticket.ReplyPage, error) {
	if _, err := a.Read(ctx, ref); err != nil {
		return ticket.ReplyPage{}, err
	}
	if !cursor.Ticket.Equal(ref.Identity) || cursor.Kind != "github-comments-after/v1" {
		return ticket.ReplyPage{}, ticketError("replies", ErrInvalidArgument, false)
	}
	after, err := strconv.ParseInt(cursor.Value, 10, 64)
	if err != nil || after < 0 || strconv.FormatInt(after, 10) != cursor.Value {
		return ticket.ReplyPage{}, ticketError("replies", ErrInvalidArgument, false)
	}
	repo, number, err := a.address(ref.Locator)
	if err != nil {
		return ticket.ReplyPage{}, err
	}
	comments, err := a.client.ListCommentsAfter(ctx, repo, number, after)
	if err != nil {
		return ticket.ReplyPage{}, ticketError("replies", err, false)
	}
	out := ticket.ReplyPage{Next: cursor, Complete: true}
	for _, comment := range comments {
		reply, err := a.reply(ref, comment)
		if err != nil {
			return ticket.ReplyPage{}, &ticket.Failure{Kind: ticket.Conflict, Operation: "replies", Cause: err}
		}
		out.Replies = append(out.Replies, reply)
		out.Next.Value = strconv.FormatInt(comment.ID, 10)
	}
	return out, nil
}

// PublishReply returns a qualified receipt. A lost or invalid response remains
// uncertain: only the owning publication ledger may reconcile that dispatch.
func (a *Tickets) PublishReply(ctx context.Context, ref ticket.Ref, body redact.Body) (ticket.Reply, error) {
	if _, err := a.Read(ctx, ref); err != nil {
		return ticket.Reply{}, err
	}
	repo, number, err := a.address(ref.Locator)
	if err != nil {
		return ticket.Reply{}, err
	}
	comment, err := a.client.PostComment(ctx, repo, number, body)
	if err != nil {
		return ticket.Reply{}, ticketError("publish reply", err, true)
	}
	reply, err := a.reply(ref, comment)
	if err != nil {
		return ticket.Reply{}, &ticket.Failure{Kind: ticket.Uncertain, Operation: "publish reply", Cause: err}
	}
	return reply, nil
}

// UpdateContent compares a fresh content revision and reads back the effect.
// GitHub has no atomic compare-and-swap; a concurrent edit can still race the
// PATCH. A failed readback cannot confirm the effect and must retain intent.
func (a *Tickets) UpdateContent(ctx context.Context, ref ticket.Ref, revision string, title, body redact.Body) (ticket.Snapshot, error) {
	current, err := a.Read(ctx, ref)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	if revision == "" || current.Revision != revision {
		return ticket.Snapshot{}, &ticket.Failure{Kind: ticket.Conflict, Operation: "update content", Cause: errors.New("content revision changed")}
	}
	repo, number, err := a.address(ref.Locator)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	updated, err := a.client.UpdateIssue(ctx, repo, number, title, body)
	if err != nil {
		return ticket.Snapshot{}, ticketError("update content", err, true)
	}
	if updated.ID != currentID(ref) {
		return ticket.Snapshot{}, &ticket.Failure{Kind: ticket.Uncertain, Operation: "update content", Cause: errors.New("mutation receipt identity differs")}
	}
	observed, err := a.Read(ctx, ref)
	if err != nil {
		return ticket.Snapshot{}, &ticket.Failure{Kind: ticket.Uncertain, Operation: "update content", Cause: err}
	}
	if observed.Title != title.String() || observed.Body != body.String() {
		return ticket.Snapshot{}, &ticket.Failure{Kind: ticket.Conflict, Operation: "update content", Cause: errors.New("content readback differs")}
	}
	return observed, nil
}

func currentID(ref ticket.Ref) int64 {
	id, err := strconv.ParseInt(ref.Identity.RemoteID, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func ticketError(operation string, err error, dispatched bool) error {
	kind := ticket.Unavailable
	if dispatched {
		kind = ticket.Uncertain
	}
	switch {
	case errors.Is(err, ErrInvalidArgument), errors.Is(err, ErrUninitialized), errors.Is(err, ErrUnauthorized), errors.Is(err, ErrForbidden), errors.Is(err, ErrNotFound), errors.Is(err, ErrUnprocessable), errors.Is(err, ErrRateLimited):
		kind = ticket.Refused
	}
	return &ticket.Failure{Kind: kind, Operation: operation, Cause: err}
}

// Create keeps ticket creation separate from parent attachment so an existing
// ledger can retain a created child when a later attachment fails.
func (a *Tickets) Create(ctx context.Context, container string, title, body redact.Body) (ticket.Snapshot, error) {
	repo, _, err := a.address(ticket.Locator{Provider: ticket.GitHub, Origin: a.origin, Container: container, Key: "1"})
	if err != nil {
		return ticket.Snapshot{}, err
	}
	issue, err := a.client.CreateIssue(ctx, repo, title, body)
	if err != nil {
		return ticket.Snapshot{}, ticketError("create", err, true)
	}
	snapshot, err := a.snapshot(repo, issue)
	if err != nil {
		return ticket.Snapshot{}, &ticket.Failure{Kind: ticket.Uncertain, Operation: "create", Cause: err}
	}
	return snapshot, nil
}

// Children returns verified direct child identities, with the client's existing
// pagination bounds. Parentage must still be refreshed before a mutation.
func (a *Tickets) Children(ctx context.Context, parent ticket.Ref) ([]ticket.Snapshot, error) {
	if _, err := a.Read(ctx, parent); err != nil {
		return nil, err
	}
	repo, number, err := a.address(parent.Locator)
	if err != nil {
		return nil, err
	}
	issues, err := a.client.ListSubIssues(ctx, repo, number)
	if err != nil {
		return nil, ticketError("children", err, false)
	}
	children := make([]ticket.Snapshot, 0, len(issues))
	for _, issue := range issues {
		snapshot, err := a.snapshot(repo, issue)
		if err != nil {
			return nil, err
		}
		children = append(children, snapshot)
	}
	return children, nil
}

// Parent observes the current direct parent, independently of text markers.
func (a *Tickets) Parent(ctx context.Context, child ticket.Ref) (ticket.Snapshot, error) {
	if _, err := a.Read(ctx, child); err != nil {
		return ticket.Snapshot{}, err
	}
	repo, number, err := a.address(child.Locator)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	issue, err := a.client.GetParentIssue(ctx, repo, number)
	if err != nil {
		return ticket.Snapshot{}, ticketError("parent", err, false)
	}
	return a.snapshot(repo, issue)
}

// Attach verifies both immutable identities and refuses an existing different
// direct parent. It never asks GitHub to replace a relationship.
func (a *Tickets) Attach(ctx context.Context, parent, child ticket.Ref) error {
	if parent.Locator.Container != child.Locator.Container || parent.Equal(child) {
		return ticketError("attach", ErrInvalidArgument, false)
	}
	if _, err := a.Read(ctx, parent); err != nil {
		return err
	}
	if _, err := a.Read(ctx, child); err != nil {
		return err
	}
	existing, err := a.Parent(ctx, child)
	if err == nil {
		if existing.Ref.Equal(parent) {
			return nil
		}
		return &ticket.Failure{Kind: ticket.Conflict, Operation: "attach", Cause: errors.New("child already has another parent")}
	}
	// GitHub's parent endpoint returns 404 for an unattached accessible child;
	// both objects were just read above. Other failures never permit attachment.
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	repo, number, err := a.address(parent.Locator)
	if err != nil {
		return err
	}
	if err := a.client.AddSubIssue(ctx, repo, number, currentID(child)); err != nil {
		return ticketError("attach", err, true)
	}
	observed, err := a.Parent(ctx, child)
	if err != nil {
		return &ticket.Failure{Kind: ticket.Uncertain, Operation: "attach", Cause: err}
	}
	if !observed.Ref.Equal(parent) {
		return &ticket.Failure{Kind: ticket.Conflict, Operation: "attach", Cause: errors.New("parent readback differs")}
	}
	return nil
}

func (a *Tickets) relationship(ctx context.Context, parent, child ticket.Ref) (Repo, int, int, error) {
	if parent.Locator.Container != child.Locator.Container {
		return Repo{}, 0, 0, ticketError("relationship", ErrInvalidArgument, false)
	}
	if _, err := a.Read(ctx, parent); err != nil {
		return Repo{}, 0, 0, err
	}
	if _, err := a.Read(ctx, child); err != nil {
		return Repo{}, 0, 0, err
	}
	observed, err := a.Parent(ctx, child)
	if err != nil {
		return Repo{}, 0, 0, err
	}
	if !observed.Ref.Equal(parent) {
		return Repo{}, 0, 0, &ticket.Failure{Kind: ticket.Conflict, Operation: "relationship", Cause: errors.New("direct parent changed")}
	}
	repo, parentNumber, err := a.address(parent.Locator)
	if err != nil {
		return Repo{}, 0, 0, err
	}
	_, childNumber, err := a.address(child.Locator)
	return repo, parentNumber, childNumber, err
}

// SetStoryState preserves the existing managed-child lifecycle checks.
func (a *Tickets) SetStoryState(ctx context.Context, parent, child ticket.Ref, completed bool) error {
	repo, parentNumber, childNumber, err := a.relationship(ctx, parent, child)
	if err != nil {
		return err
	}
	if err := a.client.SetStoryState(ctx, repo, parentNumber, currentID(parent), childNumber, currentID(child), completed); err != nil {
		return ticketError("story state", err, true)
	}
	return nil
}

// RetireStory preserves not-planned closure and verified direct parentage.
func (a *Tickets) RetireStory(ctx context.Context, parent, child ticket.Ref) error {
	repo, parentNumber, childNumber, err := a.relationship(ctx, parent, child)
	if err != nil {
		return err
	}
	if err := a.client.RetirePlanStory(ctx, repo, parentNumber, currentID(parent), childNumber, currentID(child)); err != nil {
		return ticketError("retire story", err, true)
	}
	return nil
}

// Close closes only the recorded ticket; integration authority remains in the
// calling workflow and is not granted by this ticket adapter.
func (a *Tickets) Close(ctx context.Context, ref ticket.Ref) error {
	if _, err := a.Read(ctx, ref); err != nil {
		return err
	}
	repo, number, err := a.address(ref.Locator)
	if err != nil {
		return err
	}
	if err := a.client.CloseDeliveredParent(ctx, repo, number, currentID(ref)); err != nil {
		return ticketError("close", err, true)
	}
	observed, err := a.Read(ctx, ref)
	if err != nil {
		return &ticket.Failure{Kind: ticket.Uncertain, Operation: "close", Cause: err}
	}
	if observed.State != "closed" {
		return &ticket.Failure{Kind: ticket.Conflict, Operation: "close", Cause: errors.New("closure readback differs")}
	}
	return nil
}

// AddLabel uses a fresh immutable binding before the workflow-label mutation.
func (a *Tickets) AddLabel(ctx context.Context, ref ticket.Ref, label string) error {
	if _, err := a.Read(ctx, ref); err != nil {
		return err
	}
	repo, number, err := a.address(ref.Locator)
	if err != nil {
		return err
	}
	return ticketErrorOrNil("add label", a.client.AddIssueLabel(ctx, repo, number, label))
}

// RemoveLabel uses a fresh immutable binding before removing a workflow label.
func (a *Tickets) RemoveLabel(ctx context.Context, ref ticket.Ref, label string) error {
	if _, err := a.Read(ctx, ref); err != nil {
		return err
	}
	repo, number, err := a.address(ref.Locator)
	if err != nil {
		return err
	}
	return ticketErrorOrNil("remove label", a.client.RemoveIssueLabel(ctx, repo, number, label))
}

func ticketErrorOrNil(operation string, err error) error {
	if err == nil {
		return nil
	}
	return ticketError(operation, err, true)
}

// RepliesPage observes one bounded page for ledger reconciliation. A completed
// traversal is not proof that an uncertain write failed to reach GitHub.
func (a *Tickets) RepliesPage(ctx context.Context, ref ticket.Ref, page, limit int) (ticket.ReplyPage, error) {
	if _, err := a.Read(ctx, ref); err != nil {
		return ticket.ReplyPage{}, err
	}
	repo, number, err := a.address(ref.Locator)
	if err != nil {
		return ticket.ReplyPage{}, err
	}
	if page < 1 || limit < 1 || limit > 100 {
		return ticket.ReplyPage{}, ticketError("reply page", ErrInvalidArgument, false)
	}
	observed, err := a.client.ListCommentsPage(ctx, repo, number, page, limit)
	if err != nil {
		return ticket.ReplyPage{}, ticketError("reply page", err, false)
	}
	out := ticket.ReplyPage{Complete: observed.NextPage == 0, Next: ticket.Cursor{Ticket: ref.Identity, Kind: "github-comments-page/v1", Value: strconv.Itoa(observed.NextPage)}}
	for _, comment := range observed.Comments {
		reply, err := a.reply(ref, comment)
		if err != nil {
			return ticket.ReplyPage{}, &ticket.Failure{Kind: ticket.Conflict, Operation: "reply page", Cause: err}
		}
		out.Replies = append(out.Replies, reply)
	}
	return out, nil
}

// verifiedReply prevents a reply ID from another ticket or API instance from
// selecting a mutation destination. Reads retain the existing complete bounds.
func (a *Tickets) verifiedReply(ctx context.Context, reply ticket.Reply) (Repo, int64, error) {
	if !reply.Identity.Equal(ticket.Identity{Provider: ticket.GitHub, Origin: a.origin, RemoteID: reply.Identity.RemoteID}) || reply.Author.Identity.Validate() != nil {
		return Repo{}, 0, ticketError("verify reply", ErrInvalidArgument, false)
	}
	cursor, err := ReplyCursor(reply.Ticket, 0)
	if err != nil {
		return Repo{}, 0, ticketError("verify reply", err, false)
	}
	page, err := a.RepliesAfter(ctx, reply.Ticket, cursor)
	if err != nil {
		return Repo{}, 0, err
	}
	for _, observed := range page.Replies {
		if observed.Identity.Equal(reply.Identity) {
			if !observed.Author.Identity.Equal(reply.Author.Identity) {
				return Repo{}, 0, &ticket.Failure{Kind: ticket.Conflict, Operation: "verify reply", Cause: errors.New("reply author differs")}
			}
			repo, _, err := a.address(reply.Ticket.Locator)
			id, parseErr := strconv.ParseInt(reply.Identity.RemoteID, 10, 64)
			if parseErr != nil || id < 1 {
				return Repo{}, 0, ticketError("verify reply", ErrInvalidArgument, false)
			}
			return repo, id, err
		}
	}
	return Repo{}, 0, &ticket.Failure{Kind: ticket.Conflict, Operation: "verify reply", Cause: errors.New("reply is not on the recorded ticket")}
}

// EditReply retains redacted writes and checks the recorded reply's membership
// and immutable author before dispatch. Bot authority remains with the caller.
func (a *Tickets) EditReply(ctx context.Context, reply ticket.Reply, body redact.Body) error {
	repo, id, err := a.verifiedReply(ctx, reply)
	if err != nil {
		return err
	}
	return ticketErrorOrNil("edit reply", a.client.EditComment(ctx, repo, id, body))
}

// React uses only a verified ticket-local reply identity.
func (a *Tickets) React(ctx context.Context, reply ticket.Reply, reaction string) error {
	repo, id, err := a.verifiedReply(ctx, reply)
	if err != nil {
		return err
	}
	return ticketErrorOrNil("react", a.client.AddCommentReaction(ctx, repo, id, ReactionContent(reaction)))
}
