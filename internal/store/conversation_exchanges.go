package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ConversationExchange is private evidence for one bounded provider step.
// Request and Result contain validated, redacted JSON owned by the factory.
type ConversationExchange struct {
	TurnID                                       string
	Ordinal                                      int
	Kind, Request, Result, ProviderAttempt, Gaps string
	CapturedAt                                   time.Time
	Complete                                     bool
}

func conversationFence(ctx context.Context, tx *sql.Tx, t ConversationTurn) error {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM chat_conversation_turns t JOIN chat_conversations c ON c.id=t.conversation_id WHERE t.id=? AND t.conversation_id=? AND t.state='running' AND t.generation=? AND t.worker=? AND c.active=1`, t.ID, t.ConversationID, t.Generation, t.Worker).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

// SaveConversationExchange inserts a request before continuation. Its result may
// be filled exactly once; repeated input never replaces saved evidence.
func (db *DB) SaveConversationExchange(ctx context.Context, t ConversationTurn, x ConversationExchange) error {
	if x.TurnID != t.ID || x.Ordinal < 1 || x.Ordinal > 16 || x.Kind == "" || len(x.Kind) > 32 || len(x.Request) > 262144 || len(x.Result) > 65536 || len(x.Gaps) > 8192 || len(x.ProviderAttempt) > 256 || x.CapturedAt.IsZero() {
		return errors.New("invalid conversation exchange")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := conversationFence(ctx, tx, t); err != nil {
			return err
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO chat_conversation_exchanges(turn_id,ordinal,kind,request,result,provider_attempt,captured_at,complete,gaps) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(turn_id,ordinal) DO UPDATE SET result=excluded.result,complete=excluded.complete,gaps=excluded.gaps WHERE chat_conversation_exchanges.kind=excluded.kind AND chat_conversation_exchanges.request=excluded.request AND chat_conversation_exchanges.provider_attempt=excluded.provider_attempt AND (chat_conversation_exchanges.result='' OR (chat_conversation_exchanges.result=excluded.result AND chat_conversation_exchanges.complete=excluded.complete AND chat_conversation_exchanges.gaps=excluded.gaps))`, x.TurnID, x.Ordinal, x.Kind, x.Request, x.Result, x.ProviderAttempt, FormatTime(x.CapturedAt), x.Complete, x.Gaps)
		return chatAffected(r, err)
	})
}

// ListConversationExchanges returns the retained private steps linked to a turn.
func (db *DB) ListConversationExchanges(ctx context.Context, turnID string) ([]ConversationExchange, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT turn_id,ordinal,kind,request,result,provider_attempt,captured_at,complete,gaps FROM chat_conversation_exchanges WHERE turn_id=? ORDER BY ordinal`, turnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConversationExchange
	for rows.Next() {
		var x ConversationExchange
		var at string
		if err = rows.Scan(&x.TurnID, &x.Ordinal, &x.Kind, &x.Request, &x.Result, &x.ProviderAttempt, &at, &x.Complete, &x.Gaps); err != nil {
			return nil, err
		}
		x.CapturedAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ConversationReference binds source bytes or a target revision to its scope.
type ConversationReference struct {
	ID, TurnID, Kind, Repository, Target, Revision, Content string
	ExpiresAt                                               time.Time
}

// SaveConversationReference pins exact source data or a resolved target under a fenced turn.
func (db *DB) SaveConversationReference(ctx context.Context, t ConversationTurn, r ConversationReference) error {
	if r.ID == "" || len(r.ID) > 256 || r.TurnID != t.ID || r.Kind == "" || len(r.Kind) > 32 || len(r.Repository) > 512 || len(r.Target) > 512 || len(r.Revision) > 256 || len(r.Content) > 32768 || r.ExpiresAt.IsZero() {
		return errors.New("invalid conversation reference")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := conversationFence(ctx, tx, t); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO chat_conversation_references(id,turn_id,kind,repository,target,revision,content,expires_at) VALUES(?,?,?,?,?,?,?,?)`, r.ID, r.TurnID, r.Kind, r.Repository, r.Target, r.Revision, r.Content, FormatTime(r.ExpiresAt))
		return chatAffected(result, err)
	})
}

// ResolveConversationReference reads an unexpired reference only in its conversation scope.
func (db *DB) ResolveConversationReference(ctx context.Context, conversationID, id string, at time.Time) (ConversationReference, error) {
	var r ConversationReference
	var expires string
	err := db.sql.QueryRowContext(ctx, `SELECT r.id,r.turn_id,r.kind,r.repository,r.target,r.revision,r.content,r.expires_at FROM chat_conversation_references r JOIN chat_conversation_turns t ON t.id=r.turn_id JOIN chat_conversations c ON c.id=t.conversation_id WHERE r.id=? AND t.conversation_id=? AND c.active=1 AND r.expires_at>?`, id, conversationID, FormatTime(at)).Scan(&r.ID, &r.TurnID, &r.Kind, &r.Repository, &r.Target, &r.Revision, &r.Content, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	return r, err
}

// ConversationActionLink preserves proposal and saved effect identities independently of narration.
type ConversationActionLink struct {
	TurnID, DialogueID, ExecutionID, PreviewID, PayloadDigest, Outcome string
	DialogueRevision                                                   int64
}

// SaveConversationActionLink links a fenced turn to an immutable proposal or saved effect.
func (db *DB) SaveConversationActionLink(ctx context.Context, t ConversationTurn, a ConversationActionLink) error {
	if a.TurnID != t.ID || len(a.DialogueID) > 256 || len(a.ExecutionID) > 256 || len(a.PreviewID) > 256 || len(a.PayloadDigest) > 256 || len(a.Outcome) > 65536 {
		return errors.New("invalid conversation action link")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := conversationFence(ctx, tx, t); err != nil {
			return err
		}
		return saveConversationAction(ctx, tx, a)
	})
}
func saveConversationAction(ctx context.Context, tx *sql.Tx, a ConversationActionLink) error {
	r, err := tx.ExecContext(ctx, `INSERT INTO chat_conversation_actions(turn_id,dialogue_id,dialogue_revision,execution_id,preview_id,payload_digest,outcome) VALUES(?,?,?,?,?,?,?) ON CONFLICT(turn_id) DO UPDATE SET execution_id=excluded.execution_id,preview_id=excluded.preview_id,outcome=excluded.outcome WHERE chat_conversation_actions.dialogue_id=excluded.dialogue_id AND chat_conversation_actions.dialogue_revision=excluded.dialogue_revision AND chat_conversation_actions.payload_digest=excluded.payload_digest AND chat_conversation_actions.execution_id IN ('',excluded.execution_id) AND chat_conversation_actions.preview_id IN ('',excluded.preview_id) AND chat_conversation_actions.outcome IN ('',excluded.outcome)`, a.TurnID, a.DialogueID, a.DialogueRevision, a.ExecutionID, a.PreviewID, a.PayloadDigest, a.Outcome)
	return chatAffected(r, err)
}

// GetConversationActionLink reads proposal and execution evidence for a turn.
func (db *DB) GetConversationActionLink(ctx context.Context, turnID string) (ConversationActionLink, error) {
	var a ConversationActionLink
	err := db.sql.QueryRowContext(ctx, `SELECT turn_id,dialogue_id,dialogue_revision,execution_id,preview_id,payload_digest,outcome FROM chat_conversation_actions WHERE turn_id=?`, turnID).Scan(&a.TurnID, &a.DialogueID, &a.DialogueRevision, &a.ExecutionID, &a.PreviewID, &a.PayloadDigest, &a.Outcome)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return a, err
}

// ListConversationSources returns retained exact owner bytes, never reconstructed
// provider history, and nothing from before the last fresh conversation start.
func (db *DB) ListConversationSources(ctx context.Context, conversationID string, at time.Time, limit int) ([]ConversationTurn, error) {
	if limit < 1 || limit > 32 {
		return nil, errors.New("invalid conversation source limit")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+conversationTurnColumns+` FROM chat_conversation_turns WHERE conversation_id=? AND source<>'' AND sequence>(SELECT fresh_after FROM chat_conversations fresh WHERE fresh.id=chat_conversation_turns.conversation_id) AND (created_at>? OR EXISTS(SELECT 1 FROM chat_conversation_actions a JOIN chat_natural_dialogues d ON d.code=a.dialogue_id AND d.revision=a.dialogue_revision WHERE a.turn_id=chat_conversation_turns.id AND EXISTS(SELECT 1 FROM chat_conversations c WHERE c.id=chat_conversation_turns.conversation_id AND c.transport=d.transport AND c.owner_id=d.owner_id AND c.channel_ref=d.channel_ref) AND d.state IN ('awaiting_confirmation','authorized','executing'))) ORDER BY sequence DESC LIMIT ?`, conversationID, FormatTime(at.Add(-24*time.Hour)), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConversationTurn
	for rows.Next() {
		t, err := scanConversationTurn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// PurgeConversationEvidence retains pending delivery and active proposal sources.
func (db *DB) PurgeConversationEvidence(ctx context.Context, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		cutoff := FormatTime(at.Add(-24 * time.Hour))
		eligible := `SELECT t.id FROM chat_conversation_turns t WHERE t.delivered_at IS NOT NULL AND t.delivered_at<=? AND NOT EXISTS(SELECT 1 FROM chat_conversation_answers ans JOIN chat_conversation_details d ON d.answer_id=ans.id WHERE ans.turn_id=t.id AND (EXISTS(SELECT 1 FROM chat_inbox request WHERE request.transport=d.transport AND request.message_id=d.request_id AND request.state IN ('pending','sent')) OR EXISTS(SELECT 1 FROM chat_conversation_details_parts part WHERE part.transport=d.transport AND part.request_id=d.request_id AND part.acknowledged_at IS NULL))) AND NOT EXISTS(SELECT 1 FROM chat_conversation_actions a JOIN chat_natural_dialogues d ON d.code=a.dialogue_id AND d.revision=a.dialogue_revision WHERE a.turn_id=t.id AND EXISTS(SELECT 1 FROM chat_conversations c WHERE c.id=t.conversation_id AND c.transport=d.transport AND c.owner_id=d.owner_id AND c.channel_ref=d.channel_ref) AND d.state IN ('awaiting_confirmation','authorized','executing'))`
		if _, err := tx.ExecContext(ctx, `UPDATE chat_conversation_answers SET blocks='',expired_at=? WHERE turn_id IN (`+eligible+`) AND expired_at IS NULL`, FormatTime(at), cutoff); err != nil {
			return err
		}
		for _, q := range []string{`DELETE FROM chat_conversation_exchanges WHERE turn_id IN (` + eligible + `)`, `DELETE FROM chat_conversation_references WHERE turn_id IN (` + eligible + `)`, `UPDATE chat_conversation_actions SET outcome='' WHERE turn_id IN (` + eligible + `)`, `UPDATE chat_conversation_turns SET source='',reply_to='' WHERE id IN (` + eligible + `)`} {
			if _, err := tx.ExecContext(ctx, q, cutoff); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListConversationReferences returns bounded unexpired references in one conversation.
func (db *DB) ListConversationReferences(ctx context.Context, conversationID string, at time.Time, limit int) ([]ConversationReference, error) {
	if limit < 1 || limit > 64 {
		return nil, errors.New("invalid conversation reference limit")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT r.id,r.turn_id,r.kind,r.repository,r.target,r.revision,r.content,r.expires_at FROM chat_conversation_references r JOIN chat_conversation_turns t ON t.id=r.turn_id WHERE t.conversation_id=? AND r.expires_at>? ORDER BY t.sequence DESC,r.id LIMIT ?`, conversationID, FormatTime(at), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConversationReference
	for rows.Next() {
		var r ConversationReference
		var expires string
		if err = rows.Scan(&r.ID, &r.TurnID, &r.Kind, &r.Repository, &r.Target, &r.Revision, &r.Content, &expires); err != nil {
			return nil, err
		}
		r.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FinishConversationActionReply saves an existing result and clears executable
// inputs without admitting a second ordinary delivery. The current turn may
// consume an asynchronous result from an earlier turn in the same conversation.
func (db *DB) FinishConversationActionReply(ctx context.Context, t ConversationTurn, r ChatReply, at time.Time) error {
	if r.ConversationTurnID == "" || len(r.ReplyText) > 65536 || at.IsZero() {
		return errors.New("invalid conversation action result")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := conversationFence(ctx, tx, t); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE chat_inbox SET state='confirmed',arguments='{}',question='',project_option='',target_ref='',reply_text='',outcome_status=?,finished_at=COALESCE(finished_at,?) WHERE transport=? AND message_id=? AND conversation_turn_id=? AND (state IN ('pending','sent','answering') OR (?='confirmed' AND state='confirmed' AND command='diagnose' AND reply_text<>'')) AND EXISTS(SELECT 1 FROM chat_conversation_turns origin WHERE origin.id=conversation_turn_id AND origin.conversation_id=?)`, r.OutcomeStatus, FormatTime(at), r.Transport, r.MessageID, r.ConversationTurnID, r.State, t.ConversationID)
		if err = chatAffected(result, err); err != nil {
			return err
		}
		result, err = tx.ExecContext(ctx, `UPDATE chat_conversation_actions SET outcome=? WHERE turn_id=? AND execution_id=? AND outcome IN ('',?)`, r.ReplyText, r.ConversationTurnID, r.MessageID, r.ReplyText)
		return chatAffected(result, err)
	})
}

// ListPendingConversationActionOutcomes finds asynchronous results ready for
// narration on the next eligible session step; reading them does not execute.
// A confirmed diagnosis was already posted to the channel and is context only.
func (db *DB) ListPendingConversationActionOutcomes(ctx context.Context, conversationID string) ([]ChatReply, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+chatColumns+` FROM chat_inbox WHERE conversation_turn_id IN (SELECT id FROM chat_conversation_turns WHERE conversation_id=?) AND (state='pending' OR (state='confirmed' AND command='diagnose')) AND reply_text<>'' AND EXISTS(SELECT 1 FROM chat_conversation_actions a JOIN chat_natural_executions e ON e.identity=a.execution_id JOIN chat_conversations c ON c.id=? WHERE a.turn_id=chat_inbox.conversation_turn_id AND a.execution_id=chat_inbox.message_id AND e.transport=chat_inbox.transport AND e.owner_id=c.owner_id AND e.channel_ref=c.channel_ref AND e.transport=c.transport AND e.state IN ('executing','finished','uncertain')) ORDER BY created_at,message_id LIMIT 16`, conversationID, conversationID)
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

// RecoverConversationActionOutcome is used only after the previous provider
// process is proven stopped. It never resumes an effect or provider input.
func (db *DB) RecoverConversationActionOutcome(ctx context.Context, t ConversationTurn, at time.Time) (string, error) {
	if at.IsZero() {
		return "", errors.New("invalid conversation recovery time")
	}
	var outcome string
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := conversationFence(ctx, tx, t); err != nil {
			return err
		}
		var identity, linked string
		err := tx.QueryRowContext(ctx, `SELECT execution_id,outcome FROM chat_conversation_actions WHERE turn_id=? AND execution_id<>''`, t.ID).Scan(&identity, &linked)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var state, saved, command, inboxState, inboxText string
		err = tx.QueryRowContext(ctx, `SELECT e.state,e.outcome,COALESCE(i.command,''),COALESCE(i.state,''),COALESCE(i.reply_text,'') FROM chat_natural_executions e LEFT JOIN chat_inbox i ON i.transport=e.transport AND i.message_id=e.identity AND i.conversation_turn_id=? WHERE e.identity=? AND EXISTS(SELECT 1 FROM chat_conversations c WHERE c.id=? AND c.transport=e.transport AND c.owner_id=e.owner_id AND c.channel_ref=e.channel_ref)`, t.ID, identity, t.ConversationID).Scan(&state, &saved, &command, &inboxState, &inboxText)
		if errors.Is(err, sql.ErrNoRows) {
			if linked != "" {
				outcome = linked
				return nil
			}
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		terminal := ""
		switch state {
		case "finished", "uncertain":
			outcome = saved
		case "authorized":
			outcome = "The confirmed action was interrupted before execution began. No change was applied."
			terminal = "finished"
		case "executing":
			if command == "diagnose" && inboxState == "working" {
				outcome = "The confirmed diagnosis is still queued or running. Its saved result will be reported in this conversation when available."
				return nil
			}
			if command == "diagnose" && inboxState == "pending" && inboxText != "" {
				outcome = inboxText
				terminal = "finished"
			} else {
				outcome = "The confirmed action was interrupted after execution began. Its effect is uncertain; it was not replayed."
				terminal = "uncertain"
			}
		default:
			return errors.New("invalid saved conversation execution state")
		}
		if outcome == "" {
			outcome = linked
		}
		if outcome == "" {
			outcome = "The action has a saved terminal state, but its outcome text is unavailable. It was not replayed."
		}
		if terminal != "" {
			r, err := tx.ExecContext(ctx, `UPDATE chat_natural_executions SET state=?,outcome=?,payload='{}',finished_at=? WHERE identity=? AND state=?`, terminal, outcome, FormatTime(at), identity, state)
			if err = chatAffected(r, err); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE chat_natural_dialogues SET state=?,payload='{}',digest='',code='',prompt_id='',updated_at=? WHERE (transport,owner_id,channel_ref,revision) IN (SELECT transport,owner_id,channel_ref,revision FROM chat_natural_executions WHERE identity=?)`, terminal, FormatTime(at), identity); err != nil {
				return err
			}
		}
		r, err := tx.ExecContext(ctx, `UPDATE chat_conversation_actions SET outcome=? WHERE turn_id=? AND execution_id=? AND outcome IN ('',?)`, outcome, t.ID, identity, outcome)
		if err = chatAffected(r, err); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE chat_inbox SET state='confirmed',arguments='{}',question='',project_option='',target_ref='',reply_text='',finished_at=? WHERE message_id=? AND conversation_turn_id=? AND state IN ('pending','sent','answering')`, FormatTime(at), identity, t.ID)
		return err
	})
	return outcome, err
}

// StartConversationActionReply claims only the current turn's authorized row
// for existing action handlers; no background delivery worker can claim it.
func (db *DB) StartConversationActionReply(ctx context.Context, t ConversationTurn, r ChatReply, at time.Time) (ChatReply, error) {
	var claimed ChatReply
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := conversationFence(ctx, tx, t); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE chat_inbox SET state='sent',attempts=attempts+1,next_attempt_at=? WHERE transport=? AND message_id=? AND conversation_turn_id=? AND state IN ('pending','answering') AND EXISTS(SELECT 1 FROM chat_natural_executions e WHERE e.identity=message_id AND e.transport=chat_inbox.transport AND e.state='authorized')`, FormatTime(at.Add(time.Minute)), r.Transport, r.MessageID, t.ID)
		if err = chatAffected(result, err); err != nil {
			return err
		}
		claimed, err = scanChat(tx.QueryRowContext(ctx, `SELECT `+chatColumns+` FROM chat_inbox WHERE transport=? AND message_id=?`, r.Transport, r.MessageID))
		return err
	})
	return claimed, err
}

// LatestConversationTarget reads the newest target reference of a
// conversation, expired or not: the project and issue it discussed last.
// Newest mention wins. It is context only; callers re-resolve the project.
func (db *DB) LatestConversationTarget(ctx context.Context, conversationID string) (ConversationReference, error) {
	var r ConversationReference
	var expires string
	err := db.sql.QueryRowContext(ctx, `SELECT r.id,r.turn_id,r.kind,r.repository,r.target,r.revision,r.content,r.expires_at FROM chat_conversation_references r JOIN chat_conversation_turns t ON t.id=r.turn_id WHERE t.conversation_id=? AND r.kind='target' ORDER BY t.sequence DESC,r.rowid DESC LIMIT 1`, conversationID).Scan(&r.ID, &r.TurnID, &r.Kind, &r.Repository, &r.Target, &r.Revision, &r.Content, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	return r, err
}
