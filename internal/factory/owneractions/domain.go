package owneractions

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
)

// Domain routes the existing reviewed operations to their factory owners.
// Composition supplies effect hosts and command availability; no transport
// participates in selecting or recovering a frozen operation.
type Domain struct {
	Actions, Standards, Diagnosis owneraction.Domain
	Availability                  Availability
}

func (d Domain) operation(command string) owneraction.Domain {
	if StandardsCommand(command) {
		return d.Standards
	}
	if command == "diagnose" {
		return d.Diagnosis
	}
	return d.Actions
}

// Resolve freezes the target through the selected factory domain.
func (d Domain) Resolve(ctx context.Context, p owneraction.Principal, in owneraction.Invocation, at time.Time) (owneraction.Facts, error) {
	return d.operation(in.Command).Resolve(ctx, p, in, at)
}

// Recheck preserves the selected domain's current authority and target fences.
func (d Domain) Recheck(ctx context.Context, p owneraction.Principal, facts owneraction.Facts, at time.Time) error {
	return d.operation(facts.Invocation.Command).Recheck(ctx, p, facts, at)
}

// Execute applies only the previously resolved operation.
func (d Domain) Execute(ctx context.Context, p owneraction.Principal, facts owneraction.Facts, key string, at time.Time) (owneraction.Result, error) {
	return d.operation(facts.Invocation.Command).Execute(ctx, p, facts, key, at)
}

// Recover queries the original operation rather than admitting another effect.
func (d Domain) Recover(ctx context.Context, p owneraction.Principal, facts owneraction.Facts, key string, at time.Time) (owneraction.Result, bool, error) {
	return d.operation(facts.Invocation.Command).Recover(ctx, p, facts, key, at)
}

// ActionCatalog returns the enabled command definitions supplied at composition.
func (d Domain) ActionCatalog() []owneraction.ActionDefinition {
	return d.Availability.ActionCatalog()
}
