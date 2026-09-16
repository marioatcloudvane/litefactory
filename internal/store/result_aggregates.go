package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// ResultAggregateCheck retains execution/check attribution independently from
// scoped story checks. Executed/passed remain agent-reported facts, not provider
// provenance certification. They cannot be rewritten under an existing ID.
type ResultAggregateCheck struct {
	Reference, ExecutionID, Kind, Name, Digest, Evidence string
	RunID                                                int64
	Attempt                                              string
	Executed, Passed                                     bool
}

// ResultAggregate is an immutable final file/check association for the exact
// parent, application context and selected frozen revision set.
type ResultAggregate struct {
	ResultID, RunID                                               int64
	Reference, Attempt, Parent, ContextHash, SelectedHash, Digest string
	Selected                                                      []ResultStoryRevision
	Files                                                         []SubmissionSnapshotFile
	Checks                                                        []ResultAggregateCheck
}

func normalizeResultAggregate(b ResultBinding, a ResultAggregate) (ResultAggregate, error) {
	if b.Mode != "implement" || a.ResultID != b.ResultID || a.RunID != b.RunID || a.Attempt != b.Attempt || a.Parent != b.Task || a.ContextHash != b.ContextHash || strings.TrimSpace(a.Reference) == "" || len(a.Reference) > 256 || len(a.Selected) == 0 || len(a.Selected) > 256 || len(a.Files) == 0 || len(a.Files) > 256 || len(a.Checks) == 0 || len(a.Checks) > 256 {
		return a, fmt.Errorf("store: aggregate requires exact Implement binding and bounded evidence")
	}
	a.Selected = slices.Clone(a.Selected)
	slices.SortFunc(a.Selected, func(x, y ResultStoryRevision) int { return strings.Compare(x.Story, y.Story) })
	contextHash, err := ResultContextHash(ResultAttempt{WorkContext: b.WorkContext, SchemaVersion: b.SchemaVersion, WorkflowVersion: b.WorkflowVersion, SubmissionVersion: b.SubmissionVersion, FrozenStories: a.Selected})
	if err != nil {
		return a, err
	}
	if contextHash != b.ContextHash {
		return a, fmt.Errorf("store: aggregate frozen revision set differs from authorized context")
	}
	selected, err := json.Marshal(a.Selected)
	if err != nil {
		return a, err
	}
	if a.SelectedHash != "" && a.SelectedHash != submissionDigest(selected) {
		return a, fmt.Errorf("store: aggregate selected-set digest mismatch")
	}
	a.SelectedHash = submissionDigest(selected)
	a.Files = slices.Clone(a.Files)
	var manifest strings.Builder
	previous := ""
	remaining := MaxSubmissionRetainedBytes
	for i, f := range a.Files {
		if len(f.Content) > remaining || !safeSubmissionPath(f.Path) || f.Path <= previous || submissionDigest(f.Content) != f.SHA256 || (f.Deleted && len(f.Content) != 0) {
			return a, fmt.Errorf("store: invalid aggregate snapshot bytes")
		}
		remaining -= len(f.Content)
		a.Files[i].Content = slices.Clone(f.Content)
		previous = f.Path
		fmt.Fprintf(&manifest, "%s\x00%s\x00%t\n", f.Path, f.SHA256, f.Deleted)
	}
	if submissionDigest([]byte(manifest.String())) != a.Digest {
		return a, fmt.Errorf("store: aggregate manifest digest mismatch")
	}
	a.Checks = slices.Clone(a.Checks)
	slices.SortFunc(a.Checks, func(x, y ResultAggregateCheck) int { return strings.Compare(x.Reference, y.Reference) })
	ids := map[string]bool{}
	for i, c := range a.Checks {
		if strings.TrimSpace(c.Reference) == "" || len(c.Reference) > 256 || (i > 0 && a.Checks[i-1].Reference == c.Reference) || strings.TrimSpace(c.ExecutionID) == "" || len(c.ExecutionID) > 256 || ids[c.ExecutionID] || c.Kind != "aggregate" || c.RunID != a.RunID || c.Attempt != a.Attempt || strings.TrimSpace(c.Name) == "" || len(c.Name) > 256 || strings.TrimSpace(c.Evidence) == "" || len(c.Evidence) > 16384 || c.Digest != a.Digest || (!c.Executed && c.Passed) {
			return a, fmt.Errorf("store: aggregate check attribution or factual outcome mismatch")
		}
		ids[c.ExecutionID] = true
	}
	return a, nil
}

// RetainResultAggregate atomically retains immutable snapshot/check facts. It
// creates no story contribution/event, result revision, acceptance or effect.
func (db *DB) RetainResultAggregate(ctx context.Context, b ResultBinding, a ResultAggregate) error {
	a, err := normalizeResultAggregate(b, a)
	if err != nil {
		return err
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, true); err != nil {
			return err
		}
		return retainResultAggregateTx(ctx, tx, b, a)
	})
}

func retainResultAggregateTx(ctx context.Context, tx *sql.Tx, b ResultBinding, a ResultAggregate) error {
	selected, err := json.Marshal(a.Selected)
	if err != nil {
		return err
	}
	files, err := json.Marshal(a.Files)
	if err != nil {
		return err
	}
	if len(files) > MaxSubmissionRetainedBytes {
		return fmt.Errorf("store: aggregate snapshot encoding exceeds retention limit")
	}
	old, err := resultAggregateTx(ctx, tx, b.ResultID, a.Reference)
	if err == nil {
		if !reflect.DeepEqual(old, a) {
			return fmt.Errorf("store: immutable aggregate reference conflict")
		}
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	if err := checkResultAggregateChecksTx(ctx, tx, a); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO application_result_aggregates(result_id,reference,run_id,attempt,parent_task,context_hash,selected,selected_hash,digest,files) VALUES(?,?,?,?,?,?,?,?,?,?)`, a.ResultID, a.Reference, a.RunID, a.Attempt, a.Parent, a.ContextHash, selected, a.SelectedHash, a.Digest, files); err != nil {
		return err
	}
	for _, c := range a.Checks {
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_result_aggregate_checks(result_id,aggregate_ref,reference,execution_id,run_id,attempt,kind,name,digest,executed,passed,evidence) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, a.ResultID, a.Reference, c.Reference, c.ExecutionID, c.RunID, c.Attempt, c.Kind, c.Name, c.Digest, c.Executed, c.Passed, c.Evidence); err != nil {
			return err
		}
	}
	return nil
}

func resultAggregateTx(ctx context.Context, tx *sql.Tx, resultID int64, reference string) (ResultAggregate, error) {
	var a ResultAggregate
	var selected, files []byte
	err := tx.QueryRowContext(ctx, `SELECT result_id,reference,run_id,attempt,parent_task,context_hash,selected,selected_hash,digest,files FROM application_result_aggregates WHERE result_id=? AND reference=?`, resultID, reference).Scan(&a.ResultID, &a.Reference, &a.RunID, &a.Attempt, &a.Parent, &a.ContextHash, &selected, &a.SelectedHash, &a.Digest, &files)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	if err = json.Unmarshal(selected, &a.Selected); err != nil {
		return a, err
	}
	if err = json.Unmarshal(files, &a.Files); err != nil {
		return a, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT reference,execution_id,run_id,attempt,kind,name,digest,executed,passed,evidence FROM application_result_aggregate_checks WHERE result_id=? AND aggregate_ref=? ORDER BY reference`, resultID, reference)
	if err != nil {
		return a, err
	}
	defer rows.Close()
	for rows.Next() {
		var c ResultAggregateCheck
		if err := rows.Scan(&c.Reference, &c.ExecutionID, &c.RunID, &c.Attempt, &c.Kind, &c.Name, &c.Digest, &c.Executed, &c.Passed, &c.Evidence); err != nil {
			return a, err
		}
		a.Checks = append(a.Checks, c)
	}
	return a, rows.Err()
}

// GetResultAggregate reads immutable aggregate facts within a historical binding.
func (db *DB) GetResultAggregate(ctx context.Context, b ResultBinding, reference string) (ResultAggregate, error) {
	var a ResultAggregate
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, false); err != nil {
			return err
		}
		var err error
		a, err = resultAggregateTx(ctx, tx, b.ResultID, reference)
		return err
	})
	return a, err
}

// GetResultEngineeringCheckpoint resolves the current successful immutable story
// checkpoint for an exact frozen revision. Conflicted, invalidated or superseded
// checkpoints cannot be used to recover aggregate-only completion. Completed
// historical acceptance retains its original checkpoint despite later transport
// conflict flags, matching the existing separated acceptance boundary.
func (db *DB) GetResultEngineeringCheckpoint(ctx context.Context, b ResultBinding, story, revision string, runID int64, sequence int) (StoryProgressEvent, error) {
	return db.resultEngineeringCheckpoint(ctx, b, story, revision, runID, sequence, 0)
}

// FindResultEngineeringCheckpoint discovers the authoritative current checkpoint
// for a frozen story in the admitted generation. It grants no writer authority.
func (db *DB) FindResultEngineeringCheckpoint(ctx context.Context, b ResultBinding, generation int, story, revision string) (StoryProgressEvent, error) {
	if generation < 1 {
		return StoryProgressEvent{}, ErrResultAuthority
	}
	return db.resultEngineeringCheckpoint(ctx, b, story, revision, 0, 0, generation)
}

func (db *DB) resultEngineeringCheckpoint(ctx context.Context, b ResultBinding, story, revision string, runID int64, sequence, generation int) (StoryProgressEvent, error) {
	var e StoryProgressEvent
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, false); err != nil {
			return err
		}
		kind := "engineering_complete"
		if b.WorkflowVersion == 1 {
			kind = "verified"
		}
		query := `SELECT e.run_id,e.sequence,e.story_key,e.attempt,e.kind,e.digest,e.payload FROM story_progress_events e JOIN story_progress_runs p ON p.run_id=e.run_id JOIN managed_stories s ON s.parent_task=p.parent_task AND s.generation=p.generation AND s.story_key=e.story_key JOIN approved_story_revisions a ON a.parent_task=s.parent_task AND a.generation=s.generation AND a.story_key=s.story_key WHERE (?=0 OR e.run_id=?) AND (?=0 OR e.sequence=?) AND (?=0 OR p.generation=?) AND e.story_key=? AND p.parent_task=? AND a.digest=? AND e.kind=? AND (e.conflicted=0 OR s.state='completed') AND ((?=2 AND (s.engineering_complete=1 OR s.state='completed')) OR (?=1 AND s.state IN ('verified','completed'))) AND NOT EXISTS(SELECT 1 FROM story_progress_events n JOIN story_progress_runs np ON np.run_id=n.run_id WHERE np.parent_task=p.parent_task AND np.generation=p.generation AND n.story_key=e.story_key AND n.conflicted=0 AND n.kind IN ('engineering_complete','verified','invalidated') AND (n.run_id>e.run_id OR (n.run_id=e.run_id AND n.sequence>e.sequence))) ORDER BY e.run_id DESC,e.sequence DESC LIMIT 1`
		err := tx.QueryRowContext(ctx, query, runID, runID, sequence, sequence, generation, generation, story, b.Task, revision, kind, b.WorkflowVersion, b.WorkflowVersion).Scan(&e.RunID, &e.Sequence, &e.Story, &e.Attempt, &e.Kind, &e.Digest, &e.Payload)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return e, err
}

// ResultCheckpointSubmission reads the immutable contribution associated with a
// retained checkpoint in this result's parent scope. It does not replay effects.
func (db *DB) ResultCheckpointSubmission(ctx context.Context, b ResultBinding, runID int64, sequence int) (SubmissionReceipt, error) {
	var receipt SubmissionReceipt
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, false); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM submission_attempts WHERE run_id=? AND parent_task=?`, runID, b.Task).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return ErrResultAuthority
		}
		var err error
		receipt, err = scanSubmissionReceipt(tx.QueryRowContext(ctx, `SELECT `+receiptColumns+` FROM submission_receipts WHERE run_id=? AND sequence=? AND input_state='valid' AND engineering_state='recorded'`, runID, sequence))
		return err
	})
	return receipt, err
}

func checkResultAggregateChecksTx(ctx context.Context, tx *sql.Tx, a ResultAggregate) error {
	for _, c := range a.Checks {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM submission_checks c JOIN submission_snapshots s ON s.run_id=c.run_id AND s.reference=c.snapshot_ref WHERE s.parent_task=? AND (c.reference=? OR c.reference=?)`, a.Parent, c.Reference, c.ExecutionID).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("store: scoped story checks cannot be relabelled aggregate evidence")
		}
	}
	return nil
}

// CheckResultAggregate validates immutable association conflicts without writes.
func (db *DB) CheckResultAggregate(ctx context.Context, b ResultBinding, a ResultAggregate) error {
	a, err := normalizeResultAggregate(b, a)
	if err != nil {
		return err
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, true); err != nil {
			return err
		}
		old, err := resultAggregateTx(ctx, tx, b.ResultID, a.Reference)
		if err == nil {
			if !reflect.DeepEqual(old, a) {
				return fmt.Errorf("store: immutable aggregate reference conflict")
			}
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		return checkResultAggregateChecksTx(ctx, tx, a)
	})
}
