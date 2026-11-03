package maintenance

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

// PreviewRetirement reads a bounded advisory inventory without changing its
// ownership or the automatic sweep schedule.
func (s *Service) PreviewRetirement(ctx context.Context, opts RetirementOptions, present contracts.RetirementPresentation) error {
	return PreviewRetirementInventory(ctx, s.db, opts, present)
}

// RetirementInventory is the trusted bounded source for preview owners.
type RetirementInventory interface {
	RetirementBoundary(context.Context) (int64, error)
	ListRetirementOwners(context.Context, int64, int64, int) ([]store.RetirementOwner, error)
}

// PreviewRetirementInventory walks the bounded trusted inventory and stops on provider rate limits.
func PreviewRetirementInventory(ctx context.Context, inventory RetirementInventory, opts RetirementOptions, present contracts.RetirementPresentation) error {
	if err := present.Start(); err != nil {
		return err
	}
	boundary, err := inventory.RetirementBoundary(ctx)
	if err != nil {
		return err
	}
	var cursor int64
	for {
		owners, err := inventory.ListRetirementOwners(ctx, cursor, boundary, 100)
		if err != nil {
			return err
		}
		if len(owners) == 0 {
			return nil
		}
		for _, owner := range owners {
			if err := ctx.Err(); err != nil {
				return err
			}
			result := EvaluateRetirement(ctx, opts, owner)
			view := contracts.RetirementPreviewView{TaskRef: owner.Session.TaskRef}
			for _, decision := range result.Decisions {
				view.Decisions = append(view.Decisions, contracts.RetirementDecisionView(decision))
			}
			if err := present.Owner(view); err != nil {
				return err
			}
			// A rate limit must not cause a request for every remaining owner.
			var limited *github.RateLimitError
			if errors.As(result.Error, &limited) {
				return result.Error
			}
			cursor = owner.Session.ID
		}
	}
}
