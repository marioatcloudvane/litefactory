package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/prevention"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// This follows the complete noninteractive CLI path with real persisted storage,
// collection and finalization. Only GitHub and the isolated analyst are fixtures.
func TestPreventionAnalysisMainPath(t *testing.T) {
	paths := newRepoHome(t)
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	book := &standards.Service{Store: db, OwnerID: 71}
	p, err := book.Create(t.Context(), "initial-prevention", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "quality", Name: "Quality"}}, {Kind: "standard.add", Standard: &standards.Standard{ID: "test-checks", Title: "Test checks", Body: "Check relevant behavior.", CategoryIDs: []string{"quality"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = book.Approve(t.Context(), p.ProposalID, p.Version, p.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	published, err := book.Publish(t.Context(), p.ProposalID, p.Version, p.ChangeDigest)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	mutations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			mutations++
			http.Error(w, "no mutation", 405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/api/issues/123":
			fmt.Fprint(w, `{"number":123,"title":"Duplicate delivery","body":"Expected one delivery; observed two on retry.","state":"closed","html_url":"https://github.com/acme/api/issues/123"}`)
		case "/repos/acme/api/issues/124":
			fmt.Fprint(w, `{"number":124,"title":"Unknown timeout","body":"Expected success; observed timeout, no cause known.","state":"open","html_url":"https://github.com/acme/api/issues/124"}`)
		case "/repos/acme/api/issues/123/comments", "/repos/acme/api/issues/124/comments":
			fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	gh, err := github.New(github.Options{APIBase: server.URL, Token: secrets.NewToken("fixture-token"), HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &preventionMainPathRunner{}
	collector := preventionMainPathCollector{Collector: prevention.Collector{GitHub: gh, Local: preventionMainPathLocal{}, Redactor: redact.New(redact.Options{})}, runner: fixture}
	run := func(args ...string) string {
		t.Helper()
		var out, diagnostics bytes.Buffer
		env := &Env{Stdout: &out, Stderr: &diagnostics, Stdin: strings.NewReader(""), Logger: slog.New(slog.DiscardHandler), Version: "test", PreventionService: func(_ context.Context, db *store.DB, _ *config.Config, _ config.Paths) (*prevention.Service, error) {
			return &prevention.Service{DB: db, Source: collector, Runner: fixture, Redactor: redact.New(redact.Options{})}, nil
		}}
		if code := NewRouter("litefactory", env, Commands()).Run(t.Context(), args); code != ExitOK {
			t.Fatalf("%v: exit %d %s output=%s", args, code, &diagnostics, &out)
		}
		return out.String()
	}
	decode := func(raw string, v any) {
		t.Helper()
		if err := json.Unmarshal([]byte(raw), v); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
	}
	var started prevention.Inspection
	decode(run("standards", "analyze", "bugs", "start", "--repo", "acme/api", "--issues", "123,124", "--request-key", "prevention-main-path", "--max-requests", "10", "--json"), &started)
	if started.Analysis.ID == "" || started.Result == nil || len(started.Proposals) != 1 {
		t.Fatalf("missing durable final result: %+v", started)
	}
	listed := run("standards", "analyze", "bugs", "list", "--repo", "acme/api", "--json")
	if !strings.Contains(listed, started.Analysis.ID) {
		t.Fatalf("analysis missing from retained list: %s", listed)
	}
	text := run("standards", "analyze", "bugs", "show", started.Analysis.ID)
	if !strings.Contains(text, "Execution:") || !strings.Contains(text, "unapproved") {
		t.Fatalf("text inspection lacks status/review guidance: %s", text)
	}
	var reopened prevention.Inspection
	decode(run("standards", "analyze", "bugs", "show", started.Analysis.ID, "--json"), &reopened)
	if !reflect.DeepEqual(started.Proposals, reopened.Proposals) || !reflect.DeepEqual(started.Result, reopened.Result) {
		t.Fatalf("reopened analysis changed accepted result")
	}
	if len(reopened.Manifest.Evidence) < 3 || !reopened.Manifest.Frozen {
		t.Fatalf("missing retained frozen evidence: %+v", reopened.Manifest)
	}
	if len(reopened.Result.Findings) != 2 || reopened.Result.Findings[0].Outcome != "proposed" || reopened.Result.Findings[0].CauseBasis != "recorded" || reopened.Result.Findings[0].PreventionRationale == "" || len(reopened.Result.Findings[0].CauseEvidence) != 1 || reopened.Result.Findings[1].Outcome != "insufficient_evidence" || len(reopened.Result.Findings[1].MissingEvidence) == 0 {
		t.Fatalf("retained findings lost causal rationale or insufficiency: %+v", reopened.Result)
	}
	retainedCause := false
	for _, e := range reopened.Manifest.Evidence {
		if e.ID == reopened.Result.Findings[0].CauseEvidence[0] && e.Kind == "diagnosis" && e.ReviewStatus == "reviewed" && e.Revision == "cause-revision-1" {
			retainedCause = true
		}
	}
	if !retainedCause {
		t.Fatal("cause citation does not resolve to exact retained reviewed diagnosis")
	}
	var evidence prevention.Manifest
	decode(run("standards", "analyze", "bugs", "evidence", started.Analysis.ID, "--json"), &evidence)
	if !reflect.DeepEqual(evidence, reopened.Manifest) {
		t.Fatal("evidence inspection lost observations")
	}
	result := run("standards", "analyze", "bugs", "result", started.Analysis.ID, "--json")
	for _, text := range []string{"insufficient_evidence", "idempotency", "cause", "Missing root-cause evidence"} {
		if !strings.Contains(result, text) {
			t.Fatalf("result missing %q: %s", text, result)
		}
	}
	proposal := reopened.Proposals[0]
	if proposal.Version != 1 || proposal.BaseRevision != published.ID || proposal.ChangeDigest == "" || proposal.ResultDigest == "" {
		t.Fatalf("inexact proposal reference: %+v", proposal)
	}
	db, err = store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.GetStandardsApproval(t.Context(), proposal.ProposalID, proposal.Version); !errors.Is(err, store.ErrStandardsApprovalRequired) {
		t.Fatalf("analysis created approval: %v", err)
	}
	current, err := db.GetStandardsRevision(t.Context(), 0)
	if err != nil || current.ID != published.ID {
		t.Fatalf("analysis published guidance: %+v %v", current, err)
	}
	var retried prevention.Inspection
	decode(run("standards", "analyze", "bugs", "start", "--repo", "acme/api", "--issues", "123,124", "--request-key", "prevention-main-path", "--max-requests", "10", "--json"), &retried)
	if retried.Analysis.ID != started.Analysis.ID || !reflect.DeepEqual(retried.Proposals, reopened.Proposals) || fixture.launches != 1 {
		t.Fatalf("retry reanalyzed or changed proposal identities")
	}
	if mutations != 0 {
		t.Fatal("analysis mutated remote source")
	}
}

type preventionMainPathLocal struct{}

func (preventionMainPathLocal) ReadBugEvidence(_ context.Context, _ string, issue, maxBytes int) ([]prevention.LocalObservation, error) {
	if issue != 123 {
		return nil, nil
	}
	return []prevention.LocalObservation{{Locator: "diagnosis:123", Revision: "cause-revision-1", ReviewStatus: "reviewed", Body: "Recorded cause: retries lacked an idempotency guard. A replay reproduces duplicate delivery; fix adds a durable idempotency key."}}, nil
}

type preventionMainPathCollector struct {
	prevention.Collector
	runner *preventionMainPathRunner
}

func (c preventionMainPathCollector) Collect(ctx context.Context, scope prevention.Scope) (prevention.Manifest, error) {
	m, err := c.Collector.Collect(ctx, scope)
	c.runner.manifest = m
	return m, err
}

type preventionMainPathRunner struct {
	manifest prevention.Manifest
	launches int
}

func (*preventionMainPathRunner) Isolation(context.Context) (string, error) {
	return "fixture broker-only tools; no native source access", nil
}
func (r *preventionMainPathRunner) Run(ctx context.Context, _ prevention.Invocation, s *prevention.Session) (agent.DoctorRunResult, error) {
	r.launches++
	indexText := ""
	for offset := 0; ; {
		page, err := s.Evidence(ctx, "manifest", offset)
		if err != nil {
			return agent.DoctorRunResult{}, err
		}
		indexText += page.Evidence.Excerpt
		if page.Complete {
			break
		}
		offset = page.NextOffset
	}
	var index struct {
		Evidence []prevention.Evidence `json:"evidence"`
		Coverage []prevention.Coverage `json:"coverage"`
	}
	if err := json.Unmarshal([]byte(indexText), &index); err != nil {
		return agent.DoctorRunResult{}, err
	}
	if len(index.Coverage) == 0 {
		return agent.DoctorRunResult{}, errors.New("missing host coverage")
	}
	if _, err := s.Categories(ctx, 20, ""); err != nil {
		return agent.DoctorRunResult{}, err
	}
	selected, err := s.Select(ctx, 0, []string{"quality"}, "Relevant prevention quality guidance")
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	if selected.Selection == nil {
		return agent.DoctorRunResult{}, errors.New("missing selection")
	}
	cursor := ""
	for {
		p, err := s.ReadStandards(ctx, selected.Selection.Version, selected.Selection.Digest, cursor, false)
		if err != nil {
			return agent.DoctorRunResult{}, err
		}
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
	}
	if _, err := s.ReadStandards(ctx, selected.Selection.Version, selected.Selection.Digest, "", true); err != nil {
		return agent.DoctorRunResult{}, err
	}
	bugs := map[int][]string{}
	cause := ""
	for _, e := range index.Evidence {
		offset := 0
		for {
			p, err := s.Evidence(ctx, e.ID, offset)
			if err != nil {
				return agent.DoctorRunResult{}, err
			}
			if p.Complete {
				break
			}
			offset = p.NextOffset
		}
		if e.Kind == "bug" {
			bugs[e.Issue] = append(bugs[e.Issue], e.ID)
		}
		if e.Kind == "diagnosis" || strings.Contains(e.Locator, "diagnosis") {
			cause = e.ID
		}
	}
	result := prevention.Result{SchemaVersion: 1, Findings: []prevention.Finding{
		{Issue: 123, Expected: "One delivery", Actual: "Two deliveries after retry", Cause: "Retries lacked an idempotency guard", CauseBasis: "recorded", Uncertainty: "Recorded diagnosis is not independent proof; other retry paths may differ", PreventionRationale: "A durable idempotency key prevents duplicate side effects during retries", BugEvidence: bugs[123], CauseEvidence: []string{cause}, Outcome: "proposed", Proposals: []standards.Operation{{Kind: "standard.add", Standard: &standards.Standard{ID: "retry-idempotency", Title: "Guard retry side effects", Body: "Use durable idempotency keys and replay tests for retried side effects.", CategoryIDs: []string{"quality"}}}}},
		{Issue: 124, Expected: "Request succeeds", Actual: "Timeout", Uncertainty: "Cause unknown", BugEvidence: bugs[124], MissingEvidence: []string{"Missing root-cause evidence and a demonstrated prevention mechanism"}, Outcome: "insufficient_evidence"},
	}}
	diagnostics, err := s.Preflight(ctx, result)
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	if len(diagnostics) > 0 {
		return agent.DoctorRunResult{}, fmt.Errorf("preflight: %v", diagnostics)
	}
	receipt, err := s.Submit(ctx, "main-path-result", result)
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	recovered, err := s.Receipt(ctx, "main-path-result")
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	if !reflect.DeepEqual(receipt, recovered) {
		return agent.DoctorRunResult{}, errors.New("receipt changed")
	}
	return agent.DoctorRunResult{ObservedModel: "fixture", ProviderVersion: "fixture-v1"}, nil
}
