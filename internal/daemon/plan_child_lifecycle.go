package daemon

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

const planChildContentHold = "Child content cannot be reconciled; restore valid story content and provenance."

// observePlanChild separates story-local discovery conflicts from shared service
// failures. Only a verified binding may adopt closure; failed reads retain facts.
func (d *Daemon) observePlanChild(ctx context.Context, t *task, draft store.PlanStoryDraft, relation storyRelationForge) (github.Issue, bool, error) {
	child, err := d.discoveryIssue(ctx, t.forgeRepo, draft.IssueNumber)
	hold := ""
	if err != nil {
		if !errors.Is(err, github.ErrNotFound) {
			return child, false, err
		}
		hold = "Child unavailable; restore access to the same bound issue."
	} else if child.ID != draft.IssueID || child.Number != draft.IssueNumber {
		hold = "Child identity mismatch; verify the original bound issue."
	}
	if hold == "" {
		parent, parentErr := discoveryCall(ctx, d, discoveryKey(t.forgeRepo, "parent", draft.IssueNumber), 1, func(ctx context.Context) (github.Issue, error) {
			return relation.GetParentIssue(ctx, t.forgeRepo, draft.IssueNumber)
		})
		if parentErr != nil {
			if !errors.Is(parentErr, github.ErrNotFound) {
				return child, false, parentErr
			}
			hold = "Child parent unavailable; verify the original parent relationship."
		} else if parent.ID != t.issue.ID || parent.Number != t.issue.Number {
			hold = "Child parent changed; resolve membership explicitly. No changes will be made through this parent."
		}
	}
	observed := planbrief.PlanLifecycle{}
	if hold == "" {
		if child.State != "open" && child.State != "closed" {
			hold = "Child lifecycle unverifiable; verify the bound issue state."
		} else {
			observed = planbrief.PlanLifecycle{State: child.State, Reason: child.StateReason, Revision: turn.PlanIssueRevision(child.Title, child.Body)}
		}
	}
	if hold == "" {
		current, _, loadErr := d.db.GetPlanWorkflowState(ctx, t.ref.String())
		if loadErr != nil {
			return child, false, loadErr
		}
		for _, st := range current.Stories {
			if st.Key == draft.Key && st.LifecycleHold == planChildContentHold {
				hold = planChildContentHold
				break
			}
		}
	}
	if err = d.db.ObservePlanStoryLifecycle(ctx, t.ref.String(), draft.Key, observed, hold); err != nil {
		return child, false, err
	}
	if (hold != "" && hold != planChildContentHold) || child.State != github.StateOpen || draft.Retired {
		return child, false, nil
	}
	feature, err := d.db.GetManagedFeature(ctx, t.ref.String())
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return child, false, err
	}
	// Observation never reopens a frozen generation. Its child input continues
	// through the existing managed-scope refusal path rather than shaping.
	if err == nil && feature.State != "planning" {
		return child, false, nil
	}
	return child, true, nil
}
