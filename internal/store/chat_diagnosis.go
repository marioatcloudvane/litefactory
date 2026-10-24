package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
)

// DeferChatDiagnosis keeps only the canonical target until its summary is ready.
func (db *DB) DeferChatDiagnosis(ctx context.Context, transport, id, target string, at time.Time) error {
	return db.DeferChatDiagnosisRepository(ctx, transport, id, target, at, 0)
}

// DeferChatDiagnosisRepository preserves genuine source ownership while the
// asynchronous diagnosis runs; no database cursor crosses the provider call.
func (db *DB) DeferChatDiagnosisRepository(ctx context.Context, transport, id, target string, at time.Time, repoID int64) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET state='working',target_ref=?,arguments='{}',next_attempt_at=?,resolved_repo_id=NULLIF(?,0) WHERE transport=? AND message_id=? AND state='sent' AND command='diagnose'`, target, FormatTime(at), repoID, transport, id)
	return chatAffected(result, err)
}

// RecoverChatDiagnoses restores abandoned leases only at daemon startup.
func (db *DB) RecoverChatDiagnoses(ctx context.Context, transport string, at time.Time) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET next_attempt_at=? WHERE transport=? AND state='working' AND command='diagnose'`, FormatTime(at), transport)
	return err
}

// ClaimChatDiagnosis fences one provider invocation. The existing bounded
// generation counter also bounds a diagnosis to one restart retry.
func (db *DB) ClaimChatDiagnosis(ctx context.Context, transport string, at time.Time) (r ChatReply, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		r, e = scanChat(tx.QueryRowContext(ctx, `SELECT `+chatColumns+` FROM chat_inbox WHERE transport=? AND command='diagnose' AND state='working' AND next_attempt_at<=? ORDER BY created_at,message_id LIMIT 1`, transport, FormatTime(at)))
		if e != nil {
			return e
		}
		if r.AnswerAttempts < 2 {
			r.AnswerAttempts++
		} else {
			r.State = "exhausted"
		}
		_, e = tx.ExecContext(ctx, `UPDATE chat_inbox SET answer_attempts=?,next_attempt_at=? WHERE transport=? AND message_id=?`, r.AnswerAttempts, FormatTime(at.Add(24*time.Hour)), r.Transport, r.MessageID)
		return e
	})
	return r, err
}

// RequeueChatDiagnosisBusy spends no restart allowance when no work began.
func (db *DB) RequeueChatDiagnosisBusy(ctx context.Context, r ChatReply, at time.Time) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET answer_attempts=answer_attempts-1,next_attempt_at=? WHERE transport=? AND message_id=? AND state='working' AND answer_attempts=?`, FormatTime(at), r.Transport, r.MessageID, r.AnswerAttempts)
	return chatAffected(result, err)
}

// CompleteChatDiagnosis makes the final summary eligible for ordinary delivery.
func (db *DB) CompleteChatDiagnosis(ctx context.Context, transport, id string, attempt int, text string, at time.Time) error {
	return db.CompleteChatDiagnosisResult(ctx, transport, id, attempt, text, at, "uncertain")
}

// CompleteChatDiagnosisResult retains the source-declared diagnostic status.
func (db *DB) CompleteChatDiagnosisResult(ctx context.Context, transport, id string, attempt int, text string, at time.Time, status string) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_inbox SET state='pending',reply_text=?,next_attempt_at=?,outcome_status=? WHERE transport=? AND message_id=? AND state='working' AND answer_attempts=?`, text, FormatTime(at), status, transport, id, attempt)
	return chatAffected(result, err)
}

// ListConversationDiagnosisResults finds completed conversation diagnoses whose
// result has not yet been posted to the owner's channel.
func (db *DB) ListConversationDiagnosisResults(ctx context.Context, transport string, limit int) ([]ChatReply, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+chatColumns+` FROM chat_inbox WHERE transport=? AND command='diagnose' AND conversation_turn_id<>'' AND state='pending' AND reply_text<>'' ORDER BY created_at,message_id LIMIT ?`, transport, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatReply
	for rows.Next() {
		r, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PushConversationDiagnosis queues the result's channel messages and finishes
// the inbox row and its execution in one transaction, so a result is posted
// once even across restarts. The row keeps its text: the next conversation
// turn still receives it as context, marked as already posted.
func (db *DB) PushConversationDiagnosis(ctx context.Context, r ChatReply, notices []ChatNotice, at time.Time) error {
	if r.ConversationTurnID == "" || len(notices) == 0 || at.IsZero() {
		return errors.New("invalid conversation diagnosis push")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE chat_inbox SET state='confirmed',finished_at=? WHERE transport=? AND message_id=? AND command='diagnose' AND conversation_turn_id=? AND state='pending' AND reply_text<>''`, FormatTime(at), r.Transport, r.MessageID, r.ConversationTurnID)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n == 0 {
			// Already posted, or a conversation turn consumed it first.
			return err
		}
		if err = insertChatNotices(ctx, tx, r.Transport, notices); err != nil {
			return err
		}
		state := "finished"
		if r.OutcomeStatus == "uncertain" {
			state = "uncertain"
		}
		if _, err = tx.ExecContext(ctx, `UPDATE chat_natural_executions SET state=?,outcome=?,payload='{}',finished_at=? WHERE identity=? AND state IN ('authorized','executing')`, state, r.ReplyText, FormatTime(at), r.MessageID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE chat_natural_dialogues SET state=?,payload='{}',digest='',code='',prompt_id='',updated_at=? WHERE state IN ('authorized','executing') AND (transport,owner_id,channel_ref,revision) IN (SELECT transport,owner_id,channel_ref,revision FROM chat_natural_executions WHERE identity=?)`, state, FormatTime(at), r.MessageID)
		return err
	})
}

// chatDoctorCommentBase reserves doctor_inbox comment identities for repair
// proposals that a chat diagnosis presents on the issue. They have no source
// comment; GitHub comment IDs stay far below this range.
const chatDoctorCommentBase int64 = 1 << 62

// PresentChatDiagnosis retains a chat diagnosis's repair proposal as a completed
// doctor command, so the ordinary doctor publication posts it on the issue
// where `/doctor apply` can approve it. A proposal is presented once.
func (db *DB) PresentChatDiagnosis(ctx context.Context, c DoctorCommand) error {
	ref, err := naming.ParseRef(c.TaskRef)
	if err != nil {
		return err
	}
	if c.ProposalID <= 0 || c.RepoID <= 0 || c.IssueID <= 0 || c.ActorID <= 0 || c.CreatedAt.IsZero() || c.IssueNumber != ref.Issue || c.ResultBody == "" || len(c.ResultBody) > 262144 {
		return errors.New("invalid chat doctor presentation")
	}
	comment := chatDoctorCommentBase + c.ProposalID
	result, err := db.sql.ExecContext(ctx, `INSERT INTO doctor_inbox(repo_id,task_ref,issue_id,issue_number,comment_id,actor_id,created_at,operation,instructions,state,result_body,conversation_id,proposal_id) SELECT ?,?,?,?,?,?,?,'diagnose',?,'completed',?,p.conversation_id,p.id FROM doctor_proposals p JOIN doctor_conversations c ON c.id=p.conversation_id JOIN doctor_attempts a ON a.id=p.attempt_id WHERE p.id=? AND c.task_ref=? AND a.source_id LIKE 'chat:%' ON CONFLICT(repo_id,comment_id) DO NOTHING`, c.RepoID, c.TaskRef, c.IssueID, c.IssueNumber, comment, c.ActorID, FormatTime(c.CreatedAt), c.Instructions, c.ResultBody, c.ProposalID, c.TaskRef)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n > 0 {
		return err
	}
	var count int
	if err = db.sql.QueryRowContext(ctx, `SELECT count(*) FROM doctor_inbox WHERE repo_id=? AND comment_id=? AND proposal_id=?`, c.RepoID, comment, c.ProposalID).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return errors.New("chat doctor proposal does not belong to the diagnosed issue")
	}
	return nil
}

// ChatDiagnosisPresentation is the issue presentation of a chat diagnosis's
// repair proposal; CommentID is its first part once posted.
type ChatDiagnosisPresentation struct {
	CommandID, CommentID int64
	TaskRef              string
	Complete             bool
}

// ReadChatDiagnosisPresentation finds the presentation of the proposal made by
// the doctor attempt with one of the given chat source IDs.
func (db *DB) ReadChatDiagnosisPresentation(ctx context.Context, sources ...string) (ChatDiagnosisPresentation, error) {
	var p ChatDiagnosisPresentation
	for _, source := range sources {
		err := db.sql.QueryRowContext(ctx, `SELECT i.id,i.task_ref FROM doctor_inbox i JOIN doctor_proposals p ON p.id=i.proposal_id JOIN doctor_attempts a ON a.id=p.attempt_id WHERE a.source_id=? AND i.comment_id>=? ORDER BY i.id DESC LIMIT 1`, source, chatDoctorCommentBase).Scan(&p.CommandID, &p.TaskRef)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return p, err
		}
		if p.Complete, err = db.DoctorPresentationComplete(ctx, p.CommandID); err != nil {
			return p, err
		}
		err = db.sql.QueryRowContext(ctx, `SELECT COALESCE(d.comment_id,0) FROM doctor_presentations p JOIN report_deliveries d ON d.delivery_key=p.delivery_key WHERE p.command_id=? AND p.part=1`, p.CommandID).Scan(&p.CommentID)
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
		return p, err
	}
	return p, ErrNotFound
}

// ListChatResearchClaims finds notes still owed, even after a claim was allocated.
func (db *DB) ListChatResearchClaims(ctx context.Context, limit int) ([]GraphOwnerWait, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE reply_source='chat' AND reply_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM report_deliveries r WHERE r.delivery_key='chat-owner:'||graph_owner_waits.delivery_key AND r.completed=1) ORDER BY rowid LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []GraphOwnerWait
	for rows.Next() {
		r, e := scanGraphOwnerWait(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
