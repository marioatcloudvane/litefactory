package turn

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type correlatedTicketCreation interface {
	CreateWithID(context.Context, string, string, redact.Body, redact.Body) (ticket.Snapshot, error)
	CreateChildWithID(context.Context, string, string, *ticket.Ref, redact.Body, redact.Body) (ticket.Snapshot, error)
	PublishReplyWithID(context.Context, ticket.Ref, string, redact.Body) (ticket.Reply, error)
	ReadReply(context.Context, ticket.Ref, string) (ticket.Reply, error)
}

// postCommentEffect passes the owning ledger's stable intent key to providers
// that support client-generated creation IDs. Existing injected fakes and
// GitHub use their original publication contract.
func postCommentEffect(ctx context.Context, forge ticketNoticePoster, key string, repo github.Repo, number int, body redact.Body) (github.Comment, error) {
	if correlated, ok := forge.(interface {
		PostCommentEffect(context.Context, string, github.Repo, int, redact.Body) (github.Comment, error)
	}); ok {
		return correlated.PostCommentEffect(ctx, key, repo, number, body)
	}
	return forge.PostComment(ctx, repo, number, body)
}

func createTicketEffect(ctx context.Context, forge interface {
	CreateIssue(context.Context, github.Repo, redact.Body, redact.Body) (github.Issue, error)
}, key string, repo github.Repo, parent int, title, body redact.Body) (github.Issue, error) {
	if correlated, ok := forge.(interface {
		CreateIssueEffect(context.Context, string, github.Repo, int, redact.Body, redact.Body) (github.Issue, error)
	}); ok {
		return correlated.CreateIssueEffect(ctx, key, repo, parent, title, body)
	}
	return forge.CreateIssue(ctx, repo, title, body)
}

// PostCommentEffect preserves the owning ledger's intent key across routing.
func (b *TicketWorkflowBridge) PostCommentEffect(ctx context.Context, key string, repo github.Repo, number int, body redact.Body) (github.Comment, error) {
	selected, err := b.selected(ctx, repo, number, 0)
	if err != nil {
		return github.Comment{}, err
	}
	return selected.Publication.PostCommentEffect(ctx, key, repo, number, body)
}

func (b *TicketPublicationBridge) publishEffect(ctx context.Context, key string, ref ticket.Ref, body redact.Body) (ticket.Reply, error) {
	if b.provider == ticket.GitHub {
		return b.tickets.PublishReply(ctx, ref, body)
	}
	correlated, ok := b.tickets.(correlatedTicketCreation)
	if !ok || key == "" {
		return ticket.Reply{}, errors.New("linear publication requires a durable owning effect key")
	}
	prepared, err := b.db.PrepareTicketEffect(ctx, key, "linear-markdown/v1", body.String(), true)
	if err != nil {
		return ticket.Reply{}, err
	}
	bound, err := b.db.GetTicketEffect(ctx, key)
	if err != nil {
		return ticket.Reply{}, err
	}
	if bound.Locator != ref.Locator || bound.Actor.Validate() != nil {
		return ticket.Reply{}, errors.New("retained publication destination or publisher differs")
	}
	reply, err := correlated.PublishReplyWithID(ctx, ref, prepared.CreationUUID, body)
	if err != nil {
		return ticket.Reply{}, err
	}
	if reply.Identity.RemoteID != prepared.CreationUUID || reply.Body != prepared.AuthoredContent || reply.Representation != prepared.Representation || !reply.Author.Identity.Equal(bound.Actor) {
		return ticket.Reply{}, &ticket.Failure{Kind: ticket.Uncertain, Operation: "publication correlation", Cause: errors.New("reply UUID or authored content differs")}
	}
	if err = b.db.ConfirmTicketEffectRepresentation(ctx, key, reply.ProviderBody); err != nil {
		return ticket.Reply{}, &ticket.Failure{Kind: ticket.Uncertain, Operation: "retain publication representation", Cause: err}
	}
	return reply, nil
}

// PostCommentEffect adds correlation metadata before the owning ledger sends.
func (b *TicketPublicationBridge) PostCommentEffect(ctx context.Context, key string, repo github.Repo, number int, body redact.Body) (github.Comment, error) {
	snapshot, err := b.destination(ctx, repo, number)
	if err != nil {
		return github.Comment{}, err
	}
	if _, err = b.db.BindTicketEffect(ctx, key, snapshot.Ref.Locator, b.bot); err != nil {
		// Existing publication binding includes its publisher. Preserve it.
		bound, readErr := b.db.GetTicketEffect(ctx, key)
		if readErr != nil || bound.Locator != snapshot.Ref.Locator || (b.bot != (ticket.Identity{}) && bound.Actor != b.bot) {
			return github.Comment{}, err
		}
	}
	bound, err := b.db.GetTicketEffect(ctx, key)
	if err != nil {
		return github.Comment{}, err
	}
	reply, err := b.publishEffect(ctx, key, snapshot.Ref, body)
	if err != nil {
		return github.Comment{}, err
	}
	if bound.Actor != (ticket.Identity{}) && !reply.Author.Identity.Equal(bound.Actor) {
		return github.Comment{}, &ticket.Failure{Kind: ticket.Uncertain, Operation: "publication actor", Cause: errors.New("reply publisher differs from retained effect")}
	}
	comment, err := compatibilityReply(ctx, b.db, reply, snapshot.Ref)
	if err != nil {
		return github.Comment{}, &ticket.Failure{Kind: ticket.Uncertain, Operation: "publication receipt", Cause: err}
	}
	if err = b.rememberReply(ctx, reply); err != nil {
		return github.Comment{}, &ticket.Failure{Kind: ticket.Uncertain, Operation: "retain publication receipt", Cause: err}
	}
	return comment, nil
}

// RecoverCommentEffect reconciles the exact retained UUID after an uncertain
// reply. A missing object leaves the owning ledger owed; it never authorizes
// another dispatch. The bool distinguishes legacy GitHub recovery behavior.
func (b *TicketWorkflowBridge) RecoverCommentEffect(ctx context.Context, key string, repo github.Repo, number int, body string) (github.Comment, bool, error) {
	selected, err := b.selected(ctx, repo, number, 0)
	if err != nil {
		return github.Comment{}, true, err
	}
	publication := selected.Publication
	if publication.provider == ticket.GitHub {
		return github.Comment{}, false, nil
	}
	correlated, ok := publication.tickets.(correlatedTicketCreation)
	if !ok {
		return github.Comment{}, true, errors.New("exact Linear reply recovery is unavailable")
	}
	prepared, err := b.DB.GetTicketEffectPreparation(ctx, key)
	if err != nil {
		return github.Comment{}, true, err
	}
	bound, err := b.DB.GetTicketEffect(ctx, key)
	if err != nil {
		return github.Comment{}, true, err
	}
	snapshot, err := publication.destination(ctx, repo, number)
	if err != nil || snapshot.Ref.Locator != bound.Locator || prepared.AuthoredContent != body {
		return github.Comment{}, true, errors.Join(err, errors.New("retained reply destination or content differs"))
	}
	reply, err := correlated.ReadReply(ctx, snapshot.Ref, prepared.CreationUUID)
	if err != nil {
		return github.Comment{}, true, err
	}
	if reply.Identity.RemoteID != prepared.CreationUUID || reply.Body != prepared.AuthoredContent || reply.Representation != prepared.Representation || (bound.Actor != (ticket.Identity{}) && !reply.Author.Identity.Equal(bound.Actor)) {
		return github.Comment{}, true, errors.New("exact reply correlation did not confirm publication")
	}
	if err = b.DB.ConfirmTicketEffectRepresentation(ctx, key, reply.ProviderBody); err != nil {
		return github.Comment{}, true, err
	}
	comment, err := compatibilityReply(ctx, b.DB, reply, snapshot.Ref)
	if err == nil {
		err = publication.rememberReply(ctx, reply)
	}
	return comment, true, err
}

// CreateIssueEffect creates a child with exact parentage in the initial mutation
// and retains the immutable ID before later attachment or report effects.
func (b *TicketWorkflowBridge) CreateIssueEffect(ctx context.Context, key string, repo github.Repo, parent int, title, body redact.Body) (github.Issue, error) {
	selected, err := b.selected(ctx, repo, parent, 0)
	if err != nil {
		return github.Issue{}, err
	}
	return selected.TicketPlanBridge.CreateIssueEffect(ctx, key, repo, parent, title, body)
}

// CreateIssueEffect extends the existing Plan publication owner with immutable
// correlation metadata; this method grants no retry rights after uncertainty.
func (b *TicketPlanBridge) CreateIssueEffect(ctx context.Context, key string, repo github.Repo, parent int, title, body redact.Body) (github.Issue, error) {
	if workflowProvider(b.Tickets) == ticket.GitHub {
		return b.CreateIssue(ctx, repo, title, body)
	}
	correlated, ok := b.Tickets.(correlatedTicketCreation)
	if !ok || key == "" {
		return github.Issue{}, errors.New("linear creation requires a durable owning effect key")
	}
	var parentRef *ticket.Ref
	locator := ticket.Locator{Provider: workflowProvider(b.Tickets), Origin: b.Tickets.Origin(), Container: repo.String(), Key: "creation:" + key}
	if parent > 0 {
		p, err := b.read(ctx, repo, parent)
		if err != nil {
			return github.Issue{}, err
		}
		parentRef, locator = &p.Ref, p.Ref.Locator
	}
	if _, err := b.DB.BindTicketEffect(ctx, key, locator, b.Bot); err != nil {
		return github.Issue{}, err
	}
	p, err := b.DB.PrepareTicketEffect(ctx, key, "linear-markdown/v1", title.String()+"\n"+body.String(), true)
	if err != nil {
		return github.Issue{}, err
	}
	snapshot, err := correlated.CreateChildWithID(ctx, repo.String(), p.CreationUUID, parentRef, title, body)
	if err != nil {
		return github.Issue{}, err
	}
	if snapshot.Ref.Identity.RemoteID != p.CreationUUID || snapshot.Title != title.String() || !linear.ContentEquivalent(body.String(), snapshot.Body) || snapshot.Representation != p.Representation || !snapshot.Author.Identity.Equal(b.Bot) {
		return github.Issue{}, &ticket.Failure{Kind: ticket.Uncertain, Operation: "creation correlation", Cause: errors.New("created issue UUID, content or publisher differs")}
	}
	if err = b.DB.ConfirmTicketEffectRepresentation(ctx, key, snapshot.Title+"\n"+snapshot.ProviderBody); err != nil {
		return github.Issue{}, &ticket.Failure{Kind: ticket.Uncertain, Operation: "retain creation representation", Cause: err}
	}
	snapshot, err = b.bind(ctx, snapshot)
	if err != nil {
		return github.Issue{}, err
	}
	return compatibilityIssue(ctx, b.DB, snapshot)
}

// RecoverIssueEffect reconciles only the exact UUID retained before dispatch.
// A missing or changed issue leaves the original creation owed without retry
// permission; the bool permits the unchanged legacy GitHub recovery path.
func (b *TicketWorkflowBridge) RecoverIssueEffect(ctx context.Context, key string, repo github.Repo, parent int, title, body string) (github.Issue, bool, error) {
	selected, err := b.selected(ctx, repo, parent, 0)
	if err != nil {
		return github.Issue{}, true, err
	}
	bridge := selected.TicketPlanBridge
	if workflowProvider(bridge.Tickets) == ticket.GitHub {
		return github.Issue{}, false, nil
	}
	prepared, err := bridge.DB.GetTicketEffectPreparation(ctx, key)
	if err != nil {
		return github.Issue{}, true, err
	}
	bound, err := bridge.DB.GetTicketEffect(ctx, key)
	if err != nil {
		return github.Issue{}, true, err
	}
	if prepared.CreationUUID == "" || prepared.AuthoredContent != title+"\n"+body || prepared.Representation != "linear-markdown/v1" || bound.Actor.Validate() != nil || !bound.Actor.Equal(bridge.Bot) {
		return github.Issue{}, true, errors.New("retained issue creation content or publisher differs")
	}
	var parentRef *ticket.Ref
	if parent > 0 {
		observedParent, readErr := bridge.read(ctx, repo, parent)
		if readErr != nil {
			return github.Issue{}, true, readErr
		}
		if observedParent.Ref.Locator != bound.Locator {
			return github.Issue{}, true, errors.New("retained issue creation parent destination differs")
		}
		parentRef = &observedParent.Ref
	} else if bound.Locator.Key != "creation:"+key {
		return github.Issue{}, true, errors.New("retained issue creation destination differs")
	}
	locator := bound.Locator
	locator.Key = prepared.CreationUUID
	if locator.Provider != workflowProvider(bridge.Tickets) || locator.Origin != bridge.Tickets.Origin() || locator.Container != repo.String() {
		return github.Issue{}, true, errors.New("retained issue creation connection differs")
	}
	snapshot, err := bridge.Tickets.Resolve(ctx, locator)
	if err != nil {
		return github.Issue{}, true, err
	}
	if snapshot.Ref.Locator != locator || snapshot.Ref.Identity.RemoteID != prepared.CreationUUID || snapshot.Title != title || !linear.ContentEquivalent(body, snapshot.Body) || snapshot.Representation != prepared.Representation || !snapshot.Author.Identity.Equal(bound.Actor) {
		return github.Issue{}, true, errors.New("exact issue creation correlation did not confirm publication")
	}
	if parentRef != nil {
		observedParent, parentErr := bridge.Tickets.Parent(ctx, snapshot.Ref)
		if parentErr != nil {
			return github.Issue{}, true, parentErr
		}
		if !observedParent.Ref.Equal(*parentRef) {
			return github.Issue{}, true, errors.New("created issue direct parent differs")
		}
	}
	if err = bridge.DB.ConfirmTicketEffectRepresentation(ctx, key, snapshot.Title+"\n"+snapshot.ProviderBody); err != nil {
		return github.Issue{}, true, err
	}
	snapshot, err = bridge.bind(ctx, snapshot)
	if err != nil {
		return github.Issue{}, true, err
	}
	issue, err := compatibilityIssue(ctx, bridge.DB, snapshot)
	return issue, true, err
}
