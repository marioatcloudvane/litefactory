package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// FamilyScope is immutable factory attribution, never a caller-selected grant.
type FamilyScope struct {
	Repository string `json:"repository"`
	Task       string `json:"task"`
	RunID      int64  `json:"run_id"`
	Attempt    string `json:"attempt"`
}

// FamilyReference points to authoritative state rather than copying decisions.
type FamilyReference struct {
	Kind      string `json:"kind,omitempty"`
	ID        int64  `json:"id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// FamilyRequest retains bounded metadata only, never authored payloads or secrets.
type FamilyRequest struct {
	Scope                                     FamilyScope
	RequestID, Tool, PayloadHash, Disposition string
	Reference                                 FamilyReference
}

func validFamilyScope(s FamilyScope) bool {
	return s.RunID > 0 && s.Attempt != "" && len(s.Attempt) <= 256 && s.Repository != "" && len(s.Repository) <= 512 && len(s.Task) <= 1024 && strings.HasPrefix(s.Task, "github:"+s.Repository+"#")
}

func familyScopeMatches(s FamilyScope, b ResultBinding) bool {
	return validFamilyScope(s) && s.Task == b.Task && s.RunID == b.RunID && s.Attempt == b.Attempt
}

const familyColumns = `run_id,attempt,request_id,repository,task,tool,payload_hash,disposition,reference_kind,reference_id,reference_request`

func scanFamilyRequest(row interface{ Scan(...interface{}) error }) (FamilyRequest, error) {
	var r FamilyRequest
	err := row.Scan(&r.Scope.RunID, &r.Scope.Attempt, &r.RequestID, &r.Scope.Repository, &r.Scope.Task, &r.Tool, &r.PayloadHash, &r.Disposition, &r.Reference.Kind, &r.Reference.ID, &r.Reference.RequestID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return r, err
}

// BeginFamilyRequest admits bounded bookkeeping under live authority. The bool
// elects the first handler invocation; a retry cannot reapply advisory effects.
func (db *DB) BeginFamilyRequest(ctx context.Context, b ResultBinding, q FamilyRequest) (FamilyRequest, bool, error) {
	var r FamilyRequest
	var created bool
	if !familyScopeMatches(q.Scope, b) || q.RequestID == "" || len(q.RequestID) > 128 || q.Tool == "" || len(q.Tool) > 128 || q.Tool == "receipts_get" || !validSubmissionDigest(q.PayloadHash) {
		return r, false, fmt.Errorf("store: invalid family correlation")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, true); err != nil {
			return err
		}
		old, err := scanFamilyRequest(tx.QueryRowContext(ctx, `SELECT `+familyColumns+` FROM family_requests WHERE run_id=? AND attempt=? AND request_id=?`, b.RunID, b.Attempt, q.RequestID))
		if err == nil {
			if old.Scope != q.Scope || old.Tool != q.Tool || old.PayloadHash != q.PayloadHash {
				return ErrResultCorrelation
			}
			r = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM family_requests WHERE run_id=?`, b.RunID).Scan(&count); err != nil {
			return err
		}
		if count >= 4096 {
			return fmt.Errorf("store: family request budget exhausted")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO family_requests(run_id,attempt,request_id,repository,task,tool,payload_hash,disposition) VALUES(?,?,?,?,?,?,?,'pending')`, b.RunID, b.Attempt, q.RequestID, q.Scope.Repository, q.Scope.Task, q.Tool, q.PayloadHash)
		if err != nil {
			return err
		}
		q.Disposition = "pending"
		q.Reference = FamilyReference{}
		r = q
		created = true
		return nil
	})
	return r, created, err
}

// FinishFamilyRequest links a handler result once. Later mutable domain outcomes
// are projected from the referenced owner rather than overwriting this ledger.
func (db *DB) FinishFamilyRequest(ctx context.Context, b ResultBinding, requestID, disposition string, ref FamilyReference) error {
	if !submissionState(disposition, "rejected", "uncertain", "confirmed") || len(ref.Kind) > 64 || ref.ID < 0 || len(ref.RequestID) > 128 || (ref.Kind == "" && (ref.ID != 0 || ref.RequestID != "")) {
		return fmt.Errorf("store: invalid family outcome")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, true); err != nil {
			return err
		}
		r, err := scanFamilyRequest(tx.QueryRowContext(ctx, `SELECT `+familyColumns+` FROM family_requests WHERE run_id=? AND attempt=? AND request_id=?`, b.RunID, b.Attempt, requestID))
		if err != nil {
			return err
		}
		if r.Disposition != "pending" {
			if r.Disposition == disposition && r.Reference == ref {
				return nil
			}
			return ErrResultCorrelation
		}
		_, err = tx.ExecContext(ctx, `UPDATE family_requests SET disposition=?,reference_kind=?,reference_id=?,reference_request=? WHERE run_id=? AND attempt=? AND request_id=?`, disposition, ref.Kind, ref.ID, ref.RequestID, b.RunID, b.Attempt, requestID)
		return err
	})
}

// ReadFamilyRequest checks the current reader while looking up only an explicitly
// permitted historical scope. Old authority is never consulted or restored.
func (db *DB) ReadFamilyRequest(ctx context.Context, b ResultBinding, scopes []FamilyScope, requestID string) (FamilyRequest, error) {
	var found FamilyRequest
	if requestID == "" || len(requestID) > 128 || len(scopes) == 0 || len(scopes) > 64 {
		return found, ErrResultAuthority
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, true); err != nil {
			return err
		}
		matches := 0
		for _, scope := range scopes {
			if !validFamilyScope(scope) {
				return ErrResultAuthority
			}
			r, err := scanFamilyRequest(tx.QueryRowContext(ctx, `SELECT `+familyColumns+` FROM family_requests WHERE run_id=? AND attempt=? AND request_id=? AND repository=? AND task=?`, scope.RunID, scope.Attempt, requestID, scope.Repository, scope.Task))
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			found = r
			matches++
		}
		if matches > 1 {
			return ErrResultCorrelation
		}
		if matches == 0 {
			return ErrNotFound
		}
		return nil
	})
	return found, err
}
