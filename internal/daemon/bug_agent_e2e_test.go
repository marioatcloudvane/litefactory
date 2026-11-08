package daemon_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/buganalysis"
	"github.com/cloudvane/litefactory/internal/bugrepair"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"github.com/cloudvane/litefactory/internal/turn"
)

// This path uses the real daemon, Bug runtime, local result broker, SQLite and
// Git. Only the external provider and GitHub integration effects are controlled.
func TestCodexBugAgentMainPath(t *testing.T) {
	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tempRoot)
	f := newFixture(t)
	ctx := t.Context()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	repo := gittest.New(t)
	check(os.Rename(repo.Dir, f.repo.Path))
	repo.Dir = f.repo.Path
	repo.WriteFile("behavior.txt", "original behavior\n")
	repo.Commit("seed Bug fixture")
	repo.Git("push", "origin", "main")
	f.repo.RemoteURL = repo.Origin
	storedRepo, err := f.db.UpsertRepo(ctx, f.repo)
	check(err)
	f.repo = storedRepo
	binary, err := git.Resolve(ctx)
	check(err)
	home, err := os.MkdirTemp("/tmp", "lf-bug-agent-")
	check(err)
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	home, err = filepath.EvalSymlinks(home)
	check(err)
	paths := config.NewPaths(home)
	authority, err := turn.NewAttemptAuthority(ctx, f.db)
	check(err)
	t.Cleanup(func() { check(authority.Close(context.WithoutCancel(ctx))) })
	cfg := config.Defaults()
	claude, codexConfig := config.DefaultClaude(), config.DefaultCodex()
	cfg.Agents = config.Agents{Claude: &claude, Codex: &codexConfig}
	judgeRunner := &bugPathJudge{controlledJudge: &controlledJudge{}}
	service := &turn.JudgeService{DB: f.db, Config: &cfg, Providers: map[agent.Kind]agent.JudgeRunner{"codex": judgeRunner}, Paths: paths, Executable: "/usr/bin/false", Git: binary, Redactor: redact.New(redact.Options{Logger: f.logger})}
	code := &bugPathForge{f: f, repo: repo}
	runner := &bugPathRunner{repo: repo}
	runners := turn.RunnerSet{"codex": runner, "claude": bugPathUnexpectedRunner{}}
	opts := turn.Options{DB: f.db, Git: binary, Runners: runners, CloneGate: &bugPathGate{}, Paths: paths, InstanceID: "daemon-instance-test", Author: git.Author{Name: "Fixture", Email: "fixture@example.invalid"}, Logger: f.logger, ResearchReporter: f.reporter, Judge: service, ConflictContentMaxBytes: 65536, Tiers: cfg.Tiers,
		MCP: &turn.MCPRuntime{Authority: authority, Paths: paths, Executable: "/usr/bin/false", Now: time.Now, Comments: f.client, BotID: botID, Redactor: redact.New(redact.Options{Logger: f.logger})}, WorkflowSource: bugPathWorkflowSource}
	runtime, err := turn.NewGraphBugRuntime(ctx, opts, turn.BugRuntimeConfig{Code: code, Tickets: code, OwnerID: ownerID, Permissions: func(kind agent.Kind, role string) (turn.BugRolePermissions, error) {
		if kind != "codex" {
			return turn.BugRolePermissions{}, fmt.Errorf("unexpected provider %s", kind)
		}
		switch role {
		case "bug-analyst":
			return turn.BugRolePermissions{Sandbox: "read-only"}, nil
		case "bug-fixer":
			return turn.BugRolePermissions{Sandbox: "workspace-write"}, nil
		default:
			return turn.BugRolePermissions{}, fmt.Errorf("unexpected Bug role %s", role)
		}
	}})
	check(err)
	opts.Bug = runtime
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) { return turn.Execute(ctx, opts, req) })
	f.daemon = f.newDaemon(daemon.Options{Bug: runtime, Agents: runners, Judge: service})
	f.plantCursor()
	f.forge.addIssue(fakeIssue{ID: 99123, Number: testIssue, Title: "Codex Bug path", Body: "Restore expected behavior", User: owner(), Labels: []fakeLabel{{Name: "bug"}, {Name: "Coding Agent: Codex"}}})
	f.settle()
	session := f.session()
	instance, err := f.db.LatestStandaloneBugInstance(ctx, session.ID)
	check(err)
	wait, err := f.db.GetGraphOwnerWait(ctx, instance.ID)
	check(err)
	if wait.State != "waiting" || wait.OriginNode != "conversation" || runner.analysts != 1 || runner.fixers != 0 || code.merges != 0 {
		bodies, _, _ := f.forge.snapshot()
		t.Logf("Bug reports: %s", strings.Join(bodies, "\n"))
		t.Fatalf("analysis bypassed approval: wait=%+v analyst=%d fixer=%d merges=%d logs=%s", wait, runner.analysts, runner.fixers, code.merges, f.logs)
	}
	// A separate ordinary advisory judge uses the immutable Bug run provider.
	runs, err := f.db.ListRuns(ctx, session.ID)
	check(err)
	if len(runs) == 0 || runs[len(runs)-1].Agent != "codex" {
		t.Fatalf("recorded runs=%+v", runs)
	}
	ref, err := naming.ParseRef(session.TaskRef)
	check(err)
	text, err := service.Execute(ctx, turn.JudgeRequest{Ref: ref, SessionID: session.ID, CommentID: 7001, TicketKey: session.TaskRef, RemoteCommentID: "7001", Root: session.WorktreePath, TaskProvider: config.Kind(runs[len(runs)-1].Agent), Cutoff: time.Now()})
	check(err)
	if text == "" || judgeRunner.calls.Load() != 1 {
		t.Fatalf("advisory judge did not run: %s", text)
	}
	judgeRequest, err := f.db.FindJudgeRequest(ctx, session.TaskRef, "7001")
	check(err)
	judgeAttempts, err := f.db.ListJudgeAttempts(ctx, judgeRequest.ID)
	check(err)
	if judgeRequest.Source.Provider != "codex" || len(judgeAttempts) != 1 || judgeAttempts[0].Provider != "codex" || judgeAttempts[0].ObservedModel == "" {
		t.Fatalf("advisory provider evidence = source %+v attempts %+v", judgeRequest.Source, judgeAttempts)
	}
	after, err := f.db.GetGraphOwnerWait(ctx, instance.ID)
	check(err)
	if after.State != wait.State || after.ReplyID != 0 {
		t.Fatal("judge answered Bug approval")
	}
	delivery, err := f.db.GetReportDelivery(ctx, wait.DeliveryKey)
	check(err)
	f.forge.addComment(testIssue, delivery.CommentID+100, owner(), "/approve-bug analysis 1")
	f.settle()
	instance, err = f.db.GetGraphInstance(ctx, instance.ID)
	check(err)
	if instance.Status != store.GraphSucceeded || runner.fixers != 2 || runner.advisors != 1 || code.creates != 1 || code.merges != 1 || code.closes != 1 {
		t.Fatalf("path incomplete: graph=%+v fixer=%d advisor=%d create=%d merge=%d close=%d logs=%s", instance, runner.fixers, runner.advisors, code.creates, code.merges, code.closes, f.logs)
	}
	runs, err = f.db.ListRuns(ctx, session.ID)
	check(err)
	for _, run := range runs {
		if run.Agent != "codex" {
			t.Fatalf("run changed provider: %+v", run)
		}
	}
	bodies, _, _ := f.forge.snapshot()
	combined := strings.Join(bodies, "\n")
	if !strings.Contains(combined, "codex") || !strings.Contains(combined, "not reported") || !strings.Contains(combined, "Factory action") {
		t.Fatalf("missing execution/factory evidence: %s", combined)
	}
}

type bugPathGate struct{ mu sync.Mutex }

func (g *bugPathGate) Acquire(ctx context.Context, _ string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	return g.mu.Unlock, nil
}

type bugPathUnexpectedRunner struct{}

func (bugPathUnexpectedRunner) Run(context.Context, agent.Request) (*agent.RunResult, error) {
	return nil, fmt.Errorf("Claude must not run a Codex Bug")
}

type bugPathJudge struct{ *controlledJudge }

func (r *bugPathJudge) RunJudge(ctx context.Context, q agent.JudgeRequest) (agent.JudgeRunResult, error) {
	if err := q.Validate(); err != nil {
		return agent.JudgeRunResult{}, err
	}
	if len(q.Invocation.MCPLaunch.PermittedTools()) != len(agent.JudgeToolNames()) {
		return agent.JudgeRunResult{}, fmt.Errorf("judge tools widened")
	}
	return r.controlledJudge.RunJudge(ctx, q)
}

type bugPathRunner struct {
	repo                       *gittest.Repo
	analysts, fixers, advisors int
}

func (*bugPathRunner) SupportsMCP() bool { return true }
func (r *bugPathRunner) Run(ctx context.Context, q agent.Request) (*agent.RunResult, error) {
	var result agentresult.Result
	sandbox, ok := q.Permissions.Payload.(codex.Sandbox)
	if !ok {
		return nil, fmt.Errorf("missing native Codex grant: %+v", q.Permissions)
	}
	switch q.Permissions.Profile {
	case "bug-analyst":
		r.analysts++
		if sandbox.Mode != "read-only" {
			return nil, fmt.Errorf("analyst grant %s", sandbox.Mode)
		}
		if err := q.ValidateBugAnalyst(); err != nil {
			return nil, err
		}
		result = agentresult.Result{Outcome: "completed", Comment: "Diagnosis ready for approval", Payload: agentresult.Payload{Bug: &agentresult.Bug{Response: "done", Analysis: &buganalysis.BugAnalysis{Expected: "Expected behavior", Actual: "Original behavior", Reproduction: buganalysis.BugReproduction{Environment: "local fixture", Steps: []string{"Read behavior.txt"}, Observation: "not_observed"}, Cause: "Old behavior", Evidence: []string{"behavior.txt contains old behavior"}, Remedy: "Update behavior.txt", Exclusions: []string{"Other files"}, Criteria: []buganalysis.BugCriterion{{ID: "C1", Requirement: "Expected behavior is present", Measurement: "Read behavior.txt"}}}}}}
	case "bug-fixer":
		r.fixers++
		if sandbox.Mode != "workspace-write" {
			return nil, fmt.Errorf("fixer grant %s", sandbox.Mode)
		}
		if r.fixers == 1 {
			if err := os.WriteFile(filepath.Join(q.WorktreePath, "behavior.txt"), []byte("expected behavior\n"), 0600); err != nil {
				return nil, err
			}
			// Move base after the fixer has synchronized. This forces the delivery
			// advisor, then fresh fixer revalidation, before merge is permitted.
			r.repo.WriteFile("behavior.txt", "base evolved behavior\n")
			r.repo.Commit("advance main during repair")
			r.repo.Git("push", "origin", "main")
		} else {
			data, err := os.ReadFile(filepath.Join(q.WorktreePath, "behavior.txt"))
			if err != nil {
				return nil, err
			}
			if string(data) != "expected behavior\n" {
				return nil, fmt.Errorf("revalidation lacks resolved source: %q", data)
			}
		}
		data, err := os.ReadFile(filepath.Join(q.WorktreePath, "behavior.txt"))
		if err != nil {
			return nil, err
		}
		if string(data) != "expected behavior\n" {
			return nil, fmt.Errorf("repair check failed: %q", data)
		}
		result = agentresult.Result{Outcome: "completed", Comment: "Approved remedy repaired and checked", Payload: agentresult.Payload{Bug: &agentresult.Bug{Response: "succeeded", Repair: &bugrepair.Evidence{Summary: "Restore expected behavior", Checks: []bugrepair.Check{{Command: "read behavior.txt", Outcome: "passed"}}}}}}
	default:
		return nil, fmt.Errorf("unexpected role %s", q.Permissions.Profile)
	}
	data, err := json.Marshal(agentresult.CreateInput{Result: result})
	if err != nil {
		return nil, err
	}
	client := submission.Client{DescriptorPath: q.MCPLaunch.DescriptorPath()}
	for _, name := range []string{"communication_preflight", "communication_submit"} {
		response, err := client.Call(ctx, name, data)
		if err != nil {
			return nil, err
		}
		if response.Error != "" {
			return nil, fmt.Errorf("%s: %s", name, response.Error)
		}
		if name == "communication_preflight" {
			var d agentresult.Diagnostics
			if err = json.Unmarshal(response.Result, &d); err != nil {
				return nil, err
			}
			if !d.Valid {
				return nil, fmt.Errorf("result refused: %s", response.Result)
			}
		} else {
			receipt, err := client.Call(ctx, "communication_receipt", []byte(fmt.Sprintf(`{"request_id":%q}`, response.RequestID)))
			if err != nil {
				return nil, fmt.Errorf("receipt: %w", err)
			}
			if receipt.Error != "" {
				return nil, fmt.Errorf("receipt: %s", receipt.Error)
			}
			var retained turn.AgentResultSubmission
			if err := json.Unmarshal(receipt.Result, &retained); err != nil {
				return nil, err
			}
			if retained.Receipt == nil || retained.Receipt.InputState != "accepted" {
				return nil, fmt.Errorf("result receipt was not accepted: %s", receipt.Result)
			}
		}
	}
	return &agent.RunResult{Outcome: agent.OutcomeSucceeded, FinishedAt: time.Now()}, nil
}
func (r *bugPathRunner) ProposeConflictResolution(_ context.Context, q agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
	r.advisors++
	if len(q.Files) != 1 || q.Files[0].Path != "behavior.txt" {
		return agent.ConflictResolution{}, fmt.Errorf("unexpected conflict files: %+v", q.Files)
	}
	out := agent.ConflictResolution{Version: git.RegionProposalVersion}
	for _, file := range q.Files {
		f := agent.ConflictResolutionFile{Path: file.Path}
		for _, region := range file.Regions {
			text := "expected behavior\n"
			f.Regions = append(f.Regions, agent.ConflictRegionResolution{ID: region.ID, Resolution: &text})
		}
		out.Files = append(out.Files, f)
	}
	return out, nil
}

func bugPathWorkflowSource(ctx context.Context, _ store.Repo, w agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
	if w != agent.WorkflowBug {
		return nil, fmt.Errorf("unexpected workflow %s", w)
	}
	definition, err := teamsourcetest.CaptureBug(ctx, specs)
	if err != nil {
		return nil, err
	}
	configBytes := []byte("installed Bug fixture mapping")
	digest := sha256.Sum256(configBytes)
	definition.Origin = teamsource.WorkflowOrigin{Kind: "local", Source: "local", Slug: "fixture", Version: "1.0.0", GraphPath: "bug/bug.dot", GraphFile: "bug.dot", Team: definition.Profile.Team, TeamConfig: configBytes, ConfigDigest: hex.EncodeToString(digest[:])}
	return definition, nil
}

type bugPathForge struct {
	f                       *fixture
	repo                    *gittest.Repo
	pr                      github.DeliveryPR
	creates, merges, closes int
	closed                  bool
}

func (f *bugPathForge) BaseURL() string { return f.f.client.BaseURL() }
func (f *bugPathForge) GetIssue(ctx context.Context, repo github.Repo, n int) (github.Issue, error) {
	q, err := f.f.client.GetIssue(ctx, repo, n)
	if f.closed {
		q.State = "closed"
	}
	return q, err
}
func (f *bugPathForge) GetDeliveryHead(_ context.Context, _ github.Repo, branch string) (string, error) {
	return f.repo.GitIn(f.repo.Origin, "rev-parse", branch), nil
}
func (f *bugPathForge) ListDeliveryPRs(context.Context, github.Repo, string) ([]github.DeliveryPR, error) {
	if f.pr.ID == 0 {
		return nil, nil
	}
	return []github.DeliveryPR{f.pr}, nil
}
func (f *bugPathForge) GetDeliveryPR(context.Context, github.Repo, int) (github.DeliveryPR, error) {
	return f.pr, nil
}
func (f *bugPathForge) CreateDeliveryPR(_ context.Context, repo github.Repo, branch, target string, _, body redact.Body) (github.DeliveryPR, error) {
	f.creates++
	f.pr = github.DeliveryPR{ID: 80, Number: 4, CreatorID: botID, Head: f.repo.GitIn(f.repo.Origin, "rev-parse", branch), Branch: branch, Target: target, HeadRepo: repo.Owner + "/" + repo.Name, BaseRepo: repo.Owner + "/" + repo.Name, State: "open", Body: body.String()}
	return f.pr, nil
}
func (f *bugPathForge) MergeDeliveryPR(_ context.Context, _ github.Repo, _ int, head string) error {
	if head != f.pr.Head {
		return fmt.Errorf("merge head changed")
	}
	f.merges++
	f.repo.Git("fetch", "origin")
	f.repo.Git("merge", "--no-ff", "origin/"+f.pr.Branch, "-m", "factory merge")
	f.repo.Git("push", "origin", "main")
	f.pr.Merged = true
	f.pr.State = "closed"
	f.pr.MergeSHA = f.repo.Git("rev-parse", "HEAD")
	return nil
}
func (f *bugPathForge) GetDeliveryCommit(_ context.Context, _ github.Repo, sha string) (github.DeliveryCommit, error) {
	parents := strings.Fields(f.repo.GitIn(f.repo.Origin, "show", "-s", "--format=%P", sha))
	return github.DeliveryCommit{SHA: sha, Parents: parents}, nil
}
func (f *bugPathForge) CloseDeliveredParent(context.Context, github.Repo, int, int64) error {
	f.closes++
	f.closed = true
	return nil
}
