package ticketbridge

import (
	"context"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// Ingress is the retained workflow compatibility input. QualifyReply translates
// an already observed legacy comment; immutable ticket identity remains the
// authority. Remove this input with the legacy workflow comment serializer.
type Ingress interface {
	Resolve(context.Context, ticket.Locator) (ticket.Snapshot, error)
	QualifyReply(ticket.Ref, github.Comment) (ticket.Reply, error)
}
