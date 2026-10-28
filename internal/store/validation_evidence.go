package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Bounds apply before structured evidence can be retained.
const (
	ValidationEvidenceVersion       = 1
	MaxValidationEvidenceBytes      = 4 << 20
	MaxValidationEvidenceReferences = 4096
)

// ValidationRequest binds an idempotent source event to one fresh generation.
// Lineage identifies both repository and validation session.
type ValidationRequest struct {
	ID         int64
	Lineage    string
	SourceKey  string
	Policy     string
	Generation int
	Commit     string
	CreatedAt  time.Time
}

// EnsureValidationRequest allocates a generation only for a distinct source.
// Replaying a source with different facts is an error.
func (db *DB) EnsureValidationRequest(ctx context.Context, in ValidationRequest) (ValidationRequest, error) {
	if in.Lineage == "" || len(in.Lineage) > 4096 || in.SourceKey == "" || len(in.SourceKey) > 4096 || !slices.Contains([]string{"auto", "full"}, in.Policy) || !validationCommit(in.Commit) {
		return ValidationRequest{}, errors.New("store: invalid validation request")
	}
	var out ValidationRequest
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = scanValidationRequest(tx.QueryRowContext(ctx, `SELECT id,lineage,source_key,policy,generation,commit_sha,created_at FROM validation_requests WHERE lineage=? AND source_key=?`, in.Lineage, in.SourceKey))
		if err == nil {
			if out.Policy != in.Policy || out.Commit != in.Commit {
				return errors.New("store: validation request replay changed facts")
			}
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		out = in
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(generation),0)+1 FROM validation_requests WHERE lineage=?`, in.Lineage).Scan(&out.Generation); err != nil {
			return err
		}
		out.CreatedAt = nowUTC()
		res, err := tx.ExecContext(ctx, `INSERT INTO validation_requests(lineage,source_key,policy,generation,commit_sha,created_at) VALUES(?,?,?,?,?,?)`, out.Lineage, out.SourceKey, out.Policy, out.Generation, out.Commit, FormatTime(out.CreatedAt))
		if err != nil {
			return err
		}
		out.ID, err = res.LastInsertId()
		return err
	})
	return out, err
}

// GetValidationRequest reads the immutable request used to authorize a generation.
func (db *DB) GetValidationRequest(ctx context.Context, id int64) (ValidationRequest, error) {
	return scanValidationRequest(db.sql.QueryRowContext(ctx, `SELECT id,lineage,source_key,policy,generation,commit_sha,created_at FROM validation_requests WHERE id=?`, id))
}

func scanValidationRequest(row interface{ Scan(...any) error }) (ValidationRequest, error) {
	var out ValidationRequest
	var stamp string
	err := row.Scan(&out.ID, &out.Lineage, &out.SourceKey, &out.Policy, &out.Generation, &out.Commit, &stamp)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out.CreatedAt, err = ParseTime(stamp)
	return out, err
}

func validationCommit(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// ValidationEvidence is an immutable, bounded structured artifact. Inline
// storage prevents path traversal and retains referenced artifacts together.
// Complete describes capture integrity, not whether test execution passed.
type ValidationEvidence struct {
	ID         string
	RequestID  int64
	RunID      int64
	Kind       string
	Version    int
	Complete   bool
	Payload    json.RawMessage
	References []string
	CreatedAt  time.Time
}

func validationEvidenceDigest(in ValidationEvidence) (string, error) {
	data, err := json.Marshal(struct {
		RequestID  int64
		RunID      int64
		Kind       string
		Version    int
		Complete   bool
		Payload    json.RawMessage
		References []string
	}{in.RequestID, in.RunID, in.Kind, in.Version, in.Complete, in.Payload, in.References})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// AppendValidationEvidence saves observations, manifests, origins or comparisons.
// A complete artifact can refer only to complete artifacts in its own lineage.
func (db *DB) AppendValidationEvidence(ctx context.Context, in ValidationEvidence) (ValidationEvidence, error) {
	return db.appendValidationEvidence(ctx, in, false)
}

// FinalizeValidationAssessment validates all durable references and publishes
// the assessment in the same transaction. Incomplete assessments remain readable
// but are never eligible as complete coverage origins.
func (db *DB) FinalizeValidationAssessment(ctx context.Context, in ValidationEvidence) (ValidationEvidence, error) {
	in.Kind = "assessment"
	return db.appendValidationEvidence(ctx, in, true)
}

func (db *DB) appendValidationEvidence(ctx context.Context, in ValidationEvidence, assessment bool) (ValidationEvidence, error) {
	if in.RequestID < 1 || in.RunID < 0 || in.Version != ValidationEvidenceVersion || !slices.Contains([]string{"manifest", "observation", "origin", "comparison", "assessment"}, in.Kind) || len(in.Payload) == 0 || len(in.Payload) > MaxValidationEvidenceBytes || !json.Valid(in.Payload) || len(in.References) > MaxValidationEvidenceReferences {
		return ValidationEvidence{}, errors.New("store: invalid validation evidence")
	}
	if in.Kind == "assessment" && !assessment {
		return ValidationEvidence{}, errors.New("store: assessment requires finalization")
	}
	if in.Kind == "assessment" && in.Complete && len(in.References) == 0 {
		return ValidationEvidence{}, errors.New("store: complete assessment requires evidence")
	}
	for _, id := range in.References {
		if len(id) != 64 {
			return ValidationEvidence{}, errors.New("store: invalid validation evidence reference")
		}
		if _, err := hex.DecodeString(id); err != nil {
			return ValidationEvidence{}, errors.New("store: invalid validation evidence reference")
		}
	}
	in.References = slices.Clone(in.References)
	slices.Sort(in.References)
	in.References = slices.Compact(in.References)
	digest, err := validationEvidenceDigest(in)
	if err != nil {
		return ValidationEvidence{}, err
	}
	if in.ID != "" && in.ID != digest {
		return ValidationEvidence{}, errors.New("store: evidence digest mismatch")
	}
	in.ID = digest
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var lineage, requestCommit string
		if err := tx.QueryRowContext(ctx, `SELECT lineage,commit_sha FROM validation_requests WHERE id=?`, in.RequestID).Scan(&lineage, &requestCommit); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if in.RunID > 0 {
			var state, runCommit string
			if err := tx.QueryRowContext(ctx, `SELECT state,commit_sha FROM suite_runs WHERE id=?`, in.RunID).Scan(&state, &runCommit); err != nil {
				return err
			}
			if in.Complete && runCommit != requestCommit {
				return errors.New("store: complete validation evidence is bound to another tested commit")
			}
			if in.Kind == "assessment" && in.Complete && state == SuiteRunning {
				return errors.New("store: running suite cannot establish complete evidence")
			}
		}
		for _, id := range in.References {
			var referenceLineage string
			var complete bool
			if err := tx.QueryRowContext(ctx, `SELECT r.lineage,e.complete FROM validation_evidence e JOIN validation_requests r ON r.id=e.request_id WHERE e.id=?`, id).Scan(&referenceLineage, &complete); err != nil {
				return fmt.Errorf("store: unavailable evidence reference: %w", err)
			}
			if referenceLineage != lineage || in.Complete && !complete {
				return errors.New("store: incompatible or incomplete evidence reference")
			}
		}
		refs, err := json.Marshal(in.References)
		if err != nil {
			return err
		}
		in.CreatedAt = nowUTC()
		_, err = tx.ExecContext(ctx, `INSERT INTO validation_evidence(id,request_id,run_id,kind,version,complete,payload,reference_ids,created_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, in.ID, in.RequestID, nullID(in.RunID), in.Kind, in.Version, in.Complete, string(in.Payload), string(refs), FormatTime(in.CreatedAt))
		if err != nil {
			return err
		}
		in, err = scanValidationEvidence(tx.QueryRowContext(ctx, `SELECT `+validationEvidenceColumns+` FROM validation_evidence WHERE id=?`, in.ID))
		return err
	})
	return in, err
}

const validationEvidenceColumns = `id,request_id,COALESCE(run_id,0),kind,version,complete,payload,reference_ids,created_at`

func scanValidationEvidence(row interface{ Scan(...any) error }) (ValidationEvidence, error) {
	var out ValidationEvidence
	var payload, refs, stamp string
	err := row.Scan(&out.ID, &out.RequestID, &out.RunID, &out.Kind, &out.Version, &out.Complete, &payload, &refs, &stamp)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out.Payload = json.RawMessage(payload)
	if err = json.Unmarshal([]byte(refs), &out.References); err != nil {
		return out, err
	}
	out.CreatedAt, err = ParseTime(stamp)
	if err != nil {
		return out, err
	}
	digest, err := validationEvidenceDigest(out)
	if err != nil || digest != out.ID {
		return ValidationEvidence{}, errors.New("store: corrupt validation evidence")
	}
	return out, nil
}

// ValidationEvidence reads one artifact and verifies its content digest.
func (db *DB) ValidationEvidence(ctx context.Context, id string) (ValidationEvidence, error) {
	return scanValidationEvidence(db.sql.QueryRowContext(ctx, `SELECT `+validationEvidenceColumns+` FROM validation_evidence WHERE id=?`, id))
}

// LatestValidationEvidence returns the latest artifact of a kind in a lineage.
// Callers must check Complete and version before considering inheritance.
func (db *DB) LatestValidationEvidence(ctx context.Context, lineage, kind string) (ValidationEvidence, error) {
	const columns = `e.id,e.request_id,COALESCE(e.run_id,0),e.kind,e.version,e.complete,e.payload,e.reference_ids,e.created_at`
	return scanValidationEvidence(db.sql.QueryRowContext(ctx, `SELECT `+columns+` FROM validation_evidence e JOIN validation_requests r ON r.id=e.request_id WHERE r.lineage=? AND e.kind=? ORDER BY e.rowid DESC LIMIT 1`, lineage, kind))
}

// ValidationEvidenceForRequest reads retained artifacts in append order.
func (db *DB) ValidationEvidenceForRequest(ctx context.Context, requestID int64) ([]ValidationEvidence, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+validationEvidenceColumns+` FROM validation_evidence WHERE request_id=? ORDER BY rowid`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ValidationEvidence
	for rows.Next() {
		v, err := scanValidationEvidence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
