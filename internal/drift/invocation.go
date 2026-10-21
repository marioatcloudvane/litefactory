package drift

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// Citation identifies an offered range of sanitized retained evidence.
type Citation struct {
	EvidenceID string `json:"evidence_id"`
	Digest     string `json:"digest"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
}

// Finding records an observed departure, its usefulness and uncertainty.
type Finding struct {
	ObservedDrift string     `json:"observed_drift"`
	Reason        string     `json:"reason"`
	Benefit       string     `json:"benefit"`
	Uncertainty   string     `json:"uncertainty"`
	Citations     []Citation `json:"citations"`
}

// Candidate associates one typed proposal operation with a cited finding.
type Candidate struct {
	Key       string              `json:"key"`
	Operation standards.Operation `json:"operation"`
	Finding   int                 `json:"finding"`
}

// Report is the analyst finding disposition, separate from host execution state.
type Report struct {
	Disposition     string      `json:"disposition"`
	Explanation     string      `json:"explanation"`
	Uncertainty     string      `json:"uncertainty"`
	SelectionDigest string      `json:"selection_digest"`
	Findings        []Finding   `json:"findings"`
	Candidates      []Candidate `json:"candidates"`
}

// ResultInput binds exact report bytes to a stable submission identity.
type ResultInput struct {
	RequestID string `json:"request_id"`
	Report    Report `json:"report"`
}
type acceptedReport struct {
	Report    Report                  `json:"report"`
	Coverage  json.RawMessage         `json:"coverage"`
	Standards standards.RuntimeStatus `json:"standards"`
}

// Invocation owns one generation. A cancellable gate serializes reservations and
// runtime side effects; the private broker admits at most two waiting workers.
type Invocation struct {
	gateOnce  sync.Once
	gate      chan struct{}
	store     *store.DB
	analysis  store.DriftAnalysis
	runtime   *standards.Runtime
	exhausted bool
	pending   []store.StandardsRunObservation
}

// ID returns the immutable analysis identity for this writer.
func (i *Invocation) ID() string { return i.analysis.ID }

// Call is the complete scoped domain dispatch. Every attempt is charged before
// work, every encoded response before delivery; failures never refund allowance.
func (i *Invocation) Call(ctx context.Context, name string, payload json.RawMessage) (json.RawMessage, error) {
	return i.execute(ctx, name, payload, false, nil)
}

func (i *Invocation) execute(ctx context.Context, name string, payload json.RawMessage, catalog bool, refusal error) (reply json.RawMessage, failure error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	i.gateOnce.Do(func() { i.gate = make(chan struct{}, 1) })
	select {
	case i.gate <- struct{}{}:
		defer func() { <-i.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	permitted := catalog || slices.Contains(agent.DriftToolNames(), name)
	final := permitted && strings.HasPrefix(name, "communication_")
	terminal := false
	charge := func(delta store.DriftUsage, offers []store.DriftOffer) error {
		a, err := i.store.GetDriftAnalysis(ctx, i.analysis.ID)
		if err != nil {
			return err
		}
		terminal = a.Execution != "running"
		if terminal && len(a.Report) != 0 {
			_, err = i.store.ChargeDriftRecovery(ctx, a.ID, delta)
		} else {
			_, err = i.store.ChargeDriftBudget(ctx, a.ID, i.analysis.Generation, delta, offers, final)
		}
		return err
	}
	// Admit every attempted call, including malformed and unknown requests. Failed
	// validation cannot be used to obtain fresh broker work outside the call cap.
	if err := charge(store.DriftUsage{Calls: 1}, nil); err != nil {
		i.exhausted = true
		return nil, err
	}
	defer func() {
		if failure == nil {
			return
		}
		failure = &toolError{cause: failure}
		diagnostic := struct {
			Error string `json:"error"`
		}{failure.Error()}
		if err := charge(store.DriftUsage{OutboundBytes: int64(encodedSize(diagnostic))}, nil); err != nil {
			i.exhausted = true
			failure = err
		}
	}()
	if refusal != nil {
		return nil, refusal
	}
	if !permitted {
		return nil, errors.New("drift tool not permitted")
	}
	if terminal && name != "communication_submit" && name != "communication_receipt" {
		return nil, store.ErrDriftConflict
	}
	if len(payload) > i.analysis.Limits.ResultBytes {
		return nil, errors.New("drift input exceeds result byte ceiling")
	}
	if name == "communication_receipt" || name == "communication_submit" {
		var requestID string
		if name == "communication_receipt" {
			var p struct {
				RequestID string `json:"request_id"`
			}
			if err := submission.DecodeJSON(payload, &p); err != nil {
				return nil, err
			}
			requestID = p.RequestID
		} else {
			var p ResultInput
			if err := submission.DecodeJSON(payload, &p); err != nil {
				return nil, err
			}
			requestID = p.RequestID
			if transportID, ok := ctx.Value(transportRequestKey{}).(string); ok {
				requestID = transportID
			}
		}
		r, err := i.store.GetDriftReceipt(ctx, i.analysis.ID, requestID)
		if err == nil {
			if name == "communication_submit" && r.Digest != digest(payload) {
				return nil, store.ErrDriftConflict
			}
			value := receiptSummary(r)
			if err := charge(store.DriftUsage{OutboundBytes: int64(encodedSize(value))}, nil); err != nil {
				return nil, err
			}
			return bytesJSON(value), nil
		}
		if !errors.Is(err, store.ErrNotFound) || name == "communication_receipt" {
			return nil, err
		}
	}
	if i.exhausted && !final {
		return nil, store.ErrDriftBudget
	}
	i.pending = nil
	var value any
	var offers []store.DriftOffer
	var err error
	if catalog {
		value = struct {
			Tools []submission.ToolSpec `json:"tools"`
		}{Tools: toolSpecs()}
	} else {
		value, offers, err = i.dispatch(ctx, name, payload)
	}
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	text, _ := json.Marshal(string(raw))
	encoded := int64(len(raw) + len(text) + 1024)
	ceiling := 32 << 10
	if strings.HasPrefix(name, "drift_evidence_") {
		ceiling = 16 << 10
	}
	if encoded > int64(ceiling) {
		return nil, errors.New("encoded tool page exceeds ceiling")
	}
	// Submit reserves its bounded response before committing, inside dispatch.
	if name != "communication_submit" {
		if _, err = i.store.ChargeDriftBudget(ctx, i.analysis.ID, i.analysis.Generation, store.DriftUsage{OutboundBytes: encoded}, offers, final); err != nil {
			i.exhausted = true
			return nil, err
		}
	}
	for _, observation := range i.pending {
		if err := i.store.AppendStandardsRunObservation(ctx, observation); err != nil {
			return nil, err
		}
	}
	return raw, nil
}

// toolError bounds diagnostics without dropping the typed cause used by the host.
type toolError struct{ cause error }

func (e *toolError) Error() string {
	message := e.cause.Error()
	if len(message) > 512 {
		message = message[:512]
		for !utf8.ValidString(message) {
			message = message[:len(message)-1]
		}
		message += " (diagnostic truncated)"
	}
	return message
}
func (e *toolError) Unwrap() error { return e.cause }

// Standards delivery is recorded only after the shared encoded budget is charged.
// Failed or uncertain delivery keeps the charge, while an unfunded page is never
// considered offered by the upstream retrieval contract.
type deferredObservations struct {
	standards.RuntimeStore
	pending *[]store.StandardsRunObservation
}

func (s deferredObservations) AppendStandardsRunObservation(_ context.Context, o store.StandardsRunObservation) error {
	*s.pending = append(*s.pending, o)
	return nil
}

func (i *Invocation) dispatch(ctx context.Context, name string, payload json.RawMessage) (any, []store.DriftOffer, error) {
	switch name {
	case "standards_categories_list":
		var p struct {
			PageSize int    `json:"page_size"`
			Cursor   string `json:"cursor"`
		}
		if e := submission.DecodeJSON(payload, &p); e != nil {
			return nil, nil, e
		}
		v, e := i.runtime.Categories(ctx, p.PageSize, p.Cursor)
		return v, nil, e
	case "standards_select":
		var p struct {
			ExpectedVersion int64    `json:"expected_version"`
			CategoryIDs     []string `json:"category_ids"`
			Reason          string   `json:"reason"`
		}
		if e := submission.DecodeJSON(payload, &p); e != nil {
			return nil, nil, e
		}
		v, e := i.runtime.Select(ctx, p.ExpectedVersion, p.CategoryIDs, p.Reason)
		return standards.BoundedStatus(v), nil, e
	case "standards_read":
		var p struct {
			SelectionVersion int64  `json:"selection_version"`
			SelectionDigest  string `json:"selection_digest"`
			Cursor           string `json:"cursor"`
			Acknowledge      bool   `json:"acknowledge"`
		}
		if e := submission.DecodeJSON(payload, &p); e != nil {
			return nil, nil, e
		}
		runtime := *i.runtime
		runtime.Store = deferredObservations{RuntimeStore: i.runtime.Store, pending: &i.pending}
		v, e := runtime.Read(ctx, p.SelectionVersion, p.SelectionDigest, p.Cursor, p.Acknowledge)
		return v, nil, e
	case "drift_evidence_list":
		return i.evidenceList(ctx, payload)
	case "drift_evidence_read":
		return i.evidenceRead(ctx, payload)
	case "communication_preflight", "communication_submit":
		var input ResultInput
		if e := submission.DecodeJSON(payload, &input); e != nil {
			return nil, nil, e
		}
		if name == "communication_submit" {
			if transportID, ok := ctx.Value(transportRequestKey{}).(string); ok {
				input.RequestID = transportID
			}
			if old, e := i.store.GetDriftReceipt(ctx, i.analysis.ID, input.RequestID); e == nil {
				if old.Digest != digest(payload) {
					return nil, nil, store.ErrDriftConflict
				}
				return receiptSummary(old), nil, nil
			} else if !errors.Is(e, store.ErrNotFound) {
				return nil, nil, e
			}
		}
		report, candidates, execution, e := i.validate(ctx, input)
		if e != nil {
			return nil, nil, e
		}
		if name == "communication_preflight" {
			return map[string]any{"valid": true, "execution": execution, "disposition": input.Report.Disposition}, nil, nil
		}
		// Bound response reservation includes exact candidate identifiers and a capped
		// acknowledgment, never an echo of the potentially 128 KiB final report.
		links := make([]store.DriftProposalLink, 0, len(candidates))
		for _, candidate := range candidates {
			links = append(links, store.DriftProposalLink{CandidateKey: candidate.Key, ProposalID: candidate.Proposal.ProposalID, Version: 1, Digest: candidate.Proposal.ChangeDigest})
		}
		response := receiptSummary(store.DriftReceipt{AnalysisID: i.analysis.ID, RequestID: input.RequestID, Digest: digest(payload), ProposalLinks: links})
		if _, e = i.store.ChargeDriftBudget(ctx, i.analysis.ID, i.analysis.Generation, store.DriftUsage{OutboundBytes: int64(encodedSize(response))}, nil, true); e != nil {
			return nil, nil, e
		}
		a, e := i.store.GetDriftAnalysis(ctx, i.analysis.ID)
		if e != nil {
			return nil, nil, e
		}
		receipt, e := i.store.FinalizeDriftAnalysis(ctx, a.ID, i.analysis.Generation, a.Version, input.RequestID, digest(payload), bytesJSON(report), execution, input.Report.Disposition, candidates)
		if e != nil {
			return nil, nil, e
		}
		return receiptSummary(receipt), nil, nil
	}
	return nil, nil, errors.New("unknown drift tool")
}
func receiptSummary(r store.DriftReceipt) any {
	return struct {
		AnalysisID    string                    `json:"analysis_id"`
		RequestID     string                    `json:"request_id"`
		Digest        string                    `json:"digest"`
		ProposalLinks []store.DriftProposalLink `json:"proposal_links"`
	}{r.AnalysisID, r.RequestID, r.Digest, r.ProposalLinks}
}

type evidenceCursor struct {
	Analysis, Digest string
	Offset           int
}

func (i *Invocation) evidenceList(ctx context.Context, payload json.RawMessage) (any, []store.DriftOffer, error) {
	var p struct {
		Cursor   string `json:"cursor"`
		PageSize int    `json:"page_size"`
	}
	if e := submission.DecodeJSON(payload, &p); e != nil {
		return nil, nil, e
	}
	if p.PageSize == 0 {
		p.PageSize = 10
	}
	if p.PageSize < 1 || p.PageSize > 25 {
		return nil, nil, errors.New("invalid evidence page size")
	}
	cursor := evidenceCursor{Analysis: i.analysis.ID, Digest: digest(i.analysis.Scope)}
	if p.Cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(p.Cursor)
		if e != nil {
			return nil, nil, e
		}
		var c evidenceCursor
		if e = submission.DecodeJSON(b, &c); e != nil || c.Analysis != cursor.Analysis || c.Digest != cursor.Digest || c.Offset < 0 {
			return nil, nil, errors.New("invalid evidence cursor")
		}
		cursor = c
	}
	items, e := i.store.ListDriftEvidence(ctx, i.analysis.ID, cursor.Offset, p.PageSize+1)
	if e != nil {
		return nil, nil, e
	}
	type manifest struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		Locator  string `json:"locator"`
		Digest   string `json:"digest"`
		Bytes    int    `json:"bytes"`
		Omission string `json:"omission,omitempty"`
	}
	out := struct {
		Evidence   []manifest `json:"evidence"`
		NextCursor string     `json:"next_cursor,omitempty"`
	}{Evidence: []manifest{}}
	for n, item := range items {
		if n == p.PageSize {
			break
		}
		m := manifest{item.ID, item.Kind, item.Locator, item.Digest, len(item.Text), item.Omission}
		out.Evidence = append(out.Evidence, m)
		if encodedSize(out) > 14<<10 {
			out.Evidence = out.Evidence[:len(out.Evidence)-1]
			break
		}
	}
	if len(items) > len(out.Evidence) {
		if len(out.Evidence) == 0 {
			return nil, nil, errors.New("evidence metadata exceeds page ceiling")
		}
		cursor.Offset += len(out.Evidence)
		out.NextCursor = base64.RawURLEncoding.EncodeToString(bytesJSON(cursor))
	}
	return out, nil, nil
}
func encodedSize(v any) int {
	raw := bytesJSON(v)
	t, _ := json.Marshal(string(raw))
	return len(raw) + len(t) + 1024
}
func (i *Invocation) evidenceRead(ctx context.Context, payload json.RawMessage) (any, []store.DriftOffer, error) {
	var p struct {
		EvidenceID string `json:"evidence_id"`
		Start      int    `json:"start"`
		Limit      int    `json:"limit"`
	}
	if e := submission.DecodeJSON(payload, &p); e != nil {
		return nil, nil, e
	}
	if p.Limit == 0 {
		p.Limit = 2048
	}
	if p.Limit < 1 || p.Limit > 4096 || p.Start < 0 {
		return nil, nil, errors.New("invalid evidence range")
	}
	item, e := i.store.GetDriftEvidence(ctx, i.analysis.ID, p.EvidenceID)
	if e != nil {
		return nil, nil, e
	}
	if item.ExcerptExpired {
		return nil, nil, errors.New("retained evidence excerpt expired; locator and digest remain inspectable")
	}
	if p.Start > len(item.Text) || !utf8.ValidString(item.Text[:p.Start]) {
		return nil, nil, errors.New("invalid evidence offset")
	}
	end := min(len(item.Text), p.Start+p.Limit)
	for !utf8.ValidString(item.Text[p.Start:end]) {
		end--
	}
	out := struct {
		EvidenceID string `json:"evidence_id"`
		Digest     string `json:"digest"`
		Locator    string `json:"locator"`
		Text       string `json:"text"`
		Start      int    `json:"start"`
		End        int    `json:"end"`
		Total      int    `json:"total"`
		Complete   bool   `json:"complete"`
		Omission   string `json:"omission,omitempty"`
	}{item.ID, item.Digest, item.Locator, item.Text[p.Start:end], p.Start, end, len(item.Text), end == len(item.Text), item.Omission}
	for encodedSize(out) > 16<<10 && end > p.Start {
		end = p.Start + (end-p.Start)/2
		for !utf8.ValidString(item.Text[p.Start:end]) {
			end--
		}
		out.Text = item.Text[p.Start:end]
		out.End = end
		out.Complete = end == len(item.Text)
	}
	return out, []store.DriftOffer{{EvidenceID: item.ID, Digest: item.Digest, Start: p.Start, End: end}}, nil
}

func (i *Invocation) validate(ctx context.Context, input ResultInput) (acceptedReport, []store.DriftCandidate, string, error) {
	invalid := func(s string) (acceptedReport, []store.DriftCandidate, string, error) {
		return acceptedReport{}, nil, "", errors.New(s)
	}
	r := input.Report
	if input.RequestID == "" || len(input.RequestID) > 128 || strings.TrimSpace(r.Explanation) == "" || strings.TrimSpace(r.Uncertainty) == "" || len(r.Findings) > 30 || len(r.Candidates) > i.analysis.Limits.Proposals {
		return invalid("result requires bounded identity, explanation, uncertainty and candidates")
	}
	if !slices.Contains([]string{"proposals", "no_warranted_change", "insufficient_evidence", "not_evaluated"}, r.Disposition) || (len(r.Candidates) > 0) != (r.Disposition == "proposals") {
		return invalid("invalid finding disposition")
	}
	a, e := i.store.GetDriftAnalysis(ctx, i.analysis.ID)
	if e != nil {
		return acceptedReport{}, nil, "", e
	}
	status, e := i.runtime.Status(ctx)
	if e != nil {
		return acceptedReport{}, nil, "", e
	}
	inspection, e := i.runtime.Inspect(ctx)
	if e != nil {
		return acceptedReport{}, nil, "", e
	}
	ready := status.Integrity == "verified" && status.Selection != nil && status.RetrievalStatus == "retrieved" && status.Selection.Digest == r.SelectionDigest
	for _, consumer := range inspection.Consumers {
		if consumer.ConsumerKey == "drift-analyst" && len(consumer.RetrievalGaps) > 0 {
			ready = false
		}
	}
	if (r.Disposition == "proposals" || r.Disposition == "no_warranted_change") && !ready {
		return invalid("verified fully retrieved selected standards required")
	}
	var coverage Coverage
	if len(a.Coverage) == 0 || json.Unmarshal(a.Coverage, &coverage) != nil {
		return invalid("host coverage unavailable")
	}
	// Acquisition alone does not establish model coverage. Keep every retained
	// but unoffered body visible in the host-owned final coverage.
	for offset := 0; ; offset += 100 {
		items, err := i.store.ListDriftEvidence(ctx, a.ID, offset, 100)
		if err != nil {
			return acceptedReport{}, nil, "", err
		}
		for _, item := range items {
			if len(item.Text) == 0 {
				continue
			}
			ranges := []store.DriftOffer{}
			for _, offer := range a.Offers {
				if offer.EvidenceID == item.ID && offer.Digest == item.Digest {
					ranges = append(ranges, offer)
				}
			}
			sort.Slice(ranges, func(x, y int) bool { return ranges[x].Start < ranges[y].Start })
			end := 0
			for _, offer := range ranges {
				if offer.Start <= end && offer.End > end {
					end = offer.End
				}
			}
			if end < len(item.Text) {
				coverage.Gaps = append(coverage.Gaps, fmt.Sprintf("%s: retained evidence has unoffered ranges", item.ID))
			}
		}
		if len(items) < 100 {
			break
		}
	}
	execution := "completed"
	if len(coverage.Gaps) > 0 || coverage.UnknownRemainder || i.exhausted {
		execution = "incomplete"
	}
	if r.Disposition == "no_warranted_change" && execution != "completed" {
		return invalid("no warranted change requires a completed bounded pass")
	}
	for _, f := range r.Findings {
		if strings.TrimSpace(f.ObservedDrift) == "" || strings.TrimSpace(f.Reason) == "" || strings.TrimSpace(f.Benefit) == "" || strings.TrimSpace(f.Uncertainty) == "" || len(f.Citations) == 0 || len(f.Citations) > 30 {
			return invalid("each finding requires observed drift, reason, benefit, uncertainty and citations")
		}
		for _, c := range f.Citations {
			item, e := i.store.GetDriftEvidence(ctx, a.ID, c.EvidenceID)
			if e != nil {
				return invalid("citation evidence unavailable")
			}
			if c.Digest != item.Digest || c.Start < 0 || c.End <= c.Start || c.End > len(item.Text) {
				return invalid("invalid citation range or digest")
			}
			offers := []store.DriftOffer{}
			for _, o := range a.Offers {
				if o.EvidenceID == c.EvidenceID && o.Digest == c.Digest {
					offers = append(offers, o)
				}
			}
			sort.Slice(offers, func(x, y int) bool { return offers[x].Start < offers[y].Start })
			end := c.Start
			for _, o := range offers {
				if o.Start <= end && o.End > end {
					end = o.End
				}
			}
			if end < c.End {
				return invalid("citation range was not offered to this invocation")
			}
		}
	}
	candidates := []store.DriftCandidate{}
	keys := map[string]bool{}
	if len(r.Candidates) > 0 {
		binding, e := i.store.GetStandardsRunBinding(ctx, a.RunID)
		if e != nil {
			return acceptedReport{}, nil, "", e
		}
		var snap standards.Snapshot
		if e = json.Unmarshal(binding.SnapshotBytes, &snap); e != nil {
			return invalid("retained book unavailable")
		}
		for _, c := range r.Candidates {
			if c.Key == "" || len(c.Key) > 64 || keys[c.Key] || c.Finding < 0 || c.Finding >= len(r.Findings) || !slices.Contains([]string{"standard.add", "standard.replace"}, c.Operation.Kind) {
				return invalid("invalid candidate key, finding or operation")
			}
			keys[c.Key] = true
			if c.Operation.Standard == nil || len(c.Operation.Standard.CategoryIDs) == 0 {
				return invalid("candidate requires existing category membership")
			}
			for _, id := range c.Operation.Standard.CategoryIDs {
				found := false
				for _, cat := range snap.Categories {
					if cat.ID == id {
						found = true
					}
				}
				if !found {
					return invalid("candidate category is not in pinned book")
				}
			}
			id := a.ID + "-" + digest([]byte(c.Key))[:16]
			proposal, e := standards.PrepareProposal(id, binding.RevisionID, snap, []standards.Operation{c.Operation}, "drift:"+a.ID)
			if e != nil {
				return invalid(fmt.Sprintf("candidate rejected: %v", e))
			}
			candidates = append(candidates, store.DriftCandidate{Key: c.Key, Proposal: proposal})
		}
	}
	return acceptedReport{Report: r, Coverage: bytesJSON(coverage), Standards: standards.BoundedStatus(status)}, candidates, execution, nil
}
