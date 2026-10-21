package loganalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestNormalizeScopeAndLowerOnlyLimits(t *testing.T) {
	original := Request{RepositoryIDs: []int64{2, 1, 2}, RunIDs: []int64{3, 3}, Limits: Limits{ReportBytes: 128 << 10, Proposals: 10, Calls: 8, Bytes: 20 << 10, Seconds: 3, Evidence: logevidence.Limits{Runs: 1, Records: 2, Rows: 3, Calls: 2, Bytes: 4 << 10}}}
	got, err := normalized(original)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.RepositoryIDs, []int64{1, 2}) || !reflect.DeepEqual(original.RepositoryIDs, []int64{2, 1, 2}) || got.Limits != original.Limits {
		t.Fatalf("normalized request expanded or aliased scope: %+v original=%+v", got, original)
	}
	for _, change := range []func(*Request){func(q *Request) { q.Limits.Calls = 41 }, func(q *Request) { q.Limits.Bytes = 256<<10 + 1 }, func(q *Request) { q.Limits.Seconds = 601 }, func(q *Request) { q.Limits.Evidence.Records = 101 }, func(q *Request) { q.Limits.Evidence.Bytes = 32<<10 + 1 }, func(q *Request) { q.Limits.Calls = 4 }, func(q *Request) { q.Before = time.Now().UTC() }} {
		q := original
		change(&q)
		if _, err := normalized(q); err == nil {
			t.Fatalf("accepted expanded or ambiguous request: %+v", q)
		}
	}
}

type outcomeFixture struct {
	h    *invocation
	runs []int64
}

func newOutcomeFixture(t *testing.T, published, interval bool) *outcomeFixture {
	t.Helper()
	root := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(root, "repo"), Forge: store.ForgeGitHub, Owner: "fixture", Name: "service", RemoteURL: "git@github.com:fixture/service.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(root, "trees")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, 1, "service", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	runs := []int64{}
	for i := range 2 {
		path := filepath.Join(root, fmt.Sprintf("run-%d.jsonl", i))
		run, e := db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: int64(i + 1), StartedAt: time.Now().Add(-time.Minute), LogPath: func(int) string { return path }})
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"The relevant check detected a regression."}]}}`+"\n"), 0o600); e != nil {
			t.Fatal(e)
		}
		if _, e = db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunSucceeded, FinishedAt: time.Now().Add(-time.Second)}); e != nil {
			t.Fatal(e)
		}
		runs = append(runs, run.ID)
	}
	if published {
		s := standards.Service{Store: db, OwnerID: 71}
		p, e := s.Create(t.Context(), "seed", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "quality", Name: "Quality"}}})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.Approve(t.Context(), p.ProposalID, p.Version, p.ChangeDigest); e != nil {
			t.Fatal(e)
		}
		if _, e = s.Publish(t.Context(), p.ProposalID, p.Version, p.ChangeDigest); e != nil {
			t.Fatal(e)
		}
	}
	q := Request{RepositoryIDs: []int64{repo.ID}, RunIDs: []int64{runs[0]}}
	if interval {
		q.RunIDs = nil
		q.After = time.Now().Add(-time.Hour).UTC()
		q.Before = time.Now().UTC()
	}
	q, err = normalized(q)
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.CreateLogAnalysis(t.Context(), store.LogAnalysis{ID: "fixture-analysis", RequestDigest: digest(q), Scope: raw(Scope{Request: q}), Limits: raw(q.Limits), GrantDigest: "fixture-grant"})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{DB: db, Redactor: redact.New(redact.Options{Environ: []string{}}), RunsRoot: root}
	book := service.book(a.ID)
	if _, err = book.Bind(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if published {
		choice, e := book.Select(t.Context(), 0, []string{}, "No existing guidance is relevant to these observations")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = book.Read(t.Context(), choice.Selection.Version, choice.Selection.Digest, "", true); e != nil {
			t.Fatal(e)
		}
	}
	budget, err := logevidence.NewBudget(q.Limits.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := logevidence.New(t.Context(), logevidence.Options{Store: db, RunsRoot: root, Redactor: service.Redactor, Budget: budget, Scope: logevidence.Scope{RepositoryIDs: q.RepositoryIDs, RunIDs: q.RunIDs, StartedAfter: q.After, StartedBefore: q.Before}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	return &outcomeFixture{h: &invocation{catalogVerified: true, service: service, a: a, q: q, reader: reader, book: book}, runs: runs}
}
func (f *outcomeFixture) call(t *testing.T, name string, args any) logevidence.Result {
	t.Helper()
	b, err := f.h.handle(t.Context(), submission.Request{Method: "tools/call", RequestID: fmt.Sprintf("call-%s-%d", name, time.Now().UnixNano()), Payload: raw(map[string]any{"name": name, "arguments": args})})
	if err != nil {
		t.Fatal(err)
	}
	var v logevidence.Result
	if err = json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if v.Status != "ok" {
		t.Fatalf("evidence refused: %s", b)
	}
	return v
}
func reportFor(c logevidence.Citation) Report {
	return Report{Disposition: "no_warranted_change", Reason: "The observed practice already provides the expected benefit in this narrow scope.", Findings: []Finding{{Key: "practice", Observation: "The check found a regression.", Uncertainty: "Only one observed run.", Citations: []logevidence.Citation{c}}}}
}

func TestOutcomeRejectsUnOfferedCitationAndAcceptsVerifiedEmptySelection(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	evidence := f.call(t, "run_logs_read", map[string]any{"run_id": f.runs[0]})
	if len(evidence.Records) != 1 {
		t.Fatal(evidence)
	}
	report := reportFor(evidence.Records[0].Citation)
	if _, state, err := f.h.validate(t.Context(), report); err != nil || state != "completed" {
		t.Fatalf("verified explicit empty selection rejected: state=%s error=%v", state, err)
	}
	report.Findings[0].Citations[0].Digest = strings.Repeat("a", 64)
	if _, _, err := f.h.validate(t.Context(), report); err == nil {
		t.Fatal("accepted a citation never offered by this invocation")
	}
}
func TestOutcomeDegradedStandardsAndUnreadCoverage(t *testing.T) {
	t.Run("degraded book", func(t *testing.T) {
		f := newOutcomeFixture(t, false, false)
		evidence := f.call(t, "run_logs_read", map[string]any{"run_id": f.runs[0]})
		report := reportFor(evidence.Records[0].Citation)
		if _, _, err := f.h.validate(t.Context(), report); err == nil {
			t.Fatal("unavailable standards supported no-change conclusion")
		}
		report.Disposition = "insufficient_evidence"
		report.Reason = "Retained standards are unavailable; observations cannot support a guidance decision."
		if candidates, state, err := f.h.validate(t.Context(), report); err != nil || len(candidates) != 0 || state != "completed" {
			t.Fatalf("degraded observations=%v state=%s error=%v", candidates, state, err)
		}
	})
	t.Run("explicit unread run", func(t *testing.T) {
		f := newOutcomeFixture(t, true, false)
		report := Report{Disposition: "insufficient_evidence", Reason: "The requested run was not examined."}
		if _, state, err := f.h.validate(t.Context(), report); err != nil || state != "incomplete" {
			t.Fatalf("unread scope state=%s error=%v", state, err)
		}
	})
	t.Run("unknown interval remainder", func(t *testing.T) {
		f := newOutcomeFixture(t, true, true)
		page := f.call(t, "runs_list", map[string]any{"limit": 1})
		if page.NextCursor == "" || len(page.Runs) != 1 {
			t.Fatalf("expected bounded discovery continuation: %+v", page)
		}
		evidence := f.call(t, "run_logs_read", map[string]any{"run_id": page.Runs[0].RunID})
		report := reportFor(evidence.Records[0].Citation)
		report.Disposition = "insufficient_evidence"
		report.Reason = "Discovery stopped with an unknown remainder."
		if _, state, err := f.h.validate(t.Context(), report); err != nil || state != "incomplete" {
			t.Fatalf("unknown remainder state=%s error=%v", state, err)
		}
	})
}
func TestOuterBudgetCountsEncodedEnvelopeAndPreservesFinalReserve(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	payload := raw(map[string]string{"body": strings.Repeat("x", 1024)})
	n, err := f.h.encoded("response", payload)
	if err != nil {
		t.Fatal(err)
	}
	if n < 2*len(payload) {
		t.Fatalf("encoded text and structured copies not counted: body=%d encoded=%d", len(payload), n)
	}
	dataCalls, dataBytes := f.h.q.Limits.Calls-4, f.h.q.Limits.Bytes-(8<<10)
	if err = f.h.charge(t.Context(), dataCalls, dataBytes, false); err != nil {
		t.Fatal(err)
	}
	if err = f.h.charge(t.Context(), 1, 0, false); !errors.Is(err, store.ErrLogAnalysisBudget) {
		t.Fatalf("data calls consumed final reserve: %v", err)
	}
	if err = f.h.charge(t.Context(), 0, 1, false); !errors.Is(err, store.ErrLogAnalysisBudget) {
		t.Fatalf("data bytes consumed final reserve: %v", err)
	}
	if err = f.h.charge(t.Context(), 4, 8<<10, true); err != nil {
		t.Fatalf("final reserve unavailable: %v", err)
	}
	if err = f.h.charge(t.Context(), 1, 0, true); !errors.Is(err, store.ErrLogAnalysisBudget) {
		t.Fatalf("whole-analysis allowance expanded: %v", err)
	}
}

func TestMalformedCommunicationChargesEncodedRefusal(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	before, err := f.h.service.DB.GetLogAnalysis(t.Context(), f.h.a.ID)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := f.h.handle(t.Context(), submission.Request{Method: "tools/call", RequestID: "malformed", Payload: raw(map[string]any{"name": "communication_submit", "arguments": map[string]any{"request_id": 17, "report": false}})})
	if err != nil {
		t.Fatal(err)
	}
	var refused struct {
		Error string `json:"error"`
	}
	if err = json.Unmarshal(reply, &refused); err != nil || refused.Error == "" {
		t.Fatalf("expected bounded refusal: %s error=%v", reply, err)
	}
	after, err := f.h.service.DB.GetLogAnalysis(t.Context(), f.h.a.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := f.h.encoded("malformed", reply)
	if err != nil {
		t.Fatal(err)
	}
	if after.Calls <= before.Calls || after.OutboundBytes-before.OutboundBytes != encoded {
		t.Fatalf("malformed call escaped accounting: before=%+v after=%+v encoded=%d", before, after, encoded)
	}
}

func TestWithheldStandardsResponseCannotSupportProposal(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	evidence := f.call(t, "run_logs_read", map[string]any{"run_id": f.runs[0]})
	status, err := f.h.book.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	saved, err := f.h.service.DB.GetLogAnalysis(t.Context(), f.h.a.ID)
	if err != nil {
		t.Fatal(err)
	}
	remaining := (f.h.q.Limits.Bytes - (8 << 10)) - saved.OutboundBytes
	if err = f.h.charge(t.Context(), 0, remaining, false); err != nil {
		t.Fatal(err)
	}
	_, err = f.h.handle(t.Context(), submission.Request{Method: "tools/call", RequestID: "withheld-standards", Payload: raw(map[string]any{"name": "standards_read", "arguments": map[string]any{"selection_version": status.Selection.Version, "selection_digest": status.Selection.Digest, "ack": true}})})
	if err != nil {
		t.Fatal(err)
	}
	status, err = f.h.book.Status(t.Context())
	if err != nil || status.RetrievalStatus != "retrieved" {
		t.Fatalf("fixture acknowledgment not persisted: %+v error=%v", status, err)
	}
	report := reportFor(evidence.Records[0].Citation)
	report.Disposition = "proposals"
	report.Findings[0].Benefit = "Earlier relevant checks can catch regressions."
	report.Findings[0].Change = &standards.Operation{Kind: "standard.add", Standard: &standards.Standard{ID: "checks", Title: "Relevant checks", Body: "Run relevant checks before review.", CategoryIDs: []string{"quality"}}}
	if _, _, err = f.h.validate(t.Context(), report); err == nil || !strings.Contains(err.Error(), "unread selected standards") {
		t.Fatalf("withheld delivery supported proposal: %v", err)
	}
	links, err := f.h.service.DB.ListLogAnalysisProposalLinks(t.Context(), f.h.a.ID)
	if err != nil || len(links) != 0 {
		t.Fatalf("withheld delivery created proposals: %+v error=%v", links, err)
	}
}

func TestCitationRereadCannotSatisfyWholeRunCoverage(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	// Obtain an authentic locator without offering a whole-run page through this
	// analyst invocation's host ledger.
	data, err := f.h.reader.Handle(t.Context(), submission.Request{Method: "tools/call", RequestID: "locator", Payload: raw(map[string]any{"name": "run_logs_read", "arguments": map[string]any{"run_id": f.runs[0]}})})
	if err != nil {
		t.Fatal(err)
	}
	var source logevidence.Result
	if err = json.Unmarshal(data, &source); err != nil || len(source.Records) != 1 {
		t.Fatalf("locator=%s error=%v", data, err)
	}
	cited := f.call(t, "run_logs_read", map[string]any{"run_id": f.runs[0], "citation": source.Records[0].Citation})
	report := reportFor(cited.Records[0].Citation)
	report.Disposition = "insufficient_evidence"
	report.Reason = "A citation reread does not examine the complete requested run."
	if _, state, err := f.h.validate(t.Context(), report); err != nil || state != "incomplete" {
		t.Fatalf("citation-only pass state=%s error=%v", state, err)
	}
	report.Disposition = "no_warranted_change"
	if _, _, err = f.h.validate(t.Context(), report); err == nil {
		t.Fatal("citation-only page supported a complete no-change conclusion")
	}
}

type refusalRunner struct{ calls int }

func (r *refusalRunner) LogAnalystIsolation(context.Context) (string, error) {
	r.calls++
	return "fixture", nil
}
func (r *refusalRunner) RunLogAnalyst(context.Context, agent.LogAnalystRequest) (agent.LogAnalystRunResult, error) {
	r.calls++
	return agent.LogAnalystRunResult{}, errors.New("unexpected launch")
}

func TestUnsupportedStartRetainsHostFailureAndStartKey(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("disabled-MCP-%t", disabled), func(t *testing.T) {
			f := newOutcomeFixture(t, true, false)
			s := f.h.service
			s.DisableMCP = disabled
			observer := &refusalRunner{}
			if disabled {
				s.Runner = observer
			}
			q := f.h.q
			q.StartKey = "refused-start"
			before, err := s.DB.ListLogAnalyses(t.Context(), "", 100)
			if err != nil {
				t.Fatal(err)
			}
			first, err := s.Start(t.Context(), q)
			if err == nil || first.ID == "" || first.State != "failed" || first.Disposition != "not_evaluated" {
				t.Fatalf("refused invocation=%+v error=%v", first, err)
			}
			reportRaw, err := s.DB.GetLogAnalysisReport(t.Context(), first.ID)
			if err != nil {
				t.Fatal(err)
			}
			var report Report
			if err = json.Unmarshal(reportRaw, &report); err != nil || report.Disposition != "not_evaluated" || report.Reason == "" || len(report.Findings) != 0 {
				t.Fatalf("host report=%s error=%v", reportRaw, err)
			}
			second, err := s.Start(t.Context(), q)
			if err != nil || second.ID != first.ID || second.State != "failed" {
				t.Fatalf("identical retry changed failed invocation: %+v error=%v", second, err)
			}
			after, err := s.DB.ListLogAnalyses(t.Context(), "", 100)
			if err != nil || len(after) != len(before)+1 {
				t.Fatalf("retry allocated another invocation: before=%d after=%d error=%v", len(before), len(after), err)
			}
			if observer.calls != 0 {
				t.Fatalf("unsupported start reached provider %d times", observer.calls)
			}
			links, err := s.DB.ListLogAnalysisProposalLinks(t.Context(), first.ID)
			if err != nil || len(links) != 0 {
				t.Fatalf("host failure created proposals: %+v error=%v", links, err)
			}
		})
	}
}

func TestWithheldEvidenceRetainsCoverageWithoutAuthorizingCitations(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	dataBytes := f.h.q.Limits.Bytes - (8 << 10)
	if err := f.h.charge(t.Context(), 0, dataBytes, false); err != nil {
		t.Fatal(err)
	}
	f.callRefusedEvidence(t)
	rows, err := f.h.service.DB.ListLogAnalysisEvidence(t.Context(), f.h.a.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		var evidence Evidence
		if err = json.Unmarshal(row.Metadata, &evidence); err != nil {
			t.Fatal(err)
		}
		if evidence.Tool != "run_logs_read" || evidence.Status != "delivery_withheld" {
			continue
		}
		found = true
		var selector logevidence.ReadRequest
		if err = json.Unmarshal(evidence.Selector, &selector); err != nil {
			t.Fatal(err)
		}
		if selector.RunID != f.runs[0] || evidence.Coverage.Fingerprint == "" || !evidence.Coverage.Incomplete || len(evidence.Citations) != 0 || len(evidence.Events) != 0 {
			t.Fatalf("withheld source metadata lost coverage or authorized undelivered evidence: %+v", evidence)
		}
	}
	if !found {
		t.Fatal("withheld evidence was omitted from the coverage ledger")
	}
}

func (f *outcomeFixture) callRefusedEvidence(t *testing.T) {
	t.Helper()
	v, err := f.h.handle(t.Context(), submission.Request{Method: "tools/call", RequestID: "withheld-log", Payload: raw(map[string]any{"name": "run_logs_read", "arguments": map[string]any{"run_id": f.runs[0]}})})
	if err != nil {
		t.Fatal(err)
	}
	var refusal struct {
		Error string `json:"error"`
	}
	if err = json.Unmarshal(v, &refusal); err != nil || refusal.Error == "" {
		t.Fatalf("expected withheld evidence refusal: %s error=%v", v, err)
	}
}

func TestEvidenceSelectorsDiscardUnknownFieldsAndRedactKnownText(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	unknown := raw(map[string]any{"run_id": f.runs[0], "extra": "/private/should-not-be-retained"})
	selector, err := f.h.evidenceSelector(t.Context(), "run_logs_read", unknown)
	if err == nil || strings.Contains(string(selector), "/private/should-not-be-retained") {
		t.Fatalf("unknown model fields accepted or retained: %s error=%v", selector, err)
	}
	const token = "ghp_AbCdEf0123456789AbCdEf0123456789"
	selector, err = f.h.evidenceSelector(t.Context(), "runs_list", raw(map[string]string{"task_ref": token}))
	if err != nil || strings.Contains(string(selector), token) {
		t.Fatalf("known secret retained in selector: %s error=%v", selector, err)
	}
	var request logevidence.ListRequest
	if err = json.Unmarshal(selector, &request); err != nil || request.TaskRef == "" {
		t.Fatalf("sanitized narrowing selector lost its structure: %s error=%v", selector, err)
	}
}

func TestReaderFailureRetainsGapAfterEarlierCompleteRead(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	page := f.call(t, "run_logs_read", map[string]any{"run_id": f.runs[0]})
	if err := f.h.reader.Close(); err != nil {
		t.Fatal(err)
	}
	f.callRefusedEvidence(t)
	report := reportFor(page.Records[0].Citation)
	report.Disposition = "insufficient_evidence"
	if _, state, err := f.h.validate(t.Context(), report); err != nil || state != "incomplete" {
		t.Fatalf("late reader failure erased host coverage gap: state=%s error=%v", state, err)
	}
}

func TestOuterBudgetExhaustionDoesNotResumeWithSmallerRequest(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	if err := f.h.charge(t.Context(), 0, f.h.q.Limits.Bytes, false); !errors.Is(err, store.ErrLogAnalysisBudget) {
		t.Fatalf("oversized response accepted: %v", err)
	}
	if err := f.h.charge(t.Context(), 1, 1, false); !errors.Is(err, store.ErrLogAnalysisBudget) {
		t.Fatalf("smaller data request restarted exhausted analysis: %v", err)
	}
	if err := f.h.charge(t.Context(), 1, 1, true); err != nil {
		t.Fatalf("finalization reserve unavailable after exhaustion: %v", err)
	}
}

func TestEvidenceSelectorOmitsCallerCursorAndCitationProse(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	const privatePath = "/private/operator/secret-session.jsonl"
	const prose = "arbitrary caller prose that is not authorized log evidence"
	citation := logevidence.Citation{Version: 1, RunID: f.runs[0], TaskRef: privatePath, Stream: prose, Digest: privatePath, Policy: prose}
	for _, name := range []string{"runs_list", "run_logs_read"} {
		t.Run(name, func(t *testing.T) {
			var args any = map[string]any{"cursor": privatePath + prose, "task_ref": privatePath + " " + prose}
			if name == "run_logs_read" {
				args = map[string]any{"run_id": f.runs[0], "cursor": privatePath + prose, "citation": citation}
			}
			selector, err := f.h.evidenceSelector(t.Context(), name, raw(args))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(selector), privatePath) || strings.Contains(string(selector), prose) {
				t.Fatalf("caller-controlled prose retained: %s", selector)
			}
			var fields map[string]json.RawMessage
			if err = json.Unmarshal(selector, &fields); err != nil {
				t.Fatal(err)
			}
			if _, exists := fields["cursor"]; exists {
				t.Fatalf("raw cursor field retained: %s", selector)
			}
			if len(fields["cursor_digest"]) == 0 {
				t.Fatalf("cursor correlation digest missing: %s", selector)
			}
			if name == "run_logs_read" && string(fields["citation"]) != "{}" {
				t.Fatalf("caller citation retained instead of presence marker: %s", selector)
			}
			if name == "runs_list" && string(fields["task_ref"]) != `"[invalid task selector]"` {
				t.Fatalf("invalid task selector not canonicalized: %s", selector)
			}
		})
	}
}

func TestCatalogVerificationGatesDataAndSubmission(t *testing.T) {
	f := newOutcomeFixture(t, true, false)
	f.h.catalogVerified = false
	catalog, err := f.h.handle(t.Context(), submission.Request{Method: "tools/list", RequestID: "initial-catalog", Payload: raw(map[string]any{})})
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Tools []submission.ToolSpec `json:"tools"`
	}
	if err = json.Unmarshal(catalog, &list); err != nil || len(list.Tools) != 8 {
		t.Fatalf("initial catalog unavailable: %s error=%v", catalog, err)
	}
	for _, name := range []string{"run_logs_read", "communication_submit"} {
		args := any(map[string]any{"run_id": f.runs[0]})
		if name == "communication_submit" {
			args = map[string]any{"request_id": "ungated-submit", "report": Report{Disposition: "insufficient_evidence", Reason: "No authorized delivery occurred."}}
		}
		reply, callErr := f.h.handle(t.Context(), submission.Request{Method: "tools/call", RequestID: "ungated-" + name, Payload: raw(map[string]any{"name": name, "arguments": args})})
		var refused struct {
			Error string `json:"error"`
		}
		if callErr != nil || json.Unmarshal(reply, &refused) != nil || refused.Error == "" {
			t.Fatalf("unverified catalog allowed %s: %s error=%v", name, reply, callErr)
		}
	}
	rows, err := f.h.service.DB.ListLogAnalysisEvidence(t.Context(), f.h.a.ID, 0, 100)
	if err != nil || len(rows) != 0 {
		t.Fatalf("unverified data was offered: %+v error=%v", rows, err)
	}
	if _, err = f.h.service.DB.GetLogAnalysisReport(t.Context(), f.h.a.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unverified submit retained a report: %v", err)
	}
	if err = f.h.verifyCatalog(t.Context(), "controlled-fixture-v1"); err != nil {
		t.Fatal(err)
	}
	page := f.call(t, "run_logs_read", map[string]any{"run_id": f.runs[0]})
	if len(page.Records) != 1 {
		t.Fatalf("verified catalog did not open data gate: %+v", page)
	}
}
