package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrPreventionConflict signals reused scope keys, stale writers or changed final results.
var ErrPreventionConflict = errors.New("prevention analysis: identity or writer conflict")

// PreventionAnalysis retains bounded advisory work independently of workflow authority.
type PreventionAnalysis struct {
	OutputBytes       int                        `json:"output_bytes"`
	OutputLimited     bool                       `json:"output_limited"`
	Provenance        json.RawMessage            `json:"provenance,omitempty"`
	DeadlineAt        time.Time                  `json:"deadline_at"`
	DeliveredEvidence []string                   `json:"delivered_evidence,omitempty"`
	Draft             json.RawMessage            `json:"draft,omitempty"`
	Drafts            []PreventionDraft          `json:"drafts,omitempty"`
	ID                string                     `json:"id"`
	RequestKey        string                     `json:"request_key"`
	ScopeDigest       string                     `json:"scope_digest"`
	Repository        string                     `json:"repository"`
	Scope             json.RawMessage            `json:"scope"`
	Limits            json.RawMessage            `json:"limits"`
	Status            string                     `json:"status"`
	AttemptID         string                     `json:"attempt_id"`
	Manifest          json.RawMessage            `json:"manifest,omitempty"`
	Result            json.RawMessage            `json:"result,omitempty"`
	ResultDigest      string                     `json:"result_digest,omitempty"`
	Diagnostics       json.RawMessage            `json:"diagnostics,omitempty"`
	CreatedAt         time.Time                  `json:"created_at"`
	UpdatedAt         time.Time                  `json:"updated_at"`
	Proposals         []StandardsProposalVersion `json:"proposals,omitempty"`
}

// PreventionDraft preserves one invalid submission and its validation diagnostics.
type PreventionDraft struct {
	Draft       json.RawMessage `json:"draft"`
	Diagnostics json.RawMessage `json:"diagnostics"`
	CreatedAt   time.Time       `json:"created_at"`
}

// PreventionProposal is an upstream proposal and its retained causal provenance.
type PreventionProposal struct {
	Proposal   StandardsProposalVersion `json:"proposal"`
	Provenance json.RawMessage          `json:"provenance"`
}

func readPreventionAnalysis(ctx context.Context, tx *sql.Tx, key, value string) (PreventionAnalysis, error) {
	var a PreventionAnalysis
	var raw string
	query := `SELECT record_json FROM prevention_analyses WHERE id=?`
	if key == "request_key" {
		query = `SELECT record_json FROM prevention_analyses WHERE request_key=?`
	}
	err := tx.QueryRowContext(ctx, query, value).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	if err = json.Unmarshal([]byte(raw), &a); err != nil {
		return a, fmt.Errorf("prevention analysis: corrupt record: %w", err)
	}
	return a, nil
}
func writePreventionAnalysis(ctx context.Context, tx *sql.Tx, a PreventionAnalysis) error {
	a.UpdatedAt = nowUTC()
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if len(raw) > 1024*1024 {
		return errors.New("prevention analysis: record exceeds 1 MiB")
	}
	_, err = tx.ExecContext(ctx, `UPDATE prevention_analyses SET status=?,record_json=? WHERE id=?`, a.Status, string(raw), a.ID)
	return err
}

// CreatePreventionAnalysis persists an invocation before external work. Identical scope retries recover it.
func (db *DB) CreatePreventionAnalysis(ctx context.Context, a PreventionAnalysis) (PreventionAnalysis, bool, error) {
	if a.ID == "" || a.RequestKey == "" || a.ScopeDigest == "" || a.Repository == "" || a.AttemptID == "" || !json.Valid(a.Scope) || !json.Valid(a.Limits) {
		return a, false, errors.New("prevention analysis: incomplete invocation")
	}
	if a.DeadlineAt.IsZero() {
		a.DeadlineAt = nowUTC().Add(10 * time.Minute)
	}
	if !a.DeadlineAt.After(nowUTC()) || a.DeadlineAt.After(nowUTC().Add(10*time.Minute)) {
		return a, false, errors.New("prevention analysis: deadline must be within ten minutes")
	}
	a.Status = "collecting"
	a.CreatedAt = nowUTC()
	a.UpdatedAt = a.CreatedAt
	a.Manifest = nil
	a.Result = nil
	a.Proposals = nil
	a.ResultDigest = ""
	a.Draft = nil
	a.Drafts = nil
	a.Diagnostics = nil
	created := false
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		old, err := readPreventionAnalysis(ctx, tx, "request_key", a.RequestKey)
		if err == nil {
			if old.ScopeDigest != a.ScopeDigest || old.Repository != a.Repository {
				return ErrPreventionConflict
			}
			a = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		raw, err := json.Marshal(a)
		if err != nil {
			return err
		}
		if len(raw) > 65536 {
			return errors.New("prevention analysis: invocation exceeds 64 KiB")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO prevention_analyses(id,request_key,scope_digest,repository,attempt_id,status,record_json) VALUES(?,?,?,?,?,?,?)`, a.ID, a.RequestKey, a.ScopeDigest, a.Repository, a.AttemptID, a.Status, string(raw))
		created = err == nil
		return err
	})
	return a, created, err
}

// GetPreventionAnalysis returns durable evidence, result and exact proposal identities without reanalysis.
func (db *DB) GetPreventionAnalysis(ctx context.Context, id string) (PreventionAnalysis, error) {
	var a PreventionAnalysis
	err := db.withTx(ctx, func(tx *sql.Tx) error { var err error; a, err = readPreventionAnalysis(ctx, tx, "id", id); return err })
	return a, err
}

// ListPreventionAnalyses returns at most 100 records, newest first.
func (db *DB) ListPreventionAnalyses(ctx context.Context, limit int) ([]PreventionAnalysis, error) {
	return db.ListPreventionAnalysesForRepository(ctx, "", limit)
}

// ListPreventionAnalysesForRepository filters repository identity before applying the finite limit.
func (db *DB) ListPreventionAnalysesForRepository(ctx context.Context, repository string, limit int) ([]PreventionAnalysis, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("prevention analysis: list limit must be 1..100")
	}
	out := []PreventionAnalysis{}
	rows, err := db.sql.QueryContext(ctx, `SELECT record_json FROM prevention_analyses WHERE (?='' OR repository=?) ORDER BY rowid DESC LIMIT ?`, repository, repository, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var a PreventionAnalysis
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func preventionWriter(a PreventionAnalysis, attempt string) error {
	if !a.DeadlineAt.After(nowUTC()) || a.AttemptID != attempt || a.ResultDigest != "" || a.Status == "failed" || a.Status == "interrupted" || a.Status == "provider_unavailable" {
		return ErrPreventionConflict
	}
	return nil
}

// FreezePreventionAnalysis makes the sanitized manifest immutable before the analyst starts.
func (db *DB) FreezePreventionAnalysis(ctx context.Context, id, attempt string, manifest json.RawMessage) error {
	if !json.Valid(manifest) || len(manifest) > 512*1024 {
		return errors.New("prevention analysis: invalid or oversized manifest")
	}
	return db.standardsWrite(ctx, func(tx *sql.Tx) error {
		a, err := readPreventionAnalysis(ctx, tx, "id", id)
		if err != nil {
			return err
		}
		expiredCollection := a.Status == "collecting" && a.AttemptID == attempt && a.ResultDigest == "" && len(a.Manifest) == 0 && !a.DeadlineAt.After(nowUTC())
		if !expiredCollection {
			if err = preventionWriter(a, attempt); err != nil {
				return err
			}
		}
		if len(a.Manifest) > 0 {
			if string(a.Manifest) == string(manifest) {
				return nil
			}
			return ErrPreventionConflict
		}
		a.Manifest = manifest
		a.Status = "analyzing"
		if expiredCollection {
			a.Status = "interrupted"
			a.Diagnostics = json.RawMessage(`{"reason":"collection deadline reached; completed evidence retained without analyst launch"}`)
		}
		return writePreventionAnalysis(ctx, tx, a)
	})
}

// UpdatePreventionAnalysis preserves failed drafts and diagnostics without authorizing proposals.
func (db *DB) UpdatePreventionAnalysis(ctx context.Context, id, attempt, status string, diagnostics json.RawMessage) error {
	if status != "analyzing" && status != "failed" && status != "interrupted" && status != "provider_unavailable" && status != "incomplete" {
		return errors.New("prevention analysis: invalid status")
	}
	if len(diagnostics) > 256*1024 || (len(diagnostics) > 0 && !json.Valid(diagnostics)) {
		return errors.New("prevention analysis: invalid diagnostics")
	}
	return db.standardsWrite(ctx, func(tx *sql.Tx) error {
		a, err := readPreventionAnalysis(ctx, tx, "id", id)
		if err != nil {
			return err
		}
		if a.AttemptID != attempt || a.ResultDigest != "" || (a.Status == "interrupted" || a.Status == "failed" || a.Status == "provider_unavailable") && a.Status != status {
			return ErrPreventionConflict
		}
		if status == "analyzing" && !a.DeadlineAt.After(nowUTC()) {
			return ErrPreventionConflict
		}
		a.Status = status
		a.Diagnostics = diagnostics
		return writePreventionAnalysis(ctx, tx, a)
	})
}

// FinalizePreventionAnalysis commits a validated result and its exact unapproved proposal batch together.
// The service performs structural and causal-reference validation before calling this method.
func (db *DB) FinalizePreventionAnalysis(ctx context.Context, id, attempt, digest string, result json.RawMessage, proposals []PreventionProposal) ([]StandardsProposalVersion, error) {
	if digest == "" || !json.Valid(result) || len(result) > 256*1024 || len(proposals) > 5 {
		return nil, errors.New("prevention analysis: invalid final result")
	}
	var out []StandardsProposalVersion
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		a, err := readPreventionAnalysis(ctx, tx, "id", id)
		if err != nil {
			return err
		}
		if a.ResultDigest != "" {
			if a.ResultDigest != digest || string(a.Result) != string(result) {
				return ErrPreventionConflict
			}
			out = a.Proposals
			return nil
		}
		if err = preventionWriter(a, attempt); err != nil {
			return err
		}
		if len(a.Manifest) == 0 {
			return errors.New("prevention analysis: manifest not frozen")
		}
		out = []StandardsProposalVersion{}
		for i, pair := range proposals {
			p := pair.Proposal
			if p.ProposalID == "" || p.SchemaVersion != 1 || p.ChangeDigest == "" || p.ResultDigest == "" || p.BaseRevision <= 0 || !json.Valid(pair.Provenance) {
				return errors.New("prevention analysis: invalid proposal metadata")
			}
			var e error
			p, e = saveStandardsProposalTx(ctx, tx, p, 0)
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO prevention_analysis_proposals(analysis_id,ordinal,result_digest,proposal_id,proposal_version,provenance_json) VALUES(?,?,?,?,1,?)`, id, i, digest, p.ProposalID, string(pair.Provenance)); e != nil {
				return e
			}
			out = append(out, p)
		}
		a.Proposals = out
		a.Result = result
		a.ResultDigest = digest
		a.Status = "completed"
		return writePreventionAnalysis(ctx, tx, a)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AppendPreventionDelivery records evidence actually offered by the fenced broker.
func (db *DB) AppendPreventionDelivery(ctx context.Context, id, attempt, evidenceID string) error {
	if evidenceID == "" || len(evidenceID) > 256 {
		return errors.New("prevention analysis: invalid evidence identity")
	}
	return db.standardsWrite(ctx, func(tx *sql.Tx) error {
		a, err := readPreventionAnalysis(ctx, tx, "id", id)
		if err != nil {
			return err
		}
		if err = preventionWriter(a, attempt); err != nil {
			return err
		}
		for _, v := range a.DeliveredEvidence {
			if v == evidenceID {
				return nil
			}
		}
		if len(a.DeliveredEvidence) >= 512 {
			return errors.New("prevention analysis: delivery identity limit reached")
		}
		a.DeliveredEvidence = append(a.DeliveredEvidence, evidenceID)
		return writePreventionAnalysis(ctx, tx, a)
	})
}

// SavePreventionDraft retains invalid bounded typed submissions for inspection.
func (db *DB) SavePreventionDraft(ctx context.Context, id, attempt string, draft, diagnostics json.RawMessage) error {
	if len(draft) > 65536 || !json.Valid(draft) || len(diagnostics) > 65536 || !json.Valid(diagnostics) {
		return errors.New("prevention analysis: invalid draft or diagnostics")
	}
	return db.standardsWrite(ctx, func(tx *sql.Tx) error {
		a, err := readPreventionAnalysis(ctx, tx, "id", id)
		if err != nil {
			return err
		}
		if err = preventionWriter(a, attempt); err != nil {
			return err
		}
		if len(a.Drafts) >= 4 {
			return errors.New("prevention analysis: correction submissions exhausted")
		}
		a.Drafts = append(a.Drafts, PreventionDraft{Draft: draft, Diagnostics: diagnostics, CreatedAt: nowUTC()})
		a.Draft = draft
		a.Diagnostics = diagnostics
		return writePreventionAnalysis(ctx, tx, a)
	})
}

// PreventionDiagnosis keeps the recorded review status separate from causal confidence.
type PreventionDiagnosis struct {
	InstanceID int64                   `json:"instance_id"`
	Diagnosis  GraphDiagnosis          `json:"diagnosis"`
	Approval   *GraphDiagnosisApproval `json:"approval,omitempty"`
}

// ListPreventionDiagnoses reads only selected-issue retained Bug records. Oversized
// records are omitted with an explicit error so callers can record a coverage gap.
func (db *DB) ListPreventionDiagnoses(ctx context.Context, repository string, issue, limit int) ([]PreventionDiagnosis, error) {
	records, err := db.ReadPreventionBugEvidence(ctx, repository, issue, limit, 65536)
	out := []PreventionDiagnosis{}
	for _, record := range records {
		if record.Kind != "diagnosis" {
			continue
		}
		var body struct {
			Diagnosis GraphDiagnosis          `json:"diagnosis"`
			Approval  *GraphDiagnosisApproval `json:"approval"`
		}
		if e := json.Unmarshal(record.Body, &body); e != nil {
			return out, errors.Join(err, e)
		}
		if record.ReviewStatus != "reviewed" {
			body.Approval = nil
		}
		out = append(out, PreventionDiagnosis{InstanceID: record.InstanceID, Diagnosis: body.Diagnosis, Approval: body.Approval})
	}
	return out, err
}

// RecoverPreventionAnalysis recovers a durable receipt first. An unfinished expired
// attempt is fenced and marked interrupted; a live request is never restarted.
func (db *DB) RecoverPreventionAnalysis(ctx context.Context, id string) (PreventionAnalysis, error) {
	var a PreventionAnalysis
	err := db.standardsWrite(ctx, func(tx *sql.Tx) error {
		var err error
		a, err = readPreventionAnalysis(ctx, tx, "id", id)
		if err != nil {
			return err
		}
		if a.ResultDigest != "" {
			return nil
		}
		if (a.Status == "collecting" || a.Status == "analyzing" || a.Status == "incomplete") && !a.DeadlineAt.After(nowUTC()) {
			a.Status = "interrupted"
			a.Diagnostics = json.RawMessage(`{"reason":"unfinished attempt exceeded its retained deadline; use a new request key for explicit new work"}`)
			return writePreventionAnalysis(ctx, tx, a)
		}
		return nil
	})
	return a, err
}

// SetPreventionProvenance retains host provider/instruction observations separately
// from drafts. A terminal observation can be captured by the same attempt only.
func (db *DB) SetPreventionProvenance(ctx context.Context, id, attempt string, raw json.RawMessage) error {
	if !json.Valid(raw) || len(raw) > 65536 {
		return errors.New("prevention analysis: invalid provenance")
	}
	return db.standardsWrite(ctx, func(tx *sql.Tx) error {
		a, err := readPreventionAnalysis(ctx, tx, "id", id)
		if err != nil {
			return err
		}
		if a.AttemptID != attempt {
			return ErrPreventionConflict
		}
		a.Provenance = raw
		return writePreventionAnalysis(ctx, tx, a)
	})
}

type preventionWriterKey struct{}
type preventionWriterLease struct{ id, attempt string }

// WithPreventionWriter attaches trusted standalone writer identity to standards
// calls, which verify it atomically inside their write transaction.
func WithPreventionWriter(ctx context.Context, id, attempt string) context.Context {
	return context.WithValue(ctx, preventionWriterKey{}, preventionWriterLease{id: id, attempt: attempt})
}
func checkPreventionLease(ctx context.Context, tx *sql.Tx) error {
	lease, ok := ctx.Value(preventionWriterKey{}).(preventionWriterLease)
	if !ok {
		return nil
	}
	a, err := readPreventionAnalysis(ctx, tx, "id", lease.id)
	if err != nil {
		return err
	}
	return preventionWriter(a, lease.attempt)
}

// SetPreventionOutput durably reserves cumulative encoded output before delivery.
// Terminal receipts share the same monotonic allowance and attempt identity.
func (db *DB) SetPreventionOutput(ctx context.Context, id, attempt string, used int, limited bool) error {
	if used < 0 || used > 128<<10 {
		return errors.New("prevention analysis: output counter exceeds allowance")
	}
	return db.standardsWrite(ctx, func(tx *sql.Tx) error {
		a, err := readPreventionAnalysis(ctx, tx, "id", id)
		if err != nil {
			return err
		}
		if a.AttemptID != attempt || used < a.OutputBytes {
			return ErrPreventionConflict
		}
		a.OutputBytes = used
		a.OutputLimited = a.OutputLimited || limited
		return writePreventionAnalysis(ctx, tx, a)
	})
}
