package projectconnection

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// TicketClient is the project client's work-management surface, independent of code delivery.
type TicketClient interface {
	Origin() ticket.Origin
	Resolve(context.Context, ticket.Locator) (ticket.Snapshot, error)
	Read(context.Context, ticket.Ref) (ticket.Snapshot, error)
	QualifyReply(ticket.Ref, github.Comment) (ticket.Reply, error)
	IssuesPage(context.Context, string, string, int, int) (ticket.SnapshotPage, error)
	OpenContext(context.Context, string, int) (ticket.SnapshotPage, error)
	Discover(context.Context, string, time.Time) ([]ticket.Snapshot, error)
	DiscoverCommented(context.Context, string, time.Time) ([]ticket.Locator, error)
	Followups(context.Context, string) ([]ticket.Snapshot, error)
	Children(context.Context, ticket.Ref) ([]ticket.Snapshot, error)
	Parent(context.Context, ticket.Ref) (ticket.Snapshot, error)
	Create(context.Context, string, redact.Body, redact.Body) (ticket.Snapshot, error)
	Attach(context.Context, ticket.Ref, ticket.Ref) error
	UpdateContent(context.Context, ticket.Ref, string, redact.Body, redact.Body) (ticket.Snapshot, error)
	RepliesPage(context.Context, ticket.Ref, int, int) (ticket.ReplyPage, error)
	RepliesAfter(context.Context, ticket.Ref, ticket.Cursor) (ticket.ReplyPage, error)
	PublishReply(context.Context, ticket.Ref, redact.Body) (ticket.Reply, error)
	EditReply(context.Context, ticket.Reply, redact.Body) error
	React(context.Context, ticket.Reply, string) error
	SetStoryState(context.Context, ticket.Ref, ticket.Ref, bool) error
	RetireStory(context.Context, ticket.Ref, ticket.Ref) error
	Close(context.Context, ticket.Ref) error
	AddLabel(context.Context, ticket.Ref, string) error
	RemoveLabel(context.Context, ticket.Ref, string) error
	ChangesPage(context.Context, string, time.Time, ticket.DiscoveryCursor, int) (ticket.ChangePage, error)
	ReplyChangesPage(context.Context, string, time.Time, ticket.DiscoveryCursor, int) (ticket.ChangePage, error)
	ReconcilePage(context.Context, string, ticket.DiscoveryCursor, int) (ticket.ChangePage, error)
	ContainerIdentity(context.Context, string) (ticket.Identity, error)
	ListSubscriptions(context.Context, string) ([]ticket.Subscription, error)
	GetSubscription(context.Context, string, ticket.Identity) (ticket.Subscription, error)
	CreateSubscription(context.Context, string, ticket.SubscriptionSpec, secrets.Token) (ticket.Subscription, error)
	UpdateSubscription(context.Context, string, ticket.Identity, ticket.SubscriptionSpec, secrets.Token) (ticket.Subscription, error)
}

type linearTickets struct{ *linear.Client }

func (a linearTickets) Provider() ticket.Provider { return ticket.Linear }
func (a linearTickets) QualifyReply(ref ticket.Ref, c github.Comment) (ticket.Reply, error) {
	if c.QualifiedReply == nil {
		return ticket.Reply{}, errors.New("qualified Linear reply evidence is required")
	}
	r := *c.QualifiedReply
	if r.Ticket != ref || r.Identity.Provider != ref.Identity.Provider || r.Identity.Origin != ref.Identity.Origin {
		return ticket.Reply{}, errors.New("linear reply destination differs")
	}
	return r, nil
}
func legacyLinearActor(a ticket.Actor, slot int64) github.User {
	return github.User{ID: slot, Login: a.Handle, IsBot: a.IsBot, IsHuman: !a.IsBot}
}
