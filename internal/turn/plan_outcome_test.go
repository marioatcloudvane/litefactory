package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

func writePlanOutcome(t *testing.T, req agent.Request, outcome string) {
	t.Helper()
	writePlanJSON(t, filepath.Join(req.WorktreePath, req.PlanOutcomePath), planbrief.PlanOutcome{Version: 1, Parent: req.Ref.String(), Generation: req.PlanGeneration, RunID: req.PlanRunID, Attempt: req.PlanAttempt, Outcome: outcome})
}
func writePlanJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	mustProgress(t, err)
	mustProgress(t, os.MkdirAll(filepath.Dir(path), 0700))
	mustProgress(t, os.WriteFile(path, data, 0600))
}
func writePlanContext(t *testing.T, f *fixture, req agent.Request) {
	t.Helper()
	s := f.reloadSession()
	writePlanJSON(t, filepath.Join(req.WorktreePath, req.PlanContextPath), planbrief.WorkingContext{Version: 1, Parent: req.Ref.String(), Branch: s.Branch, Feature: s.FeatureSlug, Scope: "Retain modes", Decisions: "Owner switches modes", Assumptions: "None", Questions: "Which default?"})
}
func writeCompletedProposal(t *testing.T, req agent.Request) {
	t.Helper()
	writePlanOutcome(t, req, "completed")
	writePlanJSON(t, filepath.Join(req.WorktreePath, req.PlanProposalPath), planbrief.PlanProposal{Version: 1, Parent: req.Ref.String(), Generation: req.PlanGeneration, Stories: []planbrief.StoryProposal{{Key: "US-1", Intent: "create", Title: "Retain modes", Body: "## User Story\nOwner keeps mode.\n## Acceptance Criteria\n- AC-1: retain selection\n## Tasks\nImplement and test.\n## Architecture\nExisting packages.\n## Dependencies\nNone.\n"}}})
	mustProgress(t, os.MkdirAll(filepath.Join(req.WorktreePath, filepath.Dir(req.PlanContextPath)), 0700))
	mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, filepath.Dir(req.PlanContextPath), "plan_brief.md"), []byte("# Plan\n\n## Story Index\n"), 0600))
}
func setupOutcomeForge(t *testing.T, f *fixture) *lifecycleForge {
	t.Helper()
	// These runner fixtures author the legacy version-1 file handoff.
	mustProgress(t, f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: f.session.TaskRef, Generation: 1, State: "planning"}))
	mustProgress(t, f.db.BindFeatureContract(t.Context(), store.FeatureContract{Parent: f.session.TaskRef, Generation: 1, ContentVersion: 1, SubmissionVersion: 1}))
	forge := &lifecycleForge{progressForge: progressForge{children: map[int]github.Issue{}, comments: map[int][]github.Comment{}}}
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: github.CommentSizeLimit})
	mustProgress(t, err)
	f.opts.SubIssueCreator, f.opts.SubIssueRenderer = forge, renderer
	_, err = f.db.UpsertIdentity(t.Context(), store.Identity{Forge: "github", Role: store.RoleBot, UserID: 9})
	mustProgress(t, err)
	return forge
}
func planRequest(f *fixture) Request {
	req := f.request()
	req.Mode = agent.ModePlan
	req.Command = ParseCommand("/plan retain modes")
	req.Trigger.Body = "/plan retain modes"
	return req
}
func assertPlanUnready(t *testing.T, f *fixture) {
	t.Helper()
	feature, err := f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	if feature.Ready {
		t.Fatal("stale managed readiness retained")
	}
	req := planRequest(f)
	req.Mode = agent.ModeImplement
	if err := prepareManagedScope(t.Context(), f.opts, req, f.ref()); err == nil {
		t.Fatal("unready Implement admitted")
	}
}

func TestPlanDiscussionThenCompletion(t *testing.T) {
	var f *fixture
	phase := 0
	var contextPath string
	runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		if req.Mode != "plan" {
			t.Fatalf("automatic mode change: %s", req.Mode)
		}
		if phase == 0 {
			writePlanOutcome(t, req, "discussion")
			writePlanContext(t, f, req)
			contextPath = req.PlanContextPath
		} else {
			data, err := os.ReadFile(filepath.Join(req.WorktreePath, contextPath))
			mustProgress(t, err)
			if !strings.Contains(string(data), "Which default?") {
				t.Fatal("discussion lost")
			}
			writeCompletedProposal(t, req)
		}
		r := succeeded(req, f.paths.Home())
		r.ResultText = "Saved planning iteration"
		return r, nil
	})
	f = newFixture(t, runner)
	forge := setupOutcomeForge(t, f)
	req := planRequest(f)
	discussion := f.execute(req)
	if !discussion.Succeeded() || !discussion.Committed || discussion.Harness.ResultText != "Saved planning iteration" {
		t.Fatalf("discussion failed: %+v logs=%s", discussion, f.logs)
	}
	assertPlanUnready(t, f)
	if len(forge.children) != 0 {
		t.Fatal("discussion published stories")
	}
	if tree := f.repo.GitIn(f.worktreeDir(), "ls-tree", "-r", "--name-only", "HEAD"); !strings.Contains(tree, "PLAN_CONTEXT.json") || strings.Contains(tree, planbrief.OutcomeFileName) {
		t.Fatalf("wrong discussion save: %s", tree)
	}
	phase = 1
	req.Trigger.CommentID++
	complete := f.execute(req)
	if !complete.Succeeded() {
		t.Fatalf("completion: %+v logs=%s", complete, f.logs)
	}
	feature, err := f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	if !feature.Ready || len(forge.children) != 1 {
		t.Fatalf("not ready after publication: %+v stories=%d", feature, len(forge.children))
	}
	if !strings.Contains(complete.Harness.ResultText, "/implement <instruction>") {
		t.Fatal("missing executable owner advice")
	}
	index, err := os.ReadFile(filepath.Join(f.worktreeDir(), filepath.Dir(contextPath), "plan_brief.md"))
	mustProgress(t, err)
	if !strings.Contains(string(index), "/issues/200") || strings.Contains(string(index), "## User Story") {
		t.Fatalf("index not link-only: %s", index)
	}
	for _, path := range []string{planbrief.OutcomeFileName, planbrief.ProposalFileName} {
		if _, err := os.Lstat(filepath.Join(f.worktreeDir(), path)); !os.IsNotExist(err) {
			t.Fatalf("transport not consumed: %s %v", path, err)
		}
	}
}

func TestPlanRejectsInvalidCurrentHandoff(t *testing.T) {
	for _, scenario := range []string{"missing outcome", "malformed", "unknown field", "wrong parent", "wrong generation", "wrong run", "wrong attempt", "completed without proposal", "discussion with proposal", "wrong context identity", "context symlink", "outcome symlink", "failed subprocess"} {
		t.Run(scenario, func(t *testing.T) {
			var f *fixture
			runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				assertPlanUnready(t, f)
				writePlanOutcome(t, req, "discussion")
				writePlanContext(t, f, req)
				path := filepath.Join(req.WorktreePath, req.PlanOutcomePath)
				switch scenario {
				case "missing outcome":
					mustProgress(t, os.Remove(path))
				case "malformed":
					mustProgress(t, os.WriteFile(path, []byte("{"), 0600))
				case "unknown field":
					data, err := os.ReadFile(path)
					mustProgress(t, err)
					mustProgress(t, os.WriteFile(path, []byte(strings.Replace(string(data), `"version":1`, `"version":1,"extra":true`, 1)), 0600))
				case "wrong parent", "wrong generation", "wrong run", "wrong attempt":
					o := planbrief.PlanOutcome{Version: 1, Parent: req.Ref.String(), Generation: req.PlanGeneration, RunID: req.PlanRunID, Attempt: req.PlanAttempt, Outcome: "discussion"}
					switch scenario {
					case "wrong parent":
						o.Parent = "github:other/repo#99"
					case "wrong generation":
						o.Generation++
					case "wrong run":
						o.RunID++
					case "wrong attempt":
						o.Attempt = "old-attempt"
					}
					writePlanJSON(t, path, o)
				case "completed without proposal":
					writePlanOutcome(t, req, "completed")
				case "discussion with proposal":
					mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, req.PlanProposalPath), []byte("{}"), 0600))
				case "wrong context identity":
					p := filepath.Join(req.WorktreePath, req.PlanContextPath)
					data, err := os.ReadFile(p)
					mustProgress(t, err)
					mustProgress(t, os.WriteFile(p, []byte(strings.Replace(string(data), req.Ref.String(), "github:other/repo#99", 1)), 0600))
				case "context symlink", "outcome symlink":
					if scenario == "context symlink" {
						path = filepath.Join(req.WorktreePath, req.PlanContextPath)
					}
					external := filepath.Join(t.TempDir(), "external")
					data, err := os.ReadFile(path)
					mustProgress(t, err)
					mustProgress(t, os.WriteFile(external, data, 0600))
					mustProgress(t, os.Remove(path))
					mustProgress(t, os.Symlink(external, path))
				}
				r := succeeded(req, f.paths.Home())
				if scenario == "failed subprocess" {
					r.Outcome = agent.OutcomeFailed
					r.ExitCode = 1
				}
				return r, nil
			})
			f = newFixture(t, runner)
			forge := setupOutcomeForge(t, f)
			mustProgress(t, f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: f.session.TaskRef, Generation: 1, State: "planning"}))
			mustProgress(t, f.db.SetManagedFeatureReady(t.Context(), f.session.TaskRef, 1, true))
			result := f.execute(planRequest(f))
			if result.Succeeded() || result.Committed {
				t.Fatalf("invalid handoff saved: %+v", result)
			}
			if len(forge.children) != 0 {
				t.Fatal("invalid handoff published")
			}
			assertPlanUnready(t, f)
		})
	}
}

func TestPlanFailuresCannotRestoreReadiness(t *testing.T) {
	for _, scenario := range []string{"publication", "save", "discussion save"} {
		t.Run(scenario, func(t *testing.T) {
			var f *fixture
			runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				assertPlanUnready(t, f)
				if scenario == "discussion save" {
					writePlanOutcome(t, req, "discussion")
					writePlanContext(t, f, req)
				} else {
					writeCompletedProposal(t, req)
				}
				if scenario != "publication" {
					lockIndex(t, req.WorktreePath)
				}
				return succeeded(req, f.paths.Home()), nil
			})
			f = newFixture(t, runner)
			setupOutcomeForge(t, f)
			if scenario == "publication" {
				f.opts.SubIssueCreator = &retryPublicationFailure{lifecycleForge{progressForge: progressForge{children: map[int]github.Issue{}, comments: map[int][]github.Comment{}}}}
			}
			mustProgress(t, f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: f.session.TaskRef, Generation: 1, State: "planning"}))
			mustProgress(t, f.db.SetManagedFeatureReady(t.Context(), f.session.TaskRef, 1, true))
			r := f.execute(planRequest(f))
			if r.Succeeded() || r.Err == nil {
				t.Fatalf("failure incorrectly succeeded: %+v", r)
			}
			assertPlanUnready(t, f)
		})
	}
}

func TestPlanFallbackRejectsPreviousAttemptTransport(t *testing.T) {
	for _, kind := range []agent.Kind{agent.Kind(store.AgentClaude), agent.Kind(store.AgentCodex)} {
		t.Run(string(kind), func(t *testing.T) {
			var f *fixture
			var oldOutcome []byte
			var firstAttempt string
			calls := 0
			runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				calls++
				if req.Mode != "plan" {
					t.Fatalf("unpinned mode: %s", req.Mode)
				}
				if calls == 1 {
					writePlanOutcome(t, req, "discussion")
					writePlanContext(t, f, req)
					r := succeeded(req, f.paths.Home())
					r.ResumeToken = "existing-plan-conversation"
					return r, nil
				}
				if calls == 2 {
					for _, p := range []string{req.PlanOutcomePath, req.PlanProposalPath} {
						if _, err := os.Lstat(filepath.Join(req.WorktreePath, p)); !os.IsNotExist(err) {
							t.Fatalf("native resume retained old transport: %s", p)
						}
					}
					if req.Path != agent.PathResume {
						t.Fatalf("expected native resume: %s", req.Path)
					}
					writePlanOutcome(t, req, "discussion")
					writePlanContext(t, f, req)
					var err error
					oldOutcome, err = os.ReadFile(filepath.Join(req.WorktreePath, req.PlanOutcomePath))
					mustProgress(t, err)
					firstAttempt = req.PlanAttempt
					return refused(req, f.paths.Home())
				}
				if req.Path != agent.PathColdAfterResumeRefused || req.PlanAttempt == firstAttempt {
					t.Fatal("fallback reused attempt")
				}
				for _, p := range []string{req.PlanOutcomePath, req.PlanProposalPath} {
					if _, err := os.Lstat(filepath.Join(req.WorktreePath, p)); !os.IsNotExist(err) {
						t.Fatalf("fallback retained transport: %s", p)
					}
				}
				if _, err := os.Stat(filepath.Join(req.WorktreePath, req.PlanContextPath)); err != nil {
					t.Fatalf("fallback lost context: %v", err)
				}
				mustProgress(t, os.WriteFile(filepath.Join(req.WorktreePath, req.PlanOutcomePath), oldOutcome, 0600))
				return succeeded(req, f.paths.Home()), nil
			})
			f = newFixture(t, runner)
			setupOutcomeForge(t, f)
			f.opts.Runners = RunnerSet{kind: runner}
			req := planRequest(f)
			req.Agent = kind
			first := f.execute(req)
			if !first.Succeeded() {
				t.Fatalf("first discussion: %+v", first)
			}
			for _, p := range []string{planbrief.OutcomeFileName, planbrief.ProposalFileName} {
				mustProgress(t, os.WriteFile(filepath.Join(f.worktreeDir(), p), []byte("preserved old transport"), 0600))
			}
			req.Trigger.CommentID++

			r := f.execute(req)
			if calls != 3 || r.Succeeded() || r.Err == nil || !strings.Contains(r.Err.Error(), "current parent") {
				t.Fatalf("stale fallback result=%+v calls=%d logs=%s", r, calls, f.logs)
			}
			assertPlanUnready(t, f)
		})
	}
}

func TestReadPlanArtifactRejectsUnsafePaths(t *testing.T) {
	for _, scenario := range []string{"escape", "absolute", "file symlink", "directory symlink", "oversize"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			root, err := os.OpenRoot(dir)
			mustProgress(t, err)
			defer root.Close()
			outside := t.TempDir()
			mustProgress(t, os.WriteFile(filepath.Join(outside, "data"), []byte("evidence"), 0600))
			path := "data"
			limit := int64(100)
			switch scenario {
			case "escape":
				path = "../data"
			case "absolute":
				path = filepath.Join(outside, "data")
			case "file symlink":
				mustProgress(t, os.Symlink(filepath.Join(outside, "data"), filepath.Join(dir, path)))
			case "directory symlink":
				mustProgress(t, os.Symlink(outside, filepath.Join(dir, "linked")))
				path = "linked/data"
			case "oversize":
				mustProgress(t, os.WriteFile(filepath.Join(dir, path), []byte("evidence"), 0600))
				limit = 2
			}
			if data, err := readPlanArtifact(root, path, limit); err == nil {
				t.Fatalf("unsafe artifact read: %q", data)
			}
		})
	}
}

func TestPlanNewColdAttemptClearsStaleTransportAndReadiness(t *testing.T) {
	var f *fixture
	calls := 0
	var old []byte
	var priorAttempt string
	var contextPath string
	runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		if calls == 1 {
			writeCompletedProposal(t, req)
			var err error
			old, err = os.ReadFile(filepath.Join(req.WorktreePath, req.PlanOutcomePath))
			mustProgress(t, err)
			priorAttempt = req.PlanAttempt
			contextPath = req.PlanContextPath
		} else {
			assertPlanUnready(t, f)
			if req.PlanAttempt == priorAttempt || req.Path == agent.PathResume {
				t.Fatal("cold attempt reused binding")
			}
			for _, p := range []string{req.PlanOutcomePath, req.PlanProposalPath} {
				if _, err := os.Stat(filepath.Join(req.WorktreePath, p)); !os.IsNotExist(err) {
					t.Fatalf("stale transport survived preparation: %s", p)
				}
			}
			writePlanOutcome(t, req, "discussion")
			writePlanContext(t, f, req)
		}
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.opts.Runners = RunnerSet{agent.Kind(store.AgentCodex): runner}
	forge := setupOutcomeForge(t, f)
	req := planRequest(f)
	req.Agent = agent.Kind(store.AgentCodex)
	first := f.execute(req)
	if !first.Succeeded() {
		t.Fatalf("initial completed plan: %+v", first)
	}
	mustProgress(t, os.WriteFile(filepath.Join(f.worktreeDir(), planbrief.OutcomeFileName), old, 0600))
	mustProgress(t, os.WriteFile(filepath.Join(f.worktreeDir(), planbrief.ProposalFileName), []byte("stale"), 0600))
	req.Trigger.CommentID++
	second := f.execute(req)
	if !second.Succeeded() {
		t.Fatalf("ready to discussion: %+v logs=%s", second, f.logs)
	}
	assertPlanUnready(t, f)
	if len(forge.children) != 1 {
		t.Fatal("discussion added publication")
	}
	data, err := os.ReadFile(filepath.Join(f.worktreeDir(), contextPath))
	mustProgress(t, err)
	if !strings.Contains(string(data), "Which default?") {
		t.Fatal("discussion context missing")
	}
}

func TestPlanReadinessInvalidatedBeforeExternalTeamCapture(t *testing.T) {
	runner := newStubRunner(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Fatal("unavailable team reached subprocess")
		return nil, nil
	})
	f := newFixture(t, runner)
	setupOutcomeForge(t, f)
	mustProgress(t, f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: f.session.TaskRef, Generation: 1, State: "planning"}))
	mustProgress(t, f.db.SetManagedFeatureReady(t.Context(), f.session.TaskRef, 1, true))
	repo := f.repoRow()
	repo.ExternalTeam = "missing-team"
	_, err := f.db.UpsertRepo(t.Context(), repo)
	mustProgress(t, err)
	_, err = Execute(t.Context(), f.opts, planRequest(f))
	if err == nil || !strings.Contains(err.Error(), "external team") {
		t.Fatalf("expected retryable team preparation failure: %v", err)
	}
	assertPlanUnready(t, f)
}

func TestPlanArtifactRejectsChangedPinnedSnapshot(t *testing.T) {
	for _, scenario := range []string{"parent replacement", "inode replacement", "content change"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			mustProgress(t, os.Mkdir(filepath.Join(dir, "session"), 0700))
			path := filepath.Join(dir, "session", "evidence")
			mustProgress(t, os.WriteFile(path, []byte("original"), 0600))
			root, err := os.OpenRoot(dir)
			mustProgress(t, err)
			defer root.Close()
			artifact, err := openPlanArtifact(root, "session/evidence", 100)
			mustProgress(t, err)
			defer artifact.dir.Close()
			switch scenario {
			case "parent replacement":
				mustProgress(t, os.Rename(filepath.Join(dir, "session"), filepath.Join(dir, "old")))
				mustProgress(t, os.Mkdir(filepath.Join(dir, "session"), 0700))
				mustProgress(t, os.WriteFile(path, []byte("original"), 0600))
			case "inode replacement":
				mustProgress(t, os.Rename(path, path+".old"))
				mustProgress(t, os.WriteFile(path, []byte("original"), 0600))
			case "content change":
				mustProgress(t, os.WriteFile(path, []byte("modified"), 0600))
			}
			if err := artifact.unchanged(root, 100); err == nil {
				t.Fatal("unstable descriptor accepted")
			}
		})
	}
}
