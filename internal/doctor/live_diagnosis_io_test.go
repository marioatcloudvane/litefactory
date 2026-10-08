package doctor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// This fixture exercises the real diagnosis service/broker/SQLite/result ledger.
// It makes no claim about an installed provider or an authentic OS sandbox.
type liveProviderFixture struct {
	t        *testing.T
	manifest PathManifest
}

func (p *liveProviderFixture) DoctorIsolation(context.Context) (string, error) {
	return "fake provider only", nil
}
func (p *liveProviderFixture) RunDoctor(ctx context.Context, r agent.DoctorRequest) (agent.DoctorRunResult, error) {
	p.t.Helper()
	if err := r.Validate(); err != nil {
		return agent.DoctorRunResult{}, err
	}
	if !strings.Contains(r.Prompt, FieldGuide) || strings.Contains(r.Prompt, "untrusted_evidence") || strings.Contains(r.Prompt, "pre-read-canary") || strings.Contains(r.Prompt, "no filesystem, shell") {
		p.t.Fatal("legacy evidence reached diagnosis prompt")
	}
	i := strings.Index(r.Prompt, "\n{\"")
	var prepared PromptContext
	if i < 0 {
		p.t.Fatal("missing JSON context")
	}
	if err := json.Unmarshal([]byte(r.Prompt[i+1:]), &prepared); err != nil {
		p.t.Fatal(err)
	}
	p.manifest = prepared.Manifest
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
	diagnosis := Diagnosis{Findings: []Finding{{Explanation: "Live logs and the database retain the stuck case; the owner comment corroborates it.", Evidence: []string{p.manifest.DaemonLog + ":1", "database:sessions task_ref=github:acme/api#406", "comment:91"}, Uncertainty: "This fake provider does not establish authentic shell isolation."}}, CoverageGaps: []string{"Git not inspected by the fake provider; real-instance check still required"}}
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
	s := doctorIOService(t)
	// Keep the Unix socket path short, independent of Go's descriptive test dirs.
	root, err := os.MkdirTemp("", "lf538-")
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
	if err := os.WriteFile(s.Paths.DaemonLogPath(), log, 0600); err != nil {
		t.Fatal(err)
	}
	provider := &liveProviderFixture{t: t}
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
	var saved PathManifest
	if err := json.Unmarshal([]byte(got.Attempts[0].Evidence), &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.Live || saved.Worktree != worktree || saved.DaemonLog != s.Paths.DaemonLogPath() || !slices.Equal(saved.RunLogs, []string{runLog}) || saved.SandboxProfile != agent.DoctorSandboxVersion || saved.Isolation == "" || strings.Contains(got.Attempts[0].Evidence, "pre-read-canary") || got.Attempts[0].PromptVersion != "doctor-diagnosis-v2" || got.Attempts[0].GuideVersion != "doctor-field-guide-v2" {
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

func TestDoctorFieldGuideAndPromptDescribeLiveSourcesOnly(t *testing.T) {
	for _, name := range []string{"daemon_log", "runs.log_path", "worktree", "sessions", "runs", "graph_instances", "graph_dispatches", "graph_node_attempts", "implement_stories", "schedules", "comments", "watermarks", "report_deliveries"} {
		if !strings.Contains(FieldGuide, name) {
			t.Errorf("field guide omits %s", name)
		}
	}
	context := PromptContext{Instructions: "Inspect this stuck case", Target: "github:acme/api#406", Manifest: PathManifest{Live: true, TaskRef: "github:acme/api#406", DaemonLog: "/factory/logs/daemon.log", RunLogs: []string{"/factory/run.jsonl"}, Worktree: "/worktree", Tools: []string{"shell", "database_query", "issue_comments"}}, Memory: []store.DoctorAttempt{{OwnerText: "Previous owner input"}}, MemoryOmissions: "older attempts omitted"}
	prompt, err := BuildPrompt(context)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{context.Instructions, context.Target, "/factory/logs/daemon.log", "/factory/run.jsonl", "/worktree", "Previous owner input", "older attempts omitted", PromptVersion, GuideVersion} {
		if !strings.Contains(prompt, value) {
			t.Errorf("missing prompt context %s", value)
		}
	}
	for _, obsolete := range []string{"untrusted_evidence", "no filesystem, shell", "\"local_log\"", "\"liveness\"", "\"inventory\""} {
		if strings.Contains(prompt, obsolete) {
			t.Errorf("obsolete diagnosis material %s", obsolete)
		}
	}
}
