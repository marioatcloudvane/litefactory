package turn

import (
	"context"
	"errors"
	"io/fs"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

// BugRepairSourceObservation is supplied by factory source and scope observers.
// Known scope is required in addition to containment; a sandbox cannot prove it.
type BugRepairSourceObservation struct {
	Commit, Tree         string
	ChangedPaths         []string
	ScopeEvidence        string
	Scope                string // within, outside, indeterminate
	Clean, SaveConfirmed bool
	SaveOperation        string
}

// BugRepairHandoff separates source-save recovery from substantive fixer work.
// Observe must inspect actual source through internal/git and retain the scope
// decision. Save recovery callers re-enter here, never launch the fixer again.
type BugRepairHandoff struct {
	Observe func(context.Context, store.BugRepair) (BugRepairSourceObservation, error)
}

// Complete confirms saved source and retained scope evidence before handing repair to testing.
func (h BugRepairHandoff) Complete(ctx context.Context, db *store.DB, in store.BugRepairCompletion) (store.BugRepairCompletion, error) {
	if db == nil || h.Observe == nil {
		return store.BugRepairCompletion{}, errors.New("bug repair handoff requires source and semantic-scope observations")
	}
	repair, err := db.GetBugRepair(ctx, in.SessionID, in.Request)
	if err != nil {
		return store.BugRepairCompletion{}, err
	}
	observed, err := h.Observe(ctx, repair)
	if err != nil {
		return store.BugRepairCompletion{}, err
	}
	if !observed.Clean || !observed.SaveConfirmed || observed.SaveOperation == "" || observed.Commit == "" || observed.Tree == "" {
		return store.BugRepairCompletion{}, errors.New("bug repair source save remains pending")
	}
	// The analysis's file list is guidance, not a gate: files outside it are
	// retained as scope evidence and shown in the delivery review, where the
	// owner decides. Refusing them made every missed test file cost a repair
	// attempt (#384, #409).
	if strings.TrimSpace(observed.ScopeEvidence) == "" {
		return store.BugRepairCompletion{}, errors.New("bug repair scope evidence is missing")
	}
	for _, path := range observed.ChangedPaths {
		if !fs.ValidPath(path) || path == "." || strings.ContainsAny(path, "\\\x00") {
			return store.BugRepairCompletion{}, errors.New("bug repair changed path escapes project")
		}
	}
	if repair.State == "completed" {
		return db.CompletedBugRepair(ctx, in.SessionID, in.Request, observed.Commit, observed.Tree)
	}
	// Any running self-check is stopped and marked cancelled before this step
	// observes and saves the source, so no self-check command is still writing
	// to the worktree while that happens (AC-007). The process group itself was
	// already stopped when the fixer's own MCP launch closed
	// (BugSelfCheckService.Stop, called from preparedMCP.closeTransports); this
	// is the durable safety net for any run that call could not reach, and it
	// never fails just because nothing was pending.
	if _, err := db.StopBugSelfChecks(ctx, in.SessionID, in.Request); err != nil {
		return store.BugRepairCompletion{}, err
	}
	in.SourceCommit, in.SourceTree, in.SaveOperation = observed.Commit, observed.Tree, observed.SaveOperation
	state, _, _, err := db.CurrentBugAnalysis(ctx, in.SessionID)
	if err != nil {
		return store.BugRepairCompletion{}, err
	}
	if err = db.CompleteBugRepair(ctx, in, state.Revision); err != nil {
		return store.BugRepairCompletion{}, err
	}
	return db.CompletedBugRepair(ctx, in.SessionID, in.Request, observed.Commit, observed.Tree)
}
