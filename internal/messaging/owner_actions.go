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
	return owneractions.Domain{Actions: b.actionDomain(), Standards: b.standardsDomain(), Diagnosis: b.diagnosisDomain(), Catalog: b.ActionCatalog}
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

// ActionCatalog derives discovery from enabled command definitions and fixed policy.
func (b OwnerActionDomain) ActionCatalog() []owneraction.ActionDefinition {
	var out []owneraction.ActionDefinition
	for _, def := range b.Service.NaturalCommands() {
		if def.Effect != ChangeEffect {
			continue
		}
		actions := []string{""}
		switch def.Name {
		case "standards-author":
			actions = []string{"start", "answer", "revise", "edit", "continue", "validate", "complete", "rebase", "publish"}
		case "standards-proposal":
			actions = []string{"edit", "approve", "publish"}
		case "standards-exception":
			actions = []string{"propose", "approve", "revoke"}
		case "standards-analysis":
			actions = []string{"start"}
		}
		for _, action := range actions {
			tier, scope, err := owneraction.Classify(owneraction.Invocation{Command: def.Name, Arguments: map[string]string{"action": action}})
			if err != nil {
				continue
			}
			entry := owneraction.ActionDefinition{Command: def.Name, Subaction: action, Tier: tier, Scope: scope}
			for _, arg := range def.Arguments {
				entry.Arguments = append(entry.Arguments, owneraction.ArgumentDefinition{Name: arg.Name, Type: string(arg.Type), Required: arg.Required, MaxBytes: argumentLimit(def.Name, arg.Name)})
			}
			out = append(out, entry)
		}
	}
	return out
}
