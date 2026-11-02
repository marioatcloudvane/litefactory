package localstandards

import (
	"context"
	"fmt"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// Runs owns retained standards inspection and task/run selection.
type Runs struct{ db *store.DB }

// NewRuns binds retained standards inspection to trusted storage.
func NewRuns(db *store.DB) *Runs { return &Runs{db: db} }

// TaskRun selects the existing newest or explicitly numbered task run.
func (s *Runs) TaskRun(ctx context.Context, r contracts.StandardsTaskRun) (int64, error) {
	session, err := s.db.FindSessionByIssue(ctx, r.Issue, r.Owner, r.Repository)
	if err != nil {
		return 0, &contracts.StandardsTaskLookupError{Err: err}
	}
	runs, err := s.db.ListRuns(ctx, session.ID)
	if err != nil {
		return 0, err
	}
	if len(runs) == 0 {
		return 0, fmt.Errorf("issue %d has no runs yet", r.Issue)
	}
	if r.Ordinal == 0 {
		return runs[len(runs)-1].ID, nil
	}
	for _, run := range runs {
		if run.Ordinal == r.Ordinal {
			return run.ID, nil
		}
	}
	return 0, fmt.Errorf("issue %d has no run %d: it has runs %d to %d", r.Issue, r.Ordinal, runs[0].Ordinal, runs[len(runs)-1].Ordinal)
}

// Inspect reads the retained evidence for the requested identity.
func (s *Runs) Inspect(ctx context.Context, runID int64) (RunInspection, error) {
	root, err := s.db.ResolveStandardsRoot(ctx, runID)
	if err != nil {
		return RunInspection{}, err
	}
	view, err := (&standards.Runtime{Store: s.db, RootRunID: fmt.Sprint(root)}).Inspect(ctx)
	return InspectRuntime(view), err
}
