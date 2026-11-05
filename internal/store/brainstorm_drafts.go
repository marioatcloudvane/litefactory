package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"
)

// Brainstorm scan purposes; a NULL purpose column reads as question.
const (
	BrainstormPurposeQuestion = "question"
	BrainstormPurposeDrafts   = "drafts"
)

// Draft set states.
const (
	BrainstormDraftSetLatest     = "latest"
	BrainstormDraftSetSuperseded = "superseded"
	BrainstormDraftSetUsed       = "used"
)

// BrainstormDraftSet is one immutable set of drafts from one accepted drafts scan.
type BrainstormDraftSet struct {
	ID, BrainstormID, ScanID string
	Seq                      int64
	State, Summary           string
	CreatedAt                time.Time
	Drafts                   []BrainstormDraft
}

// BrainstormDraft is one draft of a set. Ledger is the agentresult.Research shape.
type BrainstormDraft struct {
	ID, DraftSetID, Key string
	Ordinal             int
	Title, Size, Impact string
	RelatedIssues       []string
	DuplicateWarning    string
	SearchHits          json.RawMessage
	Ledger              json.RawMessage
	Digest              string
}

// BrainstormDraftSetInput is the validated, protected draft set kept when a drafts scan succeeds.
type BrainstormDraftSetInput struct {
	Summary string
	Drafts  []BrainstormDraft
}

func brainstormPurposeColumn(purpose string) any {
	if purpose == "" || purpose == BrainstormPurposeQuestion {
		return nil
	}
	return purpose
}

func brainstormRandomID(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b[:]), nil
}

// insertBrainstormDraftSetTx stores one set for a drafts scan, superseding the
// previous latest unused set of the brainstorm.
func insertBrainstormDraftSetTx(ctx context.Context, tx *sql.Tx, brainstormID, scanID string, in BrainstormDraftSetInput, at time.Time) error {
	if len(in.Drafts) == 0 || len(in.Drafts) > 5 || !utf8.ValidString(in.Summary) {
		return ErrConnectorInvalid
	}
	id, err := brainstormRandomID("draftset-")
	if err != nil {
		return err
	}
	var seq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM brainstorm_draft_sets WHERE brainstorm_id=?`, brainstormID).Scan(&seq); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE brainstorm_draft_sets SET state='superseded' WHERE brainstorm_id=? AND state='latest'`, brainstormID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO brainstorm_draft_sets(id,brainstorm_id,scan_id,seq,state,summary,created_at) VALUES(?,?,?,?,'latest',?,?)`, id, brainstormID, scanID, seq, in.Summary, ownerActionTime(at)); err != nil {
		return err
	}
	for i, d := range in.Drafts {
		draftID, e := brainstormRandomID("draft-")
		if e != nil {
			return e
		}
		related, e := json.Marshal(append([]string{}, d.RelatedIssues...))
		if e != nil {
			return e
		}
		if !json.Valid(d.Ledger) {
			return ErrConnectorInvalid
		}
		sum := sha256.Sum256(d.Ledger)
		if _, e = tx.ExecContext(ctx, `INSERT INTO brainstorm_drafts(id,draft_set_id,key,ordinal,title,size,impact,related_issues,duplicate_warning,search_hits,ledger,digest) VALUES(?,?,?,?,?,?,?,?,?,'',?,?)`, draftID, id, d.Key, i+1, d.Title, d.Size, d.Impact, string(related), d.DuplicateWarning, string(d.Ledger), hex.EncodeToString(sum[:])); e != nil {
			return e
		}
	}
	return nil
}

const brainstormDraftSetColumns = `id,brainstorm_id,scan_id,seq,state,summary,created_at`
const brainstormDraftColumns = `id,draft_set_id,key,ordinal,title,size,impact,related_issues,duplicate_warning,search_hits,ledger,digest`

func scanBrainstormDraftSet(row scanner) (BrainstormDraftSet, error) {
	var s BrainstormDraftSet
	var created string
	err := row.Scan(&s.ID, &s.BrainstormID, &s.ScanID, &s.Seq, &s.State, &s.Summary, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	err = parseBrainstormTime(created, &s.CreatedAt)
	return s, err
}

func scanBrainstormDraft(row scanner) (BrainstormDraft, error) {
	var d BrainstormDraft
	var related, hits, ledger string
	err := row.Scan(&d.ID, &d.DraftSetID, &d.Key, &d.Ordinal, &d.Title, &d.Size, &d.Impact, &related, &d.DuplicateWarning, &hits, &ledger, &d.Digest)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	if err = json.Unmarshal([]byte(related), &d.RelatedIssues); err != nil {
		return d, err
	}
	if hits != "" {
		d.SearchHits = json.RawMessage(hits)
	}
	d.Ledger = json.RawMessage(ledger)
	return d, nil
}

func (db *DB) brainstormDraftSetWithDrafts(ctx context.Context, s BrainstormDraftSet, err error) (BrainstormDraftSet, error) {
	if err != nil {
		return s, err
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+brainstormDraftColumns+` FROM brainstorm_drafts WHERE draft_set_id=? ORDER BY ordinal`, s.ID)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	s.Drafts = []BrainstormDraft{}
	for rows.Next() {
		d, e := scanBrainstormDraft(rows)
		if e != nil {
			return s, e
		}
		s.Drafts = append(s.Drafts, d)
	}
	return s, rows.Err()
}

// ReadBrainstormDraftSet reads one set with its drafts ordered by ordinal.
func (db *DB) ReadBrainstormDraftSet(ctx context.Context, id string) (BrainstormDraftSet, error) {
	s, err := scanBrainstormDraftSet(db.sql.QueryRowContext(ctx, `SELECT `+brainstormDraftSetColumns+` FROM brainstorm_draft_sets WHERE id=?`, id))
	return db.brainstormDraftSetWithDrafts(ctx, s, err)
}

// LatestBrainstormDraftSet reads the newest set of a brainstorm by seq, whatever its state.
func (db *DB) LatestBrainstormDraftSet(ctx context.Context, brainstormID string) (BrainstormDraftSet, error) {
	s, err := scanBrainstormDraftSet(db.sql.QueryRowContext(ctx, `SELECT `+brainstormDraftSetColumns+` FROM brainstorm_draft_sets WHERE brainstorm_id=? ORDER BY seq DESC LIMIT 1`, brainstormID))
	return db.brainstormDraftSetWithDrafts(ctx, s, err)
}

// BrainstormDraftSetForScan reads the set stored for one drafts scan.
func (db *DB) BrainstormDraftSetForScan(ctx context.Context, scanID string) (BrainstormDraftSet, error) {
	s, err := scanBrainstormDraftSet(db.sql.QueryRowContext(ctx, `SELECT `+brainstormDraftSetColumns+` FROM brainstorm_draft_sets WHERE scan_id=?`, scanID))
	return db.brainstormDraftSetWithDrafts(ctx, s, err)
}

// ReadBrainstormDraft reads one draft.
func (db *DB) ReadBrainstormDraft(ctx context.Context, id string) (BrainstormDraft, error) {
	return scanBrainstormDraft(db.sql.QueryRowContext(ctx, `SELECT `+brainstormDraftColumns+` FROM brainstorm_drafts WHERE id=?`, id))
}

// SetBrainstormDraftSearchHits records the factory's duplicate search once;
// the draft content itself never changes. It reports whether it wrote.
func (db *DB) SetBrainstormDraftSearchHits(ctx context.Context, draftID string, hits json.RawMessage) (bool, error) {
	if !json.Valid(hits) {
		return false, ErrConnectorInvalid
	}
	res, err := db.sql.ExecContext(ctx, `UPDATE brainstorm_drafts SET search_hits=? WHERE id=? AND search_hits=''`, string(hits), draftID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
