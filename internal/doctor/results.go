package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/resultservice"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// ResultInput is a revision-bound communication contribution, not authority.
type ResultInput struct {
	ExpectedRevision int64     `json:"expected_revision"`
	Diagnosis        Diagnosis `json:"diagnosis"`
}

// ResultDiagnostics reports preflight without recording proof or changing state.
type ResultDiagnostics struct {
	CurrentRevision int64    `json:"current_revision"`
	Valid           bool     `json:"valid"`
	Errors          []string `json:"errors"`
}

// ResultService binds communication preflight/submit/receipt to one doctor attempt.
// It reuses the application result ledger and its immutable replay semantics.
type ResultService struct {
	comments CommentReader
	repairID string
	db       *store.DB
	binding  store.ResultBinding
	evidence store.DoctorEvidence
	redactor *redact.Redactor
}

// BindResults installs only doctor result authority; no workflow runner is used.
func BindResults(ctx context.Context, db *store.DB, id string, e store.DoctorEvidence, r *redact.Redactor) (*ResultService, error) {
	if db == nil || r == nil {
		return nil, errors.New("doctor result dependencies missing")
	}
	b, err := db.BindDoctorResultAttempt(ctx, id)
	if err != nil {
		return nil, err
	}
	if b.Task != e.TaskRef {
		return nil, errors.New("doctor result evidence identity mismatch")
	}
	if e.Live {
		// Live manifests carry only the branch record; resolve the other
		// proposal targets from the issue's current store rows, keeping the
		// manifest's own gaps.
		ref, err := naming.ParseRef(e.TaskRef)
		if err != nil {
			return nil, err
		}
		snapshot, err := db.ReadDoctorEvidence(ctx, ref)
		if err != nil {
			return nil, err
		}
		e.Records = append(append([]store.DoctorEvidenceRecord(nil), e.Records...), snapshot.Records...)
	}
	e.Records = append([]store.DoctorEvidenceRecord(nil), e.Records...)
	e.Gaps = append([]string(nil), e.Gaps...)
	return &ResultService{db: db, binding: b, evidence: e, redactor: r}, nil
}

func (s *ResultService) prepare(ctx context.Context, data []byte) (ResultInput, []byte, error) {
	var input ResultInput
	if len(data) > 65536 {
		return input, nil, errors.New("doctor result input exceeds 65536 bytes")
	}
	if err := submission.DecodeJSON(data, &input); err != nil {
		return input, nil, fmt.Errorf("decode doctor result: %w", err)
	}
	if input.ExpectedRevision < 0 {
		return input, nil, errors.New("doctor result revision must be nonnegative")
	}
	if err := ValidateDiagnosis(input.Diagnosis, s.evidence); err != nil {
		return input, nil, err
	}
	if s.repairID != "" {
		summary := input.Diagnosis.Repair
		if summary == nil || summary.OperationID != s.repairID || len(input.Diagnosis.Proposal) != 0 || len(summary.Observations) > 30 || len(summary.Unresolved) > 30 {
			return input, nil, errors.New("doctor repair result binding mismatch")
		}
		steps, e := s.db.DoctorRepairSteps(ctx, s.repairID)
		if e != nil {
			return input, nil, e
		}
		if len(steps) != len(summary.CompletedSteps) {
			return input, nil, errors.New("doctor repair step references incomplete")
		}
		for i, step := range steps {
			if summary.CompletedSteps[i] != step.Number || step.State != "verified" {
				return input, nil, errors.New("doctor result cannot claim unverified repair steps")
			}
		}
		for i := range summary.Observations {
			if e := s.clean(ctx, &summary.Observations[i]); e != nil {
				return input, nil, e
			}
		}
		for i := range summary.Unresolved {
			if e := s.clean(ctx, &summary.Unresolved[i]); e != nil {
				return input, nil, e
			}
		}
	} else if input.Diagnosis.Repair != nil {
		return input, nil, errors.New("repair result has no bound repair authority")
	}
	// Redact each authored value before encoding, preserving valid JSON and exact
	// factory-owned handles. Applying a truncating redactor to JSON can corrupt it.
	for i := range input.Diagnosis.Findings {
		f := &input.Diagnosis.Findings[i]
		if s.evidence.Live {
			for j := range f.Evidence {
				if err := s.clean(ctx, &f.Evidence[j]); err != nil {
					return input, nil, err
				}
			}
		}
		for _, p := range []*string{&f.Explanation, &f.Uncertainty} {
			if err := s.clean(ctx, p); err != nil {
				return input, nil, err
			}
		}
	}
	for i := range input.Diagnosis.Questions {
		if err := s.clean(ctx, &input.Diagnosis.Questions[i]); err != nil {
			return input, nil, err
		}
	}
	for i := range input.Diagnosis.CoverageGaps {
		if err := s.clean(ctx, &input.Diagnosis.CoverageGaps[i]); err != nil {
			return input, nil, err
		}
	}
	for i := range input.Diagnosis.Proposal {
		p := &input.Diagnosis.Proposal[i]
		if p.Action != nil {
			if err := s.clean(ctx, &p.Action.After); err != nil {
				return input, nil, err
			}
		}
		for _, v := range []*string{&p.Precondition, &p.Change, &p.Reason, &p.Reversibility, &p.Rollback} {
			if err := s.clean(ctx, v); err != nil {
				return input, nil, err
			}
		}
	}
	snapshot, err := json.Marshal(input.Diagnosis)
	return input, snapshot, err
}
func (s *ResultService) clean(ctx context.Context, value *string) error {
	body, err := s.redactor.Apply(ctx, *value, 32768)
	if err != nil {
		return err
	}
	if body.Truncated() {
		return errors.New("doctor result redaction exceeded budget")
	}
	*value = body.String()
	return nil
}

// Preflight checks active authority and semantics without persisting a request.
func (s *ResultService) Preflight(ctx context.Context, data []byte) (ResultDiagnostics, error) {
	if err := s.db.CheckResultWriter(ctx, s.binding); err != nil {
		return ResultDiagnostics{}, err
	}
	input, _, err := s.prepare(ctx, data)
	if err != nil {
		// Name the failed check: a generic message left the agent guessing
		// shapes until it gave up. Messages come from our own validators.
		return ResultDiagnostics{Errors: []string{"Invalid diagnosis: " + err.Error()}}, nil //nolint:nilerr // Preflight reports invalid input as diagnostics, not a transport failure.
	}
	revision, err := s.db.GetResultRevision(ctx, s.binding, 0)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return ResultDiagnostics{}, err
	}
	current := revision.Revision
	if input.ExpectedRevision != current {
		return ResultDiagnostics{CurrentRevision: current, Errors: []string{"Stale expected_revision; use the current revision with a new request"}}, nil
	}
	return ResultDiagnostics{Valid: true, CurrentRevision: current, Errors: []string{}}, nil
}

// Submit stores only redacted input and diagnoses in the shared result ledger.
// Correlation hashes apply to the redacted canonical input; callers retry the
// same request ID and input, then recover its receipt before any new submission.
func (s *ResultService) Submit(ctx context.Context, requestID string, data []byte) (store.ResultReceipt, error) {
	input, snapshot, err := s.prepare(ctx, data)
	if err != nil {
		return store.ResultReceipt{}, err
	}
	digest := sha256.Sum256(data)
	payload, err := json.Marshal(struct {
		Input          ResultInput `json:"input"`
		OriginalDigest string      `json:"original_digest"`
	}{input, hex.EncodeToString(digest[:])})
	if err != nil {
		return store.ResultReceipt{}, err
	}

	receipt, _, err := resultservice.Submit(ctx, s.db, s.binding, requestID, payload, func(context.Context) (*store.ResultRecord, error) {
		return &store.ResultRecord{Diagnostics: []byte(`{"valid":true,"errors":[]}`), Snapshot: snapshot, Retired: []byte(`[]`), ExpectedRevision: input.ExpectedRevision, Accepted: true}, nil
	})
	if err != nil {
		return store.ResultReceipt{}, err
	}
	return *receipt, nil

}

// Receipt recovers the exact accepted input after reply loss, including after stop.
func (s *ResultService) Receipt(ctx context.Context, requestID string) (store.ResultReceipt, error) {
	return s.db.GetResultReceipt(ctx, s.binding, 0, requestID)
}

// Adopt checks a durable receipt and derives proposal bytes from its immutable
// result, never from terminal prose or a mutable current model response.
func (s *ResultService) Adopt(ctx context.Context, requestID string) error {
	return s.adopt(ctx, requestID, false)
}

func (s *ResultService) adopt(ctx context.Context, requestID string, recovered bool) error {
	receipt, err := s.Receipt(ctx, requestID)
	if err != nil {
		return err
	}
	if receipt.InputState != "accepted" || receipt.Attempt != s.binding.Attempt {
		return store.ErrResultAuthority
	}
	revision, err := s.db.GetResultRevision(ctx, s.binding, receipt.AppliedRevision)
	if err != nil {
		return err
	}
	d, err := DecodeDiagnosis(revision.Snapshot)
	if err != nil {
		return err
	}
	if err := validateRetainedDiagnosis(d, s.evidence); err != nil {
		return err
	}
	var proposal []byte
	if len(d.Proposal) > 0 {
		proposal, err = json.Marshal(d.Proposal)
		if err != nil {
			return err
		}
	}
	if recovered {
		return s.db.AdoptRecoveredDoctorResult(ctx, s.binding, requestID, proposal)
	}
	return s.db.AdoptDoctorResult(ctx, s.binding, requestID, proposal)
}
