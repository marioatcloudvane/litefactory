package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// AdmitPlanPath freezes the Plan route for a session on its first admission.
// New sessions get graph Plan, which decomposes the work into a story set and
// returns every node result through the shared MCP result service (#377).
// Sessions already admitted keep their route, including legacy sessions, and an
// admitted route never changes on reply.
func (db *DB) AdmitPlanPath(ctx context.Context, sessionID int64) (string, error) {
	return db.AdmitPlanWorkflowPath(ctx, sessionID, "graph")
}

// GraphPlanStory is a live story issue of a graph Plan parent: the story key,
// the parent task that owns the Plan state and the story's own issue.
type GraphPlanStory struct {
	Parent, Key string
	IssueID     int64
	IssueNumber int
	RepoID      int64
}

const graphPlanStoryColumns = `d.parent,d.story_key,d.issue_id,d.issue_number,s.repo_id`

// GraphPlanStoryAt returns the live graph Plan story an issue is, named by
// repository and number as a turn knows its task, when it is a task of its
// own: it runs its story's stages while its parent keeps the story set, the
// summary and the assembly.
func (db *DB) GraphPlanStoryAt(ctx context.Context, repoID int64, number int) (GraphPlanStory, bool, error) {
	var story GraphPlanStory
	if number < 1 {
		return story, false, nil
	}
	err := db.sql.QueryRowContext(ctx, `SELECT `+graphPlanStoryColumns+` FROM plan_story_drafts d JOIN sessions s ON s.task_ref=d.parent JOIN plan_execution_paths p ON p.session_id=s.id WHERE s.repo_id=? AND d.issue_number=? AND d.retired=0 AND p.path='graph' LIMIT 1`, repoID, number).Scan(&story.Parent, &story.Key, &story.IssueID, &story.IssueNumber, &story.RepoID)
	if errors.Is(err, sql.ErrNoRows) {
		return story, false, nil
	}
	if err != nil {
		return story, false, fmt.Errorf("store: graph plan story at %d: %w", number, err)
	}
	return story, true, nil
}

// ListGraphPlanStories lists every live story issue of every open graph Plan
// parent, so discovery can offer the stories that have work.
func (db *DB) ListGraphPlanStories(ctx context.Context) ([]GraphPlanStory, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+graphPlanStoryColumns+` FROM plan_story_drafts d JOIN sessions s ON s.task_ref=d.parent JOIN plan_execution_paths p ON p.session_id=s.id WHERE d.issue_id>0 AND d.retired=0 AND p.path='graph' AND d.parent NOT IN (SELECT parent_task FROM managed_parent_closures) ORDER BY s.id,d.ordinal`)
	if err != nil {
		return nil, fmt.Errorf("store: list graph plan stories: %w", err)
	}
	defer rows.Close()
	var out []GraphPlanStory
	for rows.Next() {
		var story GraphPlanStory
		if err = rows.Scan(&story.Parent, &story.Key, &story.IssueID, &story.IssueNumber, &story.RepoID); err != nil {
			return nil, fmt.Errorf("store: scan graph plan story: %w", err)
		}
		out = append(out, story)
	}
	return out, rows.Err()
}

// AdmitPlanWorkflowPath freezes a factory-selected route on first admission.
// This compatibility seam is not agent input or an upgrade/downgrade operation:
// an existing persisted route always wins, including against a changed default.
func (db *DB) AdmitPlanWorkflowPath(ctx context.Context, sessionID int64, selected string) (string, error) {
	if sessionID < 1 || (selected != "legacy" && selected != "graph") {
		return "", errors.New("invalid plan workflow admission")
	}
	var out string
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO plan_execution_paths(session_id,path) VALUES(?,?) ON CONFLICT(session_id) DO NOTHING`, sessionID, selected); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT path FROM plan_execution_paths WHERE session_id=?`, sessionID).Scan(&out)
	})
	return out, err
}

// PlanExecutionPath reads persisted admission without selecting or upgrading it.
func (db *DB) PlanExecutionPath(ctx context.Context, sessionID int64) (string, error) {
	var selected string
	err := db.sql.QueryRowContext(ctx, `SELECT path FROM plan_execution_paths WHERE session_id=?`, sessionID).Scan(&selected)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return selected, err
}

// ErrPlanStateConflict reports that Plan state changed since it was read, so a
// save bound to the old revision was refused. Re-reading and re-applying the
// change is always safe.
var ErrPlanStateConflict = errors.New("plan state revision conflict")

// ErrPlanStateUncertain reports that Plan state kept changing under every
// attempt to apply one change, so the change is not known to have been saved.
var ErrPlanStateUncertain = errors.New("plan state kept changing; the change was not saved")

// planStateAttempts bounds how often one change is re-read and re-applied after
// a revision conflict. Parallel stories write different entries, so one retry
// normally lands once the other write has.
const planStateAttempts = 8

// WithPlanWorkflowState reads the Plan state fresh, hands it with its revision
// to apply, and repeats on a revision conflict. apply must save with the
// revision it is given and return ErrPlanStateConflict (possibly wrapped) when
// the save was refused; no revision is ever kept across a remote call.
func (db *DB) WithPlanWorkflowState(ctx context.Context, parent string, apply func(state *planbrief.PlanState, revision int64) error) error {
	for attempt := range planStateAttempts {
		state, revision, err := db.GetPlanWorkflowState(ctx, parent)
		if err != nil {
			return err
		}
		if err = apply(&state, revision); !errors.Is(err, ErrPlanStateConflict) {
			return err
		}
		// A short jittered pause keeps two writers that clashed from clashing again.
		pause := time.Duration(1+rand.IntN(4*(attempt+1))) * time.Millisecond // #nosec G404 -- backoff jitter only
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pause):
		}
	}
	return ErrPlanStateUncertain
}

// UpdatePlanWorkflowState is the one write path for a change to one story of
// the Plan state: it reads fresh, lets change edit the state, and saves with
// the revision check, retrying on conflict. change reports whether it altered
// anything; an unchanged state is not saved.
func (db *DB) UpdatePlanWorkflowState(ctx context.Context, parent string, change func(state *planbrief.PlanState) (bool, error)) error {
	return db.WithPlanWorkflowState(ctx, parent, func(state *planbrief.PlanState, revision int64) error {
		changed, err := change(state)
		if err != nil || !changed {
			return err
		}
		if state.SharedRevision == "" {
			state.SharedRevision = "initial"
		}
		_, err = db.SavePlanWorkflowState(ctx, parent, revision, *state)
		return err
	})
}

// GetPlanWorkflowState returns the current state and optimistic local revision.
// Missing state is an empty value with revision zero, suitable for first save.
func (db *DB) GetPlanWorkflowState(ctx context.Context, parent string) (planbrief.PlanState, int64, error) {
	var state planbrief.PlanState
	var revision int64
	var raw string
	err := db.sql.QueryRowContext(ctx, `SELECT revision,state_json FROM plan_workflow_states WHERE parent=?`, parent).Scan(&revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return state, 0, nil
	}
	if err != nil {
		return state, 0, err
	}
	if err = json.Unmarshal([]byte(raw), &state); err != nil {
		return state, 0, fmt.Errorf("decode retained Plan state: %w", err)
	}
	return state, revision, nil
}

// SavePlanWorkflowState changes current state with its revision check in one
// local transaction. No transaction spans remote publication or git save.
func (db *DB) SavePlanWorkflowState(ctx context.Context, parent string, expected int64, state planbrief.PlanState) (int64, error) {
	if parent == "" || expected < 0 || state.SharedRevision == "" {
		return 0, errors.New("plan parent, shared revision and expected revision required")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return 0, err
	}
	if len(raw) > 4<<20 {
		return 0, errors.New("plan state exceeds 4 MiB")
	}
	revision := expected + 1
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var result sql.Result
		var e error
		if expected == 0 {
			result, e = tx.ExecContext(ctx, `INSERT INTO plan_workflow_states(parent,revision,state_json) VALUES(?,?,?) ON CONFLICT(parent) DO NOTHING`, parent, revision, string(raw))
		} else {
			result, e = tx.ExecContext(ctx, `UPDATE plan_workflow_states SET revision=?,state_json=? WHERE parent=? AND revision=?`, revision, string(raw), parent, expected)
		}
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrPlanStateConflict
		}
		return retainPlanNoticesTx(ctx, tx, parent, state)
	})
	if err != nil {
		return 0, err
	}
	return revision, nil
}
