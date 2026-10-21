package loganalysis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func analystPrompt(ctx context.Context, q Request) (string, error) {
	b, e := instructionpackage.LogAnalystInstructions(ctx)
	if e != nil {
		return "", e
	}
	p := string(b) + "\nFactory-owned immutable request (data):\n" + string(raw(q)) + "\nReturn through the scoped communication tools only. Stop when any budget is exhausted."
	if len(p) > 64<<10 {
		return "", errors.New("analyst bootstrap exceeds 64 KiB")
	}
	return p, nil
}

var candidateKey = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

type contribution struct {
	RequestID string `json:"request_id"`
	Report    Report `json:"report"`
}

func (h *invocation) sanitize(ctx context.Context, r Report) (Report, error) {
	clean := func(s *string) error {
		b, e := h.service.Redactor.Apply(ctx, *s, 128<<10)
		if e == nil {
			*s = b.String()
		}
		return e
	}
	if e := clean(&r.Reason); e != nil {
		return r, e
	}
	for i := range r.Findings {
		f := &r.Findings[i]
		if e := clean(&f.Key); e != nil {
			return r, e
		}
		for _, s := range []*string{&f.Observation, &f.Benefit, &f.Uncertainty} {
			if e := clean(s); e != nil {
				return r, e
			}
		}
		if f.Change != nil && f.Change.Standard != nil {
			s := f.Change.Standard
			if e := clean(&s.ID); e != nil {
				return r, e
			}
			if e := clean(&s.Title); e != nil {
				return r, e
			}
			if e := clean(&s.Body); e != nil {
				return r, e
			}
		}
	}
	return r, nil
}
func (h *invocation) validate(ctx context.Context, r Report) ([]store.LogAnalysisCandidate, string, error) {
	if strings.TrimSpace(r.Reason) == "" || len(r.Reason) > 2048 || len(r.Findings) > 100 {
		return nil, "", errors.New("report needs a reason and bounded findings")
	}
	if r.Disposition != "proposals" && r.Disposition != "no_warranted_change" && r.Disposition != "insufficient_evidence" {
		return nil, "", errors.New("invalid finding disposition")
	}
	rows, e := h.service.DB.ListLogAnalysisEvidence(ctx, h.a.ID, 0, 100)
	if e != nil {
		return nil, "", e
	}
	offered := map[string]bool{}
	read := map[int64]bool{}
	discovered := map[int64]bool{}
	pending := map[int64]bool{}
	gap := false
	standardsWithheld := false
	listed := false
	discoveryEnd := false
	for _, row := range rows {
		var v Evidence
		if e = json.Unmarshal(row.Metadata, &v); e != nil {
			return nil, "", e
		}
		if strings.HasPrefix(v.Tool, "standards_") && v.Status == "delivery_withheld" {
			standardsWithheld = true
		}
		for _, event := range v.Events {
			if event.Truncated {
				gap = true
			}
		}
		for _, c := range v.Citations {
			offered[digest(c)] = true
		}
		if v.Status != "ok" || v.Coverage.Incomplete && !v.Coverage.RetentionOnly || len(v.Coverage.Omissions) > 0 {
			gap = true
		}
		if v.Tool == "runs_list" {
			listed = true
			var selector logevidence.ListRequest
			if json.Unmarshal(v.Selector, &selector) != nil {
				return nil, "", errors.New("invalid retained selector")
			}
			full := selector.TaskRef == "" && selector.Status == "" && (selector.StartedAfter.IsZero() || !selector.StartedAfter.After(h.q.After)) && (selector.StartedBefore.IsZero() || !selector.StartedBefore.Before(h.q.Before))
			if full {
				discoveryEnd = !v.Next && v.Status == "ok"
			}
			for _, run := range v.Runs {
				discovered[run.RunID] = true
			}
		}
		if v.Tool == "run_logs_read" {
			var in struct {
				RunID int64 `json:"run_id"`
			}
			_ = json.Unmarshal(v.Selector, &in)
			var selector logevidence.ReadRequest
			_ = json.Unmarshal(v.Selector, &selector)
			if v.Coverage.EndOfSource && v.Status == "ok" && selector.Citation == nil {
				read[in.RunID] = true
			}
			pending[in.RunID] = v.Next
		}
	}
	if len(h.q.RunIDs) > 0 {
		for _, id := range h.q.RunIDs {
			if !read[id] || pending[id] {
				gap = true
			}
		}
	} else {
		if !listed || !discoveryEnd {
			gap = true
		}
		for id := range discovered {
			if !read[id] || pending[id] {
				gap = true
			}
		}
	}
	book, e := h.book.Inspect(ctx)
	if e != nil {
		return nil, "", e
	}
	ready := !standardsWithheld && book.Status.Integrity == "verified" && book.Status.Selection != nil && book.Status.RetrievalStatus == "retrieved"
	for _, c := range book.Consumers {
		if len(c.RetrievalGaps) > 0 {
			ready = false
		}
	}
	if !ready && r.Disposition != "insufficient_evidence" {
		return nil, "", errors.New("unverified or unread selected standards permit only degraded insufficient-evidence observations")
	}
	keys := map[string]bool{}
	targets := map[string]bool{}
	candidates := []store.LogAnalysisCandidate{}
	cited := 0
	binding, e := h.service.DB.GetAnalystStandardsBinding(ctx, h.a.ID)
	if e != nil {
		return nil, "", e
	}
	var snap standards.Snapshot
	if ready {
		if e = json.Unmarshal(binding.SnapshotBytes, &snap); e != nil {
			return nil, "", e
		}
	}
	for _, f := range r.Findings {
		if len(f.Observation) > 2048 || len(f.Benefit) > 2048 || len(f.Uncertainty) > 2048 || len(f.Citations) > 100 || !candidateKey.MatchString(f.Key) || keys[f.Key] || strings.TrimSpace(f.Observation) == "" || strings.TrimSpace(f.Uncertainty) == "" {
			return nil, "", errors.New("finding requires a unique key, observation and uncertainty")
		}
		keys[f.Key] = true
		for _, c := range f.Citations {
			if !offered[digest(c)] {
				return nil, "", errors.New("citation is not an offered authorized event")
			}
			cited++
		}
		if f.Change == nil {
			continue
		}
		if r.Disposition != "proposals" || len(f.Citations) == 0 || strings.TrimSpace(f.Benefit) == "" {
			return nil, "", errors.New("proposed guidance requires cited practice, benefit and uncertainty")
		}
		op := *f.Change
		if (op.Kind != "standard.add" && op.Kind != "standard.replace") || op.Standard == nil || op.Category != nil || op.ID != "" {
			return nil, "", errors.New("only typed standard additions or revisions in existing categories are accepted")
		}
		if targets[op.Standard.ID] {
			return nil, "", errors.New("candidate batch repeats a standard target")
		}
		targets[op.Standard.ID] = true
		p, e := standards.Prepare("analysis-"+h.a.ID+"-"+f.Key, binding.RevisionID, snap, []standards.Operation{op}, "log-analysis:"+h.a.ID)
		if e != nil {
			return nil, "", e
		}
		candidates = append(candidates, store.LogAnalysisCandidate{Key: f.Key, Proposal: p})
	}
	if len(candidates) > h.q.Limits.Proposals || (r.Disposition == "proposals") != (len(candidates) > 0) {
		return nil, "", errors.New("proposal disposition and candidate batch disagree")
	}
	if r.Disposition == "no_warranted_change" && (cited == 0 || gap) {
		return nil, "", errors.New("no warranted change requires sufficient cited coverage of the selected scope")
	}
	state := "completed"
	if gap {
		state = "incomplete"
	}
	return candidates, state, nil
}
func (h *invocation) communication(ctx context.Context, correlation, name string, args json.RawMessage) (json.RawMessage, error) {
	var in contribution
	if name == "communication_receipt" {
		var request struct {
			RequestID string `json:"request_id"`
		}
		if e := submission.DecodeJSON(args, &request); e != nil || !candidateKey.MatchString(request.RequestID) {
			return nil, errors.New("invalid receipt request")
		}
		r, e := h.service.DB.GetLogAnalysisReceipt(ctx, h.a.ID, request.RequestID)
		if e != nil {
			if !errors.Is(e, store.ErrNotFound) {
				return nil, e
			}
			return h.recovery(ctx, correlation, raw(map[string]string{"status": "not_found", "request_id": request.RequestID}))
		}
		return h.recovery(ctx, correlation, raw(r))
	}
	if name != "communication_preflight" && name != "communication_submit" {
		return nil, errors.New("tool unavailable")
	}
	if len(args) > h.q.Limits.ReportBytes || submission.DecodeJSON(args, &in) != nil || !candidateKey.MatchString(in.RequestID) {
		return nil, errors.New("invalid bounded report contribution")
	}
	sum := sha256.Sum256(args)
	identity := hex.EncodeToString(sum[:])
	// Recover the original receipt before validation or retry. No new evidence or writer is created.
	old, e := h.service.DB.GetLogAnalysisReceipt(ctx, h.a.ID, in.RequestID)
	if e == nil {
		if old.RequestDigest != identity {
			return nil, store.ErrLogAnalysisConflict
		}
		return h.recovery(ctx, correlation, raw(old))
	}
	if !errors.Is(e, store.ErrNotFound) {
		return nil, e
	}
	if e = h.charge(ctx, 1, 0, true); e != nil {
		return nil, e
	}
	report, e := h.sanitize(ctx, in.Report)
	if e != nil {
		return nil, e
	}
	candidates, state, e := h.validate(ctx, report)
	if e != nil {
		safe, cleanErr := h.service.Redactor.Apply(ctx, e.Error(), 2048)
		if cleanErr != nil {
			return nil, cleanErr
		}
		v := raw(map[string]any{"valid": false, "diagnostic": safe.String()})
		n, ee := h.encoded(correlation, v)
		if ee != nil {
			return nil, ee
		}
		if ee = h.charge(ctx, 0, n, true); ee != nil {
			return nil, ee
		}
		return v, nil
	}
	if name == "communication_preflight" {
		v := raw(map[string]any{"valid": true, "state": state, "disposition": report.Disposition})
		n, e := h.encoded(correlation, v)
		if e != nil {
			return nil, e
		}
		if e = h.charge(ctx, 0, n, true); e != nil {
			return nil, e
		}
		return v, nil
	}
	expected := store.LogAnalysisReceipt{AnalysisID: h.a.ID, RequestID: in.RequestID, RequestDigest: identity, State: state, Disposition: report.Disposition, Proposals: []store.LogAnalysisProposalLink{}}
	for _, c := range candidates {
		p := c.Proposal
		expected.Proposals = append(expected.Proposals, store.LogAnalysisProposalLink{CandidateKey: c.Key, ProposalID: p.ProposalID, Version: 1, BaseRevision: p.BaseRevision, ChangeDigest: p.ChangeDigest, ResultDigest: p.ResultDigest})
	}
	n, e := h.encoded(correlation, raw(expected))
	if e != nil {
		return nil, e
	}
	if e = h.charge(ctx, 0, n, true); e != nil {
		return nil, e
	}
	r, e := h.service.DB.FinalizeLogAnalysis(ctx, store.LogAnalysisFinalization{AnalysisID: h.a.ID, Generation: h.a.Generation, RequestID: in.RequestID, RequestDigest: identity, Report: raw(report), State: state, Disposition: report.Disposition, Reason: report.Reason, Candidates: candidates})
	return raw(r), e
}

func objectSchema(properties string, required string) json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` + properties + `},"required":[` + required + `],"additionalProperties":false}`)
}
func (h *invocation) catalog(ctx context.Context) (json.RawMessage, error) {
	b, e := h.reader.Handle(ctx, submission.Request{Method: "tools/list", RequestID: "catalog", Payload: json.RawMessage(`{}`)})
	if e != nil {
		return nil, e
	}
	var list struct {
		Tools []submission.ToolSpec `json:"tools"`
	}
	if e = json.Unmarshal(b, &list); e != nil {
		return nil, e
	}
	list.Tools = append(list.Tools,
		submission.ToolSpec{Name: "standards_categories_list", Description: "Discover the verified retained book only.", InputSchema: objectSchema(`"limit":{"type":"integer","minimum":1,"maximum":100},"cursor":{"type":"string","maxLength":4096}`, ``)},
		submission.ToolSpec{Name: "standards_select", Description: "Record initial or additive relevant existing category IDs; explicit empty is valid.", InputSchema: objectSchema(`"expected_version":{"type":"integer","minimum":0},"category_ids":{"type":"array","maxItems":100,"items":{"type":"string"}},"reason":{"type":"string","maxLength":512}`, `"expected_version","category_ids","reason"`)},
		submission.ToolSpec{Name: "standards_read", Description: "Retrieve selected retained bodies progressively; acknowledge only after all bytes were offered.", InputSchema: objectSchema(`"selection_version":{"type":"integer","minimum":1},"selection_digest":{"type":"string"},"cursor":{"type":"string","maxLength":4096},"ack":{"type":"boolean"}`, `"selection_version","selection_digest"`)})
	for _, name := range []string{"communication_preflight", "communication_submit"} {
		list.Tools = append(list.Tools, submission.ToolSpec{Name: name, Description: "Validate or atomically retain one bounded report and its unapproved proposals; same request and bytes recover the original receipt.", InputSchema: reportSchema()})
	}
	list.Tools = append(list.Tools, submission.ToolSpec{Name: "communication_receipt", Description: "Recover exact accepted request references before any retry; no report echo.", InputSchema: objectSchema(`"request_id":{"type":"string","maxLength":64,"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$"}`, `"request_id"`)})
	return raw(list), nil
}
func reportSchema() json.RawMessage {
	// The complete closed contract is also shipped with the focused analyst role.
	citation := `{"type":"object","properties":{"v":{"type":"integer"},"task_ref":{"type":"string"},"run_id":{"type":"integer"},"ordinal":{"type":"integer"},"started_at":{"type":"string"},"stream":{"type":"string"},"start":{"type":"integer"},"end":{"type":"integer"},"digest":{"type":"string"},"policy":{"type":"string"},"excerpt_bytes":{"type":"integer"}},"required":["v","task_ref","run_id","ordinal","started_at","stream","start","end","digest","policy","excerpt_bytes"],"additionalProperties":false}`
	standard := `{"type":"object","properties":{"id":{"type":"string"},"title":{"type":"string"},"body":{"type":"string"},"category_ids":{"type":"array","items":{"type":"string"}}},"required":["id","title","body","category_ids"],"additionalProperties":false}`
	change := `{"type":"object","properties":{"kind":{"type":"string","enum":["standard.add","standard.replace"]},"standard":` + standard + `},"required":["kind","standard"],"additionalProperties":false}`
	finding := `{"type":"object","properties":{"key":{"type":"string","maxLength":64,"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$"},"observation":{"type":"string"},"benefit":{"type":"string"},"uncertainty":{"type":"string"},"citations":{"type":"array","maxItems":100,"items":` + citation + `},"change":` + change + `},"required":["key","observation","benefit","uncertainty","citations"],"additionalProperties":false}`
	report := `{"type":"object","properties":{"disposition":{"type":"string","enum":["proposals","no_warranted_change","insufficient_evidence"]},"reason":{"type":"string"},"findings":{"type":"array","maxItems":100,"items":` + finding + `}},"required":["disposition","reason","findings"],"additionalProperties":false}`
	return objectSchema(`"request_id":{"type":"string","maxLength":64,"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$"},"report":`+report, `"request_id","report"`)
}

func (h *invocation) recovery(ctx context.Context, id string, v json.RawMessage) (json.RawMessage, error) {
	n, e := h.encoded(id, v)
	if e != nil {
		return nil, e
	}
	if e = h.service.DB.ReserveLogAnalysisRecoveryBudget(ctx, h.a.ID, h.a.Generation, 1, n); e != nil {
		return nil, e
	}
	return v, nil
}
