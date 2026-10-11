package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrGraphOwnerWait prevents dispatch while an owner question has no admitted reply.
var ErrGraphOwnerWait = errors.New("store: graph awaits an authorized owner reply")

// ErrGraphReplyPredatesQuestion refuses an otherwise authenticated owner
// comment that was written before the open question was published, for
// example while a node was still running between two questions. Unlike a
// running attempt it is permanent: the comment can never answer this question,
// so the caller must refuse it visibly and consume it rather than retry it.
// It is deliberately distinct from ErrGraphInvariant, which still covers every
// other mismatch (task, mode, instance status or incomplete publication).
var ErrGraphReplyPredatesQuestion = errors.New("store: owner comment predates the published graph question")

// GraphOwnerWait retains a reply until its continuation attempt is allocated.
// Publication is read from the original report journal, never inferred from state.
type GraphOwnerWait struct {
	DeliveryKey                 string
	InstanceID, SourceAttemptID int64
	State, ReplySource          string
	OriginNode                  string
	AllowRunless                bool
	ReplyID                     int64
	ReplyBody                   string
	ReplyAuthorID               int64
	ClaimedAt                   time.Time
	ContinuationAttemptID       int64
	AllowanceAttemptID          int64
}

const graphWaitColumns = `delivery_key,instance_id,COALESCE(source_attempt_id,0),state,COALESCE(reply_id,0),COALESCE(reply_body,''),COALESCE(reply_author_id,0),claimed_at,COALESCE(continuation_attempt_id,0),COALESCE(origin_node,''),allow_runless,COALESCE(allowance_attempt_id,0),reply_source`

func scanGraphOwnerWait(row scanner) (GraphOwnerWait, error) {
	var out GraphOwnerWait
	var at sql.NullString
	err := row.Scan(&out.DeliveryKey, &out.InstanceID, &out.SourceAttemptID, &out.State, &out.ReplyID, &out.ReplyBody, &out.ReplyAuthorID, &at, &out.ContinuationAttemptID, &out.OriginNode, &out.AllowRunless, &out.AllowanceAttemptID, &out.ReplySource)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out.ClaimedAt, err = scanTime(at)
	return out, err
}

// GetGraphOwnerWait reads the latest durable question and reply state for an instance.
func (db *DB) GetGraphOwnerWait(ctx context.Context, instanceID int64) (GraphOwnerWait, error) {
	return scanGraphOwnerWait(db.sql.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE instance_id=? ORDER BY rowid DESC LIMIT 1`, instanceID))
}

// GraphOwnerReply is constructed by authenticated daemon admission, not agent output.
// OwnerID is the configured numeric identity, SourceRef the observed issue source.
type GraphOwnerReply struct {
	InstanceID, OwnerID, AuthorID, CommentID, QuestionCommentID int64
	TaskRef, SourceRef, Mode, Workflow, DeliveryKey, Body       string
	At                                                          time.Time
}

// ClaimGraphOwnerReply authenticates and durably reserves an explicit reply once.
// It neither starts a process nor consumes the claim: allocation does that in the
// same transaction as StartGraphNodeAttempt. Replayed events are refused.
func (db *DB) ClaimGraphOwnerReply(ctx context.Context, in GraphOwnerReply) (out GraphOwnerWait, err error) {
	return db.claimGraphOwnerReply(ctx, in, false)
}

func (db *DB) claimGraphOwnerReply(ctx context.Context, in GraphOwnerReply, chat bool) (out GraphOwnerWait, err error) {
	if in.OwnerID < 1 || in.AuthorID != in.OwnerID || in.CommentID < 1 || (in.QuestionCommentID < 1 && in.Workflow != "bug") || (strings.TrimSpace(in.Mode) == "" && in.Workflow != "bug") || in.TaskRef == "" || in.SourceRef != in.TaskRef || in.DeliveryKey == "" || strings.TrimSpace(in.Body) == "" || len(in.Body) > 65536 || in.At.IsZero() {
		return out, graphInvariant("invalid authenticated graph reply")
	}
	if chat {
		if in.Mode != "research" || in.Workflow != "research" || !IsChatTrigger(in.CommentID) {
			return out, graphInvariant("chat may answer only Research questions")
		}
	} else if IsPrivateTrigger(in.CommentID) {
		return out, graphInvariant("forge reply cannot use a private identity")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		wait, e := scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE delivery_key=? AND instance_id=?`, in.DeliveryKey, in.InstanceID))
		if e != nil {
			return e
		}
		if in.Workflow == "bug" && wait.OriginNode == "" {
			return graphInvariant("legacy Bug waits do not use shared owner continuation")
		}
		if wait.State != "waiting" {
			return ErrGraphOwnerWait
		}
		var task, mode, workflow, status, node string
		var question int64
		var completed bool
		e = tx.QueryRowContext(ctx, `SELECT s.task_ref,CASE WHEN d.workflow='bug' THEN '' ELSE d.mode END,COALESCE(NULLIF(d.workflow,''),d.mode),i.status,COALESCE(w.origin_node,a.node),COALESCE(r.comment_id,0),COALESCE(r.completed,0) FROM graph_instances i JOIN sessions s ON s.id=i.session_id JOIN graph_definitions d ON d.id=i.definition_id JOIN graph_owner_waits w ON w.instance_id=i.id AND w.delivery_key=? LEFT JOIN graph_node_attempts a ON a.id=w.source_attempt_id LEFT JOIN report_deliveries r ON r.delivery_key=w.delivery_key WHERE i.id=?`, wait.DeliveryKey, in.InstanceID).Scan(&task, &mode, &workflow, &status, &node, &question, &completed)
		if e != nil {
			return e
		}
		replyWorkflow := in.Workflow
		if replyWorkflow == "" {
			replyWorkflow = in.Mode
		}
		ownerStop, e := validateOwnerStopReplyTx(ctx, tx, wait, in)
		if e != nil {
			return e
		}
		if task != in.TaskRef || mode != in.Mode || workflow != replyWorkflow || status != GraphRunning || (!ownerStop && (!completed || question < 1 || question != in.QuestionCommentID)) {
			return graphInvariant("reply does not match published active graph question")
		}
		var running int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_node_attempts WHERE instance_id=? AND status='running'`, in.InstanceID).Scan(&running); e != nil {
			return e
		}
		if running != 0 {
			return ErrGraphAttemptRunning
		}
		// The wait is open and its question is published and completed, but this
		// comment is not newer than that question: it was written before the
		// question existed and can never answer it.
		if !chat && !ownerStop && in.CommentID <= question {
			return ErrGraphReplyPredatesQuestion
		}
		if chat {
			var ordinal int64
			if e = tx.QueryRowContext(ctx, `SELECT rowid FROM graph_owner_waits WHERE delivery_key=?`, wait.DeliveryKey).Scan(&ordinal); e != nil {
				return e
			}
			in.CommentID = chatTriggerBase + ordinal
			if !IsChatTrigger(in.CommentID) {
				return graphInvariant("chat reply identity exhausted")
			}
			if e = validateChatResearchWaitTx(ctx, tx, wait); e != nil {
				return e
			}
		}
		source := "forge"
		if chat {
			source = "chat"
		}
		_, e = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='claimed',reply_id=?,reply_body=?,reply_author_id=?,claimed_at=?,reply_source=? WHERE delivery_key=?`, in.CommentID, in.Body, in.AuthorID, FormatTime(in.At), source, in.DeliveryKey)
		if e != nil {
			return e
		}
		if e = setGraphInstanceTx(ctx, tx, in.InstanceID, node, GraphRunning, in.At, time.Time{}); e != nil {
			return e
		}
		if chat {
			if e = parkChatResearchReplyTx(ctx, tx, wait.InstanceID, node, in.At); e != nil {
				return e
			}
		}
		out, e = scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE delivery_key=?`, in.DeliveryKey))
		return e
	})
	if err != nil {
		return out, fmt.Errorf("store: claim graph reply: %w", err)
	}
	return out, nil
}

func graphReplyAllocationTx(ctx context.Context, tx *sql.Tx, in GraphAttemptStart, attemptID int64) error {
	wait, err := scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE instance_id=? AND state IN ('waiting','claimed')`, in.InstanceID))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if wait.State != "claimed" {
		return ErrGraphOwnerWait
	}
	origin := wait.OriginNode
	if origin == "" {
		if err = tx.QueryRowContext(ctx, `SELECT node FROM graph_node_attempts WHERE id=?`, wait.SourceAttemptID).Scan(&origin); err != nil {
			return err
		}
	}
	if in.Node != origin || (in.RunID < 1 && !wait.AllowRunless) {
		return graphInvariant("reply allocation must resume the admitted origin and execution kind")
	}
	_, err = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='allocated',continuation_attempt_id=? WHERE delivery_key=?`, attemptID, wait.DeliveryKey)
	return err
}

// validateOwnerStopReplyTx recognizes only retained factory stop data bound to
// this attempt's settled run. Ordinary graph questions keep publication checks.
func validateOwnerStopReplyTx(ctx context.Context, tx *sql.Tx, wait GraphOwnerWait, in GraphOwnerReply) (bool, error) {
	if in.Workflow != "bug" || wait.SourceAttemptID == 0 {
		return false, nil
	}
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT candidate FROM graph_comment_intents WHERE delivery_key=? AND instance_id=? AND source_attempt_id=?`, wait.DeliveryKey, wait.InstanceID, wait.SourceAttemptID).Scan(&raw); err != nil {
		return false, err
	}
	var candidate struct {
		OwnerStop *OwnerStop `json:"owner_stop"`
	}
	if err := json.Unmarshal(raw, &candidate); err != nil {
		return false, err
	}
	if candidate.OwnerStop == nil {
		return false, nil
	}
	var runID int64
	if err := tx.QueryRowContext(ctx, `SELECT run_id FROM graph_node_attempts WHERE id=? AND instance_id=? AND node=? AND status<>'running'`, wait.SourceAttemptID, wait.InstanceID, wait.OriginNode).Scan(&runID); err != nil {
		return false, err
	}
	run, err := scanRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id=?`, runID))
	if err != nil {
		return false, err
	}
	stop, ok := run.OwnerStop()
	if !ok || stop != *candidate.OwnerStop || run.FinishedAt.IsZero() {
		return false, graphInvariant("owner stop source has not settled")
	}
	var created string
	var author int64
	var body, classification string
	if err := tx.QueryRowContext(ctx, `SELECT created_at,author_user_id,body,classification FROM comments WHERE session_id=? AND forge_comment_id=?`, run.SessionID, in.CommentID).Scan(&created, &author, &body, &classification); err != nil {
		return false, err
	}
	at, err := ParseTime(created)
	if err != nil {
		return false, err
	}
	if author != in.OwnerID || body != in.Body || classification != string(ClassificationInstruction) {
		return false, graphInvariant("owner stop reply source mismatch")
	}
	if stop.CommandID == 0 && !at.After(stop.At) {
		return false, ErrGraphReplyPredatesQuestion
	}
	if stop.CommandID != 0 {
		var commentID int64
		var task, state string
		if err := tx.QueryRowContext(ctx, `SELECT comment_id,task_ref,state FROM doctor_inbox WHERE id=? AND operation='stop'`, stop.CommandID).Scan(&commentID, &task, &state); err != nil {
			return false, err
		}
		if in.CommentID <= commentID {
			return false, ErrGraphReplyPredatesQuestion
		}
		if task != in.TaskRef || state != "completed" {
			return false, graphInvariant("owner stop command has not completed")
		}
		var total, pending int
		var lastDelivery int64
		if err := tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(CASE WHEN completed=0 OR comment_id IS NULL THEN 1 ELSE 0 END),0),COALESCE(max(comment_id),0) FROM report_deliveries WHERE doctor_command_id=?`, stop.CommandID).Scan(&total, &pending, &lastDelivery); err != nil {
			return false, err
		}
		if total == 0 || pending != 0 {
			return false, graphInvariant("owner stop acknowledgment remains owed")
		}
		if in.CommentID <= commentID || in.CommentID <= lastDelivery {
			return false, ErrGraphReplyPredatesQuestion
		}
	}
	return true, nil
}
