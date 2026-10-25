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
	db       *store.DB
	tickets  PublicationTickets
	origin   ticket.Origin
	provider ticket.Provider
	bot      ticket.Identity
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
	return &TicketPublicationBridge{db: db, tickets: tickets, origin: origin, provider: workflowProvider(tickets)}, nil
}
func (b *TicketPublicationBridge) locator(ctx context.Context, repo github.Repo, number int) (ticket.Locator, error) {
	if err := repo.Validate(); err != nil {
		return ticket.Locator{}, err
	}
	if b.provider != ticket.GitHub {
		return compatibilityLocator(ctx, b.db, b.provider, b.origin, repo, number)
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
	locator, err := b.locator(ctx, repo, number)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	legacy := "ticket-destination:" + string(b.provider) + ":" + locator.Container + "#" + locator.Key
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
	locator, err := b.locator(ctx, github.RepoFromRef(ref), ref.Issue)
	if err != nil {
		return err
	}
	if sessionID > 0 && b.provider == ticket.GitHub {
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
	actor, err := compatibilityIdentity(ctx, b.db, b.provider, b.origin, "actor", botID)
	if err != nil {
		return err
	}
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
	var replyTo int64
	if reply.ReplyTo.RemoteID != "" {
		if reply.ReplyTo.Provider != ref.Identity.Provider || reply.ReplyTo.Origin != ref.Identity.Origin {
			return github.Comment{}, store.ErrTicketIdentityConflict
		}
		replyTo, err = strconv.ParseInt(reply.ReplyTo.RemoteID, 10, 64)
		if err != nil || replyTo < 1 {
			return github.Comment{}, store.ErrTicketIdentityConflict
		}
	}
	return github.Comment{ReplyToCommentID: replyTo, ID: id, Body: reply.Body, Author: github.User{ID: actorID, Login: reply.Author.Handle, IsBot: reply.Author.IsBot}, HTMLURL: reply.BrowserURL, APIURL: string(ref.Identity.Origin) + "/repos/" + ref.Locator.Container + "/issues/comments/" + strconv.FormatInt(id, 10), IssueAPIURL: string(ref.Identity.Origin) + "/repos/" + ref.Locator.Container + "/issues/" + ref.Locator.Key, CreatedAt: reply.CreatedAt, UpdatedAt: reply.UpdatedAt}, nil
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
	comment, err := compatibilityReply(ctx, b.db, reply, snapshot.Ref)
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
	return compatibilityIssue(ctx, b.db, s)
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
		if !observed.Next.Ticket.Equal(s.Ref.Identity) || (b.provider == ticket.GitHub && observed.Next.Kind != "github-comments-page/v1") {
			return out, store.ErrTicketIdentityConflict
		}
		if b.provider != ticket.GitHub {
			out.NextPage = page + 1
		} else {
			out.NextPage, err = strconv.Atoi(observed.Next.Value)
		}
		if err != nil || out.NextPage <= page {
			return out, errors.New("invalid publication page cursor")
		}
	}
	for _, reply := range observed.Replies {
		c, err := compatibilityReply(ctx, b.db, reply, s.Ref)
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
	cursor, err := compatibilityReplyCursor(ctx, b.db, s.Ref, after)
	if err != nil {
		return nil, err
	}
	observed, err := completeCompatibilityReplies(ctx, b.tickets, s.Ref, cursor)
	if err != nil {
		return nil, err
	}
	if !observed.Complete {
		return nil, errors.New("publication recovery requires complete observation")
	}
	var out []github.Comment
	for _, reply := range observed.Replies {
		c, err := compatibilityReply(ctx, b.db, reply, s.Ref)
		if err != nil {
			return nil, err
		}
		if err := b.rememberReply(ctx, reply); err != nil {
			return nil, err
		}
		if c.ID > after {
			out = append(out, c)
		}
	}
	return out, nil
}

// VerifiedCommentDestination uses trusted API membership, rather than assuming
// a public GitHub browser URL, for compatibility consumers' receipt checks.
func (b *TicketPublicationBridge) VerifiedCommentDestination(c github.Comment, ref naming.Ref) bool {
	if b.provider != ticket.GitHub {
		return false // Opaque local slots require the contextual receipt check.
	}
	return c.ID > 0 && c.IssueAPIURL == string(b.origin)+"/repos/"+ref.Owner+"/"+ref.Repo+"/issues/"+strconv.Itoa(ref.Issue) && c.APIURL == string(b.origin)+"/repos/"+ref.Owner+"/"+ref.Repo+"/issues/comments/"+strconv.FormatInt(c.ID, 10)
}

func (b *TicketPublicationBridge) verifiedCommentDestination(ctx context.Context, c github.Comment, ref naming.Ref) bool {
	if b.provider != ticket.GitHub {
		q := c.QualifiedReply
		if c.ID <= 0 || q == nil || q.Ticket.Locator.Provider != b.provider || q.Ticket.Locator.Origin != b.origin || q.Ticket.Locator.Container != ref.Owner+"/"+ref.Repo {
			return false
		}
		identity, err := b.db.TicketIdentityForSlot(ctx, "issue", int64(ref.Issue))
		if err != nil || !identity.Equal(q.Ticket.Identity) {
			return false
		}
		reply, err := b.db.TicketIdentityForSlot(ctx, "comment", c.ID)
		return err == nil && reply.Equal(q.Identity)
	}
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
	if b.provider != ticket.GitHub {
		key = "publication-reply:" + string(b.provider) + ":" + container + "#" + remoteID
	}
	existing, err := b.db.GetTicketEffect(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		return key, nil
	}
	if err != nil {
		return "", err
	}
	if existing.Locator.Origin == b.origin && existing.Locator.Provider == b.provider {
		return key, nil
	}
	return naming.QualifiedTaskRef(key, ticket.Identity{Provider: b.provider, Origin: b.origin, RemoteID: remoteID}), nil
}

func (b *TicketPublicationBridge) rememberReply(ctx context.Context, reply ticket.Reply) error {
	if _, err := compatibilityReply(ctx, b.db, reply, reply.Ticket); err != nil {
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
	identity, err := compatibilityIdentity(ctx, b.db, b.provider, b.origin, "comment", id)
	if err != nil {
		return ticket.Reply{}, err
	}
	remoteID := identity.RemoteID
	key, err := b.replyKey(ctx, repo.Owner+"/"+repo.Name, remoteID)
	if err != nil {
		return ticket.Reply{}, err
	}
	bound, err := b.db.GetTicketEffect(ctx, key)
	if err != nil {
		return ticket.Reply{}, err
	}
	if bound.Locator.Provider != b.provider || bound.Locator.Origin != b.origin || bound.Locator.Container != repo.Owner+"/"+repo.Name {
		return ticket.Reply{}, store.ErrTicketIdentityConflict
	}
	ref, err := bound.Ref()
	if err != nil {
		return ticket.Reply{}, err
	}
	return ticket.Reply{Ticket: ref, Identity: ticket.Identity{Provider: b.provider, Origin: b.origin, RemoteID: remoteID}, Author: ticket.Actor{Identity: bound.Actor}}, nil
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
