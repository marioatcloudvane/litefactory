package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/commandtext"
	"strconv"
	"time"
)

// Classification is what the self-filter decided a comment is (S-8).
type Classification string

// The comment classifications. The decision is made on the author's numeric
// forge id, never the handle: handles are renameable, and a fine-grained PAT
// on a machine user account reports type "User" rather than "Bot", so the
// forge's own bot flag cannot be the only signal.
const (
	// ClassificationInstruction is a comment from the owner: the only kind
	// that can start a run.
	ClassificationInstruction Classification = "instruction"
	// ClassificationThirdParty is a comment from another human. It is recorded
	// and tagged, and triggers nothing at M0 (AC-2.15).
	ClassificationThirdParty Classification = "third_party"
	// ClassificationSelf is a comment litefactory posted, recognised by the
	// bot's numeric id or by the `<!-- lf:` marker. Recording it is what makes
	// "the daemon never reacts to its own comment" auditable after the fact.
	ClassificationSelf Classification = "self"
)

// Comment is a forge comment litefactory has seen and classified. Bodies are
// stored verbatim and unredacted: S-7 redaction is outbound-only.
type Comment struct {
	// ID is the surrogate key.
	ID int64
	// SessionID is the task the comment belongs to.
	SessionID int64
	// ForgeCommentID is the forge's id for the comment, unique per session.
	ForgeCommentID int64
	// ReplyToCommentID retains explicit parent metadata with admitted bytes.
	ReplyToCommentID int64
	// AuthorHandle is the author's login, for display.
	AuthorHandle string
	// AuthorUserID is the author's numeric forge id — the durable identity
	// classification was decided on. Zero when unknown.
	AuthorUserID int64
	// AuthorIsBot is the forge's own bot flag, one of the three self-filter
	// inputs (S-8, A-10).
	AuthorIsBot bool
	// Body is the comment text, verbatim.
	Body string
	// CreatedAt is the forge's timestamp for the comment, not litefactory's.
	CreatedAt time.Time
	// Classification is the self-filter's verdict.
	Classification Classification
	// Tag is the c1/c2/… label. It is assigned to third-party comments only,
	// and consumed by /include at M1; at M0 it is recorded and unused.
	Tag string
	// RecordedAt is when litefactory stored the row.
	RecordedAt time.Time
}

// RecordComment stores a classified comment.
//
// Recording is idempotent on (session, forge comment id): re-seeing a comment
// — which happens routinely, because the `since` filter is inclusive (D-A) —
// returns the row already stored and changes nothing. That is what makes a
// replayed tick harmless.
//
// A third-party comment with an empty Tag is assigned the session's next cN
// tag in the same transaction as the insert, so two comments can never be
// handed the same tag. Callers should leave Tag empty and let the store
// allocate.
func (db *DB) RecordComment(ctx context.Context, c Comment) (Comment, error) {
	if c.SessionID == 0 || c.ForgeCommentID == 0 || c.ReplyToCommentID < 0 {
		return Comment{}, errors.New("store: record comment: session id and forge comment id are required")
	}
	if commandtext.DiscussionSummary(c.Body) {
		c.Classification = ClassificationSelf
	}
	if !c.Classification.valid() {
		return Comment{}, fmt.Errorf("store: record comment: unknown classification %q", c.Classification)
	}

	var stored Comment
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		stored, err = recordComment(ctx, tx, c)
		return err
	})
	if err != nil {
		return Comment{}, fmt.Errorf("store: record comment %d on session %d: %w",
			c.ForgeCommentID, c.SessionID, err)
	}
	return stored, nil
}

// NextCommentTag returns the cN tag the next third-party comment on this
// session would get: c1 when none has been tagged yet, otherwise one past the
// highest.
//
// [DB.RecordComment] allocates tags itself; this exists for callers that need
// to show the tag before recording, and it is not a reservation — nothing is
// held between this call and the insert.
func (db *DB) NextCommentTag(ctx context.Context, sessionID int64) (string, error) {
	tag, err := nextCommentTag(ctx, db.sql, sessionID)
	if err != nil {
		return "", fmt.Errorf("store: next comment tag for session %d: %w", sessionID, err)
	}
	return tag, nil
}

// GetComment reads one recorded comment by its forge id, returning
// [ErrNotFound] when litefactory has not seen it.
func (db *DB) GetComment(ctx context.Context, sessionID, forgeCommentID int64) (Comment, error) {
	c, err := scanComment(db.sql.QueryRowContext(ctx,
		`SELECT `+commentColumns+` FROM comments WHERE session_id = ? AND forge_comment_id = ?`,
		sessionID, forgeCommentID))
	if err != nil {
		return Comment{}, fmt.Errorf("store: read comment %d on session %d: %w", forgeCommentID, sessionID, err)
	}
	return c, nil
}

// ListComments returns every recorded comment for a session in forge id order,
// which is the order they were written and the order they must be processed in
// (AC-2.11).
func (db *DB) ListComments(ctx context.Context, sessionID int64) ([]Comment, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT `+commentColumns+` FROM comments WHERE session_id = ? ORDER BY forge_comment_id`,
		sessionID)
	if err != nil {
		return nil, fmt.Errorf("store: list comments for session %d: %w", sessionID, err)
	}
	defer rows.Close()

	var out []Comment
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list comments for session %d: %w", sessionID, err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list comments for session %d: %w", sessionID, err)
	}
	return out, nil
}

// querier is the read surface shared by *sql.DB and *sql.Tx, so a helper can
// run either inside a caller's transaction or on its own.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// nextCommentTag computes the session's next cN tag. The maximum is taken in
// SQL over the numeric part, so c10 sorts after c9 rather than before it.
func nextCommentTag(ctx context.Context, q querier, sessionID int64) (string, error) {
	const query = `
SELECT MAX(CAST(SUBSTR(tag, 2) AS INTEGER))
FROM comments
WHERE session_id = ? AND tag IS NOT NULL AND tag LIKE 'c%'`

	var highest sql.NullInt64
	if err := q.QueryRowContext(ctx, query, sessionID).Scan(&highest); err != nil {
		return "", err
	}
	return "c" + strconv.FormatInt(scanID(highest)+1, 10), nil
}

func (c Classification) valid() bool {
	switch c {
	case ClassificationInstruction, ClassificationThirdParty, ClassificationSelf:
		return true
	default:
		return false
	}
}

// commentColumns is the column list every comment read shares, in the order
// scanComment expects.
const commentColumns = `
    id, session_id, forge_comment_id, author_handle, author_user_id,
    author_is_bot, body, created_at, classification, tag, recorded_at, reply_to_comment_id`

func scanComment(s scanner) (Comment, error) {
	var (
		c              Comment
		authorUserID   sql.NullInt64
		classification string
		tag            sql.NullString
		createdAt      sql.NullString
		recordedAt     sql.NullString
	)
	err := s.Scan(&c.ID, &c.SessionID, &c.ForgeCommentID, &c.AuthorHandle, &authorUserID,
		&c.AuthorIsBot, &c.Body, &createdAt, &classification, &tag, &recordedAt, &c.ReplyToCommentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Comment{}, ErrNotFound
		}
		return Comment{}, err
	}
	c.AuthorUserID = scanID(authorUserID)
	c.Classification = Classification(classification)
	c.Tag = tag.String

	for _, field := range []struct {
		src sql.NullString
		dst *time.Time
	}{
		{createdAt, &c.CreatedAt},
		{recordedAt, &c.RecordedAt},
	} {
		t, err := scanTime(field.src)
		if err != nil {
			return Comment{}, err
		}
		*field.dst = t
	}
	return c, nil
}

func recordComment(ctx context.Context, tx *sql.Tx, c Comment) (Comment, error) {
	existing, err := scanComment(tx.QueryRowContext(ctx,
		`SELECT `+commentColumns+` FROM comments WHERE session_id = ? AND forge_comment_id = ?`,
		c.SessionID, c.ForgeCommentID))
	switch {
	case err == nil:
		if existing.ReplyToCommentID != c.ReplyToCommentID {
			return Comment{}, graphInvariant("admitted reply-to identity changed on replay")
		}
		return existing, nil
	case !errors.Is(err, ErrNotFound):
		return Comment{}, err
	}

	tag := c.Tag
	if tag == "" && c.Classification == ClassificationThirdParty {
		if tag, err = nextCommentTag(ctx, tx, c.SessionID); err != nil {
			return Comment{}, err
		}
	}
	c.Tag = tag
	c.RecordedAt = nowUTC()

	stored, err := scanComment(tx.QueryRowContext(ctx, `
INSERT INTO comments (
    session_id, forge_comment_id, author_handle, author_user_id, author_is_bot,
    body, created_at, classification, tag, recorded_at,reply_to_comment_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?,?)
RETURNING `+commentColumns,
		c.SessionID, c.ForgeCommentID, c.AuthorHandle, nullID(c.AuthorUserID), c.AuthorIsBot,
		c.Body, FormatTime(c.CreatedAt), string(c.Classification), nullString(c.Tag),
		FormatTime(c.RecordedAt), c.ReplyToCommentID))
	return stored, err
}
