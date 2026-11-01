package turn

import (
	"context"
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
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

const flowSessionDir = ".aiteam/validate-session"

// flowSlots is a run-slot pool of fixed size.
type flowSlots struct {
	mu   sync.Mutex
	free int
	took int
}

func (s *flowSlots) TryAcquire(context.Context) (func(), bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.free <= 0 {
		return nil, false, nil
	}
	s.free--
	s.took++
	return func() { s.mu.Lock(); s.free++; s.mu.Unlock() }, true, nil
}

// flowReviewers is a fake reviewer pool. It records every start.
type flowReviewers struct {
	mu        sync.Mutex
	started   []ValidateReviewRequest
	retained  map[string]*agentresult.Result // by attempt: results a lost reply left behind
	run       func(ValidateReviewRequest) (*agentresult.Result, error)
	recovered []string
}

func (r *flowReviewers) Run(_ context.Context, req ValidateReviewRequest) (*agentresult.Result, error) {
	r.mu.Lock()
	r.started = append(r.started, req)
	r.mu.Unlock()
	if r.run != nil {
		return r.run(req)
	}
	return goodReview(req), nil
}

func (r *flowReviewers) Recover(_ context.Context, attempt string) (*agentresult.Result, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recovered = append(r.recovered, attempt)
	res, ok := r.retained[attempt]
	return res, ok, nil
}

func (r *flowReviewers) roles() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, s := range r.started {
		out = append(out, s.Role)
	}
	return out
}

// goodReview answers every required finding: a criterion is satisfied only when
// a passed suite command can be cited, otherwise it is unverified.
func goodReview(req ValidateReviewRequest) *agentresult.Result {
	var passed []string
	for _, e := range req.Evidence {
		if e.State == store.SuitePassed {
			passed = append(passed, e.ID)
		}
	}
	finding := func(name, class string, criterion bool) agent.ContributionFinding {
		f := agent.ContributionFinding{Name: name, Classification: class}
		if len(passed) > 0 {
			f.Status, f.Evidence = "satisfied", []agent.EvidenceExplanation{{Summary: "A passed suite command covers it.", CheckIDs: passed[:1]}}
		} else {
			f.Status, f.Evidence = "unverified", []agent.EvidenceExplanation{{Summary: "No passed command proves it.", CheckIDs: []string{}}}
		}
		return f
	}
	var stories []agentresult.AssessmentStory
	for _, s := range req.Stories {
		a := agentresult.AssessmentStory{Story: s.Key, SnapshotID: req.SnapshotID, Criteria: []agent.ContributionFinding{}, Reviews: []agent.ContributionFinding{}}
		switch req.Role {
		case agent.ReviewRoleAcceptance:
			for _, c := range s.Criteria {
				a.Criteria = append(a.Criteria, finding(c, "blocking", true))
			}
			a.Reviews = append(a.Reviews, finding("plan", "advisory", false))
		case agent.ReviewRoleCode:
			a.Reviews = append(a.Reviews, finding("code", "advisory", false))
		case agent.ReviewRoleArchitecture:
			a.Reviews = append(a.Reviews, finding("architecture", "advisory", false))
		}
		stories = append(stories, a)
	}
	return &agentresult.Result{Outcome: "completed", Comment: "Reviewed " + req.Role + ".", Payload: agentresult.Payload{Validate: &agentresult.Validate{Stories: stories}}}
}

// flowFixture drives the real Validate graph with a fake suite runner, a fake
// repair agent and fake reviewers over a real git worktree.
type flowFixture struct {
	t        *testing.T
	db       *store.DB
	opts     Options
	repo     *gittest.Repo
	wt       *git.Worktree
	base     string
	profile  *graphexec.Profile
	instance store.GraphInstance
	registry ActivityRegistry
	clock    time.Time

	// suite
	suiteCommands []string // commit under test per command run
	suiteExit     func(commit string) int
	suiteRepo     *suiteRepoFake

	// repair agent
	agentRuns int
	agent     func(round int, f *flowFixture) (agentresult.ValidateRepair, ActivityResult)

	slots     *flowSlots
	reviewers *flowReviewers
	// reporterRun answers the reporter step; nil gives no summary.
	reporterRun  func(ValidateReporterRequest) (*agentresult.Result, error)
	reporterReqs []ValidateReporterRequest
	stories      []ValidateStory
	delivery     ValidateDelivery
	b            ValidateFlowBindings
}

func newFlowFixture(t *testing.T) *flowFixture {
	t.Helper()
	root := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(root, "litefactory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repoRow, err := db.UpsertRepo(t.Context(), store.Repo{Path: root, Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(root, "worktrees")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repoRow, 497)
	if err != nil {
		t.Fatal(err)
	}
	f := &flowFixture{t: t, db: db, clock: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC), slots: &flowSlots{free: 3}, reviewers: &flowReviewers{retained: map[string]*agentresult.Result{}}}
	f.opts = Options{DB: db}

	f.repo = gittest.New(t)
	f.repo.WriteFile("app/app.go", "package app\n\nfunc Total() int { return 9 }\n")
	f.repo.WriteFile("app/app_test.go", "package app\n\nimport \"testing\"\n\nfunc TestTotal(t *testing.T) {\n\tif Total() != 10 {\n\t\tt.Fatal(\"total\")\n\t}\n}\n")
	f.base = f.repo.Commit("add app")
	f.repo.Git("push", "--quiet", "origin", gittest.DefaultBranch)
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	clone, err := binary.Open(t.Context(), f.repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	ensured, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{Dir: filepath.Join(root, "wt"), Branch: "lf/497-validate", BaseBranch: gittest.DefaultBranch})
	if err != nil {
		t.Fatal(err)
	}
	f.wt = ensured.Worktree
	if err := os.MkdirAll(filepath.Join(f.wt.Path(), flowSessionDir), 0o700); err != nil {
		t.Fatal(err)
	}
	head, err := f.wt.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f.base = head

	f.profile = captureValidate(t).Profile
	def, err := db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: f.profile.Team, Mode: "validate", ProfileVersion: f.profile.ProfileVersion, Path: "validate.dot", Digest: f.profile.Digest, CreatedAt: f.clock})
	if err != nil {
		t.Fatal(err)
	}
	f.instance, err = db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: f.profile.Start, StartedAt: f.clock, NextDueAt: f.clock})
	if err != nil {
		t.Fatal(err)
	}

	f.suiteRepo = &suiteRepoFake{recipe: []byte(suiteRecipe), trees: []string{"tree-a"}}
	f.suiteExit = func(string) int { return 0 }
	f.stories = []ValidateStory{{Key: "story-002", Title: "Repair", Revision: strings.Repeat("ab", 32), Criteria: []string{"AC-001", "AC-002"}}}
	f.register()
	return f
}

func (f *flowFixture) register() {
	t := f.t
	var specs graphexec.Registry

	// The suite step is the real story-001 activity with a fake runner; it asks
	// for the commit to test the way the runtime will: the newest kept repair, or
	// the commit under review.
	suiteBindings := ValidateSuiteBindings{
		DB: f.db, LockDir: t.TempDir(), Repo: f.suiteRepo, Now: func() time.Time { return f.clock },
		Bind: func(ctx context.Context, _ Options, req ActivityRequest) (ValidateSuiteAuthority, error) {
			commit, err := f.db.LatestKeptRepairCommit(ctx, req.InstanceID)
			if err != nil {
				return ValidateSuiteAuthority{Commit: f.base}, nil //nolint:nilerr // no kept repair: test the commit under review
			}
			return ValidateSuiteAuthority{Commit: commit}, nil
		},
		Stopped: func(context.Context, string) (bool, error) { return true, nil },
		Run: func(ctx context.Context, _ string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
			commit := f.currentSuiteCommit(ctx)
			f.suiteCommands = append(f.suiteCommands, commit)
			res := bugchecks.CommandResult{Started: f.clock, Ended: f.clock, CleanupConfirmed: true, CaptureComplete: true, ProcessIdentity: "id-" + c.Argv[0], Output: "output of " + c.Argv[0]}
			_ = started(res)
			code := 0
			if c.Argv[0] == "full-one" {
				code = f.suiteExit(commit)
			}
			res.Exit = &code
			switch code {
			case 0:
				res.Outcome = "passed"
			case 1:
				res.Outcome, res.Output = "failed", "--- FAIL: TestTotal\n    total: expected 10, got 9"
			default:
				res.Outcome, res.Reason = "blocked", "check infrastructure failure"
			}
			return res
		},
	}
	if err := RegisterValidateSuiteActivity(&specs, &f.registry, suiteBindings); err != nil {
		t.Fatal(err)
	}
	f.b = ValidateFlowBindings{
		Profile:  f.profile,
		Worktree: func() (*git.Worktree, error) { return f.wt, nil },
		Session:  func() (string, error) { return flowSessionDir, nil },
		KeepChange: func(ctx context.Context, summary string) (string, error) {
			r, err := f.wt.Commit(ctx, git.CommitOptions{Summary: summary, Task: "github:acme/api#497", Mode: "validate", Run: 1, Author: git.Author{Name: "Factory", Email: "factory@example.com"}})
			if err != nil {
				return "", err
			}
			return r.Head, nil
		},
		Scope: func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error) {
			return agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: "validate", WorkflowVersion: store.WorkflowSeparated}, nil
		},
		Stories:   func(context.Context, Options, ActivityRequest) ([]ValidateStory, error) { return f.stories, nil },
		Repo:      f.suiteRepo,
		Reviewers: f.reviewers,
		Reporter:  flowReporter{f},
		Slots:     f.slots,
		Delivery:  f.delivery,
		Now:       func() time.Time { return f.clock },
	}
	if err := RegisterValidateFlow(&specs, &f.registry, f.b); err != nil {
		t.Fatal(err)
	}
	if err := f.registry.Register(ResearchAgentAction, flowAgent{f}); err != nil {
		t.Fatal(err)
	}
}

// currentSuiteCommit is the commit the running suite command belongs to.
func (f *flowFixture) currentSuiteCommit(ctx context.Context) string {
	run, err := f.db.RunningSuiteRun(ctx)
	if err != nil {
		f.t.Fatalf("no running suite: %v", err)
	}
	return run.Commit
}

// flowAgent is the fake repair agent.
type flowAgent struct{ f *flowFixture }

func (a flowAgent) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	f := a.f
	f.agentRuns++
	if f.agent == nil {
		return ActivityResult{Outcome: "failed", Diagnostic: "no agent"}, nil
	}
	report, res := f.agent(f.agentRuns, f)
	if res.Outcome != "" {
		return res, nil
	}
	node := agentresult.ResearchNode{Version: 1, Response: report.Response, Result: agentresult.Result{Outcome: "completed", Comment: "Repair.", Payload: agentresult.Payload{Validate: &agentresult.Validate{Repair: &report}}}}
	if report.Response == "blocked" {
		node.Result.Outcome, node.Result.Reason = "blocked", report.Reason
	}
	raw, err := json.Marshal(node)
	if err != nil {
		return ActivityResult{}, err
	}
	return ActivityResult{Outcome: "success", Kind: store.GraphOutputJSON, Output: raw}, nil
}

// write puts a file in the Validate worktree.
func (f *flowFixture) write(rel, content string) {
	f.t.Helper()
	full := filepath.Join(f.wt.Path(), rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *flowFixture) head() string {
	f.t.Helper()
	h, err := f.wt.RevParse(f.t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		f.t.Fatal(err)
	}
	return h
}

// step runs the instance's current node once.
func (f *flowFixture) step() (GraphNodeResult, error) {
	f.clock = f.clock.Add(time.Second)
	return ExecuteNodeAction(f.t.Context(), f.opts, GraphNodeRequest{Profile: f.profile, Activities: f.registry, InstanceID: f.instance.ID, RunID: 0, At: f.clock})
}

// runToEnd runs steps until the part of the graph before the owner's turn is
// over (the instance reaches the resolve step, or leaves its running state) and
// returns the nodes it visited, in order.
func (f *flowFixture) runToEnd() []string {
	f.t.Helper()
	var visited []string
	for range 80 {
		inst, err := f.db.GetGraphInstance(f.t.Context(), f.instance.ID)
		if err != nil {
			f.t.Fatal(err)
		}
		if inst.Status != store.GraphRunning || inst.CurrentNode == "owner_resolve" {
			return visited
		}
		visited = append(visited, inst.CurrentNode)
		res, err := f.step()
		if err != nil {
			f.t.Fatalf("step %s: %v (%s)", inst.CurrentNode, err, res.Activity.Diagnostic)
		}
	}
	f.t.Fatalf("the graph did not end: %v", visited)
	return visited
}

func (f *flowFixture) final() string {
	inst, err := f.db.GetGraphInstance(f.t.Context(), f.instance.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	if inst.CurrentNode == "owner_resolve" {
		// The end Validate's own part reached is the outcome step's answer.
		attempts, err := f.db.ListGraphNodeAttempts(f.t.Context(), f.instance.ID)
		if err != nil {
			f.t.Fatal(err)
		}
		for i := len(attempts) - 1; i >= 0; i-- {
			if attempts[i].Node == "outcome" {
				return attempts[i].Outcome
			}
		}
	}
	return inst.CurrentNode
}

func (f *flowFixture) outcome(commit string) store.ValidateOutcome {
	f.t.Helper()
	o, err := f.db.GetValidateOutcome(f.t.Context(), f.instance.ID, commit)
	if err != nil {
		f.t.Fatalf("no outcome for %s: %v", commit, err)
	}
	return o
}

func visits(nodes []string, name string) int {
	n := 0
	for _, v := range nodes {
		if v == name {
			n++
		}
	}
	return n
}

func joinNodes(nodes []string) string { return strings.Join(nodes, ">") }

var _ = fmt.Sprint

func (f *flowFixture) bindings() ValidateFlowBindings { return f.b }

// reviewRequest reserves an attempt on the review node and returns the request
// that runs it, for a test that calls the activity directly.
func (f *flowFixture) reviewRequest() ActivityRequest {
	f.t.Helper()
	attempt, err := f.db.StartGraphNodeAttempt(f.t.Context(), store.GraphAttemptStart{InstanceID: f.instance.ID, Node: "review", Limit: 32, StartedAt: f.clock})
	if err != nil {
		f.t.Fatal(err)
	}
	return ActivityRequest{Mode: agent.ModeValidate, Node: "review", InstanceID: f.instance.ID, NodeAttemptID: attempt.ID}
}
