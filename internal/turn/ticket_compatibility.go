package turn

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// WorkflowTickets combines only the ticket operations needed by the legacy
// workflow boundary. GitHub code delivery remains a separate dependency.
type WorkflowTickets interface {
	PlanTickets
	PublicationTickets
	AddLabel(context.Context, ticket.Ref, string) error
	RemoveLabel(context.Context, ticket.Ref, string) error
	DiscoverCommented(context.Context, string, time.Time) ([]ticket.Locator, error)
}

func workflowProvider(adapter any) ticket.Provider {
	if qualified, ok := adapter.(interface{ Provider() ticket.Provider }); ok {
		return qualified.Provider()
	}
	return ticket.GitHub
}

func compatibilityIdentity(ctx context.Context, db *store.DB, provider ticket.Provider, origin ticket.Origin, kind string, id int64) (ticket.Identity, error) {
	if provider == ticket.GitHub {
		identity := ticket.Identity{Provider: provider, Origin: origin, RemoteID: strconv.FormatInt(id, 10)}
		if id <= 0 {
			return ticket.Identity{}, store.ErrTicketIdentityConflict
		}
		return identity, identity.Validate()
	}
	identity, err := db.TicketIdentityForSlot(ctx, kind, id)
	if err != nil {
		return ticket.Identity{}, err
	}
	if identity.Provider != provider || identity.Origin != origin {
		return ticket.Identity{}, store.ErrTicketIdentityConflict
	}
	return identity, nil
}

func compatibilityLocator(ctx context.Context, db *store.DB, provider ticket.Provider, origin ticket.Origin, repo github.Repo, number int) (ticket.Locator, error) {
	if provider == ticket.GitHub {
		l := ticket.Locator{Provider: provider, Origin: origin, Container: repo.String(), Key: strconv.Itoa(number)}
		if number < 1 {
			return l, store.ErrTicketIdentityConflict
		}
		return l, l.Validate()
	}
	id, err := compatibilityIdentity(ctx, db, provider, origin, "issue", int64(number))
	if err != nil {
		return ticket.Locator{}, err
	}
	// Existing bindings retain the provider's original locator even if its
	// visible identifier changes. Unknown tickets are resolved by immutable ID.
	bound, err := db.GetTicketBinding(ctx, id)
	if err == nil {
		if bound.Ref.Locator.Container != repo.String() {
			return ticket.Locator{}, store.ErrTicketIdentityConflict
		}
		return bound.Ref.Locator, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return ticket.Locator{}, err
	}
	return ticket.Locator{Provider: provider, Origin: origin, Container: repo.String(), Key: id.RemoteID}, nil
}

func compatibilitySlot(ctx context.Context, db *store.DB, kind string, identity ticket.Identity) (int64, error) {
	if identity.Provider == ticket.GitHub {
		id, err := strconv.ParseInt(identity.RemoteID, 10, 64)
		if err != nil || id <= 0 {
			return 0, store.ErrTicketIdentityConflict
		}
		return id, nil
	}
	return db.LocalTicketSlot(ctx, kind, identity)
}

func compatibilityActor(ctx context.Context, db *store.DB, actor ticket.Actor) (github.User, error) {
	if actor.Identity.Provider == ticket.GitHub {
		return planLegacyActor(actor)
	}
	id, err := compatibilitySlot(ctx, db, "actor", actor.Identity)
	return github.User{ID: id, Login: actor.Handle, IsBot: actor.IsBot}, err
}

func compatibilityIssue(ctx context.Context, db *store.DB, snapshot ticket.Snapshot) (github.Issue, error) {
	if snapshot.Ref.Identity.Provider == ticket.GitHub {
		return planLegacyIssue(snapshot)
	}
	if err := snapshot.Ref.Validate(); err != nil {
		return github.Issue{}, err
	}
	id, err := compatibilitySlot(ctx, db, "issue", snapshot.Ref.Identity)
	if err != nil {
		return github.Issue{}, err
	}
	author, err := compatibilityActor(ctx, db, snapshot.Author)
	if err != nil {
		return github.Issue{}, err
	}
	out := github.Issue{ID: id, Number: int(id), Title: snapshot.Title, Body: snapshot.Body, State: snapshot.State, StateReason: snapshot.StateReason, HTMLURL: snapshot.BrowserURL, Author: author, Labels: snapshot.Labels, CreatedAt: snapshot.CreatedAt, UpdatedAt: snapshot.UpdatedAt, QualifiedSnapshot: &snapshot}
	for _, actor := range snapshot.Assignees {
		if actor.Identity.Provider != snapshot.Ref.Identity.Provider || actor.Identity.Origin != snapshot.Ref.Identity.Origin {
			return github.Issue{}, store.ErrTicketIdentityConflict
		}
		u, err := compatibilityActor(ctx, db, actor)
		if err != nil {
			return github.Issue{}, err
		}
		out.Assignees = append(out.Assignees, u)
	}
	return out, nil
}

func compatibilityReply(ctx context.Context, db *store.DB, reply ticket.Reply, ref ticket.Ref) (github.Comment, error) {
	if reply.Identity.Provider == ticket.GitHub {
		return legacyPublicationReply(reply, ref)
	}
	if reply.Ticket != ref || reply.Identity.Provider != ref.Identity.Provider || reply.Identity.Origin != ref.Identity.Origin || reply.Author.Identity.Provider != ref.Identity.Provider || reply.Author.Identity.Origin != ref.Identity.Origin {
		return github.Comment{}, store.ErrTicketIdentityConflict
	}
	id, err := compatibilitySlot(ctx, db, "comment", reply.Identity)
	if err != nil {
		return github.Comment{}, err
	}
	author, err := compatibilityActor(ctx, db, reply.Author)
	if err != nil {
		return github.Comment{}, err
	}
	var replyTo int64
	if reply.ReplyTo.RemoteID != "" {
		if reply.ReplyTo.Provider != reply.Identity.Provider || reply.ReplyTo.Origin != reply.Identity.Origin {
			return github.Comment{}, store.ErrTicketIdentityConflict
		}
		replyTo, err = compatibilitySlot(ctx, db, "comment", reply.ReplyTo)
		if err != nil {
			return github.Comment{}, err
		}
	}
	return github.Comment{ReplyToCommentID: replyTo, ID: id, Author: author, Body: reply.Body, HTMLURL: reply.BrowserURL, CreatedAt: reply.CreatedAt, UpdatedAt: reply.UpdatedAt, QualifiedReply: &reply}, nil
}

func compatibilityReplyCursor(_ context.Context, _ *store.DB, ref ticket.Ref, after int64) (ticket.Cursor, error) {
	if ref.Identity.Provider == ticket.GitHub {
		return github.ReplyCursor(ref, after)
	}
	if err := ref.Validate(); err != nil {
		return ticket.Cursor{}, err
	}
	return ticket.Cursor{Ticket: ref.Identity, Kind: "linear-comments/v1"}, nil
}

func completeCompatibilityReplies(ctx context.Context, adapter interface {
	RepliesAfter(context.Context, ticket.Ref, ticket.Cursor) (ticket.ReplyPage, error)
}, ref ticket.Ref, cursor ticket.Cursor) (ticket.ReplyPage, error) {
	if ref.Identity.Provider == ticket.GitHub {
		return adapter.RepliesAfter(ctx, ref, cursor)
	}
	var out ticket.ReplyPage
	seen := make(map[string]bool)
	for page := 0; page < 100; page++ {
		observed, err := adapter.RepliesAfter(ctx, ref, cursor)
		if err != nil {
			return ticket.ReplyPage{}, err
		}
		out.Replies = append(out.Replies, observed.Replies...)
		if observed.Complete {
			out.Complete = true
			return out, nil
		}
		if observed.Next.Ticket != ref.Identity || observed.Next.Kind != "linear-comments/v1" || observed.Next.Value == "" || seen[observed.Next.Value] {
			return ticket.ReplyPage{}, store.ErrTicketIdentityConflict
		}
		seen[observed.Next.Value] = true
		cursor = observed.Next
	}
	return ticket.ReplyPage{}, errors.New("ticket reply recovery exceeds bounded traversal")
}

func commentAuthorMatches(comment github.Comment, identity ticket.Identity) bool {
	if comment.QualifiedReply != nil {
		return comment.QualifiedReply.Author.Identity.Equal(identity)
	}
	return identity.Provider == ticket.GitHub && strconv.FormatInt(comment.Author.ID, 10) == identity.RemoteID
}
