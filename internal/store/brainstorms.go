package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Brainstorm storage errors; the brainstorm service maps them to plain refusals.
var (
	ErrBrainstormClosed    = errors.New("brainstorm is closed")
	ErrBrainstormQueueFull = errors.New("brainstorm scan queue is full")
)

// BrainstormRevisionError refuses a write whose expected revision is stale.
type BrainstormRevisionError struct{ Current int64 }

func (e BrainstormRevisionError) Error() string {
	return "brainstorm revision is stale; current revision " + strconv.FormatInt(e.Current, 10)
}

// BrainstormCapError refuses a write that would pass a named size cap.
type BrainstormCapError struct{ Cap string }

func (e BrainstormCapError) Error() string { return "brainstorm size limit reached: " + e.Cap }

// Brainstorm caps enforced in the write transactions.
const (
	BrainstormMaxEntryBytes    = 4 * 1024
	BrainstormMaxDocumentBytes = 64 * 1024
	BrainstormMaxOwnerEntries  = 500
	BrainstormMaxScans         = 200
	BrainstormMaxQueuedScans   = 5
	BrainstormExpiry           = 30 * 24 * time.Hour
)

// BrainstormOwner scopes every brainstorm read and write to one owner generation.
type BrainstormOwner struct {
	OwnerID, OwnerGeneration int64
}

// BrainstormRecord is one durable brainstorm header.
type BrainstormRecord struct {
	BrainstormOwner
	ID, RepositoryRef, State, CloseReason, GrantID string
	Revision, NextSeq, NextScanSeq                 int64
	CreatedAt, LastActivityAt, ClosedAt            time.Time
}

// BrainstormEntry is one append-only kept item.
type BrainstormEntry struct {
	ID, BrainstormID                                 string
	Seq                                              int64
	Kind, Text, DocumentName, IssueReference, ScanID string
	RequestKey, Digest                               string
	CreatedAt                                        time.Time
}

// BrainstormScan is one admitted question; JobState and JobSummary are read
// live from its shared durable job.
type BrainstormScan struct {
	BrainstormOwner
	ID, BrainstormID, RepositoryRef, RequestKey, Digest, Question string
	ScanSeq                                                       int64
	TeamVersion, TeamFiles, BasisCommit, GrantID, ConsentRevision string
	JobID, ExecutionID                                            string
	Answer, Research, Questions, Usage, Cost, FailureReason       string
	ExchangeSeq                                                   int64
	CreatedAt, FinishedAt                                         time.Time
	JobState, JobSummary                                          string
	// Purpose is "" (question) or "drafts".
	Purpose string
}

const brainstormColumns = `id,owner_id,owner_generation,repository_ref,state,close_reason,revision,next_seq,next_scan_seq,grant_id,created_at,last_activity_at,closed_at`
const brainstormEntryColumns = `id,brainstorm_id,seq,kind,text,document_name,issue_reference,scan_id,request_key,digest,created_at`
const brainstormScanColumns = `s.id,s.brainstorm_id,s.scan_seq,s.owner_id,s.owner_generation,s.repository_ref,s.request_key,s.digest,s.question,s.team_version,s.team_files,s.basis_commit,s.grant_id,s.consent_revision,s.job_id,s.execution_id,s.answer,s.research,s.questions,s.usage,s.cost,s.failure_reason,s.exchange_seq,s.created_at,s.finished_at,COALESCE(j.state,''),COALESCE(j.summary,''),COALESCE(s.purpose,'')`
const brainstormScanFrom = ` FROM brainstorm_scans s LEFT JOIN durable_workflow_jobs j ON j.id=s.job_id`

func parseBrainstormTime(raw string, out *time.Time) error {
	if raw == "" {
		*out = time.Time{}
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	*out = t
	return err
}

func scanBrainstorm(row scanner) (BrainstormRecord, error) {
	var b BrainstormRecord
	var created, activity, closed string
	err := row.Scan(&b.ID, &b.OwnerID, &b.OwnerGeneration, &b.RepositoryRef, &b.State, &b.CloseReason, &b.Revision, &b.NextSeq, &b.NextScanSeq, &b.GrantID, &created, &activity, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if err != nil {
		return b, err
	}
	if err = parseBrainstormTime(created, &b.CreatedAt); err == nil {
		if err = parseBrainstormTime(activity, &b.LastActivityAt); err == nil {
			err = parseBrainstormTime(closed, &b.ClosedAt)
		}
	}
	return b, err
}

func scanBrainstormEntry(row scanner) (BrainstormEntry, error) {
	var e BrainstormEntry
	var created string
	err := row.Scan(&e.ID, &e.BrainstormID, &e.Seq, &e.Kind, &e.Text, &e.DocumentName, &e.IssueReference, &e.ScanID, &e.RequestKey, &e.Digest, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	err = parseBrainstormTime(created, &e.CreatedAt)
	return e, err
}

func scanBrainstormScan(row scanner) (BrainstormScan, error) {
	var s BrainstormScan
	var created, finished string
	err := row.Scan(&s.ID, &s.BrainstormID, &s.ScanSeq, &s.OwnerID, &s.OwnerGeneration, &s.RepositoryRef, &s.RequestKey, &s.Digest, &s.Question, &s.TeamVersion, &s.TeamFiles, &s.BasisCommit, &s.GrantID, &s.ConsentRevision, &s.JobID, &s.ExecutionID, &s.Answer, &s.Research, &s.Questions, &s.Usage, &s.Cost, &s.FailureReason, &s.ExchangeSeq, &created, &finished, &s.JobState, &s.JobSummary, &s.Purpose)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	if err = parseBrainstormTime(created, &s.CreatedAt); err == nil {
		err = parseBrainstormTime(finished, &s.FinishedAt)
	}
	return s, err
}

func brainstormOwnerValid(o BrainstormOwner) bool { return o.OwnerID > 0 && o.OwnerGeneration > 0 }

// expireBrainstormsTx closes the owner's open brainstorms for a repository (or
// one id when repository is empty) that have had no activity for 30 days.
func expireBrainstormsTx(ctx context.Context, tx *sql.Tx, o BrainstormOwner, repository, id string, at time.Time) error {
	cutoff := ownerActionTime(at.Add(-BrainstormExpiry))
	query := `UPDATE brainstorms SET state='closed',close_reason='expired',closed_at=? WHERE owner_id=? AND owner_generation=? AND state='open' AND last_activity_at<=?`
	args := []any{ownerActionTime(at), o.OwnerID, o.OwnerGeneration, cutoff}
	if repository != "" {
		query += ` AND repository_ref=?`
		args = append(args, repository)
	}
	if id != "" {
		query += ` AND id=?`
		args = append(args, id)
	}
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

func readBrainstormTx(ctx context.Context, tx *sql.Tx, o BrainstormOwner, id string, at time.Time) (BrainstormRecord, error) {
	if !brainstormOwnerValid(o) || id == "" {
		return BrainstormRecord{}, ErrNotFound
	}
	if err := expireBrainstormsTx(ctx, tx, o, "", id, at); err != nil {
		return BrainstormRecord{}, err
	}
	return scanBrainstorm(tx.QueryRowContext(ctx, `SELECT `+brainstormColumns+` FROM brainstorms WHERE owner_id=? AND owner_generation=? AND id=?`, o.OwnerID, o.OwnerGeneration, id))
}

// OpenBrainstorm returns the owner's open brainstorm for the repository, or
// creates b when none is open. The bool reports a new brainstorm.
func (db *DB) OpenBrainstorm(ctx context.Context, b BrainstormRecord, at time.Time) (BrainstormRecord, bool, error) {
	if !brainstormOwnerValid(b.BrainstormOwner) || b.ID == "" || b.RepositoryRef == "" || len(b.RepositoryRef) > 1024 || at.IsZero() {
		return BrainstormRecord{}, false, ErrConnectorInvalid
	}
	var out BrainstormRecord
	created := false
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := expireBrainstormsTx(ctx, tx, b.BrainstormOwner, b.RepositoryRef, "", at); err != nil {
			return err
		}
		old, err := scanBrainstorm(tx.QueryRowContext(ctx, `SELECT `+brainstormColumns+` FROM brainstorms WHERE owner_id=? AND owner_generation=? AND repository_ref=? AND state='open'`, b.OwnerID, b.OwnerGeneration, b.RepositoryRef))
		if err == nil {
			out = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		now := ownerActionTime(at)
		if _, err = tx.ExecContext(ctx, `INSERT INTO brainstorms(`+brainstormColumns+`) VALUES(?,?,?,?,'open','',0,1,1,?,?,?,'')`, b.ID, b.OwnerID, b.OwnerGeneration, b.RepositoryRef, b.GrantID, now, now); err != nil {
			return err
		}
		created = true
		out, err = scanBrainstorm(tx.QueryRowContext(ctx, `SELECT `+brainstormColumns+` FROM brainstorms WHERE id=?`, b.ID))
		return err
	})
	return out, created, err
}

// ReadBrainstorm reads one owner-scoped brainstorm after lazy expiry.
func (db *DB) ReadBrainstorm(ctx context.Context, o BrainstormOwner, id string, at time.Time) (BrainstormRecord, error) {
	var out BrainstormRecord
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = readBrainstormTx(ctx, tx, o, id, at)
		return err
	})
	return out, err
}

// ListBrainstorms pages the owner's brainstorms for a repository newest first,
// after the (created_at, id) position when afterID is set.
func (db *DB) ListBrainstorms(ctx context.Context, o BrainstormOwner, repository string, afterCreated time.Time, afterID string, limit int, at time.Time) ([]BrainstormRecord, error) {
	if !brainstormOwnerValid(o) || repository == "" || limit < 1 || limit > 101 {
		return nil, ErrConnectorInvalid
	}
	var out []BrainstormRecord
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := expireBrainstormsTx(ctx, tx, o, repository, "", at); err != nil {
			return err
		}
		query := `SELECT ` + brainstormColumns + ` FROM brainstorms WHERE owner_id=? AND owner_generation=? AND repository_ref=?`
		args := []any{o.OwnerID, o.OwnerGeneration, repository}
		if afterID != "" {
			query += ` AND (created_at<? OR (created_at=? AND id<?))`
			c := ownerActionTime(afterCreated)
			args = append(args, c, c, afterID)
		}
		query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
		args = append(args, limit)
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			b, err := scanBrainstorm(rows)
			if err != nil {
				return err
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}

// BrainstormEntryCounts counts entries per kind.
func (db *DB) BrainstormEntryCounts(ctx context.Context, id string) (map[string]int64, error) {
	return db.brainstormCounts(ctx, `SELECT kind,COUNT(*) FROM brainstorm_entries WHERE brainstorm_id=? GROUP BY kind`, id)
}

// BrainstormScanStateCounts counts scans per live shared job state.
func (db *DB) BrainstormScanStateCounts(ctx context.Context, id string) (map[string]int64, error) {
	return db.brainstormCounts(ctx, `SELECT COALESCE(j.state,''),COUNT(*)`+brainstormScanFrom+` WHERE s.brainstorm_id=? GROUP BY COALESCE(j.state,'')`, id)
}

func (db *DB) brainstormCounts(ctx context.Context, query, id string) (map[string]int64, error) {
	rows, err := db.sql.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var n int64
		if err = rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// BrainstormEntries returns entries with seq greater than afterSeq, oldest
// first, optionally of one kind.
func (db *DB) BrainstormEntries(ctx context.Context, id, kind string, afterSeq int64, limit int) ([]BrainstormEntry, error) {
	query := `SELECT ` + brainstormEntryColumns + ` FROM brainstorm_entries WHERE brainstorm_id=? AND seq>?`
	args := []any{id, afterSeq}
	if kind != "" {
		query += ` AND kind=?`
		args = append(args, kind)
	}
	query += ` ORDER BY seq LIMIT ?`
	args = append(args, limit)
	return db.brainstormEntries(ctx, query, args...)
}

// NewestBrainstormEntries returns the newest limit entries, oldest first.
func (db *DB) NewestBrainstormEntries(ctx context.Context, id string, limit int) ([]BrainstormEntry, error) {
	return db.brainstormEntries(ctx, `SELECT * FROM (SELECT `+brainstormEntryColumns+` FROM brainstorm_entries WHERE brainstorm_id=? ORDER BY seq DESC LIMIT ?) ORDER BY seq`, id, limit)
}

func (db *DB) brainstormEntries(ctx context.Context, query string, args ...any) ([]BrainstormEntry, error) {
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BrainstormEntry{}
	for rows.Next() {
		e, err := scanBrainstormEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AddBrainstormEntry appends one owner entry. A known request key with the
// same digest returns the first entry and replayed; another digest is
// ErrWorkflowConflict.
func (db *DB) AddBrainstormEntry(ctx context.Context, o BrainstormOwner, e BrainstormEntry, expectedRevision int64, at time.Time) (BrainstormRecord, BrainstormEntry, bool, error) {
	if e.ID == "" || e.BrainstormID == "" || e.RequestKey == "" || len(e.RequestKey) > 256 || strings.HasPrefix(e.RequestKey, "scan:") || e.Digest == "" || at.IsZero() || !utf8.ValidString(e.Text) || strings.TrimSpace(e.Text) == "" || e.ScanID != "" {
		return BrainstormRecord{}, BrainstormEntry{}, false, ErrConnectorInvalid
	}
	switch e.Kind {
	case "idea", "decision", "open_question", "related_issue", "document_note":
	default:
		return BrainstormRecord{}, BrainstormEntry{}, false, ErrConnectorInvalid
	}
	if (e.Kind == "document_note") != (e.DocumentName != "") || (e.Kind != "related_issue" && e.IssueReference != "") {
		return BrainstormRecord{}, BrainstormEntry{}, false, ErrConnectorInvalid
	}
	if len(e.Text) > BrainstormMaxEntryBytes {
		return BrainstormRecord{}, BrainstormEntry{}, false, BrainstormCapError{Cap: "an entry is at most 4 KB"}
	}
	var b BrainstormRecord
	out := e
	replayed := false
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		if b, err = readBrainstormTx(ctx, tx, o, e.BrainstormID, at); err != nil {
			return err
		}
		old, err := scanBrainstormEntry(tx.QueryRowContext(ctx, `SELECT `+brainstormEntryColumns+` FROM brainstorm_entries WHERE brainstorm_id=? AND request_key=?`, b.ID, e.RequestKey))
		if err == nil {
			if old.Digest != e.Digest {
				return ErrWorkflowConflict
			}
			out, replayed = old, true
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if b.State != "open" {
			return ErrBrainstormClosed
		}
		if b.Revision != expectedRevision {
			return BrainstormRevisionError{Current: b.Revision}
		}
		var owned, documents int64
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN kind='document_note' THEN LENGTH(CAST(text AS BLOB)) ELSE 0 END),0) FROM brainstorm_entries WHERE brainstorm_id=? AND kind<>'exchange'`, b.ID).Scan(&owned, &documents); err != nil {
			return err
		}
		if owned >= BrainstormMaxOwnerEntries {
			return BrainstormCapError{Cap: "a brainstorm holds at most 500 owner entries"}
		}
		if e.Kind == "document_note" && documents+int64(len(e.Text)) > BrainstormMaxDocumentBytes {
			return BrainstormCapError{Cap: "document notes together are at most 64 KB per brainstorm"}
		}
		out.Seq = b.NextSeq
		out.CreatedAt = at
		if _, err = tx.ExecContext(ctx, `INSERT INTO brainstorm_entries(`+brainstormEntryColumns+`) VALUES(?,?,?,?,?,?,?,'',?,?,?)`, e.ID, b.ID, out.Seq, e.Kind, e.Text, e.DocumentName, e.IssueReference, e.RequestKey, e.Digest, ownerActionTime(at)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE brainstorms SET next_seq=next_seq+1,revision=revision+1,last_activity_at=? WHERE id=?`, ownerActionTime(at), b.ID); err != nil {
			return err
		}
		b, err = scanBrainstorm(tx.QueryRowContext(ctx, `SELECT `+brainstormColumns+` FROM brainstorms WHERE id=?`, b.ID))
		return err
	})
	return b, out, replayed, err
}

// CloseBrainstorm closes an open brainstorm by owner request. Closing a
// brainstorm the owner already closed at expectedRevision+1 is a replay.
func (db *DB) CloseBrainstorm(ctx context.Context, o BrainstormOwner, id string, expectedRevision int64, at time.Time) (BrainstormRecord, bool, error) {
	var b BrainstormRecord
	replayed := false
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		if b, err = readBrainstormTx(ctx, tx, o, id, at); err != nil {
			return err
		}
		if b.State != "open" {
			if b.CloseReason == "owner" && b.Revision == expectedRevision+1 {
				replayed = true
				return nil
			}
			return ErrBrainstormClosed
		}
		if b.Revision != expectedRevision {
			return BrainstormRevisionError{Current: b.Revision}
		}
		now := ownerActionTime(at)
		if _, err = tx.ExecContext(ctx, `UPDATE brainstorms SET state='closed',close_reason='owner',closed_at=?,revision=revision+1,last_activity_at=? WHERE id=?`, now, now, b.ID); err != nil {
			return err
		}
		b, err = scanBrainstorm(tx.QueryRowContext(ctx, `SELECT `+brainstormColumns+` FROM brainstorms WHERE id=?`, b.ID))
		return err
	})
	return b, replayed, err
}

// BrainstormScans returns scans with scan_seq greater than afterSeq, oldest
// first, each with its live shared job state.
func (db *DB) BrainstormScans(ctx context.Context, id string, afterSeq int64, limit int) ([]BrainstormScan, error) {
	return db.brainstormScans(ctx, `SELECT `+brainstormScanColumns+brainstormScanFrom+` WHERE s.brainstorm_id=? AND s.scan_seq>? ORDER BY s.scan_seq LIMIT ?`, id, afterSeq, limit)
}

// NewestBrainstormScans returns the newest limit scans, oldest first.
func (db *DB) NewestBrainstormScans(ctx context.Context, id string, limit int) ([]BrainstormScan, error) {
	out, err := db.brainstormScans(ctx, `SELECT `+brainstormScanColumns+brainstormScanFrom+` WHERE s.brainstorm_id=? ORDER BY s.scan_seq DESC LIMIT ?`, id, limit)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, err
}

func (db *DB) brainstormScans(ctx context.Context, query string, args ...any) ([]BrainstormScan, error) {
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BrainstormScan{}
	for rows.Next() {
		s, err := scanBrainstormScan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ReadBrainstormScan reads one owner-scoped scan with its live job state.
func (db *DB) ReadBrainstormScan(ctx context.Context, o BrainstormOwner, id string) (BrainstormScan, error) {
	if !brainstormOwnerValid(o) || id == "" {
		return BrainstormScan{}, ErrNotFound
	}
	return scanBrainstormScan(db.sql.QueryRowContext(ctx, `SELECT `+brainstormScanColumns+brainstormScanFrom+` WHERE s.owner_id=? AND s.owner_generation=? AND s.id=?`, o.OwnerID, o.OwnerGeneration, id))
}

// BrainstormScanByRequest finds a prior admission by its request key.
func (db *DB) BrainstormScanByRequest(ctx context.Context, o BrainstormOwner, brainstormID, key string) (BrainstormScan, error) {
	return scanBrainstormScan(db.sql.QueryRowContext(ctx, `SELECT `+brainstormScanColumns+brainstormScanFrom+` WHERE s.owner_id=? AND s.owner_generation=? AND s.brainstorm_id=? AND s.request_key=?`, o.OwnerID, o.OwnerGeneration, brainstormID, key))
}

// AdmitBrainstormScan writes the scan admission, its shared durable job of
// kind brainstorm_scan and the queued attempt in one transaction. A known
// request key with the same digest returns the first scan and replayed.
func (db *DB) AdmitBrainstormScan(ctx context.Context, s BrainstormScan, at time.Time) (BrainstormScan, bool, error) {
	if !brainstormOwnerValid(s.BrainstormOwner) || s.ID == "" || s.BrainstormID == "" || s.RequestKey == "" || len(s.RequestKey) > 256 || s.Digest == "" || s.JobID == "" || s.ExecutionID == "" || s.GrantID == "" || s.TeamVersion == "" || !json.Valid([]byte(s.TeamFiles)) || strings.TrimSpace(s.Question) == "" || (s.Purpose != "" && s.Purpose != BrainstormPurposeQuestion && s.Purpose != BrainstormPurposeDrafts) || !utf8.ValidString(s.Question) || len(s.Question) > BrainstormMaxEntryBytes || at.IsZero() {
		return BrainstormScan{}, false, ErrConnectorInvalid
	}
	var id string
	replayed := false
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		b, err := readBrainstormTx(ctx, tx, s.BrainstormOwner, s.BrainstormID, at)
		if err != nil {
			return err
		}
		old, err := scanBrainstormScan(tx.QueryRowContext(ctx, `SELECT `+brainstormScanColumns+brainstormScanFrom+` WHERE s.brainstorm_id=? AND s.request_key=?`, b.ID, s.RequestKey))
		if err == nil {
			if old.Digest != s.Digest {
				return ErrWorkflowConflict
			}
			id, replayed = old.ID, true
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if b.State != "open" {
			return ErrBrainstormClosed
		}
		var total, queued int64
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN j.state='queued' THEN 1 ELSE 0 END),0)`+brainstormScanFrom+` WHERE s.brainstorm_id=?`, b.ID).Scan(&total, &queued); err != nil {
			return err
		}
		if total >= BrainstormMaxScans {
			return BrainstormCapError{Cap: "a brainstorm holds at most 200 scans"}
		}
		if queued >= BrainstormMaxQueuedScans {
			return ErrBrainstormQueueFull
		}
		now := ownerActionTime(at)
		if _, err = tx.ExecContext(ctx, `INSERT INTO brainstorm_scans(id,brainstorm_id,scan_seq,owner_id,owner_generation,repository_ref,request_key,digest,question,team_version,team_files,basis_commit,grant_id,consent_revision,job_id,execution_id,created_at,purpose) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, s.ID, b.ID, b.NextScanSeq, b.OwnerID, b.OwnerGeneration, b.RepositoryRef, s.RequestKey, s.Digest, s.Question, s.TeamVersion, s.TeamFiles, s.BasisCommit, s.GrantID, s.ConsentRevision, s.JobID, s.ExecutionID, now, brainstormPurposeColumn(s.Purpose)); err != nil {
			return err
		}
		snapshot, _ := json.Marshal(map[string]string{"brainstorm": b.ID, "scan": s.ID, "repository": b.RepositoryRef})
		if _, err = tx.ExecContext(ctx, `INSERT INTO durable_workflow_jobs(`+workflowJobColumns+`,admission_kind) VALUES(?,?,?,?,'queued',0,'{}','[]','','',?,?,'brainstorm_scan')`, s.JobID, strconv.FormatInt(b.OwnerID, 10), b.OwnerGeneration, string(snapshot), now, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO durable_workflow_attempts(job_id,proposal_id,execution_id,state,queued_at,admission_kind,brainstorm_scan_id) VALUES(?,NULL,?,'queued',?,'brainstorm_scan',?)`, s.JobID, s.ExecutionID, now, s.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE brainstorms SET next_scan_seq=next_scan_seq+1,last_activity_at=? WHERE id=?`, now, b.ID); err != nil {
			return err
		}
		id = s.ID
		return nil
	})
	if err != nil {
		return BrainstormScan{}, false, err
	}
	out, err := db.ReadBrainstormScan(ctx, s.BrainstormOwner, id)
	return out, replayed, err
}

// CancelBrainstormScan cancels a queued scan or records a running stop
// request through the shared job state. The bool reports that a terminal
// completion already won the race.
func (db *DB) CancelBrainstormScan(ctx context.Context, o BrainstormOwner, id string, at time.Time) (BrainstormScan, bool, error) {
	won := false
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		s, err := scanBrainstormScan(tx.QueryRowContext(ctx, `SELECT `+brainstormScanColumns+brainstormScanFrom+` WHERE s.owner_id=? AND s.owner_generation=? AND s.id=?`, o.OwnerID, o.OwnerGeneration, id))
		if err != nil {
			return err
		}
		now := ownerActionTime(at)
		switch s.JobState {
		case "queued":
			if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_attempts SET state='cancelled',finished_at=? WHERE job_id=? AND state='queued'`, now, s.JobID); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_jobs SET state='cancelled',revision=revision+1,updated_at=? WHERE id=?`, now, s.JobID); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `UPDATE brainstorm_scans SET finished_at=? WHERE id=? AND finished_at=''`, now, s.ID)
		case "running":
			if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_attempts SET state='stop_requested' WHERE job_id=? AND state='running'`, s.JobID); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `UPDATE durable_workflow_jobs SET state='stop_requested',updated_at=? WHERE id=?`, now, s.JobID)
		case "succeeded", "failed":
			won = true
		case "stop_requested", "cancelled", "uncertain":
		default:
			err = fmt.Errorf("brainstorm scan job state %q", s.JobState)
		}
		return err
	})
	if err != nil {
		return BrainstormScan{}, false, err
	}
	out, err := db.ReadBrainstormScan(ctx, o, id)
	return out, won, err
}
