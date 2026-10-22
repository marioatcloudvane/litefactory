package prevention

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// Session owns one serialized dispatcher and its cumulative encoded output budget.
// The broker closes and joins all callers before the invocation is settled.
type Session struct {
	mu                sync.Mutex
	service           *Service
	analysis          store.PreventionAnalysis
	scope             Scope
	manifest          Manifest
	runtime           *standards.Runtime
	closed, degraded  bool
	revoke            context.CancelFunc
	used, corrections int
	offsets           map[string]int
	index             json.RawMessage
}

// EvidencePage is a bounded contiguous excerpt from one frozen observation.
type EvidencePage struct {
	Evidence   Evidence `json:"evidence"`
	Start      int      `json:"start"`
	NextOffset int      `json:"next_offset"`
	Complete   bool     `json:"complete"`
}

func newSession(s *Service, a store.PreventionAnalysis, scope Scope, m Manifest, r *standards.Runtime) *Session {
	index := marshal(struct {
		Evidence              []Evidence       `json:"evidence"`
		Coverage              []Coverage       `json:"coverage"`
		Policy                CollectionPolicy `json:"collection_policy"`
		Omitted               int              `json:"coverage_omitted"`
		Requests              int              `json:"requests"`
		SourceBytes           int64            `json:"source_bytes"`
		StartedAt, FinishedAt time.Time
	}{evidenceIndex(m), m.Coverage, m.Policy, m.CoverageOmitted, m.Requests, m.SourceBytes, m.StartedAt, m.FinishedAt})
	return &Session{service: s, analysis: a, scope: scope, manifest: m, runtime: r, offsets: map[string]int{}, index: index}
}
func (s *Session) close() { s.mu.Lock(); defer s.mu.Unlock(); s.closed = true }
func (s *Session) check(ctx context.Context) error {
	if s.closed {
		return errors.New("analysis broker closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a, err := s.service.DB.GetPreventionAnalysis(ctx, s.analysis.ID)
	if err != nil {
		return err
	}
	if len(a.Result) == 0 && !a.DeadlineAt.IsZero() && !time.Now().UTC().Before(a.DeadlineAt) {
		return store.ErrPreventionConflict
	}
	if a.AttemptID != s.analysis.AttemptID || (a.Status != "analyzing" && len(a.Result) == 0) {
		return store.ErrPreventionConflict
	}
	return nil
}
func encodedSize(v any) int {
	b := marshal(v)
	t, _ := json.Marshal(string(b))
	return len(b) + len(t) + 1024
}
func (s *Session) account(ctx context.Context, v any) error {
	n := encodedSize(v)
	if n > 8<<10 || s.used+n > (128<<10)-(2<<10) {
		s.degraded = true
		_ = s.service.DB.SetPreventionOutput(ctx, s.analysis.ID, s.analysis.AttemptID, s.used, true)
		return errors.New("analysis output limit reached; retained coverage and unfinished reads remain inspectable")
	}
	// Redaction is hygiene at every delivery boundary, not a permission boundary.
	if s.service.Redactor == nil {
		s.degraded = true
		return errors.New("analysis output redaction unavailable")
	}
	raw := string(marshal(v))
	safe, err := s.service.Redactor.Apply(ctx, raw, len(raw)*2+4096)
	if err != nil || safe.Truncated() || safe.String() != raw {
		s.degraded = true
		return errors.New("analysis output withheld by redaction policy")
	}
	if err := s.service.DB.SetPreventionOutput(ctx, s.analysis.ID, s.analysis.AttemptID, s.used+n, s.degraded); err != nil {
		return err
	}
	s.used += n
	return nil
}

// sanitizedJSON checks every decoded string so JSON escaping cannot hide a
// registered credential. Changed results stay reviewable drafts, never proposals.
func (s *Session) sanitizedJSON(ctx context.Context, value any, limit int) (json.RawMessage, bool, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > limit {
		return nil, false, errors.New("analysis content exceeds retention bounds")
	}
	if s.service.Redactor == nil {
		return nil, false, errors.New("analysis content redaction unavailable")
	}
	var decoded any
	if err = json.Unmarshal(raw, &decoded); err != nil {
		return nil, false, errors.New("analysis content encoding refused")
	}
	changed := false
	var clean func(any) (any, error)
	clean = func(v any) (any, error) {
		switch text := v.(type) {
		case string:
			body, e := s.service.Redactor.Apply(ctx, text, limit)
			if e != nil || body.Truncated() {
				return nil, errors.New("analysis content redaction unavailable")
			}
			changed = changed || body.String() != text
			return body.String(), nil
		case []any:
			for i, item := range text {
				next, e := clean(item)
				if e != nil {
					return nil, e
				}
				text[i] = next
			}
		case map[string]any:
			for key, item := range text {
				next, e := clean(item)
				if e != nil {
					return nil, e
				}
				text[key] = next
			}
		}
		return v, nil
	}
	cleaned, err := clean(decoded)
	if err != nil {
		return nil, false, err
	}
	out, err := json.Marshal(cleaned)
	if err != nil || len(out) > limit {
		return nil, false, errors.New("analysis content exceeds retention bounds")
	}
	return out, changed, nil
}

// Evidence delivers one contiguous page and records complete observation delivery.
func (s *Session) Evidence(ctx context.Context, id string, offset int) (EvidencePage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out EvidencePage
	if err := s.check(ctx); err != nil {
		return out, err
	}
	records := s.manifest.Evidence
	if id == "manifest" {
		records = []Evidence{{ID: "manifest", Kind: "manifest", Locator: "retained analysis evidence index and coverage", ObservedAt: s.manifest.FinishedAt, Excerpt: string(s.index), Digest: hash(s.index)}}
	}
	for _, e := range records {
		if e.ID == id {
			if offset < 0 || offset > len(e.Excerpt) || offset > s.offsets[id] || !utf8.ValidString(e.Excerpt[:offset]) {
				return out, errors.New("invalid evidence continuation")
			}
			end := min(offset+1536, len(e.Excerpt))
			for end > offset && !utf8.ValidString(e.Excerpt[offset:end]) {
				end--
			}
			part := e
			part.Excerpt = e.Excerpt[offset:end]
			out = EvidencePage{Evidence: part, Start: offset, NextOffset: end, Complete: end == len(e.Excerpt)}
			if err := s.account(ctx, out); err != nil {
				return EvidencePage{}, err
			}
			s.offsets[id] = max(s.offsets[id], end)
			if out.Complete && id != "manifest" {
				if err := s.service.DB.AppendPreventionDelivery(ctx, s.analysis.ID, s.analysis.AttemptID, id); err != nil {
					return EvidencePage{}, err
				}
			}
			return out, nil
		}
	}
	return out, errors.New("evidence is outside frozen analysis manifest")
}

// Categories discovers a page from the pinned book.
func (s *Session) Categories(ctx context.Context, size int, cursor string) (standards.CategoryPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return standards.CategoryPage{}, err
	}
	out, err := s.runtime.Categories(store.WithPreventionWriter(ctx, s.analysis.ID, s.analysis.AttemptID), min(size, 5), cursor)
	if err == nil {
		err = s.account(ctx, out)
	}
	if err != nil {
		return standards.CategoryPage{}, err
	}
	return out, nil
}

// Select records an explicit selection for this analysis consumer.
func (s *Session) Select(ctx context.Context, expected int64, ids []string, reason string) (standards.RuntimeStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return standards.RuntimeStatus{}, err
	}
	if len(ids) > 100 || len(reason) > 2048 {
		return standards.RuntimeStatus{}, errors.New("selection exceeds bounds")
	}
	safe, _, err := s.sanitizedJSON(ctx, reason, 4096)
	if err != nil {
		return standards.RuntimeStatus{}, err
	}
	var retainedReason string
	if err = json.Unmarshal(safe, &retainedReason); err != nil {
		return standards.RuntimeStatus{}, errors.New("selection reason encoding refused")
	}
	if len(retainedReason) > 2048 {
		return standards.RuntimeStatus{}, errors.New("selection reason exceeds bounds after redaction")
	}
	out, err := s.runtime.Select(store.WithPreventionWriter(ctx, s.analysis.ID, s.analysis.AttemptID), expected, ids, retainedReason)
	out = standards.BoundedStatus(out)
	if err == nil {
		err = s.account(ctx, standards.BoundedStatus(out))
	}
	if err != nil {
		return standards.RuntimeStatus{}, err
	}
	return out, nil
}

// Status returns bounded pinned integrity and delivery status.
func (s *Session) Status(ctx context.Context) (standards.RuntimeStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return standards.RuntimeStatus{}, err
	}
	out, err := s.runtime.Status(ctx)
	out = standards.BoundedStatus(out)
	if err == nil {
		err = s.account(ctx, out)
	}
	return out, err
}

// ReadStandards streams and acknowledges the pinned selected guidance.
func (s *Session) ReadStandards(ctx context.Context, version int64, digest, cursor string, ack bool) (standards.ReadPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return standards.ReadPage{}, err
	}
	out, err := s.runtime.Read(store.WithPreventionWriter(ctx, s.analysis.ID, s.analysis.AttemptID), version, digest, cursor, ack)
	if err == nil {
		err = s.account(ctx, out)
	}
	if err != nil {
		return standards.ReadPage{}, err
	}
	return out, nil
}
func (s *Session) validate(ctx context.Context, result Result) ([]string, []store.PreventionProposal, error) {
	diagnostics := []string{}
	add := func(v string) {
		if len(diagnostics) < 20 {
			diagnostics = append(diagnostics, v)
		}
	}
	if result.SchemaVersion != 1 {
		add("schema_version must be 1")
	}
	if len(marshal(result)) > 64<<10 {
		return []string{"result exceeds 64 KiB"}, nil, nil
	}
	a, err := s.service.DB.GetPreventionAnalysis(ctx, s.analysis.ID)
	if err != nil {
		return nil, nil, err
	}
	delivered := map[string]bool{}
	for _, id := range a.DeliveredEvidence {
		delivered[id] = true
	}
	evidence := map[string]Evidence{}
	for _, e := range s.manifest.Evidence {
		evidence[e.ID] = e
	}
	selected := map[int]bool{}
	for _, issue := range s.scope.Issues {
		selected[issue] = true
	}
	seen := map[int]bool{}
	proposalCount := 0
	batch := []store.PreventionProposal{}
	status, err := s.runtime.Status(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, f := range result.Findings {
		if !selected[f.Issue] || seen[f.Issue] {
			add("findings must identify each selected bug once")
		}
		seen[f.Issue] = true
		if strings.TrimSpace(f.Expected) == "" || strings.TrimSpace(f.Actual) == "" || strings.TrimSpace(f.Uncertainty) == "" {
			add("expected, actual and uncertainty are required")
		}
		for _, ids := range [][]string{f.BugEvidence, f.CauseEvidence} {
			for _, id := range ids {
				e, ok := evidence[id]
				if !ok || e.Issue != f.Issue || !delivered[id] {
					add("citations must reference evidence delivered for this bug")
				}
			}
		}
		for _, v := range []string{f.Expected, f.Actual, f.Cause, f.CauseBasis, f.Uncertainty, f.PreventionRationale} {
			if len(v) > 4096 {
				add("finding text exceeds 4096 bytes")
			}
		}
		if len(f.MissingEvidence) > 20 || len(f.BugEvidence) > 100 || len(f.CauseEvidence) > 100 {
			add("finding collections exceed bounds")
		}
		for _, v := range f.MissingEvidence {
			if strings.TrimSpace(v) == "" || len(v) > 4096 {
				add("missing evidence reasons must contain 1..4096 bytes")
			}
		}
		switch f.Outcome {
		case "insufficient_evidence":
			if len(f.MissingEvidence) == 0 {
				add("insufficient_evidence requires missing evidence reasons")
			}
			if len(f.Proposals) > 0 {
				add("insufficient evidence cannot propose a standard")
			}
		case "no_warranted_change":
			if status.Integrity != "verified" || status.Selection == nil || status.SelectionStatus == "corrupt" || status.RetrievalStatus != "retrieved" || s.degraded {
				add("missing standards coverage requires insufficient_evidence rather than complete no-change")
			}
			if len(f.Proposals) > 0 {
				add("no_warranted_change cannot propose standards")
			}
			for _, c := range s.manifest.Coverage {
				if (c.Issue == f.Issue || c.Issue == 0) && c.Status != "read" {
					add("missing coverage requires insufficient_evidence rather than complete no-change")
				}
			}
			if len(f.BugEvidence) == 0 || len(f.CauseEvidence) == 0 || strings.TrimSpace(f.Cause) == "" || strings.TrimSpace(f.PreventionRationale) == "" {
				add("no_warranted_change requires supported cause and explanation")
			}
		case "proposed":
			if len(f.Proposals) == 0 {
				add("proposed finding requires a standard")
			}
			if status.Integrity != "verified" || status.Selection == nil || status.SelectionStatus == "corrupt" || status.RetrievalStatus != "retrieved" || s.degraded {
				add("degraded or partially delivered standards cannot validate proposals")
			}
			for _, id := range f.CauseEvidence {
				if slices.Contains(f.BugEvidence, id) {
					add("bug and cause evidence must be cited separately")
				}
			}
			if len(f.BugEvidence) == 0 || len(f.CauseEvidence) == 0 || strings.TrimSpace(f.Cause) == "" || strings.TrimSpace(f.PreventionRationale) == "" {
				add("proposal needs bug evidence, cause evidence, cause and prevention rationale")
			}
		default:
			add("invalid per-bug outcome")
		}
		if f.Outcome != "insufficient_evidence" && f.CauseBasis != "recorded" && f.CauseBasis != "inference" {
			add("cause_basis must distinguish recorded cause from inference")
		}
		for _, op := range f.Proposals {
			proposalCount++
			if op.Kind != "standard.add" && op.Kind != "standard.replace" {
				add("only standard additions and revisions are permitted")
				continue
			}
			if op.Standard == nil {
				add("standard content required")
				continue
			}
			if op.Kind == "standard.replace" && (status.Selection == nil || !slices.Contains(status.Selection.StandardIDs, op.Standard.ID)) {
				add("revised standard must belong to retrieved selection")
			}
			if status.Selection != nil {
				if !slices.Contains(status.Selection.CategoryIDs, op.Standard.CategoryID) {
					add("proposal category must be selected from pinned standards")
				}
			} else {
				add("proposal requires pinned category selection")
			}
			p, e := (&standards.Service{Store: s.service.DB}).Prepare(ctx, fmt.Sprintf("analysis-%s-%d", a.ID, proposalCount), status.Binding.RevisionID, []standards.Operation{op})
			if e != nil {
				add("upstream standards validation: " + e.Error())
				continue
			}
			batch = append(batch, store.PreventionProposal{Proposal: p, Provenance: marshal(f)})
		}
	}
	if len(result.Findings) != len(selected) || len(seen) != len(selected) {
		add("every selected bug requires an explicit outcome")
	}
	if proposalCount > s.scope.Limits.Proposals {
		add("proposal limit exceeded")
	}
	return diagnostics, batch, nil
}

// Preflight validates a typed draft without finalizing proposals.
func (s *Session) Preflight(ctx context.Context, result Result) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	_, changed, err := s.sanitizedJSON(ctx, result, 64<<10)
	if err != nil {
		return nil, err
	}
	ds := []string{}
	if changed {
		ds = append(ds, "result contains redacted content; revise it before submission")
	} else {
		ds, _, err = s.validate(ctx, result)
	}
	if err == nil {
		err = s.account(ctx, ds)
	}
	return ds, err
}

// Submit atomically finalizes a valid typed result or retains its invalid draft.
func (s *Session) Submit(ctx context.Context, requestID string, result Result) (Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Receipt{}, err
	}
	if requestID == "" || len(requestID) > 256 {
		return Receipt{}, errors.New("request identity required")
	}
	if _, changed, e := s.sanitizedJSON(ctx, requestID, 1024); e != nil || changed {
		return Receipt{}, errors.New("request identity withheld by redaction policy")
	}
	retained := retainedResult{Result: result, RequestID: requestID, Coverage: s.manifest.Coverage, Limits: s.scope.Limits, ManifestDigest: hash(s.manifest)}
	content := retained
	content.RequestID = ""
	digest := hash(content)
	a, err := s.service.DB.GetPreventionAnalysis(ctx, s.analysis.ID)
	if err != nil {
		return Receipt{}, err
	}
	if len(a.Result) > 0 {
		if a.ResultDigest != digest {
			return Receipt{}, store.ErrPreventionConflict
		}
		var existing retainedResult
		if err = json.Unmarshal(a.Result, &existing); err != nil {
			return Receipt{}, err
		}
		out := receipt(a, existing.RequestID)
		return out, s.account(ctx, out)
	}
	if s.corrections >= s.scope.Limits.Corrections+1 {
		return Receipt{}, errors.New("correction submissions exhausted")
	}
	s.corrections++
	draft, changed, redactErr := s.sanitizedJSON(ctx, result, 64<<10)
	if redactErr != nil || changed {
		diagnostic := "result content withheld because safe redaction was unavailable or its bounds were exceeded"
		if changed {
			diagnostic = "result contains redacted content; revise it before submission"
		}
		if redactErr != nil {
			draft = marshal(map[string]bool{"withheld": true})
		}
		ds := []string{diagnostic}
		if err = s.service.DB.SavePreventionDraft(ctx, a.ID, a.AttemptID, draft, marshal(ds)); err != nil {
			return Receipt{}, err
		}
		if err = s.account(ctx, ds); err != nil {
			return Receipt{}, err
		}
		return Receipt{}, errors.New(diagnostic)
	}
	ds, batch, err := s.validate(ctx, result)
	if err != nil {
		return Receipt{}, err
	}
	if len(ds) > 0 {
		if err = s.service.DB.SavePreventionDraft(ctx, a.ID, a.AttemptID, marshal(result), marshal(ds)); err != nil {
			return Receipt{}, err
		}
		if err = s.account(ctx, ds); err != nil {
			return Receipt{}, err
		}
		return Receipt{}, fmt.Errorf("invalid result: %s", strings.Join(ds, "; "))
	}
	// Budget the receipt before irreversible finalization so no unaccounted large response escapes.
	anticipated := Receipt{RequestID: requestID, AnalysisID: a.ID, ResultDigest: digest, Proposals: []ProposalReference{}}
	for _, p := range batch {
		anticipated.Proposals = append(anticipated.Proposals, proposalReference(p.Proposal))
	}
	if err = s.account(ctx, anticipated); err != nil {
		return Receipt{}, err
	}
	ps, err := s.service.DB.FinalizePreventionAnalysis(ctx, a.ID, a.AttemptID, digest, marshal(retained), batch)
	if err != nil {
		return Receipt{}, err
	}
	anticipated.Proposals = proposalReferences(ps)
	return anticipated, nil
}
func proposalReference(p store.StandardsProposalVersion) ProposalReference {
	return ProposalReference{ProposalID: p.ProposalID, Version: p.Version, BaseRevision: p.BaseRevision, ChangeDigest: p.ChangeDigest, ResultDigest: p.ResultDigest}
}
func proposalReferences(ps []store.StandardsProposalVersion) []ProposalReference {
	out := make([]ProposalReference, 0, len(ps))
	for _, p := range ps {
		out = append(out, proposalReference(p))
	}
	return out
}
func receipt(a store.PreventionAnalysis, id string) Receipt {
	return Receipt{RequestID: id, AnalysisID: a.ID, ResultDigest: a.ResultDigest, Proposals: proposalReferences(a.Proposals)}
}

// Receipt recovers the exact durable accepted submission by request identity.
func (s *Session) Receipt(ctx context.Context, requestID string) (Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Receipt{}, err
	}
	a, err := s.service.DB.GetPreventionAnalysis(ctx, s.analysis.ID)
	if err != nil {
		return Receipt{}, err
	}
	var r retainedResult
	if len(a.Result) == 0 {
		return Receipt{}, store.ErrNotFound
	}
	if err = json.Unmarshal(a.Result, &r); err != nil {
		return Receipt{}, err
	}
	if requestID != r.RequestID {
		return Receipt{}, store.ErrNotFound
	}
	out := receipt(a, requestID)
	if err = s.account(ctx, out); err != nil {
		return Receipt{}, err
	}
	return out, nil
}
