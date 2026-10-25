package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// This fixture exercises the real diagnosis service/broker/SQLite/result ledger.
// It makes no claim about an installed provider or an authentic OS sandbox.
type liveProviderFixture struct {
	t          *testing.T
	manifest   PathManifest
	planOwners bool
}

// promptManifest reads the path manifest from a prepared diagnosis prompt.
func promptManifest(t *testing.T, prompt string) PathManifest {
	t.Helper()
	i := strings.LastIndex(prompt, "\n{\"")
	var prepared PromptContext
	if i < 0 {
		t.Fatal("missing JSON context")
	}
	if err := json.Unmarshal([]byte(prompt[i+1:]), &prepared); err != nil {
		t.Fatal(err)
	}
	return prepared.Manifest
}

func (p *liveProviderFixture) DoctorIsolation(context.Context) (string, error) {
	return "fake provider only", nil
}
func (p *liveProviderFixture) RunDoctor(ctx context.Context, r agent.DoctorRequest) (agent.DoctorRunResult, error) {
	p.t.Helper()
	if err := r.Validate(); err != nil {
		return agent.DoctorRunResult{}, err
	}
	if !strings.Contains(r.Prompt, testDoctorGuide(p.t)) || strings.Contains(r.Prompt, "untrusted_evidence") || strings.Contains(r.Prompt, "pre-read-canary") {
		p.t.Fatal("legacy evidence reached diagnosis prompt")
	}
	p.manifest = promptManifest(p.t, r.Prompt)
	data, err := os.ReadFile(p.manifest.DaemonLog)
	if err != nil || !strings.Contains(string(data), "pre-read-canary") {
		p.t.Fatalf("live log unavailable: %s %v", data, err)
	}
	client := submission.Client{DescriptorPath: r.MCPLaunch.DescriptorPath()}
	list, err := client.List(ctx)
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	var catalog struct {
		Tools []submission.ToolSpec `json:"tools"`
	}
	if err := json.Unmarshal(list.Result, &catalog); err != nil {
		p.t.Fatal(err)
	}
	names := []string{}
	for _, tool := range catalog.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, agent.DoctorToolNames()) {
		p.t.Fatalf("catalog: %v", names)
	}
	query, err := client.Call(ctx, "database_query", json.RawMessage(`{"sql":"SELECT task_ref,worktree_path FROM sessions"}`))
	if err != nil || query.Error != "" {
		p.t.Fatalf("live query: %+v %v", query, err)
	}
	if !strings.Contains(string(query.Result), "github:acme/api#406") {
		p.t.Fatalf("missing live database row: %s", query.Result)
	}
	refused, err := client.Call(ctx, "database_query", json.RawMessage(`{"sql":"DELETE FROM sessions"}`))
	if err != nil || refused.Error != "" {
		p.t.Fatalf("read refusal became a transport/receipt failure: %+v %v", refused, err)
	}
	var readError struct {
		Valid *bool  `json:"valid"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(refused.Result, &readError); err != nil || readError.Valid == nil || *readError.Valid || !strings.Contains(readError.Error, "exactly one SELECT") {
		p.t.Fatalf("missing plain tool error: %s %v", refused.Result, err)
	}
	comments, err := client.Call(ctx, "issue_comments", json.RawMessage(`{}`))
	if err != nil || comments.Error != "" || !strings.Contains(string(comments.Result), "live issue finding") {
		p.t.Fatalf("live comments: %+v %v", comments, err)
	}
	diagnosis := Diagnosis{Findings: []Finding{{Explanation: "Live logs and the database retain the stuck case; the owner comment corroborates it.", Evidence: []string{p.manifest.DaemonLog + ":1", "database:sessions task_ref=github:acme/api#406", "comment:91"}, Uncertainty: "This fake provider does not establish authentic shell isolation."}}, CoverageGaps: append([]string{"Git not inspected by the fake provider; real-instance check still required"}, p.manifest.Gaps...)}
	if p.planOwners {
		if !strings.Contains(string(data), "plan scope: release declares no task owners") || !strings.Contains(string(data), "go-development 1.0.0") {
			p.t.Fatal("missing representative failure evidence")
		}
		diagnosis.Findings = []Finding{{Explanation: "Current fixture log reports plan scope: release declares no task owners for go-development 1.0.0. A stack-profile Plan capture needs explicit task_owner: true declarations. The owner must cut and link a new go-development release containing implementation-engineer, integration-test-engineer and unit-test-engineer declarations. Relinking alone does not replace a bad retained capture; separate diagnosis is required. No repair or approval is claimed and no retained capture rewrite is proposed.", Evidence: []string{p.manifest.DaemonLog + ":1"}, Uncertainty: "Linked release and retained capture bytes were not inspected; canonical source is not proof of live state. This is a controlled provider fixture, not live delivery."}}
	}
	raw, err := json.Marshal(ResultInput{Diagnosis: diagnosis})
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	pre, err := client.Call(ctx, "communication_preflight", raw)
	if err != nil || pre.Error != "" {
		p.t.Fatalf("preflight: %+v %v", pre, err)
	}
	var check ResultDiagnostics
	if err := json.Unmarshal(pre.Result, &check); err != nil || !check.Valid {
		p.t.Fatalf("invalid result: %+v %v", check, err)
	}
	submit, err := client.Call(ctx, "communication_submit", raw)
	if err != nil || submit.Error != "" {
		p.t.Fatalf("submit: %+v %v", submit, err)
	}
	receiptInput, err := json.Marshal(map[string]string{"request_id": submit.RequestID})
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	receipt, err := client.Call(ctx, "communication_receipt", receiptInput)
	if err != nil || receipt.Error != "" {
		p.t.Fatalf("receipt: %+v %v", receipt, err)
	}
	return agent.DoctorRunResult{ObservedModel: r.Model, ProviderVersion: "fake-only"}, nil
}
func TestDoctorLiveDiagnosisEndToEndRetainsOnlyManifest(t *testing.T) {
	doctorLiveDiagnosisFixture(t, false)
}

func TestDoctorMissingPlanOwnersDiagnosisPresentation(t *testing.T) {
	doctorLiveDiagnosisFixture(t, true)
}

func doctorLiveDiagnosisFixture(t *testing.T, planOwners bool) {
	t.Helper()
	s := doctorIOService(t)
	s.BuildVersion = "running-daemon-commit"
	s.FactorySource = "/known/factory-source"
	// Keep the Unix socket path short, independent of Go's descriptive test dirs.
	root, err := os.MkdirTemp("/tmp", "lf538-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	}()
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	s.Paths = config.NewPaths(root)
	repo, err := s.DB.UpsertRepo(t.Context(), store.Repo{Forge: "github", Owner: "acme", Name: "api", Path: filepath.Join(root, "clone"), WorktreeRoot: filepath.Join(root, "worktrees"), BaseBranch: "main", RemoteURL: "git@github.com:acme/api.git"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.DB.GetOrCreateSession(t.Context(), repo, 406)
	if err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(root, "worktrees", "406")
	if err := s.DB.SetSessionWorktree(t.Context(), session.ID, worktree); err != nil {
		t.Fatal(err)
	}
	segment, err := s.DB.GetOrCreateSegment(t.Context(), session.ID, 1, "fixture-session", "claude")
	if err != nil {
		t.Fatal(err)
	}
	runLog := filepath.Join(root, "live-run.jsonl")
	if _, err := s.DB.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: store.ModeSpike, Agent: store.AgentClaude, TriggerCommentID: 91, LogPath: func(int) string { return runLog }}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.Paths.LogsDir(), 0700); err != nil {
		t.Fatal(err)
	}
	log := []byte("{\"task\":\"github:acme/api#406\",\"message\":\"pre-read-canary: real diagnosis source\"}\n")
	if planOwners {
		log = []byte("{\"task\":\"github:acme/api#406\",\"message\":\"pre-read-canary: plan scope: release declares no task owners; go-development 1.0.0\"}\n")
	}
	if err := os.WriteFile(s.Paths.DaemonLogPath(), log, 0600); err != nil {
		t.Fatal(err)
	}
	provider := &liveProviderFixture{t: t, planOwners: planOwners}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s.Runner = &Adapters{Paths: s.Paths, Executable: executable, Providers: map[string]agent.DoctorRunner{"claude": provider}}
	s.Comments = &commentsFixture{result: github.CommentPage{Comments: []github.Comment{{ID: 91, Author: github.User{Login: "owner"}, Body: "live issue finding"}}}}
	target, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Diagnose(t.Context(), DiagnoseRequest{Target: target, Instructions: "Inspect live sources", SourceID: "live-source"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Attempts) != 1 || got.Attempts[0].State != "completed" || got.Diagnosis == nil {
		t.Fatalf("not adopted: %+v", got)
	}
	if planOwners {
		body := RenderConversation(got)
		for _, want := range []string{"plan scope: release declares no task owners", "go-development 1.0.0", "cut and link a new go-development release", "implementation-engineer", "integration-test-engineer", "unit-test-engineer", "Relinking alone", "retained capture bytes were not inspected", "not proof of live state", "No repair or approval", "no retained capture rewrite"} {
			if !strings.Contains(body, want) {
				t.Errorf("diagnosis presentation missing %q", want)
			}
		}
		if len(got.CurrentProposal) != 0 || got.ProposalID != 0 {
			t.Fatal("diagnosis granted repair authority")
		}
	}
	var saved PathManifest
	if err := json.Unmarshal([]byte(got.Attempts[0].Evidence), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.DaemonVersion != s.BuildVersion || saved.FactorySource != s.FactorySource || !saved.Live || saved.Worktree != worktree || saved.DaemonLog != s.Paths.DaemonLogPath() || !slices.Equal(saved.RunLogs, []string{runLog}) || saved.SandboxProfile != agent.DoctorSandboxVersion || saved.Isolation == "" || strings.Contains(got.Attempts[0].Evidence, "pre-read-canary") || got.Attempts[0].PromptVersion != "doctor-diagnosis-v2" || got.Attempts[0].GuideVersion != "doctor-field-guide-v2" {
		t.Fatalf("saved manifest: %+v", got.Attempts[0])
	}
	after, err := os.ReadFile(s.Paths.DaemonLogPath())
	if err != nil || string(after) != string(log) {
		t.Fatal("source log changed")
	}
	retained, err := s.DB.GetSession(t.Context(), session.ID)
	if err != nil || retained.State != session.State || retained.NeedsPoll != session.NeedsPoll || retained.WorktreePath != worktree {
		t.Fatalf("workflow state changed: %+v %v", retained, err)
	}
}

type sandboxFailureFixture struct{ calls int }

func (*sandboxFailureFixture) IsolationEvidence(context.Context, string) (string, error) {
	return agent.DoctorSandboxVersion, nil
}
func (f *sandboxFailureFixture) Run(context.Context, Invocation, *ResultService) (InvocationOutcome, error) {
	f.calls++
	return InvocationOutcome{}, ErrIsolationUnavailable
}

func TestDoctorSandboxStartupFailureKeepsInputWithoutFallback(t *testing.T) {
	s := doctorIOService(t)
	cfg, err := config.Decode(strings.NewReader("agents:\n  claude: {}\n  codex: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	s.Config = cfg
	runner := &sandboxFailureFixture{}
	s.Runner = runner
	target, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.Diagnose(t.Context(), DiagnoseRequest{Target: target, Instructions: "Keep this owner input", SourceID: "sandbox-unavailable"})
	if err == nil || runner.calls != 1 || len(saved.Attempts) != 1 || saved.Attempts[0].State != "failed" || saved.Attempts[0].OwnerText != "Keep this owner input" || saved.Attempts[0].Evidence == "" {
		t.Fatalf("refusal lost input or fell back: %+v calls=%d err=%v", saved, runner.calls, err)
	}
}

func TestDoctorPromptCarriesLiveSourcesOnly(t *testing.T) {
	context := PromptContext{Instructions: "Inspect this stuck case", Target: "github:acme/api#406", Manifest: PathManifest{Live: true, TaskRef: "github:acme/api#406", DaemonLog: "/factory/logs/daemon.log", RunLogs: []string{"/factory/run.jsonl"}, Worktree: "/worktree", Tools: []string{"shell", "database_query", "issue_comments"}}, Memory: []store.DoctorAttempt{{OwnerText: "Previous owner input"}}, MemoryOmissions: "older attempts omitted"}
	prompt, err := BuildPrompt(context, testDoctorCapture(t, "doctor-diagnosis"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{context.Instructions, context.Target, "/factory/logs/daemon.log", "/factory/run.jsonl", "/worktree", "Previous owner input", "older attempts omitted", PromptVersion, GuideVersion} {
		if !strings.Contains(prompt, value) {
			t.Errorf("missing prompt context %s", value)
		}
	}
	for _, obsolete := range []string{"untrusted_evidence", "\"local_log\"", "\"liveness\"", "\"inventory\""} {
		if strings.Contains(prompt, obsolete) {
			t.Errorf("obsolete diagnosis material %s", obsolete)
		}
	}
}

// proposalProviderFixture finds its repair targets only through the live
// database_query tool, then proposes typed repairs for them (#578).
type proposalProviderFixture struct {
	t       *testing.T
	receipt string
}

func (p *proposalProviderFixture) DoctorIsolation(context.Context) (string, error) {
	return "fake provider only", nil
}

func (p *proposalProviderFixture) RunDoctor(ctx context.Context, r agent.DoctorRequest) (agent.DoctorRunResult, error) {
	p.t.Helper()
	if err := r.Validate(); err != nil {
		return agent.DoctorRunResult{}, err
	}
	client := submission.Client{DescriptorPath: r.MCPLaunch.DescriptorPath()}
	query := func(sql string) string {
		payload, err := json.Marshal(map[string]string{"sql": sql})
		if err != nil {
			p.t.Fatal(err)
		}
		got, err := client.Call(ctx, "database_query", payload)
		if err != nil || got.Error != "" {
			p.t.Fatalf("live query: %+v %v", got, err)
		}
		var out store.DoctorQueryResult
		if err := json.Unmarshal(got.Result, &out); err != nil || len(out.Rows) != 1 || len(out.Rows[0]) != 1 {
			p.t.Fatalf("query rows: %s %v", got.Result, err)
		}
		value, ok := out.Rows[0][0].(string)
		if !ok {
			p.t.Fatalf("query value: %s", got.Result)
		}
		return value
	}
	graph := query(`SELECT CAST(g.id AS TEXT) FROM graph_instances g JOIN sessions s ON s.id=g.session_id WHERE s.task_ref='github:acme/api#406' AND g.status='running'`)
	comment := query(`SELECT CAST(c.forge_comment_id AS TEXT) FROM comments c JOIN sessions s ON s.id=c.session_id WHERE s.task_ref='github:acme/api#406'`)
	watermark := query(`SELECT CAST(w.last_comment_id AS TEXT) FROM watermarks w JOIN sessions s ON s.id=w.session_id WHERE s.task_ref='github:acme/api#406'`)
	step := func(n int, kind, target, before, after string) RepairStep {
		return RepairStep{Action: &store.DoctorRepairAction{Kind: kind, Before: before, After: after}, Number: n, Target: target, Precondition: "Row still holds the observed state", Change: "Apply the typed repair", Reason: "The instance is stuck", Reversibility: "Retained in the repair journal", Rollback: "Restore from the journal backup"}
	}
	diagnosis := Diagnosis{Findings: []Finding{{Explanation: "A Validate instance is still running and an owner comment sits above the watermark.", Evidence: []string{"database:graph_instances id=" + graph, "database:comments forge_comment_id=" + comment}, Uncertainty: "Fake provider; no process inspection."}}, CoverageGaps: append([]string{"Process state not inspected by the fake provider"}, promptManifest(p.t, r.Prompt).Gaps...)}
	preflight := func(d Diagnosis) ResultDiagnostics {
		raw, err := json.Marshal(ResultInput{Diagnosis: d})
		if err != nil {
			p.t.Fatal(err)
		}
		pre, err := client.Call(ctx, "communication_preflight", raw)
		if err != nil || pre.Error != "" {
			p.t.Fatalf("preflight: %+v %v", pre, err)
		}
		var check ResultDiagnostics
		if err := json.Unmarshal(pre.Result, &check); err != nil {
			p.t.Fatal(err)
		}
		return check
	}
	unknown := diagnosis
	unknown.Proposal = []RepairStep{step(1, "graph_fail", "graph:999999", "running", "failed")}
	if check := preflight(unknown); check.Valid || len(check.Errors) != 1 || !strings.Contains(check.Errors[0], "unresolved target") {
		p.t.Fatalf("nonexistent target accepted: %+v", check)
	}
	diagnosis.Proposal = []RepairStep{step(1, "graph_fail", "graph:"+graph, "running", "failed")}
	_ = watermark
	if check := preflight(diagnosis); !check.Valid {
		p.t.Fatalf("live proposal rejected: %+v", check)
	}
	raw, err := json.Marshal(ResultInput{Diagnosis: diagnosis})
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	submit, err := client.Call(ctx, "communication_submit", raw)
	if err != nil || submit.Error != "" {
		p.t.Fatalf("submit: %+v %v", submit, err)
	}
	receiptInput, err := json.Marshal(map[string]string{"request_id": submit.RequestID})
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	receipt, err := client.Call(ctx, "communication_receipt", receiptInput)
	if err != nil || receipt.Error != "" {
		p.t.Fatalf("receipt: %+v %v", receipt, err)
	}
	p.receipt = string(receipt.Result)
	return agent.DoctorRunResult{ObservedModel: r.Model, ProviderVersion: "fake-only"}, nil
}

func TestDoctorLiveDiagnosisProposesRepairForQueriedTargets(t *testing.T) {
	s := doctorIOService(t)
	root, err := os.MkdirTemp("/tmp", "lf578-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	}()
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	s.Paths = config.NewPaths(root)
	repo, err := s.DB.UpsertRepo(t.Context(), store.Repo{Forge: "github", Owner: "acme", Name: "api", Path: filepath.Join(root, "clone"), WorktreeRoot: filepath.Join(root, "worktrees"), BaseBranch: "main", RemoteURL: "git@github.com:acme/api.git"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.DB.GetOrCreateSession(t.Context(), repo, 406)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.SetSessionWorktree(t.Context(), session.ID, filepath.Join(root, "worktrees", "406")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.GetOrCreateSegment(t.Context(), session.ID, 1, "fixture-session", "claude"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	definition, err := s.DB.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "core", Mode: "validate", ProfileVersion: 1, Path: "core/validate.dot", Digest: strings.Repeat("a", 64), CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := s.DB.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "validate", StartedAt: now, NextDueAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.DB.BootstrapWatermark(t.Context(), session.ID, 6084334000); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.RecordComment(t.Context(), store.Comment{SessionID: session.ID, ForgeCommentID: 6084334752, AuthorHandle: "owner", Body: "/approve", CreatedAt: now, Classification: store.ClassificationInstruction}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.Paths.LogsDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Paths.DaemonLogPath(), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	provider := &proposalProviderFixture{t: t}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s.Runner = &Adapters{Paths: s.Paths, Executable: executable, Providers: map[string]agent.DoctorRunner{"claude": provider}}
	s.Comments = &commentsFixture{}
	target, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Diagnose(t.Context(), DiagnoseRequest{Target: target, Instructions: "Release the stuck Validate instance", SourceID: "live-proposal"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(provider.receipt, `"accepted"`) {
		t.Fatalf("receipt not accepted: %s", provider.receipt)
	}
	if len(got.Attempts) != 1 || got.Attempts[0].State != "completed" || got.ProposalID == 0 || len(got.CurrentProposal) != 1 {
		t.Fatalf("proposal not adopted: %+v", got)
	}
	release := got.CurrentProposal[0]
	if release.Target != fmt.Sprintf("graph:%d", instance.ID) || release.Action.Kind != "graph_fail" || !store.DoctorGraphRepairTarget(release.Action.Kind, release.Target, release.Action.Before, release.Action.After) {
		t.Fatalf("adopted proposal: %+v", got.CurrentProposal)
	}
	var saved PathManifest
	if err := json.Unmarshal([]byte(got.Attempts[0].Evidence), &saved); err != nil || !saved.Live {
		t.Fatalf("stored evidence is no longer the path manifest: %s %v", got.Attempts[0].Evidence, err)
	}
}

// syncBranchRunner proposes sync_branch from the live manifest's branch record.
type syncBranchRunner struct {
	t      *testing.T
	record store.DoctorEvidenceRecord
	gaps   []string
}

func (r *syncBranchRunner) IsolationEvidence(context.Context, string) (string, error) {
	return "controlled fixture only", nil
}
func (r *syncBranchRunner) Run(ctx context.Context, inv Invocation, results *ResultService) (InvocationOutcome, error) {
	manifest := promptManifest(r.t, inv.Prompt)
	r.gaps = manifest.Gaps
	for _, record := range manifest.Records {
		if record.Kind == "branch" {
			r.record = record
		}
	}
	target := "branch:" + r.record.ID
	input := ResultInput{Diagnosis: Diagnosis{Findings: []Finding{{Explanation: "The feature branch is behind its base.", Evidence: []string{target}, Uncertainty: "Local refs only."}}, Proposal: []RepairStep{{Number: 1, Target: target, Precondition: "Captured head", Change: "Merge captured base", Reason: "Restore synchronization", Reversibility: "Backed up head", Rollback: "Restore head", Action: &store.DoctorRepairAction{Kind: "sync_branch", Before: r.record.State, After: r.record.Identity}}}}}
	input.Diagnosis.CoverageGaps = r.gaps
	raw, err := json.Marshal(input)
	if err != nil {
		return InvocationOutcome{}, err
	}
	if check, err := results.Preflight(ctx, raw); err != nil || !check.Valid {
		r.t.Fatalf("live sync_branch proposal rejected: %+v %v", check, err)
	}
	if _, err := results.Submit(ctx, "sync-branch-result", raw); err != nil {
		return InvocationOutcome{}, err
	}
	return InvocationOutcome{RequestID: "sync-branch-result", ObservedModel: inv.ConfiguredModel}, nil
}

func TestDoctorLiveDiagnosisProposesSyncBranchForBehindBranch(t *testing.T) {
	// "stale": origin moved but the clone's remote-tracking ref still names the
	// old head, so the diagnosis must fetch to propose the current base (#702).
	// "fetch fails": an unreachable origin keeps the local observation with a gap.
	for _, mode := range []string{"fetched", "stale", "fetch fails"} {
		t.Run(mode, func(t *testing.T) { liveSyncBranchProposal(t, mode) })
	}
}

func liveSyncBranchProposal(t *testing.T, mode string) {
	s := doctorIOService(t)
	ctx := t.Context()
	fixture := gittest.New(t)
	repo, err := s.DB.UpsertRepo(ctx, store.Repo{Path: fixture.Dir, Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: fixture.Origin, BaseBranch: gittest.DefaultBranch, FeaturePrefix: "lf", WorktreeRoot: filepath.Join(filepath.Dir(fixture.Dir), "trees")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.DB.GetOrCreateSession(ctx, repo, 406)
	if err != nil {
		t.Fatal(err)
	}
	if session, err = s.DB.FreezeSessionSlug(ctx, session.ID, "case", "lf/406-case"); err != nil {
		t.Fatal(err)
	}
	binary, err := git.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	clone, err := binary.Open(ctx, fixture.Dir)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := naming.WorktreePath(repo.WorktreeRoot, 406)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: dir, Branch: session.Branch, BaseBranch: repo.BaseBranch}); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.SetSessionWorktree(ctx, session.ID, dir); err != nil {
		t.Fatal(err)
	}
	head := fixture.GitIn(dir, "rev-parse", "HEAD")
	fixture.WriteFile("base.txt", "base moved on\n")
	baseHead := fixture.Commit("base advances")
	fixture.Git("push", "--quiet", "origin", gittest.DefaultBranch)
	switch mode {
	case "stale":
		fixture.Git("update-ref", "refs/remotes/origin/"+gittest.DefaultBranch, head)
	case "fetch fails":
		fixture.SetOriginURL(filepath.Join(t.TempDir(), "unreachable"))
	}
	runner := &syncBranchRunner{t: t}
	s.Runner = runner
	target, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Diagnose(ctx, DiagnoseRequest{Target: target, Instructions: "Bring main into the feature branch", SourceID: "live-sync"})
	if err != nil {
		t.Fatal(err)
	}
	if runner.record.ID != session.Branch || runner.record.State != head || runner.record.Identity != baseHead {
		t.Fatalf("manifest branch record: %+v", runner.record)
	}
	const fetchGap = "Branch synchronization base fetch failed; local observation used"
	if slices.Contains(runner.gaps, fetchGap) != (mode == "fetch fails") {
		t.Fatalf("fetch gap for %s: %v", mode, runner.gaps)
	}
	if len(got.CurrentProposal) != 1 || got.CurrentProposal[0].Target != "branch:"+session.Branch || got.CurrentProposal[0].Action.Kind != "sync_branch" {
		t.Fatalf("sync_branch proposal not adopted: %+v", got)
	}
	if err = s.DB.PresentDoctorProposal(ctx, target.String(), "live-sync-presentation", got.ProposalID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ReserveDoctorRepair(ctx, store.DoctorApproval{ID: "live-sync-repair", SourceID: "owner-apply", Principal: "local-owner", TaskRef: target.String(), PresentationID: "live-sync-presentation"}); err != nil {
		t.Fatalf("live sync_branch proposal not reservable: %v", err)
	}
}

func TestDoctorManifestBuildIdentity(t *testing.T) {
	s := doctorIOService(t)
	ref, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	fixture := gittest.New(t)
	repo, err := s.DB.UpsertRepo(t.Context(), store.Repo{Path: fixture.Dir, Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: fixture.Origin, BaseBranch: gittest.DefaultBranch, WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.DB.GetOrCreateSession(t.Context(), repo, 406)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.SetSessionWorktree(t.Context(), session.ID, fixture.Dir); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ version, source, want string }{
		{"daemon-commit", "/known/factory-checkout", "daemon-commit"},
		{"dev", "", "dev"}, {"", "", "unknown"},
	} {
		s.BuildVersion, s.FactorySource = tc.version, tc.source
		m, _, err := s.pathManifest(t.Context(), ref)
		if err != nil {
			t.Fatal(err)
		}
		if m.Worktree != fixture.Dir {
			t.Fatal("missing independent task checkout")
		}
		if m.DaemonVersion != tc.want || m.FactorySource != tc.source {
			t.Fatalf("manifest: %+v", m)
		}
		if m.FactorySource != "" && m.FactorySource == m.Worktree {
			t.Fatal("task worktree substituted")
		}
	}
}
