package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// BugTestRequest is factory-bound; no graph path or command is supplied by agents.
// Resolution names a retained resolved infrastructure cause for a new generation.
type BugTestRequest struct {
	SessionID        int64           `json:"session_id"`
	Operation        string          `json:"operation"`
	ParentInstance   int64           `json:"parent_instance"`
	ParentAttempt    int64           `json:"parent_attempt"`
	ChildDefinition  int64           `json:"child_definition"`
	DefinitionDigest string          `json:"definition_digest"`
	Binding          BugCheckBinding `json:"binding"`
	PreviousCall     int64           `json:"previous_call,omitempty"`
	Resolution       string          `json:"resolution,omitempty"`
}

// BugTestCall binds one durable child execution to its parent and tested inputs.
type BugTestCall struct {
	ID, ChildInstance            int64
	Request                      BugTestRequest
	Generation                   int
	Outcome, Reason              string
	Cancelled, Consumed, Current bool
}

const bugTestColumns = `id,child_instance,binding,generation,outcome,reason,cancelled,consumed,current`

func scanBugTest(row scanner) (BugTestCall, error) {
	var c BugTestCall
	var raw []byte
	err := row.Scan(&c.ID, &c.ChildInstance, &raw, &c.Generation, &c.Outcome, &c.Reason, &c.Cancelled, &c.Consumed, &c.Current)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(raw, &c.Request)
	return c, err
}

// GetBugTestCall loads a child call by durable identity.
func (d *DB) GetBugTestCall(ctx context.Context, id int64) (BugTestCall, error) {
	return scanBugTest(d.sql.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_test_calls WHERE id=?`, id))
}

// BugTestParentCall finds the child call owned by a parent execution envelope.
func (d *DB) BugTestParentCall(ctx context.Context, instance int64) (BugTestCall, error) {
	return scanBugTest(d.sql.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_test_calls WHERE parent_instance=? AND consumed=0 ORDER BY id DESC LIMIT 1`, instance))
}

// BugTestChildCall finds the call owning a child execution envelope.
func (d *DB) BugTestChildCall(ctx context.Context, instance int64) (BugTestCall, error) {
	return scanBugTest(d.sql.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_test_calls WHERE child_instance=?`, instance))
}

// PhaseBinding derives the exact durable binding for one phase of this child call.
func (c BugTestCall) PhaseBinding(phase string) BugCheckBinding {
	b := c.Request.Binding
	b.Phase = phase
	b.Operation = fmt.Sprintf("bug-test:%d:%s", c.ID, phase)
	b.Generation = fmt.Sprintf("bug-test:%d", c.ID)
	return b
}
func bugTestRepairTx(ctx context.Context, tx *sql.Tx, r BugTestRequest) error {
	repair, err := bugRepairTx(ctx, tx, r.SessionID, r.Binding.Repair)
	if err != nil {
		return err
	}
	if repair.State != "completed" {
		return errors.New("bug test requires completed repair")
	}
	if err = bugRepairApprovalTx(ctx, tx, repair.BugRepairRequest); err != nil {
		return err
	}
	var commit, tree string
	if err = tx.QueryRowContext(ctx, `SELECT source_commit,source_tree FROM bug_repair_completions WHERE session_id=? AND request=?`, r.SessionID, r.Binding.Repair).Scan(&commit, &tree); err != nil {
		return err
	}
	if commit != r.Binding.SourceCommit || tree != r.Binding.SourceTree {
		return errors.New("bug test source differs from completed repair")
	}
	return nil
}

// CreateBugTestCall atomically allocates the child, its dispatch and parent wait.
// The parent attempt stays running until consumption; it is never orphan work.
func (d *DB) CreateBugTestCall(ctx context.Context, r BugTestRequest, at time.Time) (BugTestCall, error) {
	if r.SessionID <= 0 || r.Operation == "" || len(r.Operation) > 128 || r.ParentInstance <= 0 || r.ParentAttempt <= 0 || r.ChildDefinition <= 0 || !validSubmissionDigest(r.DefinitionDigest) || at.IsZero() || r.Binding.SessionID != r.SessionID {
		return BugTestCall{}, errors.New("invalid Bug test call")
	}
	b := r.Binding
	b.Phase = "regression"
	b.Operation = r.Operation
	b.Generation = r.Operation
	if err := validateBugCheckBinding(b); err != nil {
		return BugTestCall{}, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return BugTestCall{}, err
	}
	digest := submissionDigest(raw)
	var id int64
	err = d.withTx(ctx, func(tx *sql.Tx) error {
		var old string
		e := tx.QueryRowContext(ctx, `SELECT id,input_digest FROM bug_test_calls WHERE session_id=? AND operation=?`, r.SessionID, r.Operation).Scan(&id, &old)
		if e == nil {
			if old != digest {
				return errors.New("bug test call replay conflict")
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e = bugTestRepairTx(ctx, tx, r); e != nil {
			return e
		}
		var guards int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_work_guards WHERE session_id=?`, r.SessionID).Scan(&guards); e != nil {
			return e
		}
		if guards != 0 {
			return errors.New("bug test worktree is occupied")
		}
		var parentSession, definition int64
		var status, node, workflow string
		if e = tx.QueryRowContext(ctx, `SELECT i.session_id,i.definition_id,a.status,a.node,COALESCE(g.workflow,'') FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions g ON g.id=i.definition_id WHERE a.id=? AND i.id=? AND i.status='running' AND i.current_node=a.node`, r.ParentAttempt, r.ParentInstance).Scan(&parentSession, &definition, &status, &node, &workflow); e != nil {
			return e
		}
		if parentSession != r.SessionID || status != GraphRunning || workflow != "bug" {
			return errors.New("bug test parent binding mismatch")
		}
		var nested int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_test_calls WHERE child_instance=?`, r.ParentInstance).Scan(&nested); e != nil {
			return e
		}
		if nested != 0 {
			return errors.New("nested Bug children forbidden")
		}
		var childDigest, childWorkflow, path, parentSnapshot, childSnapshot string
		if e = tx.QueryRowContext(ctx, `SELECT digest,workflow,path,COALESCE(team_snapshot_digest,'') FROM graph_definitions WHERE id=?`, r.ChildDefinition).Scan(&childDigest, &childWorkflow, &path, &childSnapshot); e != nil {
			return e
		}
		if e = tx.QueryRowContext(ctx, `SELECT COALESCE(team_snapshot_digest,'') FROM graph_definitions WHERE id=?`, definition).Scan(&parentSnapshot); e != nil {
			return e
		}
		if childDigest != r.DefinitionDigest || childWorkflow != "bug" || path != "bug_tests.dot" || parentSnapshot != childSnapshot || !validSubmissionDigest(parentSnapshot) {
			return errors.New("bug child definition is not pinned to parent package")
		}
		var generation int
		if e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(generation),0)+1 FROM bug_test_calls WHERE session_id=? AND repair=?`, r.SessionID, r.Binding.Repair).Scan(&generation); e != nil {
			return e
		}
		if generation > 1 {
			prior, e := scanBugTest(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_test_calls WHERE id=? AND session_id=? AND repair=?`, r.PreviousCall, r.SessionID, r.Binding.Repair))
			if e != nil {
				return e
			}
			if prior.Generation != generation-1 || (prior.Outcome != "blocked" && (prior.Outcome != "passed" || prior.Current)) || !prior.Consumed || r.Resolution == "" || len(r.Resolution) > 8192 {
				return errors.New("new Bug generation needs resolved quiescent infrastructure block")
			}
			var unreliable int
			if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_check_phases WHERE session_id=? AND unreliable=1 AND json_extract(binding,'$.source_commit')=? AND json_extract(binding,'$.target_commit')=? AND json_extract(binding,'$.recipe_digest')=?`, r.SessionID, r.Binding.SourceCommit, r.Binding.TargetCommit, r.Binding.RecipeDigest).Scan(&unreliable); e != nil {
				return e
			}
			if unreliable > 0 {
				return errors.New("known flakiness cannot start a fresh generation")
			}
		} else if r.PreviousCall != 0 || r.Resolution != "" {
			return errors.New("initial generation cannot claim recovery")
		}
		child, e := scanGraphInstance(tx.QueryRowContext(ctx, `INSERT INTO graph_instances(session_id,definition_id,status,current_node,started_at) VALUES(?,?,'running','regression',?) RETURNING `+graphInstanceColumns, r.SessionID, r.ChildDefinition, FormatTime(at)))
		if e != nil {
			return e
		}
		e = tx.QueryRowContext(ctx, `INSERT INTO bug_test_calls(session_id,operation,input_digest,binding,repair,generation,parent_instance,parent_attempt,child_instance,definition_digest,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) RETURNING id`, r.SessionID, r.Operation, digest, raw, r.Binding.Repair, generation, r.ParentInstance, r.ParentAttempt, child.ID, r.DefinitionDigest, FormatTime(at)).Scan(&id)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_instances SET next_due_at=NULL WHERE id=?`, r.ParentInstance); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='finished',finished_at=? WHERE instance_id=? AND status IN ('pending','reserved','running')`, FormatTime(at), r.ParentInstance); e != nil {
			return e
		}
		if e = bugTestDispatchTx(ctx, tx, child.ID, r.SessionID, "regression", at); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, r.SessionID)
		return e
	})
	if err != nil {
		return BugTestCall{}, err
	}
	return d.GetBugTestCall(ctx, id)
}
func bugTestDispatchTx(ctx context.Context, tx *sql.Tx, instance, session int64, node string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,status,created_at) SELECT ?,?,?,?,'pending',? WHERE NOT EXISTS(SELECT 1 FROM graph_dispatches WHERE instance_id=? AND status IN ('pending','reserved','running'))`, instance, session, node, FormatTime(at), FormatTime(at), instance)
	return err
}
