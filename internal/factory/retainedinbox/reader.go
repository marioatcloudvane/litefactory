package retainedinbox

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// Reader owns retained routing verification before and after provider reads.
type Reader struct {
	db          *store.DB
	connections *projectconnection.Factory
}

// New is trusted wiring; consumers receive only ReadOwnerInboxTicket.
func New(db *store.DB, connections *projectconnection.Factory) *Reader {
	return &Reader{db: db, connections: connections}
}

// ReadOwnerInboxTicket returns protected content only while its retained source remains current.
func (r Reader) ReadOwnerInboxTicket(ctx context.Context, decision contracts.InboxDecision) (contracts.OwnerInboxTicketDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	d := r
	version, err := d.db.CurrentConnection(ctx, decision.RepoID, store.TicketConnection)
	if err != nil {
		return contracts.OwnerInboxTicketDetail{}, err
	}
	if !version.Enabled || version.ID != decision.ConnectionID {
		return contracts.OwnerInboxTicketDetail{Unavailable: "Source access changed; refresh the inbox."}, nil
	}
	if decision.Ref.Validate() != nil || decision.Ref.Identity.Provider != version.Provider || decision.Ref.Identity.Origin != version.Origin || decision.Ref.Locator.Container != version.Destination {
		return contracts.OwnerInboxTicketDetail{Unavailable: "The ticket identity cannot be verified; refresh the inbox."}, nil
	}
	if d.connections == nil {
		return contracts.OwnerInboxTicketDetail{Unavailable: "The configured ticket reader is unavailable."}, nil
	}
	clients, err := d.connections.Clients(ctx, version, 0)
	if err != nil {
		return readFailure(err), nil
	}
	container, err := clients.Tickets.ContainerIdentity(ctx, version.Destination)
	if err != nil {
		return readFailure(err), nil
	}
	if container.Provider != version.Provider || container.Origin != version.Origin || version.RemoteID == "" || container.RemoteID != version.RemoteID {
		return contracts.OwnerInboxTicketDetail{Unavailable: "The source identity cannot be verified; refresh the inbox."}, nil
	}
	snapshot, err := clients.Tickets.Read(ctx, decision.Ref)
	if err != nil {
		return readFailure(err), nil
	}
	if !snapshot.Ref.Equal(decision.Ref) {
		return contracts.OwnerInboxTicketDetail{Unavailable: "The ticket identity changed; refresh the inbox."}, nil
	}
	// Recheck current configuration after provider I/O before exposing its content.
	current, err := d.db.CurrentConnection(ctx, decision.RepoID, store.TicketConnection)
	if err != nil {
		return contracts.OwnerInboxTicketDetail{}, err
	}
	if !current.Enabled || current.ID != version.ID {
		return contracts.OwnerInboxTicketDetail{Unavailable: "Source access changed; refresh the inbox."}, nil
	}
	// The adapter redactor knows source credentials as well as the chat token.
	// Protect each provider field before it crosses into saved chat display data.
	detail := contracts.OwnerInboxTicketDetail{}
	for _, field := range []struct {
		value  string
		target *string
	}{{snapshot.Title, &detail.Title}, {snapshot.BrowserURL, &detail.URL}, {snapshot.Body, &detail.Body}} {
		body, protectErr := clients.Redactor.Apply(ctx, field.value, max(4096, len(field.value)+redact.Margin))
		if protectErr != nil {
			return contracts.OwnerInboxTicketDetail{}, protectErr
		}
		*field.target = body.String()
	}
	return detail, nil
}

func readFailure(err error) contracts.OwnerInboxTicketDetail {
	message := "Ticket details are unavailable; the recorded decision remains in the inbox."
	var quota *ticket.RateLimitError
	var githubQuota *github.RateLimitError
	var failure *ticket.Failure
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		message = "Ticket detail refresh was deferred or cancelled; try again later."
	case errors.As(err, &quota), errors.As(err, &githubQuota), errors.Is(err, github.ErrRateLimited), errors.Is(err, ticket.ErrDiscoveryDeferred):
		message = "Ticket detail refresh is deferred by the provider budget; try again after the provider retry window."
	case errors.Is(err, github.ErrUnauthorized), errors.Is(err, github.ErrForbidden):
		message = "The provider refused access to this ticket."
	case errors.As(err, &failure):
		if failure.Kind == ticket.Refused {
			message = "The provider refused access to this ticket."
		}
	}
	return contracts.OwnerInboxTicketDetail{Unavailable: message}
}
