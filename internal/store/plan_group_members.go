package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// This band is distinct from chat, graph dispatch, continuation and schedule
// triggers. A member has no independent outer graph dispatch or forge comment.
const planMemberTriggerBase int64 = 1 << 58

// IsPlanMemberTrigger identifies a private child invocation rather than a forge reply.
func IsPlanMemberTrigger(id int64) bool { return id > planMemberTriggerBase && id < chatTriggerBase }

// PlanGroupMember binds one role execution to its coordinator and immutable input basis.
type PlanGroupMember struct {
	ID, ParentAttemptID, AttemptID, RunID int64
	Role, Node, Basis                     string
	Sequence                              int
}

// PlanGroupMemberRunStart supplies factory-owned identity and isolated run paths.
type PlanGroupMemberRunStart struct {
	ParentAttemptID                      int64
	Role, Node, Basis, Agent, SessionRef string
	StartedAt                            time.Time
	LogPath                              func(int) string
	SessionRefForOrdinal                 func(int) (string, error)
}

// AllocatePlanGroupMemberRun reserves a private conversation, run, and child
// attempt together. No network work or lock wait occurs inside this transaction.
func (db *DB) AllocatePlanGroupMemberRun(ctx context.Context, in PlanGroupMemberRunStart) (run Run, segment Segment, attempt GraphNodeAttempt, member PlanGroupMember, err error) {
	if in.ParentAttemptID <= 0 || in.Node == "" || in.Basis == "" || in.Agent == "" || (in.SessionRef == "" && in.SessionRefForOrdinal == nil) || in.StartedAt.IsZero() || in.LogPath == nil || (in.Role != "story" && in.Role != "technical" && in.Role != "acceptance") {
		err = graphInvariant("invalid Plan member allocation")
		return
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		parent, e := scanGraphAttempt(tx.QueryRowContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE id=?`, in.ParentAttemptID))
		if e != nil {
			return e
		}
		instance, e := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, parent.InstanceID))
		if e != nil {
			return e
		}
		var mode string
		if e = tx.QueryRowContext(ctx, `SELECT mode FROM graph_definitions WHERE id=?`, instance.DefinitionID).Scan(&mode); e != nil {
			return e
		}
		if mode != "plan" || parent.Status != GraphRunning || instance.Status != GraphRunning || parent.Node != instance.CurrentNode || in.Node == parent.Node {
			return graphInvariant("Plan member parent is not current and running")
		}
		var running int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM plan_group_members m JOIN graph_node_attempts a ON a.id=m.attempt_id WHERE m.parent_attempt_id=? AND m.role=? AND a.status='running'`, parent.ID, in.Role).Scan(&running); e != nil {
			return e
		}
		if running != 0 {
			return ErrGraphAttemptRunning
		}
		var sequence int
		if e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM plan_group_members WHERE parent_attempt_id=? AND role=?`, parent.ID, in.Role).Scan(&sequence); e != nil {
			return e
		}
		member = PlanGroupMember{ParentAttemptID: parent.ID, Role: in.Role, Node: in.Node, Basis: in.Basis, Sequence: sequence}
		if e = tx.QueryRowContext(ctx, `INSERT INTO plan_group_members(parent_attempt_id,role,node,basis,sequence) VALUES(?,?,?,?,?) RETURNING id`, parent.ID, in.Role, in.Node, in.Basis, sequence).Scan(&member.ID); e != nil {
			return e
		}
		var segmentOrdinal, runOrdinal int
		if e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal),0)+1 FROM segments WHERE session_id=?`, instance.SessionID).Scan(&segmentOrdinal); e != nil {
			return e
		}
		sessionRef := in.SessionRef
		if in.SessionRefForOrdinal != nil {
			sessionRef, e = in.SessionRefForOrdinal(segmentOrdinal)
			if e != nil {
				return e
			}
		}
		if sessionRef == "" {
			return graphInvariant("Plan member session reference is blank")
		}
		segment, e = scanSegment(tx.QueryRowContext(ctx, `INSERT INTO segments(session_id,ordinal,session_ref,agent,created_at) VALUES(?,?,?,?,?) RETURNING `+segmentColumns, instance.SessionID, segmentOrdinal, sessionRef, in.Agent, FormatTime(in.StartedAt)))
		if e != nil {
			return e
		}
		if e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal),0)+1 FROM runs WHERE session_id=?`, instance.SessionID).Scan(&runOrdinal); e != nil {
			return e
		}
		path := in.LogPath(runOrdinal)
		if path == "" {
			return graphInvariant("Plan member log path is blank")
		}
		run, e = scanRun(tx.QueryRowContext(ctx, `INSERT INTO runs(session_id,segment_id,ordinal,mode,agent,trigger_comment_id,status,started_at,log_path,workflow) VALUES(?,?,?,?,?,?,?,?,?,'') RETURNING `+runColumns, instance.SessionID, segment.ID, runOrdinal, "plan", in.Agent, planMemberTriggerBase+member.ID, string(RunRunning), FormatTime(in.StartedAt), path))
		if e != nil {
			return e
		}
		var maximum int
		if e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt),0) FROM graph_node_attempts WHERE instance_id=? AND node=?`, instance.ID, in.Node).Scan(&maximum); e != nil {
			return e
		}
		attempt, e = scanGraphAttempt(tx.QueryRowContext(ctx, `INSERT INTO graph_node_attempts(instance_id,node,attempt,run_id,status,started_at,launch_id) VALUES(?,?,?,?,?,?,?) RETURNING `+graphAttemptColumns, instance.ID, in.Node, maximum+1, run.ID, GraphRunning, FormatTime(in.StartedAt), nullID(parent.LaunchID)))
		if e != nil {
			return e
		}
		member.AttemptID = attempt.ID
		member.RunID = run.ID
		_, e = tx.ExecContext(ctx, `UPDATE plan_group_members SET attempt_id=?,run_id=? WHERE id=?`, attempt.ID, run.ID, member.ID)
		return e
	})
	if err != nil {
		err = fmt.Errorf("store: allocate Plan group member: %w", err)
	}
	return
}

// CompletePlanGroupMember retains its output and closes only the child attempt.
// The coordinator alone may transition the outer graph.
func (db *DB) CompletePlanGroupMember(ctx context.Context, attemptID int64, status, outcome string, payload []byte, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var parentID int64
		if err := tx.QueryRowContext(ctx, `SELECT parent_attempt_id FROM plan_group_members WHERE attempt_id=?`, attemptID).Scan(&parentID); err != nil {
			return err
		}
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM graph_node_attempts p JOIN graph_instances i ON i.id=p.instance_id WHERE p.id=? AND p.status='running' AND i.status='running' AND i.current_node=p.node`, parentID).Scan(&active); err != nil {
			return err
		}
		if active != 1 {
			return ErrGraphAttemptTerminal
		}
		a, err := scanGraphAttempt(tx.QueryRowContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE id=?`, attemptID))
		if err != nil {
			return err
		}
		if graphTerminal(a.Status) {
			if a.Status == status && a.Outcome == outcome {
				if payload != nil {
					var old []byte
					if err := tx.QueryRowContext(ctx, `SELECT payload FROM graph_outputs WHERE node_attempt_id=? AND kind='json' ORDER BY id LIMIT 1`, attemptID).Scan(&old); err != nil {
						return err
					}
					if !bytes.Equal(old, payload) {
						return graphInvariant("Plan member completion payload changed")
					}
				}
				return nil
			}
			return ErrGraphAttemptTerminal
		}
		if _, err = finishGraphAttemptTx(ctx, tx, attemptID, status, outcome, at); err != nil {
			return err
		}
		if payload != nil {
			_, err = recordGraphOutputTx(ctx, tx, GraphOutput{NodeAttemptID: attemptID, Kind: GraphOutputJSON, Payload: payload, CreatedAt: at})
		}
		return err
	})
}

// ListPlanGroupMembers returns durable members of one coordinator attempt.
func (db *DB) ListPlanGroupMembers(ctx context.Context, parentID int64) ([]PlanGroupMember, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT id,parent_attempt_id,role,node,basis,sequence,COALESCE(attempt_id,0),COALESCE(run_id,0) FROM plan_group_members WHERE parent_attempt_id=? ORDER BY id`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlanGroupMember{}
	for rows.Next() {
		var m PlanGroupMember
		if err := rows.Scan(&m.ID, &m.ParentAttemptID, &m.Role, &m.Node, &m.Basis, &m.Sequence, &m.AttemptID, &m.RunID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func planMemberParentTx(ctx context.Context, tx *sql.Tx, runID int64) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT parent_attempt_id FROM plan_group_members WHERE run_id=?`, runID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}
func checkPlanMemberWriterTx(ctx context.Context, tx *sql.Tx, runID, parentID int64) error {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM plan_group_members m JOIN graph_node_attempts c ON c.id=m.attempt_id JOIN graph_node_attempts p ON p.id=m.parent_attempt_id JOIN graph_instances i ON i.id=p.instance_id WHERE m.run_id=? AND p.id=? AND c.status='running' AND p.status='running' AND i.status='running' AND i.current_node=p.node AND NOT EXISTS(SELECT 1 FROM plan_group_members newer WHERE newer.parent_attempt_id=m.parent_attempt_id AND newer.role=m.role AND newer.sequence>m.sequence)`, runID, parentID).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrResultAuthority
	}
	return nil
}

// ListPlanGroupMembersByBasis recovers child identities across coordinator
// attempts and successor instances of the same story session. The immutable
// basis prevents a prior planning input being adopted.
func (db *DB) ListPlanGroupMembersByBasis(ctx context.Context, instanceID int64, basis string) ([]PlanGroupMember, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT m.id,m.parent_attempt_id,m.role,m.node,m.basis,m.sequence,COALESCE(m.attempt_id,0),COALESCE(m.run_id,0) FROM plan_group_members m JOIN graph_node_attempts p ON p.id=m.parent_attempt_id WHERE p.instance_id IN (SELECT i.id FROM graph_instances i WHERE i.session_id=(SELECT session_id FROM graph_instances WHERE id=?)) AND m.basis=? ORDER BY m.id`, instanceID, basis)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := []PlanGroupMember{}
	for rows.Next() {
		var m PlanGroupMember
		if err := rows.Scan(&m.ID, &m.ParentAttemptID, &m.Role, &m.Node, &m.Basis, &m.Sequence, &m.AttemptID, &m.RunID); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// CloseOrphanPlanGroupMembers revokes only children whose coordinator has
// already stopped. Accepted results and receipts stay available to recovery.
func (db *DB) CloseOrphanPlanGroupMembers(ctx context.Context, instanceID, currentParent int64, at time.Time) error {
	if instanceID <= 0 || currentParent <= 0 || at.IsZero() {
		return graphInvariant("invalid Plan member recovery identity")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var current int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM graph_node_attempts p JOIN graph_instances i ON i.id=p.instance_id JOIN graph_definitions d ON d.id=i.definition_id WHERE p.id=? AND p.instance_id=? AND p.status='running' AND i.status='running' AND i.current_node=p.node AND d.mode='plan'`, currentParent, instanceID).Scan(&current); err != nil {
			return err
		}
		if current != 1 {
			return ErrGraphAttemptTerminal
		}
		rows, err := tx.QueryContext(ctx, `SELECT m.attempt_id,m.run_id,child.status FROM plan_group_members m JOIN graph_node_attempts p ON p.id=m.parent_attempt_id JOIN graph_node_attempts child ON child.id=m.attempt_id JOIN runs r ON r.id=m.run_id WHERE p.instance_id IN (SELECT i.id FROM graph_instances i WHERE i.session_id=(SELECT session_id FROM graph_instances WHERE id=?)) AND p.id<>? AND p.status NOT IN ('running','pending') AND (child.status IN ('running','pending') OR r.status='running')`, instanceID, currentParent)
		if err != nil {
			return err
		}
		type identity struct {
			attempt, run int64
			status       string
		}
		orphans := []identity{}
		for rows.Next() {
			var item identity
			if err := rows.Scan(&item.attempt, &item.run, &item.status); err != nil {
				_ = rows.Close()
				return err
			}
			orphans = append(orphans, item)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		for _, item := range orphans {
			if item.status == GraphRunning || item.status == GraphPending {
				if _, err = finishGraphAttemptTx(ctx, tx, item.attempt, GraphCancelled, "", at); err != nil {
					return err
				}
			}
			var running int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM runs WHERE id=? AND status='running'`, item.run).Scan(&running); err != nil {
				return err
			}
			if running == 1 {
				outcome := RunOutcome{Status: RunFailed, Error: "Plan member coordinator stopped"}
				if item.status == GraphSucceeded {
					outcome = RunOutcome{Status: RunSucceeded}
				}
				if _, err = finishRunTx(ctx, tx, item.run, outcome, at); err != nil {
					return err
				}
			}
			if _, err = tx.ExecContext(ctx, `UPDATE application_result_attempts SET active=0 WHERE run_id=?`, item.run); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE mcp_attempt_leases SET state='closed' WHERE run_id=? AND state='active'`, item.run); err != nil {
				return err
			}
		}
		return nil
	})
}

// PlanGroupMemberResultBinding recovers an attributed accepted shared result
// even when the process stopped before writing its graph output reference.
// It grants historical reads only; it never reactivates a stopped writer.
func (db *DB) PlanGroupMemberResultBinding(ctx context.Context, attemptID int64) (binding ResultBinding, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var runID int64
		if err := tx.QueryRowContext(ctx, `SELECT run_id FROM plan_group_members WHERE attempt_id=?`, attemptID).Scan(&runID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT a.attempt,a.result_id FROM application_result_revisions v JOIN application_result_attempts a ON a.result_id=v.result_id AND a.run_id=v.run_id AND a.attempt=v.attempt JOIN application_results r ON r.id=a.result_id WHERE a.run_id=? AND r.mode='plan' ORDER BY v.revision DESC`, runID)
		if err != nil {
			return err
		}
		chosen := ""
		var resultID int64
		for rows.Next() {
			var attempt string
			var id int64
			if err := rows.Scan(&attempt, &id); err != nil {
				_ = rows.Close()
				return err
			}
			if chosen == "" {
				chosen = attempt
				resultID = id
			} else if resultID != id {
				_ = rows.Close()
				return ErrResultAuthority
			}
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		if chosen == "" {
			return ErrNotFound
		}
		binding, err = readResultBinding(ctx, tx, runID, chosen, false)
		return err
	})
	return
}
