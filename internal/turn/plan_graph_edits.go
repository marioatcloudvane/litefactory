package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// ErrPlanChildEdit identifies a story-local edit conflict, not a storage failure.
var ErrPlanChildEdit = errors.New("plan child edit requires resolution")

// ReconcilePlanChildEdit turns observed issue edits into unapproved candidates.
// The caller observes the direct parent relationship; edit text never proves
// author identity or grants approval. Presentation-only edits keep semantic gates.
func ReconcilePlanChildEdit(ctx context.Context, db *store.DB, parent string, parentNumber int, parentID int64, issue github.Issue, scope planbrief.ContentScope) error {
	feature, err := db.GetManagedFeature(ctx, parent)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if err == nil && feature.State != "planning" {
		return fmt.Errorf("%w: managed scope is frozen", ErrPlanChildEdit)
	}
	drafts, err := db.ListPlanStoryDrafts(ctx, parent)
	if err != nil {
		return err
	}
	var binding store.PlanStoryDraft
	for _, d := range drafts {
		if d.IssueID == issue.ID && d.IssueNumber == issue.Number {
			binding = d
			break
		}
	}
	if binding.Key == "" || binding.Retired || issue.State != github.StateOpen || parentID < 1 {
		return fmt.Errorf("%w: direct edit lacks active bound identity", ErrPlanChildEdit)
	}
	marker := fmt.Sprintf("<!-- lf:plan-story parent=%d key=%s -->", parentID, binding.Key)
	if !strings.HasPrefix(issue.Body, marker+"\n") {
		return fmt.Errorf("%w: changed factory provenance", ErrPlanChildEdit)
	}
	observed := PlanIssueRevision(issue.Title, issue.Body)
	if observed == binding.Revision {
		return nil
	}
	var prior planbrief.PlanDraft
	if err = json.Unmarshal([]byte(binding.DraftJSON), &prior); err != nil {
		return err
	}
	edit, err := planbrief.ReconcilePlanDraftEdit(prior, int64(parentNumber), binding.Ordinal, issue.Title, issue.Body, scope)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPlanChildEdit, err)
	}
	stage := edit.Stage
	if stage == "" {
		stage = binding.Stage
	}
	candidate := binding.Candidate
	if !edit.PresentationOnly {
		candidate = "owner-edit-" + observed
	}
	if err = db.ReconcilePlanStoryDraft(ctx, parent, binding.Key, binding.Revision, observed, candidate, stage, edit.Draft, issue.Title, issue.Body); err != nil {
		return err
	}
	if edit.PresentationOnly {
		return nil
	}
	state, rev, err := db.GetPlanWorkflowState(ctx, parent)
	if err != nil {
		return err
	}
	if err = state.PutCandidate(edit.Draft, stage, candidate); err != nil {
		return err
	}
	_, err = db.SavePlanWorkflowState(ctx, parent, rev, state)
	return err
}
