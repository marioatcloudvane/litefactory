package store

import (
	"testing"
	"time"
)

// SeedHistoricalComment writes the pre-reply-target comment shape for migration
// fixtures. It deliberately avoids current readers until migration completes.
func SeedHistoricalComment(t *testing.T, db *DB, c Comment) (Comment, error) {
	t.Helper()
	c.RecordedAt = time.Now().UTC().Truncate(time.Millisecond)
	result, err := db.sql.ExecContext(t.Context(), `INSERT INTO comments(session_id,forge_comment_id,author_handle,author_user_id,author_is_bot,body,created_at,classification,tag,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, c.SessionID, c.ForgeCommentID, c.AuthorHandle, nullID(c.AuthorUserID), c.AuthorIsBot, c.Body, FormatTime(c.CreatedAt), string(c.Classification), nullString(c.Tag), FormatTime(c.RecordedAt))
	if err != nil {
		return Comment{}, err
	}
	c.ID, err = result.LastInsertId()
	return c, err
}
