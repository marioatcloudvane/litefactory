package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// BugEvent is one append-only row in the bug's shared history log. It replaces
// the per-step event tables, the three narrow side logs (answers, review
// changes, test resolutions), the two retry tables' history, superseded
// reviews and earlier merge/close observations. kind is one of a closed list
// validated by each caller; reference plus kind is the idempotent replay key.
type BugEvent struct {
	ID              int64
	SessionID       int64
	Kind, Reference string
	AttemptID       int64
	DeliveryKey     string
	Content         []byte
	Reported        bool
	RecordedAt      time.Time
}

// bugEventTx is the one internal idempotent-append helper every bug domain
// write uses to retire history into the shared log. An identical existing row
// (same kind, reference and content) is a no-op; a changed one is refused.
// attempt and deliveryKey are optional (zero/empty omits the column).
func bugEventTx(ctx context.Context, tx *sql.Tx, session int64, kind, reference string, attempt int64, deliveryKey string, content []byte) error {
	if kind == "" || reference == "" {
		return errors.New("store: invalid Bug event")
	}
	if content == nil {
		content = []byte("{}")
	}
	if len(content) > 1<<20 {
		return errors.New("store: Bug event content exceeds 1 MiB")
	}
	var oldContent []byte
	var oldAttempt sql.NullInt64
	var oldKey sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT content,attempt_id,delivery_key FROM bug_events WHERE session_id=? AND kind=? AND reference=?`, session, kind, reference).Scan(&oldContent, &oldAttempt, &oldKey)
	if err == nil {
		if string(oldContent) != string(content) || oldAttempt.Int64 != attempt || oldKey.String != deliveryKey {
			return errors.New("store: Bug event replay conflict")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bug_events(session_id,kind,reference,attempt_id,delivery_key,content,recorded_at) VALUES(?,?,?,?,?,?,?)`, session, kind, reference, nullID(attempt), nullString(deliveryKey), content, FormatTime(time.Now()))
	return err
}

// bugEventJSONTx marshals v and appends it as one bug_events row.
func bugEventJSONTx(ctx context.Context, tx *sql.Tx, session int64, kind, reference string, v any) error {
	content, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return bugEventTx(ctx, tx, session, kind, reference, 0, "", content)
}

// bugEventByReferenceTx reads one retained event's content by its exact kind
// and reference. ErrNotFound means no such event was ever recorded.
func bugEventByReferenceTx(ctx context.Context, tx *sql.Tx, session int64, kind, reference string) ([]byte, error) {
	var content []byte
	err := tx.QueryRowContext(ctx, `SELECT content FROM bug_events WHERE session_id=? AND kind=? AND reference=?`, session, kind, reference).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return content, err
}

// bugEventByDeliveryKeyTx reads one retained event's content by its unique
// delivery key. ErrNotFound means no event carries that delivery key.
func bugEventByDeliveryKeyTx(ctx context.Context, tx *sql.Tx, session int64, kind, deliveryKey string) ([]byte, error) {
	var content []byte
	err := tx.QueryRowContext(ctx, `SELECT content FROM bug_events WHERE session_id=? AND kind=? AND delivery_key=?`, session, kind, deliveryKey).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return content, err
}

// BugEventCount counts retained events of one kind for a bug; used by replay
// and history-bound checks (for example counting prior test generations).
func bugEventCountTx(ctx context.Context, tx *sql.Tx, session int64, kind string) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_events WHERE session_id=? AND kind=?`, session, kind).Scan(&n)
	return n, err
}

// bugEventJSONFullTx is bugEventJSONTx with an explicit attempt and delivery
// key, for the few event kinds (like "report") that need both.
func bugEventJSONFullTx(ctx context.Context, tx *sql.Tx, session int64, kind, reference string, attempt int64, deliveryKey string, v any) error {
	content, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return bugEventTx(ctx, tx, session, kind, reference, attempt, deliveryKey, content)
}
