package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"
)

// AdvisorySourceOutput ties saved JSON evidence to its exact current graph attempt.
type AdvisorySourceOutput struct {
	AttemptID        int64           `json:"attempt_id"`
	Node             string          `json:"node"`
	Status           string          `json:"status"`
	OutputID         int64           `json:"output_id"`
	AcceptedResultID int64           `json:"accepted_result_id"`
	AcceptedRevision int64           `json:"accepted_revision"`
	RunID            int64           `json:"-"`
	OutputCount      int64           `json:"-"`
	Payload          json.RawMessage `json:"payload,omitempty"`
}

// TaskAdvisorySnapshot is one consistent read; it grants no workflow authority.
type TaskAdvisorySnapshot struct {
	Session              Session                `json:"session"`
	Repository           Repo                   `json:"repository"`
	Instance             GraphInstance          `json:"instance"`
	Definition           GraphDefinition        `json:"definition"`
	Files                json.RawMessage        `json:"-"`
	Guard                string                 `json:"-"`
	Wait                 *GraphOwnerWait        `json:"wait,omitempty"`
	Outputs              []AdvisorySourceOutput `json:"outputs"`
	PlanParent, StoryKey string
	PlanRevision         int64
	PlanState            json.RawMessage
}

// ReadTaskAdvisorySnapshot reads current local target facts in one transaction.
// It never selects another task, chooses an old workflow or clears a wait.
func (db *DB) ReadTaskAdvisorySnapshot(ctx context.Context, task string) (TaskAdvisorySnapshot, error) {
	return db.readTaskAdvisorySnapshot(ctx, task, false)
}

func (db *DB) readTaskAdvisorySnapshot(ctx context.Context, task string, savedPlan bool) (TaskAdvisorySnapshot, error) {
	var s TaskAdvisorySnapshot
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		s.Session, err = scanSession(tx.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE task_ref=?`, task))
		if err != nil {
			return err
		}
		s.Repository, err = scanRepo(tx.QueryRowContext(ctx, `SELECT `+repoColumns+` FROM repos WHERE id=?`, s.Session.RepoID))
		if err != nil {
			return err
		}
		// A story belongs to exactly one live parent. Multiple parents are ambiguous.
		rows, err := tx.QueryContext(ctx, `SELECT d.parent,d.story_key FROM plan_story_drafts d JOIN sessions p ON p.task_ref=d.parent JOIN plan_execution_paths x ON x.session_id=p.id WHERE p.repo_id=? AND d.issue_number=? AND d.retired=0 AND x.path='graph' LIMIT 2`, s.Session.RepoID, s.Session.IssueNumber)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			if err = rows.Scan(&s.PlanParent, &s.StoryKey); err != nil {
				_ = rows.Close()
				return err
			}
			count++
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return err
		}
		if count > 1 {
			return fmt.Errorf("%w: ambiguous current Plan membership", ErrJudgeSourceUnavailable)
		}
		s.Instance, err = scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE session_id=? ORDER BY id DESC LIMIT 1`, s.Session.ID))
		if err != nil {
			return err
		}
		s.Definition, err = scanGraphDefinition(tx.QueryRowContext(ctx, `SELECT `+graphDefinitionColumns+` FROM graph_definitions WHERE id=?`, s.Instance.DefinitionID))
		if err != nil {
			return err
		}
		if s.Instance.Status != GraphRunning && (!savedPlan || s.Definition.Mode != "plan" || s.Instance.Status != GraphSucceeded) {
			return ErrJudgeSourceUnavailable
		}
		err = tx.QueryRowContext(ctx, `SELECT files FROM research_launch_definitions WHERE instance_id=? AND length(files)<=16777216 ORDER BY id DESC LIMIT 1`, s.Instance.ID).Scan(&s.Files)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		wait, e := scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE instance_id=? AND state IN ('waiting','claimed') ORDER BY rowid DESC LIMIT 1`, s.Instance.ID))
		if e == nil {
			s.Wait = &wait
		} else if !errors.Is(e, ErrNotFound) {
			return e
		}
		if s.Definition.Mode == "plan" {
			if s.PlanParent == "" {
				s.PlanParent = task
			}
			err = tx.QueryRowContext(ctx, `SELECT revision,CAST(state_json AS BLOB) FROM plan_workflow_states WHERE parent=?`, s.PlanParent).Scan(&s.PlanRevision, &s.PlanState)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
		} else if s.PlanParent != "" {
			return ErrJudgeSourceUnavailable
		}
		rows, err = tx.QueryContext(ctx, `SELECT a.id,a.node,a.status,COALESCE(o.id,0),CAST(COALESCE(o.payload,'null') AS BLOB),COALESCE(a.run_id,0),(SELECT COUNT(*) FROM graph_outputs p WHERE p.node_attempt_id=a.id AND p.kind='json' AND json_valid(CAST(p.payload AS TEXT)) AND json_extract(CAST(p.payload AS TEXT),'$.record') IS NULL) FROM graph_node_attempts a LEFT JOIN graph_outputs o ON o.id=(SELECT MAX(p.id) FROM graph_outputs p WHERE p.node_attempt_id=a.id AND p.kind='json' AND length(p.payload)<=4194304 AND json_valid(CAST(p.payload AS TEXT)) AND json_extract(CAST(p.payload AS TEXT),'$.record') IS NULL) WHERE a.instance_id=? AND a.id=(SELECT MAX(n.id) FROM graph_node_attempts n WHERE n.instance_id=a.instance_id AND n.node=a.node) ORDER BY a.id LIMIT 101`, s.Instance.ID)
		if err != nil {
			return err
		}
		total := 0
		for rows.Next() {
			var o AdvisorySourceOutput
			if err = rows.Scan(&o.AttemptID, &o.Node, &o.Status, &o.OutputID, &o.Payload, &o.RunID, &o.OutputCount); err != nil {
				_ = rows.Close()
				return err
			}
			total += len(o.Payload)
			if len(s.Outputs) >= 100 || total > 16<<20 {
				_ = rows.Close()
				return ErrConnectorCapacity
			}
			s.Outputs = append(s.Outputs, o)
		}
		if err = errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		for i := range s.Outputs {
			output := &s.Outputs[i]
			if output.OutputCount != 1 || output.RunID == 0 {
				output.Payload = nil
				continue
			}
			var accepted []byte
			e := tx.QueryRowContext(ctx, `SELECT result_id,revision,snapshot FROM application_result_revisions WHERE run_id=? ORDER BY request_row DESC LIMIT 1`, output.RunID).Scan(&output.AcceptedResultID, &output.AcceptedRevision, &accepted)
			if errors.Is(e, sql.ErrNoRows) {
				output.Payload = nil
				continue
			}
			if e != nil {
				return e
			}
			if !advisoryOutputMatchesAccepted(output.Payload, accepted) {
				output.Payload = nil
			}
		}
		s.Guard, err = taskAdvisoryGuardTx(ctx, tx, task)
		return err
	})
	return s, err
}

func advisoryOutputMatchesAccepted(output, accepted []byte) bool {
	var left, right map[string]json.RawMessage
	if json.Unmarshal(output, &left) != nil || json.Unmarshal(accepted, &right) != nil {
		return false
	}
	if node := left["node"]; len(node) > 0 {
		if json.Unmarshal(node, &left) != nil {
			return false
		}
	}
	var a, b any
	return len(left["result"]) > 0 && json.Unmarshal(left["result"], &a) == nil && json.Unmarshal(right["result"], &b) == nil && reflect.DeepEqual(a, b)
}

// ListPendingDiscussionConclusions reads bounded dispatch/reconciliation work.
func (db *DB) ListPendingDiscussionConclusions(ctx context.Context, at time.Time, limit int) ([]DiscussionConclusion, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrConnectorInvalid
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+conclusionColumns+` FROM discussion_conclusions WHERE state IN ('pending','sending','uncertain') AND (next_attempt_at='' OR next_attempt_at<=?) ORDER BY created_at,id LIMIT ?`, ownerActionTime(at), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DiscussionConclusion{}
	for rows.Next() {
		c, e := scanConclusion(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// taskAdvisoryGuardTx fingerprints current identities under the admission transaction.
// Append-only captures and outputs need their identities, not another copy of their bytes.
func taskAdvisoryGuardTx(ctx context.Context, tx *sql.Tx, task string) (string, error) {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT json_object(
 'session',json_array(s.id,s.repo_id,s.worktree_path,r.path),
 'graph',json_array(g.id,g.definition_id,g.status,g.current_node),
 'capture',(SELECT MAX(id) FROM research_launch_definitions WHERE instance_id=g.id),
 'attempts',(SELECT json_group_array(json_array(id,node,status,COALESCE(outcome,''),COALESCE(run_id,0))) FROM graph_node_attempts WHERE instance_id=g.id),
 'outputs',(SELECT json_group_array(json_array(o.id,o.node_attempt_id)) FROM graph_outputs o JOIN graph_node_attempts n ON n.id=o.node_attempt_id WHERE n.instance_id=g.id),
 'results',(SELECT json_group_array(json_array(v.result_id,v.revision,v.snapshot_hash)) FROM application_result_revisions v JOIN graph_node_attempts n ON n.run_id=v.run_id WHERE n.instance_id=g.id),
 'waits',(SELECT json_group_array(json_array(delivery_key,state,COALESCE(source_attempt_id,0),COALESCE(reply_id,0),COALESCE(continuation_attempt_id,0))) FROM graph_owner_waits WHERE instance_id=g.id),
 'membership',(SELECT json_group_array(json_array(d.parent,d.story_key,d.revision,d.retired)) FROM plan_story_drafts d JOIN sessions p ON p.task_ref=d.parent WHERE p.repo_id=s.repo_id AND d.issue_number=s.issue_number),
 'plan',(SELECT json_group_array(json_array(p.parent,p.revision)) FROM plan_workflow_states p WHERE p.parent=s.task_ref OR p.parent IN (SELECT d.parent FROM plan_story_drafts d JOIN sessions ps ON ps.task_ref=d.parent WHERE ps.repo_id=s.repo_id AND d.issue_number=s.issue_number AND d.retired=0))
 ) FROM sessions s JOIN repos r ON r.id=s.repo_id JOIN graph_instances g ON g.id=(SELECT MAX(id) FROM graph_instances WHERE session_id=s.id) WHERE s.task_ref=?`, task).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return submissionDigest([]byte(raw)), nil
}

// CurrentTaskAdvisoryGuard observes current workflow identities even after the
// captured analytical stage is no longer eligible for a new consultation.
func (db *DB) CurrentTaskAdvisoryGuard(ctx context.Context, task string) (string, error) {
	var guard string
	err := db.withTx(ctx, func(tx *sql.Tx) error { var err error; guard, err = taskAdvisoryGuardTx(ctx, tx, task); return err })
	return guard, err
}
