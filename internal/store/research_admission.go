package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

// AdmitWorkflowPath freezes the execution path once for this workflow session.
// Existing paths always win over changed repository configuration/defaults.
func (db *DB) AdmitWorkflowPath(ctx context.Context, sessionID int64, selected string) (string, error) {
	if sessionID < 1 || (selected != "legacy" && selected != "graph") {
		return "", graphInvariant("invalid workflow admission")
	}
	var out string
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO research_execution_paths(session_id,path) VALUES(?,?) ON CONFLICT(session_id) DO NOTHING`, sessionID, selected)
		if err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT path FROM research_execution_paths WHERE session_id=?`, sessionID).Scan(&out)
	})
	return out, err
}

// WorkflowLaunch pins the actual owned package bytes at an authorized boundary.
// The foundation definition remains immutable when only instructions change.
type WorkflowLaunch struct {
	Workflow                             string
	Mode                                 string
	SessionID, TriggerID                 int64
	DefinitionID                         int64
	Start, ContentDigest, TopologyDigest string
	Files                                []byte
	Provenance                           WorkflowProvenance
	// Reply is authenticated owner input from turn admission, never node output.
	Reply string
	// ResearchCompatible retains the current node when its action/source still maps.
	ResearchCompatible bool
}

// WorkflowAdmission identifies the pinned launch and any predecessor whose findings survive.
type WorkflowAdmission struct {
	Instance      GraphInstance
	PredecessorID int64
	LaunchID      int64
}

// ErrWorkflowEffectsPending prevents successor admission during an uncertain publication.
var ErrWorkflowEffectsPending = errors.New("prior workflow publication must reconcile under its original identity before successor admission")

// ErrResearchEffectsPending is the Research policy name for the shared refusal.
var ErrResearchEffectsPending = ErrWorkflowEffectsPending

// LatestWorkflowInstance is the daemon's read-only seam for correlating an
// authenticated owner comment with a saved question before launch admission.
func (db *DB) LatestWorkflowInstance(ctx context.Context, sessionID int64, mode string) (GraphInstance, error) {
	return scanGraphInstance(db.sql.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE session_id=? AND definition_id IN (SELECT id FROM graph_definitions WHERE mode=?) ORDER BY id DESC LIMIT 1`, sessionID, mode))
}

// AdmitWorkflowLaunch atomically records the launch and, when topology changes
// or an owner continues terminal work, links a successor at the current start.
// Calling it is explicit owner authority; recovery must never call this method.
func (db *DB) AdmitWorkflowLaunch(ctx context.Context, in WorkflowLaunch) (out WorkflowAdmission, err error) {
	if in.Workflow == "bug" {
		if in.Mode != "" {
			return out, graphInvariant("standalone launch cannot have a mode")
		}
		in.Mode = standaloneGraphStorageKey
	} else if in.Workflow != "" && in.Workflow != in.Mode {
		return out, graphInvariant("conflicting launch workflow binding")
	}
	validDigest := func(s string) bool {
		raw, e := hex.DecodeString(s)
		return e == nil && len(raw) == 32 && hex.EncodeToString(raw) == s
	}
	if in.Mode == "" || in.SessionID < 1 || in.TriggerID < 0 || in.DefinitionID < 1 || in.Start == "" || !validDigest(in.ContentDigest) || !validDigest(in.TopologyDigest) || len(in.Files) == 0 || len(in.Files) > 16<<20 || len(in.Reply) > 65536 {
		return out, graphInvariant("invalid workflow launch capture")
	}
	metadata, metadataErr := serializedWorkflowProvenance(in.Provenance)
	if metadataErr != nil {
		return out, metadataErr
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		admission, admissionErr := readTaskWorkflowAdmission(ctx, tx, in.SessionID)
		if admissionErr != nil && !errors.Is(admissionErr, ErrNotFound) {
			return admissionErr
		}
		if in.Workflow == "bug" && (admissionErr != nil || admission.Family != "standalone") {
			return graphInvariant("Bug launch requires retained standalone admission")
		}
		if admissionErr == nil && admission.Family == "standalone" && in.Workflow != "bug" {
			return graphInvariant("mode launch conflicts with standalone admission")
		}
		var mode string
		if e := tx.QueryRowContext(ctx, `SELECT mode FROM graph_definitions WHERE id=?`, in.DefinitionID).Scan(&mode); e != nil {
			return e
		}
		if mode != in.Mode {
			return graphInvariant("workflow definition mode mismatch")
		}
		var path string
		pathQuery := `SELECT path FROM research_execution_paths WHERE session_id=?`
		if in.Mode == "plan" {
			pathQuery = `SELECT path FROM plan_execution_paths WHERE session_id=?`
		}
		if e := tx.QueryRowContext(ctx, pathQuery, in.SessionID).Scan(&path); e != nil {
			return e
		}
		if path != "graph" {
			return graphInvariant("workflow launch requires admitted graph path")
		}
		if dispatchID, ok := GraphTriggerDispatch(in.TriggerID); ok {
			dispatch, e := scanGraphDispatch(tx.QueryRowContext(ctx, `SELECT `+graphDispatchColumns+` FROM graph_dispatches WHERE id=? AND session_id=?`, dispatchID, in.SessionID))
			if e != nil {
				return e
			}
			out.Instance, e = scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, dispatch.InstanceID))
			if e != nil {
				return e
			}
			var topology string
			if e = tx.QueryRowContext(ctx, `SELECT id,topology_digest FROM research_launch_definitions WHERE id=? AND instance_id=?`, dispatch.LaunchID, dispatch.InstanceID).Scan(&out.LaunchID, &topology); e != nil {
				return e
			}
			if out.Instance.Status != GraphRunning || out.Instance.CurrentNode != dispatch.Node || out.Instance.DefinitionID != in.DefinitionID || topology != in.TopologyDigest || (dispatch.Status != "reserved" && dispatch.Status != "running") {
				return graphInvariant("graph dispatch cannot change its saved execution target")
			}
			return nil
		}
		var replayInstance int64
		var replayDigest string
		var replayFiles, replayMetadata []byte
		e := tx.QueryRowContext(ctx, `SELECT id,instance_id,content_digest,files,provenance FROM research_launch_definitions WHERE session_id=? AND trigger_id=?`, in.SessionID, in.TriggerID).Scan(&out.LaunchID, &replayInstance, &replayDigest, &replayFiles, &replayMetadata)
		if e == nil {
			var replayMode string
			if e = tx.QueryRowContext(ctx, `SELECT d.mode FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE i.id=?`, replayInstance).Scan(&replayMode); e != nil {
				return e
			}
			if replayMode != in.Mode {
				return graphInvariant("workflow replay mode mismatch")
			}
			if replayDigest != in.ContentDigest || !bytes.Equal(replayFiles, in.Files) || !bytes.Equal(replayMetadata, metadata) {
				return graphInvariant("workflow launch replay cannot replace captured instructions; a new owner continuation is required")
			}
			out.Instance, e = scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, replayInstance))
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		var prior GraphInstance
		prior, e = scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE session_id=? AND definition_id IN (SELECT id FROM graph_definitions WHERE mode=?) ORDER BY id DESC LIMIT 1`, in.SessionID, in.Mode))
		if e != nil && !errors.Is(e, ErrNotFound) {
			return e
		}
		var topology string
		if prior.ID > 0 {
			if e = tx.QueryRowContext(ctx, `SELECT topology_digest FROM research_launch_definitions WHERE instance_id=? ORDER BY id DESC LIMIT 1`, prior.ID).Scan(&topology); e != nil {
				return fmt.Errorf("workflow instance lacks retained launch provenance: %w", e)
			}
			var active int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_node_attempts WHERE instance_id=? AND status IN ('pending','running')`, prior.ID).Scan(&active); e != nil {
				return e
			}
			if active > 0 {
				return graphInvariant("workflow invocation already active")
			}
			if prior.Status == GraphRunning {
				var state, reply string
				var replyID int64
				e = tx.QueryRowContext(ctx, `SELECT state,COALESCE(reply_id,0),COALESCE(reply_body,'') FROM graph_owner_waits WHERE instance_id=? AND state IN ('waiting','claimed') ORDER BY rowid DESC LIMIT 1`, prior.ID).Scan(&state, &replyID, &reply)
				if e != nil && !errors.Is(e, sql.ErrNoRows) {
					return e
				}
				if e == nil && (state != "claimed" || replyID != in.TriggerID || reply != in.Reply) {
					return ErrGraphOwnerWait
				}
			}
		}
		successor := prior.ID > 0 && ((topology != in.TopologyDigest && (in.Mode != "research" || !in.ResearchCompatible)) || prior.Status != GraphRunning)
		if successor {
			var pending int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_comment_intents c JOIN graph_instances i ON i.id=c.instance_id LEFT JOIN report_deliveries d ON d.delivery_key=c.delivery_key WHERE i.session_id=? AND (d.delivery_key IS NULL OR d.completed=0 OR d.comment_id IS NULL)`, in.SessionID).Scan(&pending); e != nil {
				return e
			}
			if pending > 0 {
				return ErrWorkflowEffectsPending
			}
			out.PredecessorID = prior.ID
			if prior.Status == GraphRunning {
				if _, e = tx.ExecContext(ctx, `UPDATE graph_instances SET status='cancelled',next_due_at=NULL,finished_at=? WHERE id=?`, FormatTime(nowUTC()), prior.ID); e != nil {
					return e
				}
			}
			if _, e = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='cancelled' WHERE instance_id=? AND state='claimed'`, prior.ID); e != nil {
				return e
			}
		}
		out.Instance = prior
		if prior.ID == 0 || successor {
			out.Instance, e = scanGraphInstance(tx.QueryRowContext(ctx, `INSERT INTO graph_instances(session_id,definition_id,status,current_node,started_at) VALUES(?,?,'running',?,?) RETURNING `+graphInstanceColumns, in.SessionID, in.DefinitionID, in.Start, FormatTime(nowUTC())))
			if e != nil {
				return e
			}
			if successor {
				if _, e = tx.ExecContext(ctx, `INSERT INTO research_successors(instance_id,predecessor_id,trigger_id,reply_body) VALUES(?,?,?,?)`, out.Instance.ID, prior.ID, in.TriggerID, in.Reply); e != nil {
					return e
				}
			}
		}
		return tx.QueryRowContext(ctx, `INSERT INTO research_launch_definitions(session_id,trigger_id,instance_id,content_digest,topology_digest,files,provenance,files_digest) VALUES(?,?,?,?,?,?,?,?) RETURNING id`, in.SessionID, in.TriggerID, out.Instance.ID, in.ContentDigest, in.TopologyDigest, in.Files, metadata, captureBytesDigest(in.Files)).Scan(&out.LaunchID)
	})
	if err != nil {
		return WorkflowAdmission{}, fmt.Errorf("store: admit workflow launch: %w", err)
	}
	return out, nil
}

// WorkflowPredecessor retains provenance and bounded owner input across a
// topology restart. Findings remain rooted in the predecessor's saved outputs.
func (db *DB) WorkflowPredecessor(ctx context.Context, instanceID int64) (int64, string, error) {
	var previous int64
	var reply string
	err := db.sql.QueryRowContext(ctx, `SELECT predecessor_id,reply_body FROM research_successors WHERE instance_id=?`, instanceID).Scan(&previous, &reply)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	return previous, reply, err
}

// ResearchLaunch and ResearchAdmission retain the Research policy-facing API.
type ResearchLaunch = WorkflowLaunch

// ResearchAdmission is the Research adapter result of shared workflow admission.
type ResearchAdmission = WorkflowAdmission

// AdmitResearchPath freezes the selected Research execution path for a session.
func (db *DB) AdmitResearchPath(ctx context.Context, sessionID int64, selected string) (string, error) {
	return db.AdmitWorkflowPath(ctx, sessionID, selected)
}

// LatestResearchInstance returns the session's most recent Research graph instance.
func (db *DB) LatestResearchInstance(ctx context.Context, sessionID int64) (GraphInstance, error) {
	return db.LatestWorkflowInstance(ctx, sessionID, "research")
}

// AdmitResearchLaunch admits only Research definitions through the shared lifecycle.
func (db *DB) AdmitResearchLaunch(ctx context.Context, in ResearchLaunch) (ResearchAdmission, error) {
	if in.Mode != "" && in.Mode != "research" {
		return ResearchAdmission{}, graphInvariant("Research definition mode mismatch")
	}
	in.Mode = "research"
	return db.AdmitWorkflowLaunch(ctx, in)
}

// LatestStandaloneBugInstance recovers only the admitted standalone graph pins.
func (db *DB) LatestStandaloneBugInstance(ctx context.Context, sessionID int64) (GraphInstance, error) {
	return scanGraphInstance(db.sql.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE session_id=? AND definition_id IN (SELECT id FROM graph_definitions WHERE workflow='bug' AND mode=?) ORDER BY id DESC LIMIT 1`, sessionID, standaloneGraphStorageKey))
}
