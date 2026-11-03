package messaging

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/owneractions"
	"github.com/cloudvane/litefactory/internal/owneraction"
)

type ownerActionPayload struct {
	Natural       naturalPayload    `json:"natural"`
	Domain        owneraction.Facts `json:"domain"`
	DomainPayload string            `json:"domain_payload,omitempty"`
}

// OwnerActionDomain binds trusted host dependencies to factory-owned decisions.
type OwnerActionDomain struct{ Service *Service }

// FactoryOwnerActionDomain composes factory-owned action domains with the
// transport's verified command catalog and existing effect hosts.
func (s *Service) FactoryOwnerActionDomain() owneractions.Domain {
	b := OwnerActionDomain{Service: s}
	return owneractions.Domain{Actions: b.actionDomain(), Standards: b.standardsDomain(), Diagnosis: b.diagnosisDomain(), Availability: s.actionAvailability()}
}

// Resolve delegates target freezing to the factory-owned domain.
func (b OwnerActionDomain) Resolve(ctx context.Context, p owneraction.Principal, in owneraction.Invocation, at time.Time) (owneraction.Facts, error) {
	facts, err := b.Service.FactoryOwnerActionDomain().Resolve(ctx, p, in, at)
	if errors.Is(err, owneractions.ErrInvalidSelection) {
		err = ErrInvalidInvocation
	}
	return facts, err
}

// Recheck delegates current target and permission checks to the factory.
func (b OwnerActionDomain) Recheck(ctx context.Context, p owneraction.Principal, f owneraction.Facts, at time.Time) error {
	return b.Service.FactoryOwnerActionDomain().Recheck(ctx, p, f, at)
}

// Execute delegates the frozen effect to the factory-owned domain.
func (b OwnerActionDomain) Execute(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, error) {
	return b.Service.FactoryOwnerActionDomain().Execute(ctx, p, f, key, at)
}

// Recover delegates uncertain-effect reconciliation to the factory.
func (b OwnerActionDomain) Recover(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, bool, error) {
	return b.Service.FactoryOwnerActionDomain().Recover(ctx, p, f, key, at)
}

// ActionCatalog derives discovery from factory-owned schemas and installed commands.
func (b OwnerActionDomain) ActionCatalog() []owneraction.ActionDefinition {
	return b.Service.actionAvailability().ActionCatalog()
}
