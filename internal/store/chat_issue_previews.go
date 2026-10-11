package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"unicode/utf8"
)

// ChatIssuePreview holds only the protected preview and its short-lived outcome.
// ClaimVersion fences stale workers; no transaction is held during forge I/O.
type ChatIssuePreview struct {
	ID, Transport, ChannelRef, SourceMessageID, LastConfirmMessageID, Code, ContentHash, Title, Body, State string
	RepoID                                                                                                  int64
	CreatedAt, ExpiresAt, LastAttemptAt, LeaseUntil, FinishedAt                                             time.Time
	ClaimVersion, IssueNumber                                                                               int
	IssueLink                                                                                               string
}

const previewColumns = `id,transport,channel_ref,source_message_id,last_confirm_message_id,code,content_hash,title,body,state,repo_id,created_at,expires_at,last_attempt_at,lease_until,finished_at,claim_version,issue_number,issue_link`

func scanPreview(row scanner) (ChatIssuePreview, error) {
	var p ChatIssuePreview
	var created, expires string
	var attempt, lease, finished sql.NullString
	err := row.Scan(&p.ID, &p.Transport, &p.ChannelRef, &p.SourceMessageID, &p.LastConfirmMessageID, &p.Code, &p.ContentHash, &p.Title, &p.Body, &p.State, &p.RepoID, &created, &expires, &attempt, &lease, &finished, &p.ClaimVersion, &p.IssueNumber, &p.IssueLink)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	for _, f := range []struct {
		dst *time.Time
		src sql.NullString
	}{{&p.CreatedAt, sql.NullString{String: created, Valid: true}}, {&p.ExpiresAt, sql.NullString{String: expires, Valid: true}}, {&p.LastAttemptAt, attempt}, {&p.LeaseUntil, lease}, {&p.FinishedAt, finished}} {
		if *f.dst, err = scanTime(f.src); err != nil {
			return p, err
		}
	}
	return p, nil
}

// CurrentChatIssuePreview reads the channel's active draft without storing history.
func (db *DB) CurrentChatIssuePreview(ctx context.Context, transport, channel string) (ChatIssuePreview, error) {
	return scanPreview(db.sql.QueryRowContext(ctx, `SELECT `+previewColumns+` FROM chat_issue_previews WHERE transport=? AND channel_ref=? AND state IN ('previewed','creating','uncertain')`, transport, channel))
}

// ChatIssuePreviewForMessage lets a restarted draft reuse its saved preview.
func (db *DB) ChatIssuePreviewForMessage(ctx context.Context, transport, message string) (ChatIssuePreview, error) {
	return scanPreview(db.sql.QueryRowContext(ctx, `SELECT `+previewColumns+` FROM chat_issue_previews WHERE transport=? AND source_message_id=?`, transport, message))
}

// ChatIssuePreviewByCode reads the retained preview without claiming or creating
// it, allowing reply usage to retain the actual repository owner.
func (db *DB) ChatIssuePreviewByCode(ctx context.Context, transport, channel, code string) (ChatIssuePreview, error) {
	return scanPreview(db.sql.QueryRowContext(ctx, `SELECT `+previewColumns+` FROM chat_issue_previews WHERE transport=? AND channel_ref=? AND code=?`, transport, channel, code))
}

// SaveChatIssuePreview replaces the draft atomically. A live creating claim
// cannot be superseded while its external operation may still be running.
func (db *DB) SaveChatIssuePreview(ctx context.Context, p ChatIssuePreview) error {
	if p.ID == "" || p.Transport == "" || p.ChannelRef == "" || p.SourceMessageID == "" || len(p.Code) != 8 || p.RepoID < 1 || p.ContentHash == "" || p.Title == "" || p.Body == "" || !utf8.ValidString(p.Title) || !utf8.ValidString(p.Body) || utf8.RuneCountInString(p.Title) > 120 || utf8.RuneCountInString(p.Body) > 1500 || p.CreatedAt.IsZero() || !p.ExpiresAt.Equal(p.CreatedAt.Add(24*time.Hour)) {
		return errors.New("invalid chat issue preview")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := expireChatPreviews(ctx, tx, p.CreatedAt); err != nil {
			return err
		}
		var busy int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM chat_issue_previews WHERE transport=? AND channel_ref=? AND state='creating'`, p.Transport, p.ChannelRef).Scan(&busy); err != nil {
			return err
		}
		if busy > 0 {
			return errors.New("chat issue creation unresolved")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE chat_issue_previews SET state='superseded',title='',body='',finished_at=? WHERE transport=? AND channel_ref=? AND state IN ('previewed','uncertain')`, FormatTime(p.CreatedAt), p.Transport, p.ChannelRef); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO chat_issue_previews(id,transport,channel_ref,source_message_id,code,content_hash,title,body,state,repo_id,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?,'previewed',?,?,?)`, p.ID, p.Transport, p.ChannelRef, p.SourceMessageID, p.Code, p.ContentHash, p.Title, p.Body, p.RepoID, FormatTime(p.CreatedAt), FormatTime(p.ExpiresAt))
		return err
	})
}

// ClaimChatIssuePreview returns a status rather than performing an external
// effect. Recovery claims preserve the prior create time for the two-minute guard.
func (db *DB) ClaimChatIssuePreview(ctx context.Context, transport, channel, message, code string, at time.Time) (p ChatIssuePreview, status string, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		p, e = scanPreview(tx.QueryRowContext(ctx, `SELECT `+previewColumns+` FROM chat_issue_previews WHERE transport=? AND channel_ref=? AND code=?`, transport, channel, code))
		if errors.Is(e, ErrNotFound) {
			status = "unknown"
			return nil
		}
		if e != nil {
			return e
		}
		status = p.State
		if message == p.SourceMessageID {
			status = "separate"
			return nil
		}
		if p.State == "created" || p.State == "superseded" || p.State == "expired" {
			return nil
		}
		if message == p.LastConfirmMessageID {
			status = "repeat"
			return nil
		}
		if p.State == "creating" && at.Before(p.LeaseUntil) {
			status = "busy"
			if !at.Before(p.ExpiresAt) {
				status = "expired"
			}
			return nil
		}
		if !at.Before(p.ExpiresAt) {
			status = "expired"
			_, e = tx.ExecContext(ctx, `UPDATE chat_issue_previews SET state='expired',title='',body='',finished_at=? WHERE id=?`, FormatTime(p.ExpiresAt), p.ID)
			return e
		}
		if p.State == "creating" {
			status = "uncertain"
		}
		if p.State == "previewed" {
			p.LastAttemptAt = at
		}
		p.ClaimVersion++
		p.LeaseUntil = at.Add(5 * time.Minute)
		_, e = tx.ExecContext(ctx, `UPDATE chat_issue_previews SET state='creating',last_attempt_at=?,claim_version=?,lease_until=?,last_confirm_message_id=? WHERE id=?`, FormatTime(p.LastAttemptAt), p.ClaimVersion, FormatTime(p.LeaseUntil), message, p.ID)
		return e
	})
	return p, status, err
}

// FinishChatIssuePreview records a fenced outcome and clears ended text.
func (db *DB) FinishChatIssuePreview(ctx context.Context, p ChatIssuePreview, state string, number int, link string, at time.Time) error {
	if state != "previewed" && state != "uncertain" && state != "created" {
		return errors.New("invalid preview outcome")
	}
	if state == "created" && (number < 1 || link == "") {
		return errors.New("invalid created issue")
	}
	var finished any
	title, body := p.Title, p.Body
	if state == "created" {
		title, body = "", ""
		finished = FormatTime(at)
	}
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_issue_previews SET state=?,title=?,body=?,issue_number=?,issue_link=?,finished_at=?,lease_until=NULL WHERE id=? AND state='creating' AND claim_version=?`, state, title, body, number, link, finished, p.ID, p.ClaimVersion)
	return chatAffected(result, err)
}

// PurgeChatIssuePreviews clears expired text and retains ended codes for 24 hours.
// A live claim may finish after its deadline; purge never interrupts its lease.
func (db *DB) PurgeChatIssuePreviews(ctx context.Context, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := expireChatPreviews(ctx, tx, at); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM chat_issue_previews WHERE state IN ('created','superseded','expired') AND finished_at<=?`, FormatTime(at.Add(-24*time.Hour)))
		return err
	})
}

// BeginChatIssueCreate records the new attempt only after marker reconciliation.
func (db *DB) BeginChatIssueCreate(ctx context.Context, p ChatIssuePreview, at time.Time) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE chat_issue_previews SET last_attempt_at=? WHERE id=? AND state='creating' AND claim_version=?`, FormatTime(at), p.ID, p.ClaimVersion)
	return chatAffected(result, err)
}

// LatestChatIssuePreview returns the newest retained code, including a created
// preview, so an old code can always identify the draft that replaced it.
func (db *DB) LatestChatIssuePreview(ctx context.Context, transport, channel string) (ChatIssuePreview, error) {
	return scanPreview(db.sql.QueryRowContext(ctx, `SELECT `+previewColumns+` FROM chat_issue_previews WHERE transport=? AND channel_ref=? ORDER BY created_at DESC,rowid DESC LIMIT 1`, transport, channel))
}

func expireChatPreviews(ctx context.Context, tx *sql.Tx, at time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE chat_issue_previews SET state='expired',title='',body='',finished_at=expires_at WHERE expires_at<=? AND (state IN ('previewed','uncertain') OR (state='creating' AND lease_until<=?))`, FormatTime(at), FormatTime(at))
	return err
}
