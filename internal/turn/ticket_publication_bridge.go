package turn

import (
	"context"
	"errors"
	"strconv"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// PublicationTickets is the narrow neutral contract for publication and
// bounded reconciliation. No repository delivery or credential operation is
// available through this dependency.
type PublicationTickets interface {
	Resolve(context.Context, ticket.Locator) (ticket.Snapshot, error)
	Read(context.Context, ticket.Ref) (ticket.Snapshot, error)
	PublishReply(context.Context, ticket.Ref, redact.Body) (ticket.Reply, error)
	RepliesPage(context.Context, ticket.Ref, int, int) (ticket.ReplyPage, error)
	RepliesAfter(context.Context, ticket.Ref, ticket.Cursor) (ticket.ReplyPage, error)
	EditReply(context.Context, ticket.Reply, redact.Body) error
	React(context.Context, ticket.Reply, string) error
}

// TicketPublicationBridge preserves the historical report and comment ledger
// formats while translating their locators into recorded qualified tickets.
// It is an explicit GitHub compatibility boundary, not a second transport.
type TicketPublicationBridge struct {
	db      *store.DB
	tickets PublicationTickets
	origin  ticket.Origin
}

// NewTicketPublicationBridge binds the trusted configured API origin. Legacy
// test fakes may still implement Poster directly; production wiring uses this
// bridge so no publication bypasses the neutral contract.
func NewTicketPublicationBridge(db *store.DB, tickets PublicationTickets, origin ticket.Origin) (*TicketPublicationBridge, error) {
	if db == nil || tickets == nil {
		return nil, errors.New("ticket publication requires factory dependencies")
	}
	if err := (ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "validation"}).Validate(); err != nil {
		return nil, err
	}
	return &TicketPublicationBridge{db: db, tickets: tickets, origin: origin}, nil
}
func (b *TicketPublicationBridge) locator(repo github.Repo, number int) (ticket.Locator, error) {
	if err := repo.Validate(); err != nil {
		return ticket.Locator{}, err
	}
	l := ticket.Locator{Provider: ticket.GitHub, Origin: b.origin, Container: repo.Owner + "/" + repo.Name, Key: strconv.Itoa(number)}
	if number < 1 {
		return ticket.Locator{}, errors.New("ticket publication requires a positive locator")
	}
	return l, l.Validate()
}
func (b *TicketPublicationBridge) resolve(ctx context.Context, key string, locator ticket.Locator, actor ticket.Identity) (ticket.Snapshot, error) {
	if _, err := retainedTicketSession(ctx, b.db, locator); err != nil {
		return ticket.Snapshot{}, err
	}
	saved, err := b.db.BindTicketEffect(ctx, key, locator, actor)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	var current ticket.Snapshot
	if saved.RemoteID == "" {
		current, err = b.tickets.Resolve(ctx, saved.Locator)
	} else {
		var ref ticket.Ref
		ref, err = saved.Ref()
		if err == nil {
			current, err = b.tickets.Read(ctx, ref)
		}
	}
	if err != nil {
		return ticket.Snapshot{}, err
	}
	if err := current.Ref.Validate(); err != nil {
		return ticket.Snapshot{}, err
	}
	if current.Ref.Locator != saved.Locator || (saved.RemoteID != "" && current.Ref.Identity.RemoteID != saved.RemoteID) {
		return ticket.Snapshot{}, store.ErrTicketIdentityConflict
	}
	if err := bindKnownTicketSession(ctx, b.db, current); err != nil {
		return ticket.Snapshot{}, err
	}
	bound, err := b.db.ResolveTicketEffect(ctx, key, current.Ref)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	if _, err = bound.Ref(); err != nil {
		return ticket.Snapshot{}, err
	}
	return current, nil
}
func (b *TicketPublicationBridge) destination(ctx context.Context, repo github.Repo, number int) (ticket.Snapshot, error) {
	locator, err := b.locator(repo, number)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	legacy := "ticket-destination:github:" + locator.Container + "#" + locator.Key
	key, err := ticketMetadataKey(ctx, b.db, legacy, locator)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	return b.resolve(ctx, key, locator, ticket.Identity{})
}

// BindPublication records the exact effect destination and qualified bot before
// dispatch or reconciliation. The existing ledger remains its lifecycle owner.
func (b *TicketPublicationBridge) BindPublication(ctx context.Context, key string, sessionID int64, ref naming.Ref, botID int64) error {
	if botID < 1 {
		return errors.New("publication requires qualified bot identity")
	}
	locator, err := b.locator(github.RepoFromRef(ref), ref.Issue)
	if err != nil {
		return err
	}
	if sessionID > 0 {
		mapping, err := b.db.BindLegacySessionOrigin(ctx, sessionID, b.origin)
		if err != nil {
			return err
		}
		if mapping.Locator == locator {
			snapshot, err := b.destination(ctx, github.RepoFromRef(ref), ref.Issue)
			if err != nil {
				return err
			}
			if _, err = b.db.BindSessionTicket(ctx, sessionID, snapshot.Ref); err != nil {
				return err
			}
		}
	}
	actor := ticket.Identity{Provider: ticket.GitHub, Origin: b.origin, RemoteID: strconv.FormatInt(botID, 10)}
	_, err = b.resolve(ctx, key, locator, actor)
	return err
}
func legacyPublicationReply(reply ticket.Reply, ref ticket.Ref) (github.Comment, error) {
	if reply.Ticket.Validate() != nil || reply.Ticket.Locator != ref.Locator || !reply.Ticket.Equal(ref) || reply.Identity.Provider != ref.Identity.Provider || reply.Identity.Origin != ref.Identity.Origin {
		return github.Comment{}, store.ErrTicketIdentityConflict
	}
	if err := reply.Identity.Validate(); err != nil {
		return github.Comment{}, err
	}
	if reply.Author.Identity.Provider != ref.Identity.Provider || reply.Author.Identity.Origin != ref.Identity.Origin {
		return github.Comment{}, store.ErrTicketIdentityConflict
	}
	id, err := strconv.ParseInt(reply.Identity.RemoteID, 10, 64)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != reply.Identity.RemoteID {
		return github.Comment{}, errors.New("legacy publication receipt is not a GitHub reply ID")
	}
	var actorID int64
	if reply.Author.Identity.RemoteID != "" {
		actorID, err = strconv.ParseInt(reply.Author.Identity.RemoteID, 10, 64)
		if err != nil || actorID < 1 || strconv.FormatInt(actorID, 10) != reply.Author.Identity.RemoteID {
			return github.Comment{}, errors.New("legacy publication author is not a GitHub actor ID")
		}
	}
	return github.Comment{ID: id, Body: reply.Body, Author: github.User{ID: actorID, Login: reply.Author.Handle, IsBot: reply.Author.IsBot}, HTMLURL: reply.BrowserURL, APIURL: string(ref.Identity.Origin) + "/repos/" + ref.Locator.Container + "/issues/comments/" + strconv.FormatInt(id, 10), IssueAPIURL: string(ref.Identity.Origin) + "/repos/" + ref.Locator.Container + "/issues/" + ref.Locator.Key, CreatedAt: reply.CreatedAt, UpdatedAt: reply.UpdatedAt}, nil
}

// PostComment translates only at the existing serialized ledger boundary.
func (b *TicketPublicationBridge) PostComment(ctx context.Context, repo github.Repo, number int, body redact.Body) (github.Comment, error) {
	snapshot, err := b.destination(ctx, repo, number)
	if err != nil {
		return github.Comment{}, err
	}
	reply, err := b.tickets.PublishReply(ctx, snapshot.Ref, body)
	if err != nil {
		return github.Comment{}, err
	}
	comment, err := legacyPublicationReply(reply, snapshot.Ref)
	if err != nil {
		return github.Comment{}, &ticket.Failure{Kind: ticket.Uncertain, Operation: "publication receipt", Cause: err}
	}
	if err := b.rememberReply(ctx, reply); err != nil {
		return github.Comment{}, &ticket.Failure{Kind: ticket.Uncertain, Operation: "retain publication receipt", Cause: err}
	}
	return comment, nil
}

// GetIssue reads a qualified ticket and adapts only its historical display data.
func (b *TicketPublicationBridge) GetIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	s, err := b.destination(ctx, repo, number)
	if err != nil {
		return github.Issue{}, err
	}
	for _, actor := range append([]ticket.Actor{s.Author}, s.Assignees...) {
		if actor.Identity.Provider != s.Ref.Identity.Provider || actor.Identity.Origin != s.Ref.Identity.Origin {
			return github.Issue{}, store.ErrTicketIdentityConflict
		}
	}
	return planLegacyIssue(s)
}

// ListCommentsPage retains the original bounded reconciliation observation.
func (b *TicketPublicationBridge) ListCommentsPage(ctx context.Context, repo github.Repo, number, page, limit int) (github.CommentPage, error) {
	s, err := b.destination(ctx, repo, number)
	if err != nil {
		return github.CommentPage{}, err
	}
	observed, err := b.tickets.RepliesPage(ctx, s.Ref, page, limit)
	if err != nil {
		return github.CommentPage{}, err
	}
	var out github.CommentPage
	if !observed.Complete {
		if !observed.Next.Ticket.Equal(s.Ref.Identity) || observed.Next.Kind != "github-comments-page/v1" {
			return out, store.ErrTicketIdentityConflict
		}
		out.NextPage, err = strconv.Atoi(observed.Next.Value)
		if err != nil || out.NextPage <= page {
			return out, errors.New("invalid publication page cursor")
		}
	}
	for _, reply := range observed.Replies {
		c, err := legacyPublicationReply(reply, s.Ref)
		if err != nil {
			return github.CommentPage{}, err
		}
		if err := b.rememberReply(ctx, reply); err != nil {
			return github.CommentPage{}, err
		}
		out.Comments = append(out.Comments, c)
	}
	if len(out.Comments) > limit {
		return github.CommentPage{}, errors.New("publication observation exceeds bound")
	}
	return out, nil
}

// ListCommentsAfter retains complete ascending numeric GitHub reply ordering.
func (b *TicketPublicationBridge) ListCommentsAfter(ctx context.Context, repo github.Repo, number int, after int64) ([]github.Comment, error) {
	s, err := b.destination(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	observed, err := b.tickets.RepliesAfter(ctx, s.Ref, ticket.Cursor{Ticket: s.Ref.Identity, Kind: "github-comments-after/v1", Value: strconv.FormatInt(after, 10)})
	if err != nil {
		return nil, err
	}
	if !observed.Complete {
		return nil, errors.New("publication recovery requires complete observation")
	}
	var out []github.Comment
	for _, reply := range observed.Replies {
		c, err := legacyPublicationReply(reply, s.Ref)
		if err != nil {
			return nil, err
		}
		if err := b.rememberReply(ctx, reply); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// VerifiedCommentDestination uses trusted API membership, rather than assuming
// a public GitHub browser URL, for compatibility consumers' receipt checks.
func (b *TicketPublicationBridge) VerifiedCommentDestination(c github.Comment, ref naming.Ref) bool {
	return c.ID > 0 && c.IssueAPIURL == string(b.origin)+"/repos/"+ref.Owner+"/"+ref.Repo+"/issues/"+strconv.Itoa(ref.Issue) && c.APIURL == string(b.origin)+"/repos/"+ref.Owner+"/"+ref.Repo+"/issues/comments/"+strconv.FormatInt(c.ID, 10)
}

func bindPublicationDestination(ctx context.Context, forge interface{}, key string, sessionID int64, ref naming.Ref, botID int64) error {
	if qualified, ok := forge.(interface {
		BindPublication(context.Context, string, int64, naming.Ref, int64) error
	}); ok {
		return qualified.BindPublication(ctx, key, sessionID, ref, botID)
	}
	// Explicit legacy injected fakes preserve compatibility fixtures. Production
	// dependencies are constructed as TicketPublicationBridge at entry points.
	return nil
}

func publicationReplyKey(container, remoteID string) string {
	return "publication-reply:github:" + container + "#" + remoteID
}

// replyKey preserves historical keys while distinguishing equal numeric reply
// IDs observed at another API origin. This is additive identity metadata, not
// an owning publication ledger or its prepared bytes.
func (b *TicketPublicationBridge) replyKey(ctx context.Context, container, remoteID string) (string, error) {
	key := publicationReplyKey(container, remoteID)
	existing, err := b.db.GetTicketEffect(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		return key, nil
	}
	if err != nil {
		return "", err
	}
	if existing.Locator.Origin == b.origin {
		return key, nil
	}
	return naming.QualifiedTaskRef(key, ticket.Identity{Provider: ticket.GitHub, Origin: b.origin, RemoteID: remoteID}), nil
}

func (b *TicketPublicationBridge) rememberReply(ctx context.Context, reply ticket.Reply) error {
	if _, err := legacyPublicationReply(reply, reply.Ticket); err != nil {
		return err
	}
	actor := reply.Author.Identity
	if actor.RemoteID == "" {
		actor = ticket.Identity{}
	}
	key, err := b.replyKey(ctx, reply.Ticket.Locator.Container, reply.Identity.RemoteID)
	if err != nil {
		return err
	}
	if _, err := b.db.BindTicketEffect(ctx, key, reply.Ticket.Locator, actor); err != nil {
		// Another origin can claim the legacy key between the read and bind.
		// Retain its record and retry only under our qualified metadata key.
		updated, keyErr := b.replyKey(ctx, reply.Ticket.Locator.Container, reply.Identity.RemoteID)
		if keyErr != nil || updated == key {
			return err
		}
		key = updated
		if _, retryErr := b.db.BindTicketEffect(ctx, key, reply.Ticket.Locator, actor); retryErr != nil {
			return retryErr
		}
	}
	_, err = b.db.ResolveTicketEffect(ctx, key, reply.Ticket)
	return err
}
func (b *TicketPublicationBridge) retainedReply(ctx context.Context, repo github.Repo, id int64) (ticket.Reply, error) {
	if err := repo.Validate(); err != nil {
		return ticket.Reply{}, err
	}
	if id < 1 {
		return ticket.Reply{}, errors.New("invalid publication reply ID")
	}
	remoteID := strconv.FormatInt(id, 10)
	key, err := b.replyKey(ctx, repo.Owner+"/"+repo.Name, remoteID)
	if err != nil {
		return ticket.Reply{}, err
	}
	bound, err := b.db.GetTicketEffect(ctx, key)
	if err != nil {
		return ticket.Reply{}, err
	}
	if bound.Locator.Provider != ticket.GitHub || bound.Locator.Origin != b.origin || bound.Locator.Container != repo.Owner+"/"+repo.Name {
		return ticket.Reply{}, store.ErrTicketIdentityConflict
	}
	ref, err := bound.Ref()
	if err != nil {
		return ticket.Reply{}, err
	}
	return ticket.Reply{Ticket: ref, Identity: ticket.Identity{Provider: ticket.GitHub, Origin: b.origin, RemoteID: remoteID}, Author: ticket.Actor{Identity: bound.Actor}}, nil
}

// EditComment requires an observed qualified receipt. An unknown historical
// numeric reply ID cannot be converted directly into a mutation destination.
func (b *TicketPublicationBridge) EditComment(ctx context.Context, repo github.Repo, id int64, body redact.Body) error {
	reply, err := b.retainedReply(ctx, repo, id)
	if err != nil {
		return err
	}
	return b.tickets.EditReply(ctx, reply, body)
}

// AddCommentReaction applies only to a previously observed qualified reply.
func (b *TicketPublicationBridge) AddCommentReaction(ctx context.Context, repo github.Repo, id int64, reaction string) error {
	reply, err := b.retainedReply(ctx, repo, id)
	if err != nil {
		return err
	}
	return b.tickets.React(ctx, reply, reaction)
}
