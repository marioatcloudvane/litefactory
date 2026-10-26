package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"strings"

	"github.com/cloudvane/litefactory/internal/resultservice"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

var errReviewRedaction = errors.New("doctor result cannot be safely redacted")

// RejectedDiagnosisReview is inert review data, never a diagnosis or proposal.
type RejectedDiagnosisReview struct {
	Source           string          `json:"source"`
	OriginalDigest   string          `json:"original_digest"`
	CollectorGaps    []string        `json:"collector_gaps"`
	VerifiedEvidence []string        `json:"verified_evidence"`
	Diagnostics      json.RawMessage `json:"diagnostics,omitempty"`
	RequestID        string          `json:"request_id,omitempty"`
}

func (s *ResultService) reject(ctx context.Context, requestID string, data []byte, cause error) (store.ResultReceipt, error) {
	if len(data) > 65536 || errors.Is(cause, errReviewRedaction) {
		return store.ResultReceipt{}, cause
	}
	source, err := s.redactor.ApplyReviewSource(ctx, string(data), 131072)
	if err != nil {
		return store.ResultReceipt{}, err
	}
	digest := sha256.Sum256(data)
	review := RejectedDiagnosisReview{Source: source.String(), OriginalDigest: hex.EncodeToString(digest[:]), CollectorGaps: append([]string(nil), s.evidence.Gaps...)}
	for _, record := range s.evidence.Records {
		review.VerifiedEvidence = append(review.VerifiedEvidence, EvidenceHandle(record))
	}
	// Collector context and validator errors can contain authored values too.
	for i := range review.CollectorGaps {
		if err = s.clean(ctx, &review.CollectorGaps[i]); err != nil {
			return store.ResultReceipt{}, err
		}
	}
	for i := range review.VerifiedEvidence {
		if err = s.clean(ctx, &review.VerifiedEvidence[i]); err != nil {
			return store.ResultReceipt{}, err
		}
	}
	message := cause.Error()
	if err = s.clean(ctx, &message); err != nil {
		return store.ResultReceipt{}, err
	}
	diagnostics, err := json.Marshal(ResultDiagnostics{Errors: []string{"Invalid diagnosis: " + message}})
	if err != nil {
		return store.ResultReceipt{}, err
	}
	payload, err := json.Marshal(review)
	if err != nil {
		return store.ResultReceipt{}, err
	}
	receipt, _, err := resultservice.Submit(ctx, s.db, s.binding, requestID, payload, func(ctx context.Context) (*store.ResultRecord, error) {
		revision, e := s.db.GetResultRevision(ctx, s.binding, 0)
		if e != nil && !errors.Is(e, store.ErrNotFound) {
			return nil, e
		}
		return &store.ResultRecord{Diagnostics: diagnostics, ExpectedRevision: revision.Revision}, nil
	})
	if err != nil {
		return store.ResultReceipt{}, err
	}
	return *receipt, nil
}

// RenderRejectedDiagnosisReview preserves the entire submitted source as escaped
// text. Parsed detail is supplementary and cannot authorize any action.
func RenderRejectedDiagnosisReview(r RejectedDiagnosisReview) string {
	var body strings.Builder
	body.WriteString("\nUnaccepted Doctor diagnosis retained for review only. This content did not change the accepted diagnosis or repair proposal and does not enable Apply.\n\n")
	body.WriteString("Evidence references in submitted content are unverified unless explicitly labelled below as collector verified references.\n\nValidation limitations:\n<pre>" + html.EscapeString(string(r.Diagnostics)) + "</pre>\n\n")
	var input ResultInput
	if submission.DecodeJSON([]byte(r.Source), &input) == nil {
		known := map[string]bool{}
		for _, handle := range r.VerifiedEvidence {
			known[handle] = true
		}
		for _, finding := range input.Diagnosis.Findings {
			for _, handle := range finding.Evidence {
				label := "unverified"
				if known[handle] {
					label = "collector verified reference"
				}
				body.WriteString("Evidence " + label + ": <code>" + html.EscapeString(handle) + "</code>\n\n")
			}
		}
	}
	body.WriteString("Complete submitted content (redacted, escaped; includes findings, uncertainty, questions and submitted coverage gaps):\n<pre>" + html.EscapeString(r.Source) + "</pre>\n\n")
	if len(r.CollectorGaps) > 0 {
		body.WriteString("Factory collector context (separate from the submitted diagnosis; these gaps may have been omitted):\n<pre>" + html.EscapeString(strings.Join(r.CollectorGaps, "\n")) + "</pre>\n")
	}
	return body.String()
}

func (s *Service) currentReview(ctx context.Context, cid int64) (*RejectedDiagnosisReview, error) {
	receipt, err := s.DB.DoctorRejectedReview(ctx, cid)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.reviewFromReceipt(ctx, receipt)
}

func (s *Service) reviewFromReceipt(ctx context.Context, receipt store.ResultReceipt) (*RejectedDiagnosisReview, error) {
	var err error
	var review RejectedDiagnosisReview
	if err = json.Unmarshal(receipt.Payload, &review); err != nil {
		return nil, err
	}
	review.Diagnostics = receipt.Diagnostics
	review.RequestID = receipt.RequestID
	clean := func(value *string) error {
		body, e := s.Redactor.Apply(ctx, *value, 131072)
		if e != nil {
			return e
		}
		if body.Truncated() {
			return errors.New("doctor review exceeds display redaction budget")
		}
		*value = body.String()
		return nil
	}
	source, err := s.Redactor.ApplyReviewSource(ctx, review.Source, 131072)
	if err != nil {
		return nil, err
	}
	review.Source = source.String()
	for i := range review.CollectorGaps {
		if err = clean(&review.CollectorGaps[i]); err != nil {
			return nil, err
		}
	}
	for i := range review.VerifiedEvidence {
		if err = clean(&review.VerifiedEvidence[i]); err != nil {
			return nil, err
		}
	}
	var diagnostics ResultDiagnostics
	if err = json.Unmarshal(review.Diagnostics, &diagnostics); err != nil {
		return nil, err
	}
	for i := range diagnostics.Errors {
		if err = clean(&diagnostics.Errors[i]); err != nil {
			return nil, err
		}
	}
	review.Diagnostics, err = json.Marshal(diagnostics)
	if err != nil {
		return nil, err
	}
	return &review, nil
}
