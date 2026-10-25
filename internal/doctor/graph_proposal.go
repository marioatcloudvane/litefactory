package doctor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

// prepareGraphProposal captures a read-only exact manifest before retaining and
// presenting it. The resulting bytes, not the agent's status shorthand, are what
// the owner approves. No workflow row changes until that exact proposal is applied.
func (s *ResultService) prepareGraphProposal(ctx context.Context, d *Diagnosis) error {
	if d.RollbackRepairID != "" {
		if s.repairID != "" || d.WithdrawProposal || len(d.Proposal) != 0 || !boundedText(d.RollbackRepairID) || !boundedText(d.RollbackReason) {
			return errors.New("graph rollback requires one retained repair and reason, without another proposal")
		}
		if err := s.clean(ctx, &d.RollbackReason); err != nil {
			return err
		}
		steps, err := s.db.PrepareDoctorGraphRollback(ctx, s.binding.Task, d.RollbackRepairID, d.RollbackReason)
		if err != nil {
			return err
		}
		for _, step := range steps {
			d.Proposal = append(d.Proposal, RepairStep{Number: step.Number, Target: step.Target, Action: &store.DoctorRepairAction{Kind: step.Kind, Before: step.Before, After: step.After}, Precondition: "Every exact postimage and related progress identity still matches the verified repair", Change: "Reverse all dependent transitions together from the retained backup and journal", Reason: d.RollbackReason, Reversibility: "The separately approved inverse is atomic; no partial rollback", Rollback: "No reversal is allowed after related work advances"})
		}
		// The expanded immutable proposal retains the reversal identity in its exact
		// manifests. Keeping the request fields would ask for expansion on adoption.
		d.RollbackRepairID, d.RollbackReason = "", ""
	} else if d.RollbackReason != "" {
		return errors.New("graph rollback reason has no retained repair")
	}
	graph := false
	for i := range d.Proposal {
		p := &d.Proposal[i]
		if p.Action == nil || !store.DoctorGraphRepairKind(p.Action.Kind) {
			continue
		}
		graph = true
		if strings.HasPrefix(p.Action.Before, "{") {
			continue
		}
		observed := false
		for _, record := range s.evidence.Records {
			if EvidenceHandle(record) == p.Target && record.State == p.Action.Before {
				observed = true
				break
			}
		}
		if !observed {
			return errors.New("doctor proposal has unresolved target or unobserved graph row precondition")
		}
		// Validate the shorthand destination before factory capture; an agent cannot
		// request another transition and have it silently converted to cancellation.
		destination := "cancelled"
		if p.Action.Kind == "graph_fail" {
			destination = "failed"
		}
		if p.Action.After != destination {
			return errors.New("unsupported graph repair destination")
		}
		if err := s.clean(ctx, &p.Reason); err != nil {
			return err
		}
		captured, err := s.db.PrepareDoctorGraphAction(ctx, s.binding.Task, p.Action.Kind, p.Target, p.Reason)
		if err != nil {
			return fmt.Errorf("capture exact graph proposal: %w", err)
		}
		if store.DoctorGraphRepairBeforeStatus(captured.Kind, captured.Before) != p.Action.Before {
			return errors.New("graph row changed while capturing proposal")
		}
		p.Action = &captured
	}
	if graph {
		for _, p := range d.Proposal {
			if p.Action == nil || !store.DoctorGraphRepairKind(p.Action.Kind) {
				return errors.New("graph repair cannot mix unrelated effect kinds")
			}
			// Exact approval cannot bind raw row bytes while presenting redacted
			// replacements. Refuse instead of silently changing the manifest.
			for _, exact := range []string{p.Action.Before, p.Action.After} {
				visible, err := s.redactor.Apply(ctx, exact, 65536)
				if err != nil {
					return err
				}
				if visible.Truncated() || visible.String() != exact {
					return errors.New("graph repair unavailable: exact row manifest contains protected values and cannot be displayed unchanged; inspect the retained stop with Doctor")
				}
			}
		}
	}
	return nil
}
