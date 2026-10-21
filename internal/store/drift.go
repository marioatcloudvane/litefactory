package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrDriftConflict reports stale writer authority or a conflicting request identity.
var ErrDriftConflict = errors.New("drift: stale writer or conflicting request")

// ErrDriftBudget reports exhaustion of a retained analysis allowance.
var ErrDriftBudget = errors.New("drift: work allowance exhausted")

// DriftLimits holds immutable source, delivery and analyst work ceilings.
type DriftLimits struct {
	Paths              int   `json:"paths"`
	Issues             int   `json:"issues"`
	Comments           int   `json:"comments"`
	Requests           int   `json:"requests"`
	SourceBytes        int64 `json:"source_bytes"`
	ItemBytes          int   `json:"item_bytes"`
	AcquisitionSeconds int   `json:"acquisition_seconds"`
	AgentSeconds       int   `json:"agent_seconds"`
	Calls              int   `json:"calls"`
	OutboundBytes      int64 `json:"outbound_bytes"`
	ResultBytes        int   `json:"result_bytes"`
	Proposals          int   `json:"proposals"`
}

// DriftUsage records cumulative charges against an analysis's retained limits.
type DriftUsage struct {
	Paths         int   `json:"paths"`
	Comments      int   `json:"comments"`
	Requests      int   `json:"requests"`
	SourceBytes   int64 `json:"source_bytes"`
	Calls         int   `json:"calls"`
	OutboundBytes int64 `json:"outbound_bytes"`
	Invocations   int   `json:"invocations"`
}

// DriftRange identifies a half-open byte range in a sanitized evidence excerpt.
type DriftRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// DriftEvidence retains sanitized source content and its provenance or expiry metadata.
type DriftEvidence struct {
	ID              string          `json:"id"`
	AnalysisID      string          `json:"analysis_id"`
	Kind            string          `json:"kind"`
	Locator         string          `json:"locator"`
	Path            string          `json:"path,omitempty"`
	Commit          string          `json:"commit,omitempty"`
	Blob            string          `json:"blob,omitempty"`
	Issue           int             `json:"issue,omitempty"`
	CommentID       int64           `json:"comment_id,omitempty"`
	URL             string          `json:"url,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	CollectedAt     time.Time       `json:"collected_at"`
	Text            string          `json:"text"`
	Digest          string          `json:"digest"`
	RedactionPolicy string          `json:"redaction_policy"`
	Omission        string          `json:"omission,omitempty"`
	ExcerptExpired  bool            `json:"excerpt_expired,omitempty"`
	ExcerptBytes    int             `json:"excerpt_bytes,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

// DriftOffer records an evidence range reserved for delivery to the analyst.
type DriftOffer struct {
	EvidenceID string `json:"evidence_id"`
	Digest     string `json:"digest"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
}

// DriftProposalLink identifies the exact unapproved version created for a candidate.
type DriftProposalLink struct {
	CandidateKey string `json:"candidate_key"`
	ProposalID   string `json:"proposal_id"`
	Version      int64  `json:"version"`
	Digest       string `json:"digest"`
}

// DriftAnalysis is a persisted invocation with immutable scope and fenced progress.
type DriftAnalysis struct {
	ID            string              `json:"id"`
	RunID         string              `json:"run_id"`
	Repository    string              `json:"repository"`
	RequestKey    string              `json:"request_key,omitempty"`
	RequestDigest string              `json:"request_digest"`
	Scope         json.RawMessage     `json:"scope"`
	Pinned        json.RawMessage     `json:"pinned,omitempty"`
	Limits        DriftLimits         `json:"limits"`
	Usage         DriftUsage          `json:"usage"`
	Generation    int64               `json:"generation"`
	Version       int64               `json:"version"`
	Execution     string              `json:"execution"`
	Disposition   string              `json:"disposition"`
	Coverage      json.RawMessage     `json:"coverage,omitempty"`
	Standards     json.RawMessage     `json:"standards,omitempty"`
	Provenance    json.RawMessage     `json:"provenance,omitempty"`
	Report        json.RawMessage     `json:"report,omitempty"`
	Offers        []DriftOffer        `json:"offers"`
	ProposalLinks []DriftProposalLink `json:"proposal_links"`
	Reason        string              `json:"reason,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

// DriftReceipt recovers one accepted report and its original proposal references.
type DriftReceipt struct {
	AnalysisID    string              `json:"analysis_id"`
	RequestID     string              `json:"request_id"`
	Digest        string              `json:"digest"`
	Report        json.RawMessage     `json:"report"`
	ProposalLinks []DriftProposalLink `json:"proposal_links"`
	CreatedAt     time.Time           `json:"created_at"`
}

// DriftCandidate pairs an analysis-local key with a host-validated proposal.
type DriftCandidate struct {
	Key      string
	Proposal StandardsProposalVersion
}

func readDrift(ctx context.Context, tx *sql.Tx, id string) (DriftAnalysis, error) {
	var a DriftAnalysis
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT record_json FROM drift_analyses WHERE id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	err = json.Unmarshal([]byte(raw), &a)
	return a, err
}
func writeDrift(ctx context.Context, tx *sql.Tx, a DriftAnalysis) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE drift_analyses SET generation=?,version=?,execution=?,record_json=? WHERE id=?`, a.Generation, a.Version, a.Execution, string(raw), a.ID)
	return err
}

// CreateDriftAnalysis allocates an analysis and standalone analyst run atomically.
// An identical keyed request recovers the original analysis without creating work.
func (db *DB) CreateDriftAnalysis(ctx context.Context, a DriftAnalysis) (DriftAnalysis, bool, error) {
	if a.ID == "" || a.Repository == "" || a.RequestDigest == "" {
		return a, false, errors.New("drift: identity and request required")
	}
	if a.RunID == "" {
		a.RunID = a.ID
	}
	created := false
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		var existing string
		if a.RequestKey != "" {
			err := tx.QueryRowContext(ctx, `SELECT id FROM drift_analyses WHERE repository=? AND request_key=?`, a.Repository, a.RequestKey).Scan(&existing)
			if err == nil {
				old, e := readDrift(ctx, tx, existing)
				if e != nil {
					return e
				}
				if old.RequestDigest != a.RequestDigest {
					return ErrDriftConflict
				}
				a = old
				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		a.Generation = 1
		a.Version = 1
		a.Execution = "running"
		a.Disposition = "not_evaluated"
		a.CreatedAt = nowUTC()
		a.UpdatedAt = a.CreatedAt
		raw, e := json.Marshal(a)
		if e != nil {
			return e
		}
		var key any
		if a.RequestKey != "" {
			key = a.RequestKey
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO drift_analyses(id,run_id,repository,request_key,request_digest,generation,version,execution,record_json) VALUES(?,?,?,?,?,?,?,?,?)`, a.ID, a.RunID, a.Repository, key, a.RequestDigest, a.Generation, a.Version, a.Execution, string(raw))
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO drift_analyst_runs(id,analysis_id,created_at) VALUES(?,?,?)`, a.RunID, a.ID, FormatTime(a.CreatedAt))
		created = e == nil
		if e != nil {
			return e
		}
		return attachAnalystAllocation(ctx, tx, "drift", a.ID, a.RequestKey)
	})
	return a, created && err == nil, err
}

// GetDriftAnalysis reads retained analysis state without changing execution.
func (db *DB) GetDriftAnalysis(ctx context.Context, id string) (DriftAnalysis, error) {
	var a DriftAnalysis
	err := db.withTx(ctx, func(tx *sql.Tx) error { var e error; a, e = readDrift(ctx, tx, id); return e })
	return a, err
}

// ListDriftAnalyses returns a bounded ID-ordered page, optionally scoped to a repository.
func (db *DB) ListDriftAnalyses(ctx context.Context, repository string, offset, limit int) ([]DriftAnalysis, error) {
	if offset < 0 || limit < 1 || limit > 100 {
		return nil, errors.New("drift: invalid page")
	}
	out := []DriftAnalysis{}
	rows, e := db.sql.QueryContext(ctx, `SELECT record_json FROM drift_analyses WHERE (?='' OR repository=?) ORDER BY id LIMIT ? OFFSET ?`, repository, repository, limit, offset)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var a DriftAnalysis
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &a); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpdateDriftAnalysis changes host-owned progress while preserving immutable scope and counters.
func (db *DB) UpdateDriftAnalysis(ctx context.Context, a DriftAnalysis, expectedGeneration, expectedVersion int64) (DriftAnalysis, error) {
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		old, e := readDrift(ctx, tx, a.ID)
		if e != nil {
			return e
		}
		if old.Generation != expectedGeneration || old.Version != expectedVersion || old.Execution != "running" {
			return ErrDriftConflict
		}
		old.Pinned = chooseDriftPin(old.Pinned, a.Pinned)
		if len(a.Pinned) > 0 && string(old.Pinned) != string(a.Pinned) {
			return ErrDriftConflict
		}
		if a.Execution != "" && a.Execution != "running" && a.Execution != "incomplete" && a.Execution != "failed" {
			return errors.New("drift: accepted completion requires finalization")
		}
		if a.Disposition != "" && a.Disposition != "not_evaluated" && a.Disposition != "insufficient_evidence" {
			return errors.New("drift: finding disposition requires finalization")
		}
		old.Coverage = a.Coverage
		old.Standards = a.Standards
		old.Provenance = a.Provenance
		old.Reason = a.Reason
		if a.Execution != "" {
			old.Execution = a.Execution
		}
		if a.Disposition != "" {
			old.Disposition = a.Disposition
		}
		old.Version++
		old.UpdatedAt = nowUTC()
		a = old
		return writeDrift(ctx, tx, old)
	})
	return a, err
}
func chooseDriftPin(old, next json.RawMessage) json.RawMessage {
	if len(old) > 0 {
		return old
	}
	return next
}
func checkDriftWriter(a DriftAnalysis, generation int64) error {
	if a.Generation != generation || a.Execution != "running" {
		return ErrDriftConflict
	}
	return nil
}

// AppendDriftEvidence retains one sanitized excerpt under a live writer generation.
func (db *DB) AppendDriftEvidence(ctx context.Context, id string, generation int64, e DriftEvidence) error {
	if e.ID == "" || len(e.Text) > 256*1024 || e.ExcerptExpired || e.ExcerptBytes != 0 {
		return errors.New("drift: invalid evidence")
	}
	sum := sha256.Sum256([]byte(e.Text))
	digest := hex.EncodeToString(sum[:])
	if e.Digest != "" && e.Digest != digest {
		return errors.New("drift: evidence digest mismatch")
	}
	e.Digest = digest
	e.AnalysisID = id
	return db.standardsWrite(ctx, func(tx *sql.Tx) error {
		a, err := readDrift(ctx, tx, id)
		if err != nil {
			return err
		}
		if err = checkDriftWriter(a, generation); err != nil {
			return err
		}
		if a.Limits.ItemBytes > 0 && len(e.Text) > a.Limits.ItemBytes {
			return ErrDriftBudget
		}
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO drift_evidence(analysis_id,id,evidence_json) VALUES(?,?,?)`, id, e.ID, string(raw))
		return err
	})
}

// GetDriftEvidence reads analysis-scoped evidence, including explicit expiry metadata.
func (db *DB) GetDriftEvidence(ctx context.Context, id, evidenceID string) (DriftEvidence, error) {
	var e DriftEvidence
	var raw string
	err := db.sql.QueryRowContext(ctx, `SELECT evidence_json FROM drift_evidence WHERE analysis_id=? AND id=?`, id, evidenceID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	err = json.Unmarshal([]byte(raw), &e)
	return e, err
}

// ExpireDriftEvidence removes terminal analysis excerpts while retaining their
// source identity, sanitized digest and original byte length for provenance.
// It introduces no age policy and never changes an accepted report or receipt.
func (db *DB) ExpireDriftEvidence(ctx context.Context, id string) (int, error) {
	expired := 0
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		a, err := readDrift(ctx, tx, id)
		if err != nil {
			return err
		}
		if a.Execution == "running" {
			return ErrDriftConflict
		}
		rows, err := tx.QueryContext(ctx, `SELECT evidence_json FROM drift_evidence WHERE analysis_id=? ORDER BY id`, id)
		if err != nil {
			return err
		}
		var items []DriftEvidence
		for rows.Next() {
			var raw string
			var item DriftEvidence
			if err := rows.Scan(&raw); err != nil {
				return errors.Join(err, rows.Close())
			}
			if err := json.Unmarshal([]byte(raw), &item); err != nil {
				return errors.Join(err, rows.Close())
			}
			if !item.ExcerptExpired {
				items = append(items, item)
			}
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		for _, item := range items {
			item.ExcerptBytes = len(item.Text)
			item.Text = ""
			item.ExcerptExpired = true
			if item.Omission != "" {
				item.Omission += "; "
			}
			item.Omission += "retained excerpt expired"
			raw, err := json.Marshal(item)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE drift_evidence SET evidence_json=? WHERE analysis_id=? AND id=?`, string(raw), id, item.ID); err != nil {
				return err
			}
			expired++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return expired, nil
}

// ListDriftEvidence returns a bounded ID-ordered page of retained evidence records.
func (db *DB) ListDriftEvidence(ctx context.Context, id string, offset, limit int) ([]DriftEvidence, error) {
	if offset < 0 || limit < 1 || limit > 201 {
		return nil, errors.New("drift: invalid page")
	}
	out := []DriftEvidence{}
	rows, err := db.sql.QueryContext(ctx, `SELECT evidence_json FROM drift_evidence WHERE analysis_id=? ORDER BY id LIMIT ? OFFSET ?`, id, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var e DriftEvidence
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ChargeDriftBudget reserves encoded delivery before any response is offered. Uncertain deliveries remain charged.
func (db *DB) ChargeDriftBudget(ctx context.Context, id string, generation int64, delta DriftUsage, offers []DriftOffer, final bool) (DriftAnalysis, error) {
	var out DriftAnalysis
	if delta.Paths < 0 || delta.Comments < 0 || delta.Requests < 0 || delta.SourceBytes < 0 || delta.Calls < 0 || delta.OutboundBytes < 0 || delta.Invocations < 0 {
		return out, errors.New("drift: negative budget charge")
	}
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		a, e := readDrift(ctx, tx, id)
		if e != nil {
			return e
		}
		if e = checkDriftWriter(a, generation); e != nil {
			return e
		}
		u := a.Usage
		l := a.Limits
		if delta.Paths > l.Paths-u.Paths || delta.Comments > l.Comments-u.Comments || delta.Requests > l.Requests-u.Requests || delta.SourceBytes > l.SourceBytes-u.SourceBytes || delta.Calls > l.Calls-u.Calls || delta.OutboundBytes > l.OutboundBytes-u.OutboundBytes || delta.Invocations > 1-u.Invocations {
			return ErrDriftBudget
		}
		u.Paths += delta.Paths
		u.Comments += delta.Comments
		u.Requests += delta.Requests
		u.SourceBytes += delta.SourceBytes
		u.Calls += delta.Calls
		u.OutboundBytes += delta.OutboundBytes
		u.Invocations += delta.Invocations
		calls := l.Calls
		bytes := l.OutboundBytes
		if !final && (delta.Calls > 0 || delta.OutboundBytes > 0) {
			calls -= 4
			bytes -= 8192
		}
		if u.Paths > l.Paths || u.Comments > l.Comments || u.Requests > l.Requests || u.SourceBytes > l.SourceBytes || u.Calls > calls || u.OutboundBytes > bytes || u.Invocations > 1 {
			return ErrDriftBudget
		}
		for _, o := range offers {
			var raw string
			if e = tx.QueryRowContext(ctx, `SELECT evidence_json FROM drift_evidence WHERE analysis_id=? AND id=?`, id, o.EvidenceID).Scan(&raw); e != nil {
				return e
			}
			var item DriftEvidence
			if e = json.Unmarshal([]byte(raw), &item); e != nil {
				return e
			}
			if item.Digest != o.Digest || o.Start < 0 || o.End < o.Start || o.End > len(item.Text) {
				return errors.New("drift: invalid offered range")
			}
		}
		a.Usage = u
		a.Offers = append(a.Offers, offers...)
		a.Version++
		a.UpdatedAt = nowUTC()
		out = a
		return writeDrift(ctx, tx, a)
	})
	return out, err
}
func readDriftReceipt(ctx context.Context, tx *sql.Tx, id, request string) (DriftReceipt, error) {
	var r DriftReceipt
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT receipt_json FROM drift_receipts WHERE analysis_id=? AND request_id=?`, id, request).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	err = json.Unmarshal([]byte(raw), &r)
	return r, err
}

// GetDriftReceipt recovers an accepted request without requiring live writer authority.
func (db *DB) GetDriftReceipt(ctx context.Context, id, request string) (DriftReceipt, error) {
	var r DriftReceipt
	err := db.withTx(ctx, func(tx *sql.Tx) error { var e error; r, e = readDriftReceipt(ctx, tx, id, request); return e })
	return r, err
}

// FinalizeDriftAnalysis commits prepared validated proposals, exact links and the accepted report together.
// The caller validates candidate semantics against the retained book before this transaction.
func (db *DB) FinalizeDriftAnalysis(ctx context.Context, id string, generation, expectedVersion int64, requestID, digest string, report json.RawMessage, execution, disposition string, candidates []DriftCandidate) (DriftReceipt, error) {
	var out DriftReceipt
	if requestID == "" || digest == "" || !json.Valid(report) || len(report) > 128*1024 || len(candidates) > 10 {
		return out, errors.New("drift: invalid final result")
	}
	var accepted *struct {
		Coverage json.RawMessage `json:"coverage"`
	}
	if err := json.Unmarshal(report, &accepted); err != nil || accepted == nil {
		return out, errors.New("drift: invalid report object")
	}
	if len(accepted.Coverage) > 0 {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(accepted.Coverage, &object); err != nil || object == nil {
			return out, errors.New("drift: invalid report coverage")
		}
	}
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		existing, e := readDriftReceipt(ctx, tx, id, requestID)
		if e == nil {
			if existing.Digest != digest {
				return ErrDriftConflict
			}
			out = existing
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		a, e := readDrift(ctx, tx, id)
		if e != nil {
			return e
		}
		if e = checkDriftWriter(a, generation); e != nil {
			return e
		}
		if a.Version != expectedVersion {
			return ErrDriftConflict
		}
		if len(report) > a.Limits.ResultBytes || len(candidates) > a.Limits.Proposals {
			return ErrDriftBudget
		}
		if execution != "completed" && execution != "incomplete" && execution != "failed" {
			return errors.New("drift: invalid execution state")
		}
		if disposition != "proposals" && disposition != "no_warranted_change" && disposition != "insufficient_evidence" && disposition != "not_evaluated" {
			return errors.New("drift: invalid disposition")
		}
		if (len(candidates) > 0) != (disposition == "proposals") {
			return errors.New("drift: candidate disposition mismatch")
		}
		if disposition == "no_warranted_change" && execution != "completed" {
			return errors.New("drift: no-change conclusion requires completed execution")
		}
		if disposition == "proposals" && execution == "failed" {
			return errors.New("drift: failed execution cannot create proposals")
		}
		links := []DriftProposalLink{}
		keys := map[string]bool{}
		for _, candidate := range candidates {
			p := candidate.Proposal
			if candidate.Key == "" || keys[candidate.Key] || p.ProposalID == "" || p.SchemaVersion != 1 || p.ChangeDigest == "" || p.ResultDigest == "" {
				return errors.New("drift: invalid candidate metadata")
			}
			keys[candidate.Key] = true
			p, e = saveStandardsProposalTx(ctx, tx, p, 0)
			if e != nil {
				return e
			}
			link := DriftProposalLink{CandidateKey: candidate.Key, ProposalID: p.ProposalID, Version: 1, Digest: p.ChangeDigest}
			links = append(links, link)
			if _, e = tx.ExecContext(ctx, `INSERT INTO drift_proposal_links(analysis_id,candidate_key,proposal_id,version,digest) VALUES(?,?,?,?,?)`, id, candidate.Key, p.ProposalID, 1, p.ChangeDigest); e != nil {
				return e
			}
		}
		out = DriftReceipt{AnalysisID: id, RequestID: requestID, Digest: digest, Report: report, ProposalLinks: links, CreatedAt: nowUTC()}
		raw, e := json.Marshal(out)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO drift_receipts(analysis_id,request_id,digest,receipt_json) VALUES(?,?,?,?)`, id, requestID, digest, string(raw)); e != nil {
			return e
		}
		a.Execution = execution
		a.Disposition = disposition
		a.Report = report
		if len(accepted.Coverage) > 0 {
			a.Coverage = accepted.Coverage
		}
		a.ProposalLinks = links
		a.Generation++
		a.Version++
		a.UpdatedAt = nowUTC()
		return writeDrift(ctx, tx, a)
	})
	return out, err
}

// InterruptDriftAnalysis fences an abandoned writer without relaunching or changing its budget.
func (db *DB) InterruptDriftAnalysis(ctx context.Context, id string, generation int64, reason string) (DriftAnalysis, error) {
	var out DriftAnalysis
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		a, e := readDrift(ctx, tx, id)
		if e != nil {
			return e
		}
		if a.Execution != "running" {
			out = a
			return nil
		}
		if a.Generation != generation {
			return ErrDriftConflict
		}
		a.Generation++
		a.Version++
		a.Execution = "incomplete"
		a.Disposition = "not_evaluated"
		a.Reason = reason
		a.UpdatedAt = nowUTC()
		out = a
		return writeDrift(ctx, tx, a)
	})
	return out, err
}

// DriftDigest returns the lowercase SHA-256 digest of the supplied bytes.
func DriftDigest(raw []byte) string { sum := sha256.Sum256(raw); return fmt.Sprintf("%x", sum) }

// ChargeDriftRecovery spends only retained receipt-delivery allowance after an
// accepted result. It cannot offer evidence, launch work or change the result.
func (db *DB) ChargeDriftRecovery(ctx context.Context, id string, delta DriftUsage) (DriftAnalysis, error) {
	var out DriftAnalysis
	if delta.Paths != 0 || delta.Comments != 0 || delta.Requests != 0 || delta.SourceBytes != 0 || delta.Invocations != 0 || delta.Calls < 0 || delta.OutboundBytes < 0 {
		return out, errors.New("drift: invalid recovery charge")
	}
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		a, e := readDrift(ctx, tx, id)
		if e != nil {
			return e
		}
		if a.Execution == "running" || len(a.Report) == 0 {
			return ErrDriftConflict
		}
		if delta.Calls > a.Limits.Calls-a.Usage.Calls || delta.OutboundBytes > a.Limits.OutboundBytes-a.Usage.OutboundBytes {
			return ErrDriftBudget
		}
		a.Usage.Calls += delta.Calls
		a.Usage.OutboundBytes += delta.OutboundBytes
		a.Version++
		a.UpdatedAt = nowUTC()
		out = a
		return writeDrift(ctx, tx, a)
	})
	return out, err
}
