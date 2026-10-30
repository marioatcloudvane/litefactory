package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

// NaturalDialogue is the single current protected proposal or clarification.
type NaturalDialogue struct {
	Transport, OwnerID, ChannelRef, State, SourceID, Payload, Digest, Code, PromptID string
	Revision                                                                         int64
	ExpiresAt, UpdatedAt                                                             time.Time
}

// NaturalExecution owns the immutable invocation and recovery identity.
type NaturalExecution struct {
	Identity, Transport, OwnerID, ChannelRef, Payload, State, Outcome string
	Revision                                                          int64
	CreatedAt, FinishedAt                                             time.Time
}

func scanNaturalDialogue(row scanner) (NaturalDialogue, error) {
	var d NaturalDialogue
	var expires, updated string
	err := row.Scan(&d.Transport, &d.OwnerID, &d.ChannelRef, &d.Revision, &d.State, &d.SourceID, &d.Payload, &d.Digest, &d.Code, &d.PromptID, &expires, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	d.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return d, err
	}
	d.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	return d, err
}

const naturalDialogueColumns = `transport,owner_id,channel_ref,revision,state,source_id,payload,digest,code,prompt_id,expires_at,updated_at`

// ReadNaturalDialogue returns the current owner and channel dialogue.
func (db *DB) ReadNaturalDialogue(ctx context.Context, transport, owner, channel string) (NaturalDialogue, error) {
	return scanNaturalDialogue(db.sql.QueryRowContext(ctx, `SELECT `+naturalDialogueColumns+` FROM chat_natural_dialogues WHERE transport=? AND owner_id=? AND channel_ref=?`, transport, owner, channel))
}

// SaveNaturalDialogue replaces only a pending revision. Every correction increments revision.
func (db *DB) SaveNaturalDialogue(ctx context.Context, d NaturalDialogue, expected int64) error {
	if d.Transport == "" || d.OwnerID == "" || d.ChannelRef == "" || d.SourceID == "" || len(d.Payload) > 32768 || !json.Valid([]byte(d.Payload)) || d.Revision != expected+1 || d.UpdatedAt.IsZero() || !d.ExpiresAt.After(d.UpdatedAt) || (d.State != "clarifying" && d.State != "awaiting_confirmation") {
		return errors.New("invalid natural dialogue")
	}
	if d.State == "awaiting_confirmation" && (d.Code == "" || d.Digest == "") {
		return errors.New("invalid natural proposal")
	}
	if expected == 0 {
		r, e := db.sql.ExecContext(ctx, `INSERT INTO chat_natural_dialogues(`+naturalDialogueColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, d.Transport, d.OwnerID, d.ChannelRef, d.Revision, d.State, d.SourceID, d.Payload, d.Digest, d.Code, "", FormatTime(d.ExpiresAt), FormatTime(d.UpdatedAt))
		return chatAffected(r, e)
	}
	r, e := db.sql.ExecContext(ctx, `UPDATE chat_natural_dialogues SET revision=?,state=?,source_id=?,payload=?,digest=?,code=?,prompt_id='',expires_at=?,updated_at=? WHERE transport=? AND owner_id=? AND channel_ref=? AND revision=? AND state NOT IN ('authorized','executing')`, d.Revision, d.State, d.SourceID, d.Payload, d.Digest, d.Code, FormatTime(d.ExpiresAt), FormatTime(d.UpdatedAt), d.Transport, d.OwnerID, d.ChannelRef, expected)
	return chatAffected(r, e)
}

// BindNaturalPrompt grants confirmation binding only after a successful prompt delivery.
func (db *DB) BindNaturalPrompt(ctx context.Context, d NaturalDialogue, prompt string) error {
	if prompt == "" {
		return errors.New("missing natural prompt identity")
	}
	r, e := db.sql.ExecContext(ctx, `UPDATE chat_natural_dialogues SET prompt_id=? WHERE transport=? AND owner_id=? AND channel_ref=? AND revision=? AND digest=? AND state='awaiting_confirmation' AND (prompt_id='' OR prompt_id=?)`, prompt, d.Transport, d.OwnerID, d.ChannelRef, d.Revision, d.Digest, prompt)
	return chatAffected(r, e)
}

// AuthorizeNaturalDialogue consumes a live, delivered revision and creates one inbox invocation atomically.
// Confirmation grammar, authenticated identity and reply/code binding are checked by the caller.
func (db *DB) AuthorizeNaturalDialogue(ctx context.Context, d NaturalDialogue, confirmation string, r ChatReply, at time.Time) error {
	return db.authorizeNaturalDialogue(ctx, nil, d, confirmation, r, at)
}

// AuthorizeConversationDialogue atomically fences the turn, consumes the exact
// proposal and journals the conversation-owned execution before any effect.
func (db *DB) AuthorizeConversationDialogue(ctx context.Context, t ConversationTurn, d NaturalDialogue, confirmation string, r ChatReply, at time.Time) error {
	r.ConversationTurnID = t.ID
	return db.authorizeNaturalDialogue(ctx, &t, d, confirmation, r, at)
}
func (db *DB) authorizeNaturalDialogue(ctx context.Context, t *ConversationTurn, d NaturalDialogue, confirmation string, r ChatReply, at time.Time) error {
	sourceID, sourceErr := strconv.ParseUint(d.SourceID, 10, 64)
	confirmationID, confirmationErr := strconv.ParseUint(confirmation, 10, 64)
	if sourceErr != nil || confirmationErr != nil || confirmationID <= sourceID {
		return errors.New("confirmation must follow proposal source")
	}
	if r.Command == "" || r.MessageID == "" || r.Transport != d.Transport || r.ChannelRef != d.ChannelRef || confirmation == d.SourceID {
		return errors.New("invalid natural authorization")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if t != nil {
			if err := conversationFence(ctx, tx, *t); err != nil {
				return err
			}
			var transport, owner, channel string
			if err := tx.QueryRowContext(ctx, `SELECT transport,owner_id,channel_ref FROM chat_conversations WHERE id=?`, t.ConversationID).Scan(&transport, &owner, &channel); err != nil {
				return err
			}
			if transport != d.Transport || owner != d.OwnerID || channel != d.ChannelRef {
				return errors.New("conversation authorization scope mismatch")
			}
			if err := saveConversationAction(ctx, tx, ConversationActionLink{TurnID: t.ID, DialogueID: d.Code, DialogueRevision: d.Revision, ExecutionID: r.MessageID, PayloadDigest: d.Digest}); err != nil {
				return err
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE chat_natural_dialogues SET state='authorized',updated_at=? WHERE transport=? AND owner_id=? AND channel_ref=? AND revision=? AND digest=? AND code=? AND payload=? AND prompt_id!='' AND state='awaiting_confirmation' AND expires_at>?`, FormatTime(at), d.Transport, d.OwnerID, d.ChannelRef, d.Revision, d.Digest, d.Code, d.Payload, FormatTime(at))
		if err = chatAffected(result, err); err != nil {
			return err
		}
		err = insertNaturalExecution(ctx, tx, NaturalExecution{Identity: r.MessageID, Transport: d.Transport, OwnerID: d.OwnerID, ChannelRef: d.ChannelRef, Revision: d.Revision, Payload: d.Payload, CreatedAt: at})
		if err != nil {
			return err
		}
		args, err := json.Marshal(r.Arguments)
		if err != nil {
			return err
		}
		state, question, project := "pending", "", ""
		switch r.Command {
		case "ask":
			state, question, project = "answering", r.Arguments["question"], r.Arguments["project"]
		case "draft":
			state, question, project = "answering", r.Arguments["note"], r.Arguments["project"]
		case "confirm":
			state, question = "answering", r.Arguments["code"]
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO chat_inbox(transport,message_id,channel_ref,command,arguments,state,next_attempt_at,created_at,question,project_option,reply_text,presentation_state,outcome_status,conversation_turn_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, r.Transport, r.MessageID, r.ChannelRef, r.Command, string(args), state, FormatTime(at), FormatTime(at), question, project, r.ReplyText, r.PresentationState, r.OutcomeStatus, r.ConversationTurnID)
		return err
	})
}

// EndNaturalDialogue conditionally ends pending work and clears protected content.
func (db *DB) EndNaturalDialogue(ctx context.Context, d NaturalDialogue, state string, at time.Time) error {
	if state != "cancelled" && state != "expired" && state != "finished" && state != "uncertain" {
		return errors.New("invalid natural terminal state")
	}
	r, e := db.sql.ExecContext(ctx, `UPDATE chat_natural_dialogues SET state=?,payload='{}',digest='',code='',prompt_id='',updated_at=? WHERE transport=? AND owner_id=? AND channel_ref=? AND revision=? AND state IN ('clarifying','awaiting_confirmation')`, state, FormatTime(at), d.Transport, d.OwnerID, d.ChannelRef, d.Revision)
	return chatAffected(r, e)
}

// ReadNaturalExecution returns an immutable invocation or its saved outcome.
func (db *DB) ReadNaturalExecution(ctx context.Context, identity string) (NaturalExecution, error) {
	var e NaturalExecution
	var created string
	var finished sql.NullString
	err := db.sql.QueryRowContext(ctx, `SELECT identity,transport,owner_id,channel_ref,revision,payload,state,outcome,created_at,finished_at FROM chat_natural_executions WHERE identity=?`, identity).Scan(&e.Identity, &e.Transport, &e.OwnerID, &e.ChannelRef, &e.Revision, &e.Payload, &e.State, &e.Outcome, &created, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	e.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return e, err
	}
	e.FinishedAt, err = scanTime(finished)
	return e, err
}

// StartNaturalExecution claims one authorized invocation before any effect.
func (db *DB) StartNaturalExecution(ctx context.Context, identity string, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, `UPDATE chat_natural_executions SET state='executing' WHERE identity=? AND state='authorized'`, identity)
		if e = chatAffected(r, e); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE chat_natural_dialogues SET state='executing',updated_at=? WHERE (transport,owner_id,channel_ref,revision) IN (SELECT transport,owner_id,channel_ref,revision FROM chat_natural_executions WHERE identity=?)`, FormatTime(at), identity)
		return e
	})
}

// FinishNaturalExecution saves the outcome and clears executable text before delivery.
func (db *DB) FinishNaturalExecution(ctx context.Context, identity, outcome string, uncertain bool, at time.Time) error {
	if len(outcome) > 65536 {
		return errors.New("natural outcome too large")
	}
	state := "finished"
	if uncertain {
		state = "uncertain"
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, `UPDATE chat_natural_executions SET state=?,outcome=?,payload='{}',finished_at=? WHERE identity=? AND state IN ('authorized','executing')`, state, outcome, FormatTime(at), identity)
		if e = chatAffected(r, e); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE chat_natural_dialogues SET state=?,payload='{}',digest='',code='',prompt_id='',updated_at=? WHERE (transport,owner_id,channel_ref,revision) IN (SELECT transport,owner_id,channel_ref,revision FROM chat_natural_executions WHERE identity=?)`, state, FormatTime(at), identity)
		return e
	})
}

// PurgeNaturalChat clears expired text and removes terminal replay identities after 24 hours.
func (db *DB) PurgeNaturalChat(ctx context.Context, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE chat_natural_dialogues SET state='expired',payload='{}',digest='',code='',prompt_id='',updated_at=? WHERE state IN ('clarifying','awaiting_confirmation') AND expires_at<=?`, FormatTime(at), FormatTime(at))
		if e != nil {
			return e
		}
		// Expired intake keeps only replay identities, freeing bounded queue space.
		_, e = tx.ExecContext(ctx, `UPDATE chat_natural_messages SET state='finished',content='',reply_to='',finished_at=? WHERE state IN ('staged','pending') AND created_at<=? AND NOT (state='pending' AND EXISTS(SELECT 1 FROM chat_conversations c WHERE c.channel_ref=chat_natural_messages.channel_ref AND c.transport='discord' AND c.active=1))`, FormatTime(at), FormatTime(at.Add(-24*time.Hour)))
		if e != nil {
			return e
		}
		for _, q := range []string{`DELETE FROM chat_natural_messages WHERE state='finished' AND finished_at<=?`, `DELETE FROM chat_natural_executions WHERE state IN ('finished','uncertain') AND finished_at<=? AND NOT EXISTS(SELECT 1 FROM owner_action_proposals p WHERE p.execution_id=chat_natural_executions.identity) AND NOT EXISTS(SELECT 1 FROM chat_conversation_actions a JOIN chat_conversation_turns t ON t.id=a.turn_id WHERE a.execution_id=chat_natural_executions.identity AND (t.delivered_at IS NULL OR EXISTS(SELECT 1 FROM chat_inbox i WHERE i.message_id=a.execution_id AND i.conversation_turn_id=t.id AND i.state IN ('working','pending','sent','answering'))))`, `DELETE FROM chat_natural_dialogues WHERE state IN ('cancelled','expired','finished','uncertain') AND updated_at<=?`} {
			if _, e = tx.ExecContext(ctx, q, FormatTime(at.Add(-24*time.Hour))); e != nil {
				return e
			}
		}
		return nil
	})
}

// insertNaturalExecution is the common admission journal for owner actions.
// Transport adapters supply real provenance; admission does not require an inbox.
func insertNaturalExecution(ctx context.Context, tx *sql.Tx, e NaturalExecution) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO chat_natural_executions(identity,transport,owner_id,channel_ref,revision,payload,created_at) VALUES(?,?,?,?,?,?,?)`, e.Identity, e.Transport, e.OwnerID, e.ChannelRef, e.Revision, e.Payload, FormatTime(e.CreatedAt))
	return err
}
