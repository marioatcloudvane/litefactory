package turn

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

// BugRepairScopeDecision is an explicit factory assessment of the actual diff
// against the approved remedy. Paths constrain access but cannot prove semantics.
type BugRepairScopeDecision struct {
	AnalysisDigest, DiffDigest  string
	AllowedPaths, ExcludedPaths []string
	Disposition, Evidence       string
}

// BugRepairScopeResolver optionally supplies additional factory observations.
// The default uses concrete path boundaries in the exact approved analysis.
// Missing historical bounds remain indeterminate and require renewed analysis.
type BugRepairScopeResolver func(context.Context, store.BugAnalysisRevision, git.BugRepairSnapshot) (BugRepairScopeDecision, error)

// NewBugRepairHandoff wires actual git observation and durable run-save evidence.
// Recovery rereads saved source; it performs no provider launch or new commit.
func NewBugRepairHandoff(db *store.DB, worktree *git.Worktree, resolve BugRepairScopeResolver) (BugRepairHandoff, error) {
	if db == nil || worktree == nil {
		return BugRepairHandoff{}, errors.New("bug repair handoff requires store and task worktree")
	}
	return BugRepairHandoff{Observe: func(ctx context.Context, repair store.BugRepair) (BugRepairSourceObservation, error) {
		var out BugRepairSourceObservation
		run, err := db.GetRun(ctx, repair.RunID)
		if err != nil {
			return out, err
		}
		if run.SessionID != repair.SessionID || !run.Succeeded() || run.CommitSHA == "" || !run.Published {
			return out, errors.New("bug repair source save remains pending; reconcile the original factory save")
		}
		snapshot, err := worktree.ObserveBugRepair(ctx, repair.SourceCommit, repair.SourceTree, run.CommitSHA)
		if err != nil {
			return out, err
		}
		analysis, err := db.GetBugAnalysisRevision(ctx, repair.SessionID, repair.AnalysisRevision)
		if err != nil {
			return out, err
		}
		decision := BugRepairBoundedScope(analysis, snapshot)
		if resolve != nil {
			decision, err = resolve(ctx, analysis, snapshot)
			if err != nil {
				return out, err
			}
		}
		if decision.AnalysisDigest != analysis.Digest || decision.DiffDigest != snapshot.DiffDigest {
			return out, errors.New("bug repair scope decision is bound to different changes")
		}
		if strings.TrimSpace(decision.Evidence) == "" {
			return out, errors.New("bug repair scope decision lacks evidence")
		}
		if decision.Disposition == "within" {
			if len(snapshot.Paths) == 0 || len(decision.AllowedPaths) == 0 {
				return out, errors.New("bug repair scope decision lacks explicit changed-path boundaries")
			}
			for _, path := range append(slices.Clone(decision.AllowedPaths), decision.ExcludedPaths...) {
				if !fs.ValidPath(path) || path == "." || strings.ContainsAny(path, "\\\x00") {
					return out, errors.New("bug repair scope path is invalid")
				}
			}
			for _, path := range snapshot.Paths {
				if !slices.Contains(decision.AllowedPaths, path) || slices.Contains(decision.ExcludedPaths, path) {
					decision.Disposition = "outside"
					decision.Evidence += " Actual changed path outside approved boundaries: " + path
				}
			}
		}
		err = db.RetainBugRepairScope(ctx, store.BugRepairScopeObservation{SessionID: repair.SessionID, Request: repair.Request, SourceCommit: snapshot.Commit, SourceTree: snapshot.Tree, DiffDigest: snapshot.DiffDigest, Disposition: decision.Disposition, Evidence: decision.Evidence, Paths: snapshot.Paths})
		if err != nil {
			return out, err
		}
		return BugRepairSourceObservation{Commit: snapshot.Commit, Tree: snapshot.Tree, ChangedPaths: snapshot.Paths, Scope: decision.Disposition, ScopeEvidence: decision.Evidence, Clean: true, SaveConfirmed: true, SaveOperation: fmt.Sprintf("run:%d:save:%s", run.ID, run.CommitSHA)}, nil
	}}, nil
}

// BugRepairBoundedScope observes the immutable diff against typed approved file
// boundaries. It makes no blanket semantic proof claim: the fixer's scoped duties,
// full criterion coverage and any explicit adverse observation remain required.
func BugRepairBoundedScope(a store.BugAnalysisRevision, s git.BugRepairSnapshot) BugRepairScopeDecision {
	d := BugRepairScopeDecision{AnalysisDigest: a.Digest, DiffDigest: s.DiffDigest, Disposition: "indeterminate", Evidence: "Approved analysis has no explicit file boundaries; revised analysis and renewed approval are required."}
	b := a.Analysis.Boundaries
	if b == nil {
		return d
	}
	// Boundary notices (path count, duplicates) were already recorded as facts
	// when this analysis was approved (internal/store/bug_analyses.go); only a
	// hard fault here (an escaping path) forces an indeterminate disposition.
	if _, err := b.Validate(); err != nil {
		d.Evidence = err.Error()
		return d
	}
	d.AllowedPaths = slices.Clone(b.AllowedPaths)
	d.ExcludedPaths = slices.Clone(b.ExcludedPaths)
	if len(s.Paths) == 0 || s.DiffDigest == "" {
		d.Evidence = "Actual immutable changed paths or diff evidence are missing."
		return d
	}
	d.Disposition = "within"
	d.Evidence = "Actual immutable changed files match the exact approved remedy boundaries. Semantic compliance remains the fixer duty; complete criterion mapping and regression references are independently required."
	for _, p := range s.Paths {
		if !slices.Contains(b.AllowedPaths, p) || slices.Contains(b.ExcludedPaths, p) {
			d.Disposition = "outside"
			d.Evidence = "Actual changed file outside the exact approved remedy boundaries: " + p
			break
		}
	}
	return d
}
