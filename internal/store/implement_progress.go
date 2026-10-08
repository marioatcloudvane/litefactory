package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// ImplementProgressReport is immutable advisory text, never a completion claim.
// ID survives compaction; the natural identity is RunID, Attempt and Sequence.
type ImplementProgressReport struct {
	ID, RunID, Generation, Sequence                       int64
	Parent, StoryKey, Attempt, Kind, Message, MessageHash string
	StoryAttempts                                         int
	RecordedAt                                            time.Time
}

// ImplementProgressOutcome separates sequence disposition from stored activity.
type ImplementProgressOutcome struct {
	Disposition, Code string
	Report            ImplementProgressReport
}

const implementProgressColumns = `id,parent,generation,story_key,run_id,attempt,sequence,kind,message,message_hash,story_attempts,recorded_at`

func scanImplementProgress(row interface{ Scan(...interface{}) error }) (ImplementProgressReport, error) {
	var r ImplementProgressReport
	var at string
	err := row.Scan(&r.ID, &r.Parent, &r.Generation, &r.StoryKey, &r.RunID, &r.Attempt, &r.Sequence, &r.Kind, &r.Message, &r.MessageHash, &r.StoryAttempts, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err == nil {
		r.RecordedAt, err = ParseTime(at)
	}
	return r, err
}

// IsImplementProgressWorker observes membership; writes recheck it transactionally.
func (db *DB) IsImplementProgressWorker(ctx context.Context, b ResultBinding) (bool, error) {
	var n int
	err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM implement_stories i JOIN runs r ON r.session_id=i.worker_session WHERE r.id=? AND r.mode='implement'`, b.RunID).Scan(&n)
	return n > 0, err
}

// RecordImplementProgress atomically records text and confirms its request receipt.
// Pending and uncertain retries recover by report identity before inserting once.
func (db *DB) RecordImplementProgress(ctx context.Context, b ResultBinding, requestID string, sequence int64, kind, message string) (ImplementProgressOutcome, error) {
	out := ImplementProgressOutcome{Disposition: "rejected"}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, true); err != nil {
			return err
		}
		receipt, err := scanFamilyRequest(tx.QueryRowContext(ctx, `SELECT `+familyColumns+` FROM family_requests WHERE run_id=? AND attempt=? AND request_id=?`, b.RunID, b.Attempt, requestID))
		if err != nil {
			return err
		}
		if receipt.Tool != "activity_update" {
			return ErrResultCorrelation
		}
		finish := func() error {
			disposition := "rejected"
			ref := FamilyReference{}
			if out.Disposition != "rejected" {
				disposition = "confirmed"
			}
			if out.Report.ID > 0 {
				ref = FamilyReference{Kind: "activity", ID: out.Report.ID}
			}
			if out.Code == "authority" && (receipt.Disposition == "confirmed" || receipt.Disposition == "rejected") {
				return nil // Refusal cannot rewrite an earlier receipt.
			}
			if receipt.Disposition == "confirmed" || receipt.Disposition == "rejected" {
				if receipt.Disposition != disposition || receipt.Reference != ref {
					return ErrResultCorrelation
				}
				return nil
			}
			_, err := tx.ExecContext(ctx, `UPDATE family_requests SET disposition=?,reference_kind=?,reference_id=?,reference_request='' WHERE run_id=? AND attempt=? AND request_id=?`, disposition, ref.Kind, ref.ID, b.RunID, b.Attempt, requestID)
			return err
		}
		var parent, key string
		var generation int64
		var attempts int
		err = tx.QueryRowContext(ctx, `SELECT i.parent,i.generation,i.story_key,i.attempts FROM implement_stories i JOIN runs r ON r.session_id=i.worker_session WHERE r.id=? AND r.mode='implement' AND i.state='running'`, b.RunID).Scan(&parent, &generation, &key, &attempts)
		if errors.Is(err, sql.ErrNoRows) || b.Mode != "implement" {
			out.Code = "authority"
			return finish()
		}
		if err != nil {
			return err
		}
		switch {
		case kind == "":
			out.Code = "kind_required"
		case kind != "implementation" && kind != "testing" && kind != "repair":
			out.Code = "invalid_progress"
		case sequence < 1 || strings.TrimSpace(message) == "" || !utf8.ValidString(message):
			out.Code = "invalid_progress"
		case len(message) > 2048:
			out.Code = "message_limit"
		}
		if out.Code != "" {
			return finish()
		}
		hash := submissionDigest([]byte(kind + "\x00" + message))
		old, err := scanImplementProgress(tx.QueryRowContext(ctx, `SELECT `+implementProgressColumns+` FROM implement_progress_reports WHERE run_id=? AND attempt=? AND sequence=?`, b.RunID, b.Attempt, sequence))
		if err == nil {
			if old.MessageHash != hash {
				out.Code = "sequence_conflict"
			} else {
				out.Disposition = "replayed"
				out.Report = old
			}
			return finish()
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		var count int
		var highest int64
		if err := tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(max(sequence),0) FROM implement_progress_reports WHERE run_id=? AND attempt=?`, b.RunID, b.Attempt).Scan(&count, &highest); err != nil {
			return err
		}
		if sequence <= highest {
			out.Disposition = "stale"
			return finish()
		}
		if count >= 1024 {
			out.Code = "update_limit"
			return finish()
		}
		// Allocate a stable receipt identity in this transaction. Physical rowids
		// can change during archive compaction; retained IDs must never move.
		_, err = tx.ExecContext(ctx, `INSERT INTO implement_progress_reports(id,parent,generation,story_key,run_id,attempt,sequence,kind,message,message_hash,story_attempts,recorded_at) SELECT COALESCE(MAX(id),0)+1,?,?,?,?,?,?,?,?,?,?,? FROM implement_progress_reports`, parent, generation, key, b.RunID, b.Attempt, sequence, kind, message, hash, attempts, FormatTime(nowUTC()))
		if err != nil {
			return err
		}
		out.Report, err = scanImplementProgress(tx.QueryRowContext(ctx, `SELECT `+implementProgressColumns+` FROM implement_progress_reports WHERE run_id=? AND attempt=? AND sequence=?`, b.RunID, b.Attempt, sequence))
		if err != nil {
			return err
		}
		out.Disposition = "accepted"
		return finish()
	})
	return out, err
}

// ImplementProgressForAttempt returns the latest activity and total retained count.
func (db *DB) ImplementProgressForAttempt(ctx context.Context, b ResultBinding) (ImplementProgressReport, int, error) {
	var r ImplementProgressReport
	var count int
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, true); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM implement_progress_reports WHERE run_id=? AND attempt=?`, b.RunID, b.Attempt).Scan(&count); err != nil {
			return err
		}
		var err error
		r, err = scanImplementProgress(tx.QueryRowContext(ctx, `SELECT `+implementProgressColumns+` FROM implement_progress_reports WHERE run_id=? AND attempt=? ORDER BY sequence DESC LIMIT 1`, b.RunID, b.Attempt))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	})
	return r, count, err
}

// LatestImplementProgress returns one latest report per story and kind, limited
// to the requested parent generation. Stable identity breaks equal timestamp ties.
func (db *DB) LatestImplementProgress(ctx context.Context, parent string, generation int64) ([]ImplementProgressReport, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+implementProgressColumns+` FROM implement_progress_reports p WHERE parent=? AND generation=? AND NOT EXISTS (SELECT 1 FROM implement_progress_reports n WHERE n.parent=p.parent AND n.generation=p.generation AND n.story_key=p.story_key AND n.kind=p.kind AND (n.recorded_at>p.recorded_at OR (n.recorded_at=p.recorded_at AND n.id>p.id))) ORDER BY story_key,kind`, parent, generation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reports []ImplementProgressReport
	for rows.Next() {
		r, err := scanImplementProgress(rows)
		if err != nil {
			return nil, err
		}
		reports = append(reports, r)
	}
	return reports, rows.Err()
}
