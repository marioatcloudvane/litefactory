package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/buganalysis"
)

// GraphDiagnosis is an immutable proposal retained in shared graph output history.
// Revision changes only when canonical content changes, not when it is rendered.
type GraphDiagnosis struct {
	Record          string                  `json:"record"`
	Revision        int64                   `json:"revision"`
	Digest          string                  `json:"digest"`
	Analysis        buganalysis.BugAnalysis `json:"analysis"`
	ReviewReady     bool                    `json:"review_ready"`
	SourceAttemptID int64                   `json:"source_attempt_id"`
	ResultID        int64                   `json:"result_id"`
	ResultRevision  int64                   `json:"result_revision"`
	At              time.Time               `json:"at"`
}

// GraphDiagnosisInput references the current accepted analyst submission; it
// cannot provide different proposal bytes or claim successful execution.
type GraphDiagnosisInput struct {
	InstanceID, ConversationAttemptID, SourceAttemptID int64
	ResultID, ResultRevision, ExpectedRevision         int64
	At                                                 time.Time
}

// GraphDiagnosisApproval is factory-authenticated authority, separate from an
// agent result. Historical approvals cannot authorize a superseding candidate.
type GraphDiagnosisApproval struct {
	Record                       string `json:"record"`
	Revision                     int64  `json:"revision"`
	Digest                       string `json:"digest"`
	Review                       string `json:"review"`
	QuestionID, OwnerID, ReplyID int64
	At                           time.Time `json:"at"`
}

func graphRecordTx(ctx context.Context, tx *sql.Tx, instance int64, record string, out interface{}) error {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT o.payload FROM graph_outputs o JOIN graph_node_attempts a ON a.id=o.node_attempt_id WHERE a.instance_id=? AND o.kind='json' AND json_valid(CAST(o.payload AS TEXT)) AND json_extract(CAST(o.payload AS TEXT),'$.record')=? ORDER BY o.id DESC LIMIT 1`, instance, record).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func graphBugInstanceTx(ctx context.Context, tx *sql.Tx, instance int64) (GraphInstance, error) {
	v, e := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, instance))
	if e != nil {
		return v, e
	}
	var workflow, task string
	var version int
	e = tx.QueryRowContext(ctx, `SELECT d.workflow,s.task_ref,d.profile_version FROM graph_definitions d JOIN sessions s ON s.id=? JOIN workflow_admissions w ON w.session_id=s.id WHERE d.id=? AND w.family='standalone'`, v.SessionID, v.DefinitionID).Scan(&workflow, &task, &version)
	if e != nil {
		return v, e
	}
	if workflow != "bug" || version != 2 || v.Status != GraphRunning {
		return v, graphInvariant("diagnosis requires admitted running Bug instance")
	}
	return v, nil
}

// CurrentGraphDiagnosis reads the current immutable candidate and eligible
// approval. A retained reshape suspends authority without deleting its history.
func (db *DB) CurrentGraphDiagnosis(ctx context.Context, instance int64) (candidate GraphDiagnosis, approval *GraphDiagnosisApproval, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		if _, e := graphBugInstanceTx(ctx, tx, instance); e != nil {
			return e
		}
		e := graphRecordTx(ctx, tx, instance, "diagnosis", &candidate)
		if errors.Is(e, ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		approval, e = graphDiagnosisApprovalTx(ctx, tx, instance, candidate)
		return e
	})
	return
}

func graphDiagnosisApprovalTx(ctx context.Context, tx *sql.Tx, instance int64, c GraphDiagnosis) (*GraphDiagnosisApproval, error) {
	var a GraphDiagnosisApproval
	e := graphRecordTx(ctx, tx, instance, "diagnosis_approval", &a)
	if errors.Is(e, ErrNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if a.Revision != c.Revision || a.Digest != c.Digest || !c.ReviewReady {
		return nil, nil
	}
	var invalidated int
	e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_outputs o JOIN graph_node_attempts n ON n.id=o.node_attempt_id WHERE n.instance_id=? AND o.kind='json' AND json_valid(CAST(o.payload AS TEXT)) AND json_extract(CAST(o.payload AS TEXT),'$.record')='diagnosis_reshape' AND json_extract(CAST(o.payload AS TEXT),'$.revision')=? AND o.id>(SELECT MAX(p.id) FROM graph_outputs p JOIN graph_node_attempts x ON x.id=p.node_attempt_id WHERE x.instance_id=? AND p.kind='json' AND json_valid(CAST(p.payload AS TEXT)) AND json_extract(CAST(p.payload AS TEXT),'$.record')='diagnosis_approval')`, instance, c.Revision, instance).Scan(&invalidated)
	if e != nil {
		return nil, e
	}
	if invalidated != 0 {
		return nil, nil
	}
	return &a, nil
}

// RetainGraphDiagnosis binds a candidate to an accepted result from the exact
// successful source attempt, before question/review publication. Replay is inert.
func (db *DB) RetainGraphDiagnosis(ctx context.Context, in GraphDiagnosisInput) (out GraphDiagnosis, err error) {
	if in.InstanceID < 1 || in.ConversationAttemptID < 1 || in.SourceAttemptID < 1 || in.ResultID < 1 || in.ResultRevision < 1 || in.ExpectedRevision < 0 || in.At.IsZero() {
		return out, graphInvariant("invalid diagnosis input")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		inst, e := graphBugInstanceTx(ctx, tx, in.InstanceID)
		if e != nil {
			return e
		}
		var prior GraphDiagnosis
		e = graphRecordTx(ctx, tx, in.InstanceID, "diagnosis", &prior)
		if e != nil && !errors.Is(e, ErrNotFound) {
			return e
		}
		if prior.SourceAttemptID == in.SourceAttemptID && prior.ResultID == in.ResultID && prior.ResultRevision == in.ResultRevision {
			out = prior
			return nil
		}
		if prior.Revision != in.ExpectedRevision {
			return graphInvariant("diagnosis revision changed")
		}
		source, e := scanGraphAttempt(tx.QueryRowContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE id=?`, in.SourceAttemptID))
		if e != nil {
			return e
		}
		current, e := scanGraphAttempt(tx.QueryRowContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE id=?`, in.ConversationAttemptID))
		if e != nil {
			return e
		}
		if source.InstanceID != inst.ID || source.Status != GraphSucceeded || source.RunID < 1 || current.InstanceID != inst.ID || current.Status != GraphRunning || current.Node != inst.CurrentNode {
			return graphInvariant("diagnosis source or conversation mismatch")
		}
		var raw []byte
		var run int64
		var workflow, input string
		e = tx.QueryRowContext(ctx, `SELECT v.snapshot,v.run_id,r.workflow,q.input_state FROM application_result_revisions v JOIN application_results r ON r.id=v.result_id JOIN application_result_requests q ON q.id=v.request_row WHERE v.result_id=? AND v.revision=? AND r.revision=v.revision`, in.ResultID, in.ResultRevision).Scan(&raw, &run, &workflow, &input)
		if e != nil {
			return e
		}
		if run != source.RunID || workflow != "bug" || input != "accepted" {
			return graphInvariant("diagnosis result authority mismatch")
		}
		var snap agentresult.Snapshot
		if e = json.Unmarshal(raw, &snap); e != nil {
			return e
		}
		p := snap.Result.Payload.Bug
		// The result was validated when the analyst submitted it; only what
		// routing needs is checked here.
		if p == nil || p.Analysis == nil || (p.Response != "ask" && p.Response != "done") {
			return graphInvariant("diagnosis needs an analyst proposal")
		}
		// The owner approves each revision explicitly, so a revised analysis is
		// recorded as submitted. Answer references that do not name a retained
		// owner reply are dropped rather than refusing the whole analysis.
		kept := p.Analysis.AnswerReferences[:0]
		for _, ref := range p.Analysis.AnswerReferences {
			var count int
			e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_owner_waits WHERE instance_id=? AND reply_id=? AND state IN ('claimed','allocated')`, inst.ID, strings.TrimPrefix(ref, "reply:")).Scan(&count)
			if e != nil {
				return e
			}
			if strings.HasPrefix(ref, "reply:") && count == 1 {
				kept = append(kept, ref)
			}
		}
		p.Analysis.AnswerReferences = kept
		digest, e := p.Analysis.Digest()
		if e != nil {
			return e
		}
		revision := prior.Revision
		if digest != prior.Digest {
			revision++
		}
		out = GraphDiagnosis{Record: "diagnosis", Revision: revision, Digest: digest, Analysis: *p.Analysis, ReviewReady: p.Response == "done", SourceAttemptID: source.ID, ResultID: in.ResultID, ResultRevision: in.ResultRevision, At: in.At}
		b, e := json.Marshal(out)
		if e != nil {
			return e
		}
		_, e = recordGraphOutputTx(ctx, tx, GraphOutput{NodeAttemptID: current.ID, Kind: GraphOutputJSON, Payload: b, CreatedAt: in.At})
		return e
	})
	if err != nil {
		return GraphDiagnosis{}, fmt.Errorf("store: retain graph diagnosis: %w", err)
	}
	return out, nil
}

// ApproveGraphDiagnosis consumes the exact owner command and records authority
// atomically. Ordinary guidance uses ClaimGraphOwnerReply and never calls this.
func (db *DB) ApproveGraphDiagnosis(ctx context.Context, in GraphOwnerReply, revision int64) (out GraphDiagnosisApproval, err error) {
	if revision < 1 || in.OwnerID < 1 || in.AuthorID != in.OwnerID || in.CommentID < 1 || in.QuestionCommentID < 1 || in.SourceRef != in.TaskRef || in.TaskRef == "" || in.Workflow != "bug" || in.Mode != "" || in.DeliveryKey == "" || in.At.IsZero() || in.Body != "/approve-bug analysis "+strconv.FormatInt(revision, 10) {
		return out, graphInvariant("exact authenticated current diagnosis approval required")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		out, err = approveGraphDiagnosisTx(ctx, tx, in, revision, false)
		return err
	})
	if err != nil {
		return GraphDiagnosisApproval{}, fmt.Errorf("store: approve graph diagnosis: %w", err)
	}
	return out, nil
}

func approveGraphDiagnosisTx(ctx context.Context, tx *sql.Tx, in GraphOwnerReply, revision int64, chat bool) (out GraphDiagnosisApproval, err error) {
	inst, e := graphBugInstanceTx(ctx, tx, in.InstanceID)
	if e != nil {
		return out, e
	}
	var candidate GraphDiagnosis
	if e = graphRecordTx(ctx, tx, inst.ID, "diagnosis", &candidate); e != nil {
		return out, e
	}
	if candidate.Revision != revision || !candidate.ReviewReady {
		return out, graphInvariant("approval candidate is stale or incomplete")
	}
	var boundTask string
	if e = tx.QueryRowContext(ctx, `SELECT task_ref FROM sessions WHERE id=?`, inst.SessionID).Scan(&boundTask); e != nil {
		return out, e
	}
	if boundTask != in.TaskRef {
		return out, graphInvariant("approval task mismatch")
	}
	var prior GraphDiagnosisApproval
	e = graphRecordTx(ctx, tx, inst.ID, "diagnosis_approval", &prior)
	if e == nil && prior.ReplyID == in.CommentID && prior.Review == in.DeliveryKey && prior.Revision == revision && prior.OwnerID == in.OwnerID {
		out = prior
		return out, nil
	}
	if e != nil && !errors.Is(e, ErrNotFound) {
		return out, e
	}
	wait, e := scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE delivery_key=? AND instance_id=?`, in.DeliveryKey, inst.ID))
	if e != nil {
		return out, e
	}
	var task, cause string
	var question int64
	var published bool
	e = tx.QueryRowContext(ctx, `SELECT s.task_ref,c.stop_cause,d.comment_id,d.completed FROM graph_comment_intents c JOIN report_deliveries d ON d.delivery_key=c.delivery_key JOIN sessions s ON s.id=? WHERE c.delivery_key=? AND c.instance_id=?`, inst.SessionID, in.DeliveryKey, inst.ID).Scan(&task, &cause, &question, &published)
	if e != nil {
		return out, e
	}
	if task != in.TaskRef || cause != "diagnosis_review" || !published || question != in.QuestionCommentID || (!chat && in.CommentID <= question) || wait.State != "waiting" || wait.OriginNode != inst.CurrentNode {
		return out, graphInvariant("approval does not match confirmed current review")
	}
	var reviewed struct {
		Diagnosis *GraphDiagnosis `json:"diagnosis"`
	}
	var bytes []byte
	if e = tx.QueryRowContext(ctx, `SELECT candidate FROM graph_comment_intents WHERE delivery_key=?`, in.DeliveryKey).Scan(&bytes); e != nil {
		return out, e
	}
	if e = json.Unmarshal(bytes, &reviewed); e != nil {
		return out, e
	}
	if reviewed.Diagnosis == nil || reviewed.Diagnosis.Revision != revision || reviewed.Diagnosis.Digest != candidate.Digest {
		return out, graphInvariant("review binding is superseded")
	}
	var running int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_node_attempts WHERE instance_id=? AND status='running'`, inst.ID).Scan(&running); e != nil {
		return out, e
	}
	if running != 0 {
		return out, ErrGraphAttemptRunning
	}
	out = GraphDiagnosisApproval{Record: "diagnosis_approval", Revision: revision, Digest: candidate.Digest, Review: in.DeliveryKey, QuestionID: question, OwnerID: in.OwnerID, ReplyID: in.CommentID, At: in.At}
	raw, e := json.Marshal(out)
	if e != nil {
		return out, e
	}
	if _, e = recordGraphOutputTx(ctx, tx, GraphOutput{NodeAttemptID: wait.SourceAttemptID, Kind: GraphOutputJSON, Payload: raw, OwnerCommentID: in.CommentID, CreatedAt: in.At}); e != nil {
		return out, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='claimed',reply_id=?,reply_body=?,reply_author_id=?,claimed_at=? WHERE delivery_key=? AND state='waiting'`, in.CommentID, in.Body, in.AuthorID, FormatTime(in.At), in.DeliveryKey); e != nil {
		return out, e
	}
	return out, setGraphInstanceTx(ctx, tx, inst.ID, wait.OriginNode, GraphRunning, in.At, time.Time{})
}

// GraphDiagnosisGuidance returns all consumed, authenticated replies in durable
// order. Preparation refuses overflow instead of silently dropping settled input.
func (db *DB) GraphDiagnosisGuidance(ctx context.Context, instance int64) (out []GraphOwnerWait, err error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE instance_id=? AND state IN ('claimed','allocated') ORDER BY rowid LIMIT 1025`, instance)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	size := 0
	for rows.Next() {
		v, e := scanGraphOwnerWait(rows)
		if e != nil {
			return nil, e
		}
		size += len(v.ReplyBody)
		if size > 1<<20 || len(out) >= 1024 {
			return nil, graphInvariant("retained conversation exceeds input bounds")
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SuspendGraphDiagnosis retains a reshape reason and approved reference. It
// suspends repair eligibility until a new reviewed candidate is approved.
func (db *DB) SuspendGraphDiagnosis(ctx context.Context, instance, source, revision int64, digest, reason string, at time.Time) error {
	if source < 1 || revision < 1 || reason == "" || at.IsZero() {
		return graphInvariant("invalid diagnosis reshape")
	}
	if len(reason) > 64<<10 {
		reason = strings.ToValidUTF8(reason[:64<<10], "")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if _, e := graphBugInstanceTx(ctx, tx, instance); e != nil {
			return e
		}
		var previous []byte
		readErr := tx.QueryRowContext(ctx, `SELECT o.payload FROM graph_outputs o JOIN graph_node_attempts n ON n.id=o.node_attempt_id WHERE n.instance_id=? AND o.kind='json' AND json_valid(CAST(o.payload AS TEXT)) AND json_extract(CAST(o.payload AS TEXT),'$.record')='diagnosis_reshape' AND json_extract(CAST(o.payload AS TEXT),'$.source_attempt_id')=? ORDER BY o.id DESC LIMIT 1`, instance, source).Scan(&previous)
		if readErr == nil {
			var saved struct {
				Revision       int64
				Digest, Reason string
			}
			if e := json.Unmarshal(previous, &saved); e != nil {
				return e
			}
			if saved.Revision != revision || saved.Digest != digest || saved.Reason != reason {
				return graphInvariant("reshape correction requires a new source")
			}
			return nil
		}
		if !errors.Is(readErr, sql.ErrNoRows) {
			return readErr
		}
		var c GraphDiagnosis
		if e := graphRecordTx(ctx, tx, instance, "diagnosis", &c); e != nil {
			return e
		}
		a, e := graphDiagnosisApprovalTx(ctx, tx, instance, c)
		if e != nil {
			return e
		}
		if a == nil || c.Revision != revision || c.Digest != digest {
			return graphInvariant("reshape lacks current approved scope")
		}
		var belongs int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_node_attempts WHERE id=? AND instance_id=? AND run_id IS NOT NULL AND status='succeeded' AND outcome='reshape'`, source, instance).Scan(&belongs); e != nil {
			return e
		}
		if belongs != 1 {
			return graphInvariant("reshape source has not succeeded")
		}
		b, e := json.Marshal(struct {
			Record          string `json:"record"`
			Revision        int64  `json:"revision"`
			SourceAttemptID int64  `json:"source_attempt_id"`
			Digest, Reason  string
			At              time.Time
		}{"diagnosis_reshape", revision, source, digest, reason, at})
		if e != nil {
			return e
		}
		_, e = recordGraphOutputTx(ctx, tx, GraphOutput{NodeAttemptID: source, Kind: GraphOutputJSON, Payload: b, CreatedAt: at})
		return e
	})
}

// GraphDiagnosisReshapes retains every reason the analyst must resolve. It
// returns immutable factory events, never grants approval or delivery authority.
func (db *DB) GraphDiagnosisReshapes(ctx context.Context, instance int64) (out []json.RawMessage, err error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT o.payload FROM graph_outputs o JOIN graph_node_attempts a ON a.id=o.node_attempt_id WHERE a.instance_id=? AND o.kind='json' AND json_valid(CAST(o.payload AS TEXT)) AND json_extract(CAST(o.payload AS TEXT),'$.record')='diagnosis_reshape' ORDER BY o.id LIMIT 1025`, instance)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	size := 0
	for rows.Next() {
		var b []byte
		if e := rows.Scan(&b); e != nil {
			return nil, e
		}
		size += len(b)
		// Context for a prompt: keep the oldest reasons that fit, never fail.
		if len(out) >= 1024 || size > 1<<20 {
			break
		}
		out = append(out, json.RawMessage(b))
	}
	return out, rows.Err()
}
