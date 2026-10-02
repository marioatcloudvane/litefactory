package turn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// RetainAgentResultAggregate compares complete captured bytes with the authorized
// worktree before retaining factory-bound aggregate facts. It runs no checks and
// never contributes a story event. Validate cannot call this capture boundary.
func RetainAgentResultAggregate(ctx context.Context, db *store.DB, b store.ResultBinding, worktree *git.Worktree, reference string, selected []store.ResultStoryRevision, capture agent.AggregateCapture) error {
	a, err := prepareAgentResultAggregate(ctx, db, b, worktree, reference, selected, capture)
	if err != nil {
		return err
	}
	return db.RetainResultAggregate(ctx, b, a)
}

func prepareAgentResultAggregate(ctx context.Context, db *store.DB, b store.ResultBinding, worktree *git.Worktree, reference string, selected []store.ResultStoryRevision, capture agent.AggregateCapture) (store.ResultAggregate, error) {
	if db == nil || b.Mode != "implement" {
		return store.ResultAggregate{}, fmt.Errorf("aggregate capture requires an authorized Implement binding")
	}
	if err := db.CheckResultWriter(ctx, b); err != nil {
		return store.ResultAggregate{}, err
	}
	files, err := capture.SnapshotFiles()
	if err != nil {
		return store.ResultAggregate{}, err
	}
	if err := verifyResultAggregateWorktree(ctx, worktree, files); err != nil {
		return store.ResultAggregate{}, err
	}
	a := store.ResultAggregate{ResultID: b.ResultID, RunID: b.RunID, Reference: reference, Attempt: b.Attempt, Parent: b.Task, ContextHash: b.ContextHash, Digest: capture.ImplementationDigest, Selected: slices.Clone(selected)}
	for _, f := range files {
		a.Files = append(a.Files, store.SubmissionSnapshotFile{Path: f.Path, Content: f.Content, SHA256: f.SHA256, Deleted: f.Deleted})
	}
	for _, check := range capture.Checks {
		a.Checks = append(a.Checks, store.ResultAggregateCheck{Reference: check.ID, ExecutionID: check.ExecutionID, Kind: check.Scope, Name: check.Name, Digest: check.ImplementationDigest, Evidence: check.Evidence, RunID: b.RunID, Attempt: b.Attempt, Executed: check.Executed, Passed: check.Passed})
	}
	if err := db.CheckResultAggregate(ctx, b, a); err != nil {
		return store.ResultAggregate{}, err
	}
	return a, nil
}

// isResultSessionPath reports whether a repository-relative path lies under the
// top-level aiteam session directory. Only that exact root is exempt: nested
// ".aiteam" directories and look-alike names such as ".aiteamx" are not.
func isResultSessionPath(p string) bool {
	p = filepath.ToSlash(p)
	return p == naming.AITeamDirName || strings.HasPrefix(p, naming.AITeamDirName+"/")
}

// verifyResultAggregateWorktree compares the final worktree with a snapshot.
// Session bookkeeping under the top-level .aiteam/ directory is left out of the
// comparison (#431): it cannot change what was checked, and agents keep it up
// to date after submitting. Every other path keeps full coverage, byte
// identity, symlink, unmerged and status-stability checks.
func verifyResultAggregateWorktree(ctx context.Context, w *git.Worktree, files []agent.StorySnapshotFile) error {
	if w == nil || w.Path() == "" {
		return fmt.Errorf("aggregate comparison requires an authorized git worktree")
	}
	status, err := w.Status(ctx)
	if err != nil {
		return err
	}
	changes := resultNonSessionChanges(status)
	paths := map[string]bool{}
	compared := make([]agent.StorySnapshotFile, 0, len(files))
	for _, f := range files {
		paths[f.Path] = true
		if !isResultSessionPath(f.Path) {
			compared = append(compared, f)
		}
	}
	for _, change := range changes {
		if change.Kind == git.ChangeUnmerged {
			return fmt.Errorf("aggregate snapshot cannot cover unmerged worktree path: %s", change.Path)
		}
		if !isResultSessionPath(change.Path) && !paths[change.Path] {
			return fmt.Errorf("aggregate snapshot must cover every final changed worktree path; not captured: %s", change.Path)
		}
		if change.Kind == git.ChangeRenamed && !isResultSessionPath(change.From) && !paths[change.From] {
			return fmt.Errorf("aggregate snapshot must cover every final changed worktree path; rename source not captured: %s", change.From)
		}
	}
	root, err := os.OpenRoot(w.Path())
	if err != nil {
		return err
	}
	if err := errors.Join(verifyStorySnapshot(root, agent.StoryEvidence{Files: compared}), root.Close()); err != nil {
		return err
	}
	after, err := w.Status(ctx)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(changes, resultNonSessionChanges(after)) {
		return fmt.Errorf("worktree changed during aggregate comparison")
	}
	return nil
}

// resultNonSessionChanges drops status entries that touch only the session
// directory. A rename or an unmerged entry involving any non-session path is
// kept so it is still checked.
func resultNonSessionChanges(changes []git.FileChange) []git.FileChange {
	kept := make([]git.FileChange, 0, len(changes))
	for _, change := range changes {
		session := isResultSessionPath(change.Path) && (change.From == "" || isResultSessionPath(change.From))
		if session && change.Kind != git.ChangeUnmerged {
			continue
		}
		kept = append(kept, change)
	}
	return kept
}
func aggregateDiagnostics() ar.Diagnostics {
	return ar.Diagnostics{Valid: true, Complete: true, Diagnostics: []ar.Diagnostic{}, UncheckedPaths: []string{}}
}
func aggregateFault(d *ar.Diagnostics, path, code, message string) {
	d.Valid = false
	d.Diagnostics = append(d.Diagnostics, ar.Diagnostic{Path: path, Code: code, Expected: "exact retained aggregate/engineering facts", Actual: "missing or mismatched evidence", Message: message})
}

// ResultAggregateValidator resolves immutable aggregate facts and compares final
// bytes on every call. Callers can install it as Context.AggregateValidator;
// RequireAggregate remains a separate factory policy. No execution is performed.
func ResultAggregateValidator(ctx context.Context, db *store.DB, b store.ResultBinding, worktree *git.Worktree, selected []store.ResultStoryRevision) func(agent.EngineeringContribution) ar.Diagnostics {
	selected = slices.Clone(selected)
	return func(ref agent.EngineeringContribution) ar.Diagnostics {
		d := aggregateDiagnostics()
		if db == nil {
			aggregateFault(&d, "$", "missing_evidence", "Supply the factory evidence store.")
			return d
		}
		expectedHash, err := store.ResultContextHash(store.ResultAttempt{WorkContext: b.WorkContext, SchemaVersion: b.SchemaVersion, WorkflowVersion: b.WorkflowVersion, SubmissionVersion: b.SubmissionVersion, FrozenStories: selected})
		if err != nil || expectedHash != b.ContextHash || len(selected) == 0 {
			aggregateFault(&d, "$", "scope", "Use the exact selected frozen key/revision set and context.")
			return d
		}
		a, err := db.GetResultAggregate(ctx, b, ref.SnapshotID)
		if err != nil {
			aggregateFault(&d, "$.snapshot_id", "missing_evidence", "Reference a retained aggregate association authorized for this result.")
			return d
		}
		actualSelected := slices.Clone(a.Selected)
		expectedSelected := slices.Clone(selected)
		less := func(a, b store.ResultStoryRevision) int { return strings.Compare(a.Story, b.Story) }
		slices.SortFunc(actualSelected, less)
		slices.SortFunc(expectedSelected, less)
		if a.Parent != b.Task || a.ContextHash != b.ContextHash || !reflect.DeepEqual(actualSelected, expectedSelected) {
			aggregateFault(&d, "$", "scope", "Aggregate context and exact selected revisions must match.")
		}
		ids := slices.Clone(ref.CheckIDs)
		slices.Sort(ids)
		if len(ids) != len(a.Checks) || len(ids) == 0 || len(slices.Compact(slices.Clone(ids))) != len(ids) {
			aggregateFault(&d, "$.check_ids", "coverage", "Reference every retained aggregate check exactly once.")
		}
		files := make([]agent.StorySnapshotFile, 0, len(a.Files))
		for _, f := range a.Files {
			files = append(files, agent.StorySnapshotFile{Path: f.Path, Content: f.Content, SHA256: f.SHA256, Deleted: f.Deleted})
		}
		if agent.SnapshotDigest(files) != a.Digest {
			aggregateFault(&d, "$.snapshot_id", "digest", "Use the retained manifest digest.")
		}
		for _, check := range a.Checks {
			if !slices.Contains(ids, check.Reference) || check.Kind != "aggregate" || check.RunID != a.RunID || check.Attempt != a.Attempt || check.Digest != a.Digest || check.ExecutionID == "" || !check.Executed || !check.Passed {
				aggregateFault(&d, "$.check_ids", "execution", "Every aggregate check must be executed, passed and attributed to this exact final snapshot.")
			}
		}
		if err := verifyResultAggregateWorktree(ctx, worktree, files); err != nil {
			// Keep the underlying cause, which names the path that differed, so the
			// rejection is actionable rather than generic (#431).
			d.Valid = false
			d.Diagnostics = append(d.Diagnostics, ar.Diagnostic{Path: "$.snapshot_id", Code: "worktree", Expected: "every final changed non-session file captured with identical rooted, stable, symlink-free bytes", Actual: err.Error(), Message: "Capture and check all final changed files with rooted, stable, symlink-free bytes: " + err.Error()})
		}
		return d
	}
}

// ResultEngineeringCheckpoint selects earlier immutable story proof by its
// factory source attribution, avoiding a new story contribution during recovery.
type ResultEngineeringCheckpoint struct {
	Story    string
	RunID    int64
	Sequence int
}

// ValidateAgentResultCompletion combines retained selected engineering with exact
// aggregate proof. It is a direct engineering gate, not acceptance or terminal
// save. Incomplete/blocked results need no fabricated successful checkpoints.
func ValidateAgentResultCompletion(ctx context.Context, db *store.DB, b store.ResultBinding, worktree *git.Worktree, r ar.Result, base ar.Context, checkpoints []ResultEngineeringCheckpoint) ar.Diagnostics {
	d := aggregateDiagnostics()
	if db == nil || b.Mode != "implement" || base.Mode != "implement" || base.SchemaVersion != b.SchemaVersion || base.WorkflowVersion != b.WorkflowVersion {
		aggregateFault(&d, "$", "scope", "Use the trusted Implement context and persisted versions.")
		return d
	}
	if err := db.CheckResultWriter(ctx, b); err != nil {
		aggregateFault(&d, "$", "authorization", "Use a current authorized writer for completion.")
		return d
	}
	selected := make([]store.ResultStoryRevision, 0, len(base.Stories))
	for key, s := range base.Stories {
		selected = append(selected, store.ResultStoryRevision{Story: key, Revision: s.Revision})
	}
	slices.SortFunc(selected, func(a, b store.ResultStoryRevision) int { return strings.Compare(a.Story, b.Story) })
	base.RequireAggregate = true
	base.AggregateValidator = ResultAggregateValidator(ctx, db, b, worktree, selected)
	base.EngineeringComplete = map[string]bool{}
	if r.Outcome == "completed" {
		seen := map[string]bool{}
		for _, ref := range checkpoints {
			story, ok := base.Stories[ref.Story]
			if !ok || seen[ref.Story] {
				aggregateFault(&d, "$.engineering", "scope", "Supply each selected story checkpoint exactly once.")
				continue
			}
			seen[ref.Story] = true
			retained, err := db.GetResultEngineeringCheckpoint(ctx, b, ref.Story, story.Revision, ref.RunID, ref.Sequence)
			if err != nil {
				aggregateFault(&d, "$.engineering", "missing_evidence", "Reference an earlier retained engineering checkpoint.")
				continue
			}
			event, parseErr := agent.ParseStoryEvent(retained.Payload)
			if parseErr != nil {
				aggregateFault(&d, "$.engineering", "encoding", "Retained checkpoints must contain valid immutable evidence.")
				continue
			}
			expectedKind := "engineering_complete"
			if b.WorkflowVersion == 1 {
				expectedKind = "verified"
			}
			if event.Validate() != nil || event.Parent != b.Task || event.Story != ref.Story || event.Revision != story.Revision || event.RunID != ref.RunID || event.Sequence != ref.Sequence || event.Kind != expectedKind || agent.StoryDigest(retained.Payload) != retained.Digest {
				aggregateFault(&d, "$.engineering", "attribution", "Checkpoint identity, frozen revision and workflow obligations must match.")
				continue
			}
			if b.WorkflowVersion == 1 {
				criteria := map[string]bool{}
				for _, check := range event.Evidence.Acceptance {
					criteria[check.Name] = true
				}
				covered := true
				for _, id := range story.Criteria {
					if !criteria[id] {
						covered = false
					}
				}
				if !covered {
					aggregateFault(&d, "$.engineering", "coverage", "Legacy checkpoints must retain every selected acceptance criterion.")
					continue
				}
			}
			base.EngineeringComplete[ref.Story] = true
		}
		for _, s := range selected {
			if !base.EngineeringComplete[s.Story] {
				aggregateFault(&d, "$.engineering", "coverage", "Complete every selected frozen engineering obligation before aggregate completion.")
			}
		}
	}
	validation := ar.ValidateResult(r, base)
	d.Complete = d.Complete && validation.Complete
	d.UncheckedPaths = append(d.UncheckedPaths, validation.UncheckedPaths...)
	d.Diagnostics = append(d.Diagnostics, validation.Diagnostics...)
	d.Valid = d.Complete && len(d.Diagnostics) == 0
	slices.SortFunc(d.Diagnostics, func(a, b ar.Diagnostic) int {
		if n := strings.Compare(a.Path, b.Path); n != 0 {
			return n
		}
		return strings.Compare(a.Code, b.Code)
	})
	return d
}
