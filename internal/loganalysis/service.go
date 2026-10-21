package loganalysis

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/mcpadapter"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func digest(v any) string       { h := sha256.Sum256(raw(v)); return hex.EncodeToString(h[:]) }
func normalized(q Request) (Request, error) {
	q.RepositoryIDs = append([]int64(nil), q.RepositoryIDs...)
	q.RunIDs = append([]int64(nil), q.RunIDs...)
	q.TaskRefs = append([]string(nil), q.TaskRefs...)
	slices.Sort(q.RepositoryIDs)
	q.RepositoryIDs = slices.Compact(q.RepositoryIDs)
	slices.Sort(q.RunIDs)
	q.RunIDs = slices.Compact(q.RunIDs)
	slices.Sort(q.TaskRefs)
	q.TaskRefs = slices.Compact(q.TaskRefs)
	if len(q.RepositoryIDs) == 0 || len(q.RepositoryIDs) > 50 || q.RepositoryIDs[0] < 1 || len(q.RunIDs) > 5 || len(q.RunIDs) > 0 && q.RunIDs[0] < 1 || len(q.TaskRefs) > 50 || len(q.StartKey) > 128 {
		return q, errors.New("invalid explicit selection")
	}
	interval := !q.After.IsZero() || !q.Before.IsZero()
	if (len(q.RunIDs) > 0) == interval || interval && (q.After.IsZero() || q.Before.IsZero() || q.After.After(q.Before) || q.Before.After(time.Now()) || q.After.Location() != time.UTC || q.Before.Location() != time.UTC) {
		return q, errors.New("choose up to five run IDs or a closed UTC interval")
	}
	for _, v := range q.TaskRefs {
		if _, e := naming.ParseRef(v); e != nil || len(v) > 512 {
			return q, errors.New("invalid task filter")
		}
	}
	if q.Limits.ReportBytes == 0 {
		q.Limits.ReportBytes = 128 << 10
	}
	if q.Limits.Proposals == 0 {
		q.Limits.Proposals = 10
	}
	if q.Limits.ReportBytes < 1 || q.Limits.ReportBytes > 128<<10 || q.Limits.Proposals < 1 || q.Limits.Proposals > 10 {
		return q, errors.New("report and proposal limits may only be lowered")
	}
	if q.Limits.Calls == 0 {
		q.Limits.Calls = 40
	}
	if q.Limits.Bytes == 0 {
		q.Limits.Bytes = 256 << 10
	}
	if q.Limits.Seconds == 0 {
		q.Limits.Seconds = 600
	}
	if q.Limits.Calls < 5 || q.Limits.Calls > 40 || q.Limits.Bytes < 16<<10 || q.Limits.Bytes > 256<<10 || q.Limits.Seconds < 1 || q.Limits.Seconds > 600 {
		return q, errors.New("analysis limits may only lower the finite defaults and must retain finalization reserve")
	}
	defaults := logevidence.Limits{Runs: 5, Records: 100, Rows: 50, Calls: 10, Bytes: 32 << 10}
	for _, p := range []struct {
		v *int
		d int
	}{{&q.Limits.Evidence.Runs, defaults.Runs}, {&q.Limits.Evidence.Records, defaults.Records}, {&q.Limits.Evidence.Rows, defaults.Rows}, {&q.Limits.Evidence.Calls, defaults.Calls}, {&q.Limits.Evidence.Bytes, defaults.Bytes}} {
		if *p.v == 0 {
			*p.v = p.d
		}
	}
	if _, e := logevidence.NewBudget(q.Limits.Evidence); e != nil {
		return q, e
	}
	return q, nil
}

// bookStore preserves the upstream runtime semantics with a standalone root.
type bookStore struct {
	*store.DB
	id string
}

func (b bookStore) GetStandardsRunBinding(ctx context.Context, _ string) (store.StandardsRunBinding, error) {
	return b.GetAnalystStandardsBinding(ctx, b.id)
}
func (b bookStore) SaveStandardsRunBinding(ctx context.Context, v store.StandardsRunBinding) (store.StandardsRunBinding, error) {
	return b.SaveAnalystStandardsBinding(ctx, b.id, v)
}
func (s *Service) book(id string) *standards.Runtime {
	return &standards.Runtime{Store: bookStore{s.DB, id}, RootRunID: "analysis:" + id, ConsumerKey: "log-analyst", AttemptID: "analysis:" + id}
}

// Start persists the invocation first. An identical caller key never relaunches.
func (s *Service) Start(ctx context.Context, q Request) (a store.LogAnalysis, err error) {
	q, err = normalized(q)
	if err != nil {
		return a, err
	}
	if s.DB == nil || s.Redactor == nil {
		return a, errors.New("analysis host unavailable")
	}
	for _, id := range q.RepositoryIDs {
		if _, e := s.DB.GetRepo(ctx, id); e != nil {
			return a, errors.New("repository selection is not registered")
		}
	}
	budget, err := logevidence.NewBudget(q.Limits.Evidence)
	if err != nil {
		return a, err
	}
	live, cancel := context.WithTimeout(ctx, time.Duration(q.Limits.Seconds)*time.Second)
	defer cancel()
	reader, err := logevidence.New(live, logevidence.Options{Store: s.DB, RunsRoot: s.RunsRoot, Scope: logevidence.Scope{RepositoryIDs: q.RepositoryIDs, RunIDs: q.RunIDs, TaskRefs: q.TaskRefs, StartedAfter: q.After, StartedBefore: q.Before}, Redactor: s.Redactor, Budget: budget})
	if err != nil {
		return a, err
	}
	defer func() { err = errors.Join(err, reader.Close()) }()
	cutoff, high, grant := reader.Boundary()
	id := rand.Text()
	a, err = s.DB.CreateLogAnalysis(ctx, store.LogAnalysis{ID: id, StartKey: q.StartKey, RequestDigest: digest(q), Scope: raw(Scope{q, cutoff, high}), Limits: raw(q.Limits), GrantDigest: grant})
	if err != nil {
		return a, err
	}
	if a.ID != id {
		if s.Started != nil {
			err = s.Started(a.ID)
		}
		return a, err
	}
	// Always settle a launched or refused invocation; cleanup uses an independent bounded context.
	defer func() {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer done()
		current, e := s.DB.GetLogAnalysis(cleanup, a.ID)
		if e == nil && current.State == "running" {
			state := "failed"
			reason := "provider exited without an accepted report"
			if err != nil {
				safe, cleanErr := s.Redactor.Apply(cleanup, err.Error(), 512)
				if cleanErr == nil {
					reason = safe.String()
				}
			}
			if live.Err() != nil {
				state = "incomplete"
				reason = "analysis interrupted or wall-time limit reached"
			}
			e = s.DB.InterruptLogAnalysis(cleanup, a.ID, a.Generation, state, reason)
		}
		err = errors.Join(err, e)
		if v, e := s.DB.GetLogAnalysis(cleanup, a.ID); e == nil {
			a = v
		}
	}()
	if s.Started != nil {
		if err = s.Started(a.ID); err != nil {
			return a, err
		}
	}
	book := s.book(a.ID)
	if _, err = book.Bind(live, true); err != nil {
		return a, err
	}
	if s.DisableMCP {
		return a, errors.New("native MCP is disabled; restricted analyst launch refused")
	}
	if s.Runner == nil {
		return a, errors.New("restricted analyst provider unavailable; invocation retained")
	}
	profile, err := s.Runner.LogAnalystIsolation(live)
	if err != nil {
		return a, err
	}
	if err = s.DB.SetLogAnalysisProvenance(live, a.ID, a.Generation, raw(map[string]any{"profile": profile, "model": s.Model, "tools": agent.LogAnalystToolNames()})); err != nil {
		return a, err
	}
	directory, err := os.MkdirTemp("", "lf-la-")
	if err != nil {
		return a, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(directory)) }()
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return a, err
	}
	h := &invocation{service: s, a: a, q: q, reader: reader, book: book, cancel: cancel, profile: profile}
	broker, err := submission.Start(live, submission.Options{Directory: directory, AllowTools: true, AllowResult: true, AttemptReference: a.ID, MaxWorkers: 2, RequestTimeout: 30 * time.Second, Handler: h.handle})
	if err != nil {
		return a, err
	}
	defer func() { err = errors.Join(err, broker.Close()) }()
	descriptor, reference, err := broker.LaunchReference()
	if err != nil {
		return a, err
	}
	launch, err := agent.NewMCPLaunchSpec(s.Executable, descriptor, reference, agent.LogAnalystToolNames()...)
	if err != nil {
		return a, err
	}
	prompt, err := analystPrompt(live, q)
	if err != nil {
		return a, err
	}
	var usageMu sync.Mutex
	usageEvents := []agent.UsageEvent{}
	usageBytes := 0
	capture := &agent.UsageCapture{AttemptID: a.ID, Sink: func(_ context.Context, event agent.UsageEvent) error {
		usageMu.Lock()
		defer usageMu.Unlock()
		n := len(raw(event))
		if usageBytes+n > 8<<10 {
			return errors.New("usage observation retention limit reached")
		}
		usageBytes += n
		usageEvents = append(usageEvents, event.Clone())
		return nil
	}}
	result, runErr := s.Runner.RunLogAnalyst(live, agent.LogAnalystRequest{CatalogVerified: h.verifyCatalog, UsageAttemptID: a.ID, Usage: capture, Attempt: a.ID, Model: s.Model, Prompt: prompt, WorkingDirectory: directory, MCPLaunch: launch})
	// Provider observations are host-owned and survive report fencing.
	h.mu.Lock()
	catalogVerified := h.catalogVerified
	h.mu.Unlock()
	cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer done()
	provenanceErr := s.DB.RecordLogAnalysisProviderResult(cleanup, a.ID, a.Generation, raw(map[string]any{"profile": profile, "provider": result, "usage_events": usageEvents, "catalog_verified": catalogVerified, "tools": agent.LogAnalystToolNames()}))
	// Accepted receipts win even when the provider loses its final response.
	saved, e := s.DB.GetLogAnalysis(cleanup, a.ID)
	if e == nil && saved.State != "running" {
		return saved, provenanceErr
	}
	if runErr == nil {
		runErr = errors.New("analyst exited without an accepted report; inspect retained analysis before starting new work")
	}
	return a, errors.Join(runErr, e, provenanceErr)
}

type invocation struct {
	mu              sync.Mutex
	service         *Service
	a               store.LogAnalysis
	q               Request
	reader          *logevidence.Service
	book            *standards.Runtime
	exhausted       bool
	cancel          context.CancelFunc
	catalogVerified bool
	profile         string
}

// verifyCatalog is invoked only by the provider adapter after native catalog
// validation. Discovery may precede it, but evidence and result tools may not.
func (h *invocation) verifyCatalog(ctx context.Context, version string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.catalogVerified {
		return errors.New("provider catalog already verified")
	}
	safe, e := h.service.Redactor.Apply(ctx, version, 256)
	if e != nil {
		return e
	}
	if safe.String() == "" {
		return errors.New("provider version unavailable")
	}
	if e = h.service.DB.SetLogAnalysisProvenance(ctx, h.a.ID, h.a.Generation, raw(map[string]any{"profile": h.profile, "provider_version": safe.String(), "catalog_verified": true, "tools": agent.LogAnalystToolNames()})); e != nil {
		return e
	}
	h.catalogVerified = true
	return nil
}

func (h *invocation) charge(ctx context.Context, calls, bytes int, final bool) error {
	if h.exhausted && !final {
		return store.ErrLogAnalysisBudget
	}
	maxCalls, maxBytes := h.q.Limits.Calls, h.q.Limits.Bytes
	if !final {
		maxCalls -= 4
		maxBytes -= 8 << 10
	}
	e := h.service.DB.ReserveLogAnalysisBudget(ctx, h.a.ID, h.a.Generation, calls, bytes, maxCalls, maxBytes)
	if errors.Is(e, store.ErrLogAnalysisBudget) && !h.exhausted {
		h.exhausted = true
		_, ledgerErr := h.service.DB.RecordLogAnalysisEvidence(ctx, store.LogAnalysisEvidence{AnalysisID: h.a.ID, Generation: h.a.Generation, CallKey: "host-budget-" + rand.Text(), Metadata: raw(Evidence{Tool: "host_budget", Status: "budget_exhausted", Coverage: logevidence.Coverage{Incomplete: true}})})
		if ledgerErr != nil {
			return errors.Join(e, ledgerErr)
		}
	}
	return e
}
func (h *invocation) encoded(id string, v json.RawMessage) (int, error) {
	b, e := mcpadapter.EncodeToolResult(submission.Response{RequestID: id, Result: v})
	return len(b), e
}
func (h *invocation) handle(ctx context.Context, q submission.Request) (out json.RawMessage, failure error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Refusals are bounded output too. Reserve their encoded delivery even when
	// decoding or authorization failed before domain dispatch. Exhaustion ends the
	// broker instead of emitting an unlimited stream of uncharged errors.
	defer func() {
		if failure == nil {
			return
		}
		v := raw(map[string]string{"error": "Request refused; inspect the retained analysis, arguments and remaining budget."})
		n, e := h.encoded(q.RequestID, v)
		if e == nil {
			e = h.service.DB.ReserveLogAnalysisRecoveryBudget(ctx, h.a.ID, h.a.Generation, 1, n)
		}
		if e != nil {
			if h.cancel != nil {
				h.cancel()
			}
			out = nil
			failure = e
			return
		}
		out = v
		failure = nil
	}()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if q.Method == "tools/list" {
		if e := h.charge(ctx, 1, 0, false); e != nil {
			return nil, e
		}
		v, e := h.catalog(ctx)
		if e != nil {
			return nil, e
		}
		if e = h.charge(ctx, 0, len(v), false); e != nil {
			return nil, e
		}
		return v, nil
	}
	call, e := submission.DecodeToolCall(q.Payload)
	if e != nil {
		return nil, e
	}
	if q.Method != "tools/call" {
		return nil, errors.New("only scoped tools are available")
	}
	if !h.catalogVerified {
		return nil, errors.New("provider tool catalog has not been verified")
	}
	final := strings.HasPrefix(call.Name, "communication_")
	if final {
		return h.communication(ctx, q.RequestID, call.Name, call.Arguments)
	}
	if e = h.charge(ctx, 1, 0, false); e != nil {
		return nil, e
	}
	var v json.RawMessage
	var evidence *Evidence
	switch call.Name {
	case "runs_list", "run_logs_read":
		selector, cleanErr := h.evidenceSelector(ctx, call.Name, call.Arguments)
		if cleanErr != nil {
			return nil, cleanErr
		}
		v, e = h.reader.Handle(ctx, q)
		if e != nil {
			evidence = &Evidence{Tool: call.Name, Selector: selector, Status: "unavailable", Coverage: logevidence.Coverage{Incomplete: true}}
		}
		if e == nil {
			var r logevidence.Result
			if e = json.Unmarshal(v, &r); e == nil {
				meta := Evidence{Tool: call.Name, Selector: selector, Status: r.Status, Runs: r.Runs, Coverage: r.Coverage, Next: r.NextCursor != "", Budget: r.Budget}
				for _, rec := range r.Records {
					meta.Citations = append(meta.Citations, rec.Citation)
					meta.Events = append(meta.Events, OfferedEvent{rec.Citation, rec.Redactions, rec.Truncated})
				}
				evidence = &meta
			}
		}
	case "standards_categories_list":
		var in struct {
			Limit  int    `json:"limit,omitempty"`
			Cursor string `json:"cursor,omitempty"`
		}
		if e = submission.DecodeJSON(call.Arguments, &in); e == nil {
			var out standards.CategoryPage
			out, e = h.book.Categories(ctx, in.Limit, in.Cursor)
			v = raw(out)
		}
	case "standards_select":
		var in struct {
			Expected int64    `json:"expected_version"`
			IDs      []string `json:"category_ids"`
			Reason   string   `json:"reason"`
		}
		if e = submission.DecodeJSON(call.Arguments, &in); e == nil {
			var out standards.RuntimeStatus
			safe, cleanErr := h.service.Redactor.Apply(ctx, in.Reason, 512)
			if cleanErr != nil {
				return nil, cleanErr
			}
			out, e = h.book.Select(ctx, in.Expected, in.IDs, safe.String())
			v = raw(standards.BoundedStatus(out))
		}
	case "standards_read":
		var in struct {
			Version int64  `json:"selection_version"`
			Digest  string `json:"selection_digest"`
			Cursor  string `json:"cursor,omitempty"`
			Ack     bool   `json:"ack,omitempty"`
		}
		if e = submission.DecodeJSON(call.Arguments, &in); e == nil {
			var out standards.ReadPage
			out, e = h.book.Read(ctx, in.Version, in.Digest, in.Cursor, in.Ack)
			v = raw(out)
		}
	default:
		e = errors.New("tool unavailable")
	}
	if e != nil {
		v = raw(map[string]string{"error": "request refused; inspect arguments, coverage and remaining budget"})
	}
	n, encodeErr := h.encoded(q.RequestID, v)
	if encodeErr != nil {
		return nil, encodeErr
	}
	if e = h.charge(ctx, 0, n, false); e != nil {
		if evidence != nil || strings.HasPrefix(call.Name, "standards_") {
			withheld := Evidence{Tool: call.Name, Status: "delivery_withheld", Coverage: logevidence.Coverage{Incomplete: true}}
			if evidence != nil {
				// Keep the attempted selector and source coverage visible, while
				// withholding citations for events the analyst never received.
				withheld.Selector = evidence.Selector
				withheld.Coverage = evidence.Coverage
				withheld.Coverage.Incomplete = true
				withheld.Budget = evidence.Budget
			}
			_, ledgerErr := h.service.DB.RecordLogAnalysisEvidence(ctx, store.LogAnalysisEvidence{AnalysisID: h.a.ID, Generation: h.a.Generation, CallKey: "withheld-" + rand.Text(), Metadata: raw(withheld)})
			e = errors.Join(e, ledgerErr)
		}
		return nil, e
	}
	if evidence != nil {
		if _, e = h.service.DB.RecordLogAnalysisEvidence(ctx, store.LogAnalysisEvidence{AnalysisID: h.a.ID, Generation: h.a.Generation, CallKey: q.RequestID + ":" + rand.Text(), Metadata: raw(evidence)}); e != nil {
			return nil, e
		}
	}
	return v, nil
}

// evidenceSelector retains only the closed reader contract. Invalid arguments
// must not turn the metadata ledger into storage for arbitrary model output.
func (h *invocation) evidenceSelector(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {

	var known any
	cursorDigest := ""
	if name == "runs_list" {
		request := &logevidence.ListRequest{}
		if err := submission.DecodeJSON(args, request); err != nil {
			return nil, err
		}
		if request.Cursor != "" {
			cursorDigest = digest(request.Cursor)
			request.Cursor = ""
		}
		if request.TaskRef != "" {
			if _, err := naming.ParseRef(request.TaskRef); err != nil {
				request.TaskRef = "[invalid task selector]"
			}
		}
		switch request.Status {
		case "", store.RunSucceeded, store.RunFailed, store.RunReported:
		default:
			request.Status = "invalid"
		}
		known = request
	} else {
		request := &logevidence.ReadRequest{}
		if err := submission.DecodeJSON(args, request); err != nil {
			return nil, err
		}
		if request.Cursor != "" {
			cursorDigest = digest(request.Cursor)
			request.Cursor = ""
		}
		// An unverified caller citation is only a request marker. Exact authorized
		// locators are retained separately from the reader's offered response.
		if request.Citation != nil {
			request.Citation = &logevidence.Citation{}
		}
		known = request
	}

	var tree any
	if e := json.Unmarshal(raw(known), &tree); e != nil {
		return nil, e
	}
	var clean func(any) (any, error)
	clean = func(v any) (any, error) {
		switch value := v.(type) {
		case string:
			body, e := h.service.Redactor.Apply(ctx, value, 4096)
			return body.String(), e
		case map[string]any:
			for key, item := range value {
				safe, e := clean(item)
				if e != nil {
					return nil, e
				}
				value[key] = safe
			}
		}
		return v, nil
	}
	safe, e := clean(tree)
	if object, ok := safe.(map[string]any); ok {
		if _, hasCitation := object["citation"]; hasCitation {
			object["citation"] = map[string]any{}
		}
	}
	if object, ok := safe.(map[string]any); ok && cursorDigest != "" {
		object["cursor_digest"] = cursorDigest
	}
	if e != nil {
		return nil, e
	}
	return raw(safe), nil
}

// Inspect pages host evidence while keeping the immutable report and exact links.
func (s *Service) Inspect(ctx context.Context, id string, after int64, limit int) (Inspection, error) {
	var out Inspection
	var e error
	out.Invocation, e = s.DB.GetLogAnalysis(ctx, id)
	if e != nil {
		return out, e
	}
	out.Standards, e = s.book(id).Inspect(ctx)
	if e != nil {
		return out, e
	}
	out.Evidence, e = s.DB.ListLogAnalysisEvidence(ctx, id, after, limit)
	if e != nil {
		return out, e
	}
	if len(out.Evidence) > 0 {
		out.EvidenceNext = out.Evidence[len(out.Evidence)-1].ID
	}
	out.Report, e = s.DB.GetLogAnalysisReport(ctx, id)
	if errors.Is(e, store.ErrNotFound) {
		e = nil
	}
	if e != nil {
		return out, e
	}
	out.Proposals, e = s.DB.ListLogAnalysisProposalLinks(ctx, id)
	return out, e
}

// Reread is a separate bounded pass with fresh owner authorization and source checks.
func (s *Service) Reread(ctx context.Context, id string, c logevidence.Citation) (logevidence.Result, error) {
	a, e := s.DB.GetLogAnalysis(ctx, id)
	if e != nil {
		return logevidence.Result{}, e
	}
	var scope Scope
	if e = json.Unmarshal(a.Scope, &scope); e != nil {
		return logevidence.Result{}, e
	}
	rows, e := s.DB.ListLogAnalysisEvidence(ctx, id, 0, 100)
	if e != nil {
		return logevidence.Result{}, e
	}
	found := false
	for _, row := range rows {
		var evidence Evidence
		if json.Unmarshal(row.Metadata, &evidence) != nil {
			continue
		}
		for _, offered := range evidence.Citations {
			if offered == c {
				found = true
			}
		}
	}
	if !found {
		return logevidence.Result{}, errors.New("citation was not offered by this analysis")
	}
	b, _ := logevidence.NewBudget(logevidence.Limits{})
	r, e := logevidence.New(ctx, logevidence.Options{Store: s.DB, RunsRoot: s.RunsRoot, Redactor: s.Redactor, Budget: b, Scope: logevidence.Scope{RepositoryIDs: scope.Request.RepositoryIDs, RunIDs: []int64{c.RunID}, TaskRefs: scope.Request.TaskRefs, StartedAfter: scope.Request.After, StartedBefore: scope.Request.Before}})
	if e != nil {
		return logevidence.Result{}, e
	}
	defer func() { _ = r.Close() }()
	v, e := r.Handle(ctx, submission.Request{Method: "tools/call", RequestID: "reread", Payload: raw(map[string]any{"name": "run_logs_read", "arguments": logevidence.ReadRequest{RunID: c.RunID, Citation: &c}})})
	var out logevidence.Result
	if e == nil {
		e = json.Unmarshal(v, &out)
	}
	return out, e
}

// NormalizeRequest validates and fills lower-only limits without launching work.
func NormalizeRequest(q Request) (Request, error) { return normalized(q) }
