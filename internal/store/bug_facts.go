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

// Bug fact stages, matching where in the workflow the notice was noticed.
// BugFactStageOverflow marks the single synthetic entry recording how many
// further facts were dropped once the per-bug cap was reached; it is never a
// caller-supplied stage.
const (
	BugFactStageAnalysis = "analysis"
	BugFactStageRepair   = "repair"
	BugFactStageChecks   = "checks"
	BugFactStageDelivery = "delivery"
	BugFactStageOverflow = "overflow"
)

const (
	bugFactKind              = "fact"
	bugFactOverflowReference = "overflow"

	// BugFactCap is the maximum number of recorded facts kept per bug, each
	// bounded to bugFactMaxTextBytes, so the whole list stays a bounded
	// addition to the existing 60,000-character redacted review body.
	BugFactCap          = 32
	bugFactMaxTextBytes = 1024
)

// BugFact is one plain-language observation the factory recorded instead of
// stopping the workflow (a "policy" refusal downgraded to evidence; see
// REFUSAL_REVIEW.md). It is one bug_events row of kind "fact".
type BugFact struct {
	Stage      string
	Reference  string
	Text       string
	AttemptID  int64
	RecordedAt time.Time
}

type bugFactContent struct {
	Stage string `json:"stage"`
	Text  string `json:"text"`
}

type bugFactOverflowContent struct {
	Dropped int `json:"dropped"`
}

// recordBugFactTx appends one fact for session inside the caller's own
// transaction, so a fact and the step that continues past its former refusal
// commit or fail together (AC-010). An identical existing reference is a
// no-op (idempotent replay). Past the 32-fact cap, callers past the cap are
// folded into a single overflow fact naming how many were dropped; the caller
// never sees an error purely for exceeding the cap. Text longer than 1 KiB is
// truncated, never rejected.
func recordBugFactTx(ctx context.Context, tx *sql.Tx, session, attempt int64, stage, reference, text string) error {
	if session <= 0 {
		return errors.New("store: invalid Bug fact session")
	}
	switch stage {
	case BugFactStageAnalysis, BugFactStageRepair, BugFactStageChecks, BugFactStageDelivery:
	default:
		return fmt.Errorf("store: invalid Bug fact stage %q", stage)
	}
	if reference == "" {
		return errors.New("store: invalid Bug fact reference")
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("store: invalid Bug fact text")
	}
	if len(text) > bugFactMaxTextBytes {
		text = text[:bugFactMaxTextBytes]
	}
	if _, err := bugEventByReferenceTx(ctx, tx, session, bugFactKind, reference); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_events WHERE session_id=? AND kind=? AND reference<>?`, session, bugFactKind, bugFactOverflowReference).Scan(&count); err != nil {
		return err
	}
	if count >= BugFactCap {
		return bugFactOverflowTx(ctx, tx, session)
	}
	return bugEventJSONFullTx(ctx, tx, session, bugFactKind, reference, attempt, "", bugFactContent{Stage: stage, Text: text})
}

// bugFactOverflowTx records or increments the single overflow marker fact
// once the 32-fact cap is reached. It never fails just because the cap was
// hit; only a genuine storage error propagates.
func bugFactOverflowTx(ctx context.Context, tx *sql.Tx, session int64) error {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT content FROM bug_events WHERE session_id=? AND kind=? AND reference=?`, session, bugFactKind, bugFactOverflowReference).Scan(&raw)
	var dropped int
	switch {
	case err == nil:
		var existing bugFactOverflowContent
		if jerr := json.Unmarshal(raw, &existing); jerr != nil {
			return jerr
		}
		dropped = existing.Dropped
	case errors.Is(err, sql.ErrNoRows):
	default:
		return err
	}
	dropped++
	content, merr := json.Marshal(bugFactOverflowContent{Dropped: dropped})
	if merr != nil {
		return merr
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE bug_events SET content=? WHERE session_id=? AND kind=? AND reference=?`, content, session, bugFactKind, bugFactOverflowReference)
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bug_events(session_id,kind,reference,content,recorded_at) VALUES(?,?,?,?,?)`, session, bugFactKind, bugFactOverflowReference, content, FormatTime(time.Now()))
	return err
}

// bugFactsTx reads every recorded fact for session, optionally filtered to one
// stage (empty means every stage). Facts are returned in recorded order; a
// caller wanting stage order sorts stably, which keeps this a pure read.
func bugFactsTx(ctx context.Context, tx *sql.Tx, session int64, stage string) ([]BugFact, error) {
	rows, err := tx.QueryContext(ctx, `SELECT reference,content,attempt_id,recorded_at FROM bug_events WHERE session_id=? AND kind=? ORDER BY id ASC`, session, bugFactKind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BugFact
	for rows.Next() {
		var reference, recordedAt string
		var content []byte
		var attempt sql.NullInt64
		if err := rows.Scan(&reference, &content, &attempt, &recordedAt); err != nil {
			return nil, err
		}
		recorded, err := ParseTime(recordedAt)
		if err != nil {
			return nil, err
		}
		if reference == bugFactOverflowReference {
			var overflow bugFactOverflowContent
			if err := json.Unmarshal(content, &overflow); err != nil {
				return nil, err
			}
			out = append(out, BugFact{
				Stage:      BugFactStageOverflow,
				Reference:  reference,
				Text:       fmt.Sprintf("%d further findings were dropped once the %d-fact cap was reached.", overflow.Dropped, BugFactCap),
				RecordedAt: recorded,
			})
			continue
		}
		var c bugFactContent
		if err := json.Unmarshal(content, &c); err != nil {
			return nil, err
		}
		out = append(out, BugFact{Stage: c.Stage, Reference: reference, Text: c.Text, AttemptID: attempt.Int64, RecordedAt: recorded})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if stage == "" {
		return out, nil
	}
	filtered := out[:0]
	for _, f := range out {
		if f.Stage == stage {
			filtered = append(filtered, f)
		}
	}
	return filtered, nil
}

// RecordBugFact appends one fact for a bug outside any wider transaction. Most
// production call sites downgrade a former refusal to a fact from inside a
// store method that already holds its own transaction and call
// recordBugFactTx directly so the fact commits atomically with the step it
// lets continue (AC-004, AC-010); this wrapper exists for the few callers
// (turn's pure notice sites) that have no other write of their own to share a
// transaction with.
func (d *DB) RecordBugFact(ctx context.Context, session, attempt int64, stage, reference, text string) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		return recordBugFactTx(ctx, tx, session, attempt, stage, reference, text)
	})
}

// BugFacts reads every recorded fact for a bug, optionally filtered to one
// stage ("" for every stage). Used by the delivery review (every stage) and
// the analysis review (BugFactStageAnalysis only).
func (d *DB) BugFacts(ctx context.Context, session int64, stage string) ([]BugFact, error) {
	var out []BugFact
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = bugFactsTx(ctx, tx, session, stage)
		return e
	})
	return out, err
}
