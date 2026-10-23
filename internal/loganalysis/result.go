package loganalysis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/projectread"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// maxProposals keeps the book small; the analyst may propose fewer.
const maxProposals = 3

// textLimit bounds each prose field of a report.
const textLimit = 8 << 10

// maxFindings bounds the findings of one report.
const maxFindings = 20

func analystPrompt(ctx context.Context, scope Scope) (string, error) {
	b, e := instructionpackage.LogAnalystInstructions(ctx)
	if e != nil {
		return "", e
	}
	data := map[string]any{"request": scope.Request, "repositories": scope.Repositories, "upstream": scope.Upstream}
	p := string(b) + "\nFactory-owned immutable request (data):\n" + string(raw(data)) + "\nReturn through the scoped communication tools only. Stop when any budget is exhausted."
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
	fields := []*string{&r.Summary, &r.NoChangeReason}
	list := func(values []string) {
		for i := range values {
			fields = append(fields, &values[i])
		}
	}
	for i := range r.Proposals {
		p := &r.Proposals[i]
		if c := p.NewCategory; c != nil {
			fields = append(fields, &c.ID, &c.Name, &c.Description)
		}
		if s := p.Operation.Standard; s != nil {
			fields = append(fields, &s.ID, &s.CategoryID, &s.Rule, &s.Description, &s.Evidence, &s.Scope)
			list(s.AppliesTo)
		}
		j := &p.Justification
		fields = append(fields, &j.Cost, &j.WhyItPrevents, &j.LoadAfter)
		list(j.IncidentIDs)
		list(j.Tasks)
	}
	for i := range r.Findings {
		f := &r.Findings[i]
		fields = append(fields, &f.Key, &f.Repository, &f.Target, &f.Title, &f.Expected, &f.Actual, &f.Cause, &f.Suggestion, &f.Existing)
		list(f.IncidentIDs)
	}
	for _, s := range fields {
		b, e := h.service.Redactor.Apply(ctx, *s, 128<<10)
		if e != nil {
			return r, e
		}
		*s = b.String()
	}
	return r, nil
}

// validate checks evidence integrity and the standard's structure only. What the
// analyst concludes, and how much it read first, is its own judgement.
func (h *invocation) validate(ctx context.Context, r Report) ([]store.LogAnalysisCandidate, error) {
	if strings.TrimSpace(r.Summary) == "" || len(r.Summary) > textLimit || len(r.NoChangeReason) > textLimit {
		return nil, errors.New("report needs a bounded summary")
	}
	limit := min(maxProposals, h.q.Limits.Proposals)
	if len(r.Proposals) > limit {
		return nil, fmt.Errorf("at most %d proposals", limit)
	}
	if len(r.Proposals) == 0 && strings.TrimSpace(r.NoChangeReason) == "" {
		return nil, errors.New("no_change_reason is required when there are no proposals")
	}
	if len(r.Findings) > maxFindings {
		return nil, fmt.Errorf("at most %d findings", maxFindings)
	}
	rows, e := allEvidence(ctx, h.service.DB, h.a.ID)
	if e != nil {
		return nil, e
	}
	offeredCitations := map[string]bool{}
	offeredIncidents := map[string]bool{}
	for _, row := range rows {
		var v Evidence
		if e = json.Unmarshal(row.Metadata, &v); e != nil {
			return nil, e
		}
		for _, c := range v.Citations {
			offeredCitations[digest(c)] = true
		}
		for _, id := range v.Incidents {
			offeredIncidents[id] = true
		}
	}
	incidents := func(where string, ids []string) error {
		if len(ids) == 0 || len(ids) > 200 {
			return fmt.Errorf("%s needs incident_ids", where)
		}
		for _, id := range ids {
			if !offeredIncidents[id] {
				return fmt.Errorf("%s: incident %q was not offered by incidents_list", where, id)
			}
		}
		return nil
	}
	cited := func(where string, n int, citations func(int) string) error {
		if n > 100 {
			return fmt.Errorf("%s: at most 100 citations", where)
		}
		for i := range n {
			if !offeredCitations[citations(i)] {
				return fmt.Errorf("%s: citation %d is not an offered excerpt", where, i+1)
			}
		}
		return nil
	}
	keys := map[string]bool{}
	filed := 0
	for i, f := range r.Findings {
		where := fmt.Sprintf("finding %d", i+1)
		if !candidateKey.MatchString(f.Key) || keys[f.Key] {
			return nil, fmt.Errorf("%s needs a unique key of letters, digits, - or _", where)
		}
		keys[f.Key] = true
		if f.Kind != FindingProjectSetup && f.Kind != FindingFactoryDefect && f.Kind != FindingRolePrompt {
			return nil, fmt.Errorf("%s: kind is project_setup, factory_defect or role_prompt", where)
		}
		if strings.TrimSpace(f.Target) == "" || len(f.Target) > 512 || strings.TrimSpace(f.Title) == "" || len(f.Title) > 200 || strings.ContainsAny(f.Title, "\r\n") || len(f.Existing) > 512 {
			return nil, fmt.Errorf("%s needs a target and a one-line title of at most 200 bytes", where)
		}
		for _, text := range []string{f.Expected, f.Actual, f.Cause, f.Suggestion} {
			if strings.TrimSpace(text) == "" || len(text) > textLimit {
				return nil, fmt.Errorf("%s needs bounded expected, actual, cause and suggestion", where)
			}
		}
		if e = incidents(where, f.IncidentIDs); e != nil {
			return nil, e
		}
		if e = cited(where, len(f.Citations), func(i int) string { return digest(f.Citations[i]) }); e != nil {
			return nil, e
		}
		repo, e := h.findingRepository(f)
		if e != nil {
			return nil, fmt.Errorf("%s: %w", where, e)
		}
		if !f.File {
			continue
		}
		if f.Existing != "" {
			return nil, fmt.Errorf("%s: file is not allowed when existing names an issue that already covers it", where)
		}
		if f.Kind == FindingRolePrompt && repo.TeamOwned {
			return nil, fmt.Errorf("%s: a role of the user-owned team %s is a suggested change, never filed", where, repo.Team)
		}
		if filed++; filed > maxFiled {
			return nil, fmt.Errorf("at most %d findings may be filed", maxFiled)
		}
	}
	if len(r.Proposals) == 0 {
		return []store.LogAnalysisCandidate{}, nil
	}
	base, snap, e := h.base(ctx)
	if e != nil {
		return nil, e
	}
	targets := map[string]bool{}
	candidates := []store.LogAnalysisCandidate{}
	for i, p := range r.Proposals {
		where := fmt.Sprintf("proposal %d", i+1)
		op := p.Operation
		if (op.Kind != "standard.add" && op.Kind != "standard.replace") || op.Standard == nil || op.Category != nil || op.ID != "" {
			return nil, fmt.Errorf("%s: operation is standard.add or standard.replace with a standard", where)
		}
		if targets[op.Standard.ID] {
			return nil, fmt.Errorf("%s repeats standard %s", where, op.Standard.ID)
		}
		targets[op.Standard.ID] = true
		j := p.Justification
		if len(j.Tasks) > 200 || len(j.Cost) > textLimit || len(j.WhyItPrevents) > textLimit || len(j.LoadAfter) > textLimit {
			return nil, fmt.Errorf("%s: justification exceeds its bounds", where)
		}
		if e = incidents(where, j.IncidentIDs); e != nil {
			return nil, e
		}
		if e = cited(where, len(p.Citations), func(i int) string { return digest(p.Citations[i]) }); e != nil {
			return nil, e
		}
		ops := []standards.Operation{}
		if p.NewCategory != nil {
			ops = append(ops, standards.Operation{Kind: "category.add", Category: p.NewCategory})
		}
		ops = append(ops, op)
		key := fmt.Sprintf("proposal-%d", i+1)
		prepared, e := standards.PrepareProposal("analysis-"+h.a.ID+"-"+key, base, snap, ops, "log-analysis:"+h.a.ID)
		if e != nil {
			return nil, fmt.Errorf("%s: %w", where, e)
		}
		candidates = append(candidates, store.LogAnalysisCandidate{Key: key, Proposal: prepared})
	}
	return candidates, nil
}

// base is the book this analysis bound at start. Before the first publication
// that is the empty book; a bound book that fails verification proposes nothing.
func (h *invocation) base(ctx context.Context) (int64, standards.Snapshot, error) {
	var snap standards.Snapshot
	binding, e := h.service.DB.GetAnalystStandardsBinding(ctx, h.a.ID)
	if e != nil {
		return 0, snap, e
	}
	if binding.Outcome == "verified" && len(binding.SnapshotBytes) > 0 {
		e = json.Unmarshal(binding.SnapshotBytes, &snap)
		return binding.RevisionID, snap, e
	}
	head, e := h.service.DB.StandardsHead(ctx)
	if e != nil {
		return 0, snap, e
	}
	if binding.RevisionID != 0 || head != 0 {
		return 0, snap, errors.New("the standards book bound for this analysis is unavailable; proposals cannot be prepared")
	}
	return 0, snap, nil
}

func disposition(r Report) string {
	if len(r.Proposals) > 0 {
		return "proposals"
	}
	return "no_warranted_change"
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
	candidates, e := h.validate(ctx, report)
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
		v := raw(map[string]any{"valid": true, "disposition": disposition(report)})
		n, e := h.encoded(correlation, v)
		if e != nil {
			return nil, e
		}
		if e = h.charge(ctx, 0, n, true); e != nil {
			return nil, e
		}
		return v, nil
	}
	expected := store.LogAnalysisReceipt{AnalysisID: h.a.ID, RequestID: in.RequestID, RequestDigest: identity, State: "completed", Disposition: disposition(report), Proposals: []store.LogAnalysisProposalLink{}}
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
	reason := report.Summary
	if len(report.Proposals) == 0 {
		reason = report.NoChangeReason
	}
	r, e := h.service.DB.FinalizeLogAnalysis(ctx, store.LogAnalysisFinalization{AnalysisID: h.a.ID, Generation: h.a.Generation, RequestID: in.RequestID, RequestDigest: identity, Report: raw(report), State: "completed", Disposition: disposition(report), Reason: reason, Candidates: candidates})
	return raw(r), e
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
		submission.ToolSpec{Name: "standards_categories_list", Description: "List the categories of the book bound for this analysis, with the standard count per category and the load per role.", InputSchema: projectread.ObjectSchema(`"limit":{"type":"integer","minimum":1,"maximum":100},"cursor":{"type":"string","maxLength":4096}`, ``)},
		submission.ToolSpec{Name: "standards_select", Description: "Choose the category IDs whose standards you want to read; an empty choice is valid.", InputSchema: projectread.ObjectSchema(`"expected_version":{"type":"integer","minimum":0},"category_ids":{"type":"array","maxItems":100,"items":{"type":"string"}},"reason":{"type":"string","maxLength":512}`, `"expected_version","category_ids","reason"`)},
		submission.ToolSpec{Name: "standards_read", Description: "Read the selected standards page by page; acknowledge with ack:true after the last page.", InputSchema: projectread.ObjectSchema(`"selection_version":{"type":"integer","minimum":1},"selection_digest":{"type":"string"},"cursor":{"type":"string","maxLength":4096},"ack":{"type":"boolean"}`, `"selection_version","selection_digest"`)})
	list.Tools = append(list.Tools, checkTools()...)
	for _, name := range []string{"communication_preflight", "communication_submit"} {
		description := "Check a report without storing it."
		if name == "communication_submit" {
			description = "Store the report once; its proposals become unapproved standards proposals for the owner, and the host files the findings marked file. The same request and bytes return the original receipt."
		}
		list.Tools = append(list.Tools, submission.ToolSpec{Name: name, Description: description, InputSchema: reportSchema()})
	}
	list.Tools = append(list.Tools, submission.ToolSpec{Name: "communication_receipt", Description: "Look up the receipt of an earlier submit before retrying.", InputSchema: projectread.ObjectSchema(`"request_id":{"type":"string","maxLength":64,"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$"}`, `"request_id"`)})
	return raw(list), nil
}
func reportSchema() json.RawMessage {
	text := `{"type":"string"}`
	texts := `{"type":"array","items":{"type":"string"}}`
	citation := `{"type":"object","properties":{"v":{"type":"integer"},"task_ref":{"type":"string"},"run_id":{"type":"integer"},"ordinal":{"type":"integer"},"started_at":{"type":"string"},"stream":{"type":"string"},"start":{"type":"integer"},"end":{"type":"integer"},"digest":{"type":"string"},"policy":{"type":"string"},"excerpt_bytes":{"type":"integer"}},"required":["v","task_ref","run_id","ordinal","started_at","stream","start","end","digest","policy","excerpt_bytes"],"additionalProperties":false}`
	citations := `{"type":"array","maxItems":100,"items":` + citation + `}`
	exception := `{"type":"object","properties":{"version":{"type":"integer","enum":[1]},"mode":{"type":"string","enum":["allow-task","deny"]},"all_registered_repositories":{"type":"boolean"},"repository_ids":{"type":"array","items":{"type":"integer"}},"task_refs":` + texts + `,"max_lifetime_seconds":{"type":"integer"}},"required":["version","mode"],"additionalProperties":false}`
	standard := `{"type":"object","properties":{"id":` + text + `,"category_id":` + text + `,"rule":{"type":"string","pattern":"^MUST "},"description":` + text + `,"evidence":` + text + `,"scope":` + text + `,"check":{"type":"string","enum":["automated","semi_automated","manual"]},"exception_policy":` + exception + `,"applies_to":{"type":"array","minItems":1,"items":{"type":"string"}}},"required":["id","category_id","rule","description","evidence","scope","check","applies_to"],"additionalProperties":false}`
	operation := `{"type":"object","properties":{"kind":{"type":"string","enum":["standard.add","standard.replace"]},"standard":` + standard + `},"required":["kind","standard"],"additionalProperties":false}`
	category := `{"type":"object","properties":{"id":` + text + `,"name":` + text + `,"description":` + text + `},"required":["id","name","description"],"additionalProperties":false}`
	justification := `{"type":"object","properties":{"incident_ids":{"type":"array","minItems":1,"items":{"type":"string"}},"tasks":` + texts + `,"cost":` + text + `,"why_it_prevents":` + text + `,"load_after":` + text + `},"required":["incident_ids","tasks","cost","why_it_prevents","load_after"],"additionalProperties":false}`
	proposal := `{"type":"object","properties":{"new_category":` + category + `,"operation":` + operation + `,"justification":` + justification + `,"citations":` + citations + `},"required":["operation","justification","citations"],"additionalProperties":false}`
	finding := `{"type":"object","properties":{"key":{"type":"string","pattern":"^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$"},"kind":{"type":"string","enum":["project_setup","factory_defect","role_prompt"]},"repository":` + text + `,"target":` + text + `,"title":{"type":"string","maxLength":200},"expected":` + text + `,"actual":` + text + `,"cause":` + text + `,"suggestion":` + text + `,"incident_ids":{"type":"array","minItems":1,"items":{"type":"string"}},"citations":` + citations + `,"existing":` + text + `,"file":{"type":"boolean"}},"required":["key","kind","target","title","expected","actual","cause","suggestion","incident_ids","citations","file"],"additionalProperties":false}`
	report := `{"type":"object","properties":{"summary":` + text + `,"proposals":{"type":"array","maxItems":3,"items":` + proposal + `},"findings":{"type":"array","maxItems":20,"items":` + finding + `},"no_change_reason":` + text + `},"required":["summary","proposals","findings"],"additionalProperties":false}`
	return projectread.ObjectSchema(`"request_id":{"type":"string","maxLength":64,"pattern":"^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$"},"report":`+report, `"request_id","report"`)
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

// findingRepository is the analysed repository a finding concerns: the named
// one, or the only one. Project setup and role prompts need it to be known.
func (h *invocation) findingRepository(f Finding) (Repository, error) {
	if f.Repository == "" && len(h.scope.Repositories) != 1 {
		if f.Kind == FindingFactoryDefect {
			return Repository{}, nil
		}
		return Repository{}, errors.New("name the repository (owner/name) this finding concerns")
	}
	return h.repository(f.Repository)
}
