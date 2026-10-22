package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// analysisMCPCall crosses the CLI's real stdio MCP adapter and private broker.
// The controlled analyst fixture makes no claim about live provider isolation.
func analysisMCPCall(t *testing.T, process *mcpCLIProcess, id int, name string, arguments any) submission.Response {
	t.Helper()
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}})
	if err != nil {
		t.Fatal(err)
	}
	process.send(t, string(request))
	response := cliStructured(t, process.read(t))
	if response.Error != "" {
		t.Fatalf("%s: %s (%s)", name, response.Error, response.Result)
	}
	return response
}

func analysisDecode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(fmt.Errorf("decode %s: %w", data, err))
	}
	return value
}

// Each command opens and closes storage. The final assertions therefore inspect
// retained records after the analyst's live invocation has ended.
func TestLogAnalysisEndToEndNoninteractiveRetainedProposal(t *testing.T) {
	paths := newRepoHome(t)
	const secret = "log-analysis-fixture-token-70391"
	t.Setenv("LF_ANALYSIS_FIXTURE_TOKEN", secret)
	writeInitConfig(t, paths, "providers:\n  github:\n    token_env: LF_ANALYSIS_FIXTURE_TOKEN\nagents:\n  claude: {}\n")
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, Handle: "analysis-owner", UserID: 71, Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(paths.Home(), "repo"), Forge: store.ForgeGitHub, Owner: "fixture", Name: "analysis", RemoteURL: "git@github.com:fixture/analysis.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(paths.Home(), "worktrees")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 99)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, 1, "analysis", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(paths.RunsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(paths.RunsDir(), "source.jsonl")
	sourceRun, err := db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 1, StartedAt: time.Now().Add(-time.Minute), LogPath: func(int) string { return logPath }})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(logPath, []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"A package check caught a regression before review. log-analysis-fixture-token-70391"}]}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = db.FinishRun(t.Context(), sourceRun.ID, store.RunOutcome{Status: store.RunSucceeded, FinishedAt: time.Now().Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	bookService := &standards.Service{Store: db, OwnerID: 71}
	seed, err := bookService.Create(t.Context(), "analysis-seed", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "quality", Name: "Quality"}}, {Kind: "standard.add", Standard: &standards.Standard{ID: "review", Rule: "MUST Review", Description: "Review changes before publication.", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "quality", AppliesTo: []string{"story-worker"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bookService.Approve(t.Context(), seed.ProposalID, seed.Version, seed.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	published, err := bookService.Publish(t.Context(), seed.ProposalID, seed.Version, seed.ChangeDigest)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	analyst := &analysisFixtureRunner{t: t, runID: sourceRun.ID, secret: secret}
	runCommand := func(args ...string) string {
		t.Helper()
		var out, diagnostics bytes.Buffer
		env := &Env{Stdout: &out, Stderr: &diagnostics, Stdin: strings.NewReader(""), Logger: slog.New(slog.DiscardHandler), Version: "fixture", LogAnalyst: analyst}
		if code := NewRouter("litefactory", env, Commands()).Run(t.Context(), args); code != ExitOK {
			t.Fatalf("%v: exit %d: %s", args, code, diagnostics.String())
		}
		return out.String()
	}
	started := runCommand("standards", "analyze", "logs", "--repo", "github:fixture/analysis", "--run", fmt.Sprint(sourceRun.ID), "--start-key", "main-path", "--json")
	if analyst.calls != 1 {
		t.Fatalf("model invocations=%d, output=%s", analyst.calls, started)
	}
	receipt := analysisDecode[store.LogAnalysisReceipt](t, analyst.receipt.Result)
	if receipt.AnalysisID == "" || receipt.Disposition != "proposals" || len(receipt.Proposals) != 1 {
		t.Fatalf("receipt=%+v; start=%s", receipt, started)
	}
	viewJSON := runCommand("standards", "analyze", "logs", "show", receipt.AnalysisID, "--json")
	view := analysisDecode[loganalysis.Inspection](t, []byte(viewJSON))
	if view.Invocation.ID != receipt.AnalysisID || view.Invocation.State != "completed" || view.Invocation.Disposition != "proposals" {
		t.Fatalf("reopened invocation=%+v", view.Invocation)
	}
	if len(view.Evidence) != 2 || len(view.Report) == 0 || !reflect.DeepEqual(view.Proposals, receipt.Proposals) {
		t.Fatalf("reopened report, ledger or links missing: %+v", view)
	}
	if view.Standards.Status.Integrity != "verified" || view.Standards.Status.Binding.RevisionID != published.ID {
		t.Fatalf("retained book=%+v", view.Standards.Status)
	}
	report := analysisDecode[loganalysis.Report](t, view.Report)
	if len(report.Findings) != 1 || len(report.Findings[0].Citations) != 1 || report.Findings[0].Observation == "" || report.Findings[0].Benefit == "" || report.Findings[0].Uncertainty == "" {
		t.Fatalf("retained finding=%+v", report)
	}
	offered := false
	for _, entry := range view.Evidence {
		evidence := analysisDecode[loganalysis.Evidence](t, entry.Metadata)
		if evidence.Tool == "run_logs_read" {
			if !evidence.Coverage.EndOfSource || len(evidence.Citations) != 1 || !reflect.DeepEqual(evidence.Citations[0], report.Findings[0].Citations[0]) {
				t.Fatalf("offered and cited evidence diverged: %+v", evidence)
			}
			offered = true
		}
	}
	if !offered {
		t.Fatal("no retained offered evidence")
	}
	if strings.Contains(viewJSON, secret) || strings.Contains(viewJSON, logPath) || strings.Contains(viewJSON, "A package check caught a regression before review.") {
		t.Fatalf("inspection retained secret, private path or source body: %s", viewJSON)
	}
	text := runCommand("standards", "analyze", "logs", "show", receipt.AnalysisID)
	for _, want := range []string{receipt.AnalysisID, "proposals", "relevant-checks", receipt.Proposals[0].ProposalID, receipt.Proposals[0].ChangeDigest, receipt.Proposals[0].ResultDigest, view.Standards.Status.Binding.Digest, "categories [quality]", "Limits:", report.Findings[0].Observation} {
		if !strings.Contains(text, want) {
			t.Fatalf("text inspection missing %q: %s", want, text)
		}
	}
	listed := runCommand("standards", "analyze", "logs", "list", "--json")
	if !strings.Contains(listed, receipt.AnalysisID) {
		t.Fatalf("list missing analysis: %s", listed)
	}
	replayed := runCommand("standards", "analyze", "logs", "--repo", "github:fixture/analysis", "--run", fmt.Sprint(sourceRun.ID), "--start-key", "main-path", "--json")
	if retained := analysisDecode[store.LogAnalysis](t, []byte(replayed)); retained.ID != receipt.AnalysisID || analyst.calls != 1 {
		t.Fatalf("start retry changed identity or relaunched analyst: %+v; calls=%d", retained, analyst.calls)
	}
	citationJSON, err := json.Marshal(report.Findings[0].Citations[0])
	if err != nil {
		t.Fatal(err)
	}
	rereadJSON := runCommand("standards", "analyze", "logs", "reread", receipt.AnalysisID, "--citation", string(citationJSON))
	reread := analysisDecode[logevidence.Result](t, []byte(rereadJSON))
	if len(reread.Records) != 1 || reread.Records[0].Citation != report.Findings[0].Citations[0] || strings.Contains(rereadJSON, secret) {
		t.Fatalf("separate citation reread lost provenance or redaction: %+v", reread)
	}
	reopened, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	link := receipt.Proposals[0]
	exact, err := reopened.GetStandardsProposal(t.Context(), link.ProposalID, link.Version)
	if err != nil {
		t.Fatal(err)
	}
	if link.CandidateKey != "relevant-checks" || link.Version != 1 || link.BaseRevision != published.ID || exact.ChangeDigest != link.ChangeDigest || exact.ResultDigest != link.ResultDigest || exact.BaseRevision != link.BaseRevision {
		t.Fatalf("exact unapproved version mismatch: link=%+v proposal=%+v", link, exact)
	}
	if _, err = reopened.GetStandardsApproval(t.Context(), link.ProposalID, link.Version); !errors.Is(err, store.ErrStandardsApprovalRequired) {
		t.Fatalf("generated proposal acquired approval: %v", err)
	}
	head, err := reopened.StandardsHead(t.Context())
	if err != nil || head != published.ID {
		t.Fatalf("analysis published a book: head=%d error=%v", head, err)
	}
	if analyst.calls != 1 {
		t.Fatalf("inspection relaunched analyst: %d", analyst.calls)
	}

}

type analysisFixtureRunner struct {
	t       *testing.T
	runID   int64
	secret  string
	calls   int
	report  []byte
	receipt submission.Response
}

func (r *analysisFixtureRunner) LogAnalystIsolation(context.Context) (string, error) {
	return "controlled-fixture-v1", nil
}
func (r *analysisFixtureRunner) RunLogAnalyst(ctx context.Context, request agent.LogAnalystRequest) (agent.LogAnalystRunResult, error) {
	r.calls++
	t := r.t
	if err := request.Validate(); err != nil {
		return agent.LogAnalystRunResult{}, err
	}
	p := startMCPCLI(t, request.MCPLaunch.DescriptorPath())
	defer func() { p.cancel(); _ = p.input.Close(); _ = p.output.Close(); p.join(t) }()
	p.handshake(t)
	p.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	catalog := analysisDecode[struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}](t, p.read(t).Result)
	got := make([]string, 0, len(catalog.Tools))
	for _, tool := range catalog.Tools {
		got = append(got, tool.Name)
	}
	slices.Sort(got)
	want := []string{"communication_preflight", "communication_receipt", "communication_submit", "run_logs_read", "runs_list", "standards_categories_list", "standards_read", "standards_select"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("restricted catalog=%v", got)
	}
	if request.CatalogVerified == nil {
		t.Fatal("restricted launch omitted catalog verification gate")
	}
	if err := request.CatalogVerified(ctx, "controlled-fixture-v1"); err != nil {
		return agent.LogAnalystRunResult{}, err
	}
	categories := analysisDecode[standards.CategoryPage](t, analysisMCPCall(t, p, 3, "standards_categories_list", map[string]any{}).Result)
	if categories.Status.Integrity != "verified" || len(categories.Categories) != 1 {
		t.Fatalf("categories=%+v", categories)
	}
	selected := analysisDecode[standards.RuntimeStatus](t, analysisMCPCall(t, p, 4, "standards_select", map[string]any{"expected_version": 0, "category_ids": []string{"quality"}, "reason": "Review practice evidence"}).Result)
	if selected.Selection == nil {
		t.Fatalf("selection=%+v", selected)
	}
	read := analysisDecode[standards.ReadPage](t, analysisMCPCall(t, p, 5, "standards_read", map[string]any{"selection_version": selected.Selection.Version, "selection_digest": selected.Selection.Digest}).Result)
	if !read.Complete {
		t.Fatalf("standards not completely retrieved: %+v", read)
	}
	analysisMCPCall(t, p, 15, "standards_read", map[string]any{"selection_version": selected.Selection.Version, "selection_digest": selected.Selection.Digest, "ack": true})
	listed := analysisDecode[logevidence.Result](t, analysisMCPCall(t, p, 6, "runs_list", map[string]any{}).Result)
	if len(listed.Runs) != 1 || listed.Runs[0].RunID != r.runID {
		t.Fatalf("runs=%+v", listed)
	}
	evidenceReply := analysisMCPCall(t, p, 7, "run_logs_read", map[string]any{"run_id": r.runID}).Result
	if bytes.Contains(evidenceReply, []byte(r.secret)) {
		t.Fatal("secret escaped evidence redaction")
	}
	evidence := analysisDecode[logevidence.Result](t, evidenceReply)
	if len(evidence.Records) != 1 || !evidence.Coverage.EndOfSource || evidence.Records[0].Redactions == 0 {
		t.Fatalf("evidence=%+v", evidence)
	}
	payload := map[string]any{"request_id": "fixture-supported-report", "report": map[string]any{"disposition": "proposals", "reason": "One observed review practice supports scoped guidance.", "findings": []any{map[string]any{"key": "relevant-checks", "observation": "A relevant package check caught a regression before review.", "benefit": "Running relevant checks before review may catch regressions earlier.", "uncertainty": "One run does not establish a universal rule or independent repeated incidents.", "citations": []logevidence.Citation{evidence.Records[0].Citation}, "change": standards.Operation{Kind: "standard.add", Standard: &standards.Standard{ID: "package-checks", Rule: "MUST Check relevant packages", Description: "Run relevant package checks before requesting review.", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "quality", AppliesTo: []string{"story-worker"}}}}}}}
	r.report, _ = json.Marshal(payload)
	preflight := analysisDecode[struct {
		Valid bool `json:"valid"`
	}](t, analysisMCPCall(t, p, 8, "communication_preflight", payload).Result)
	if !preflight.Valid {
		t.Fatal("supported fixture report failed preflight")
	}
	submitted := analysisMCPCall(t, p, 9, "communication_submit", payload)
	r.receipt = analysisMCPCall(t, p, 10, "communication_receipt", map[string]any{"request_id": "fixture-supported-report"})
	if !bytes.Equal(submitted.Result, r.receipt.Result) {
		t.Fatalf("receipt changed committed references: submit=%s receipt=%s", submitted.Result, r.receipt.Result)
	}
	return agent.LogAnalystRunResult{}, nil
}
