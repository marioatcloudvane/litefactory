package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

const suiteCommit = "3333333333333333333333333333333333333333"

const suiteRecipe = `version: 1
phases:
  regression:
    commands:
      - argv: ["check-regression"]
        cwd: "."
        timeout_seconds: 5
  focused:
    commands:
      - argv: ["check-focused"]
        cwd: "."
        timeout_seconds: 5
  full:
    commands:
      - argv: ["full-one"]
        cwd: "."
        timeout_seconds: 5
      - argv: ["full-two"]
        cwd: "."
        timeout_seconds: 5
      - argv: ["full-three"]
        cwd: "."
        timeout_seconds: 5
`

type suiteRepoFake struct {
	recipe    []byte
	recipeErr error
	trees     []string // digests returned by successive Observe calls
	observed  int
	begun     int
	closed    int
}

func (r *suiteRepoFake) Begin(context.Context, string) (ValidateSuiteScratch, error) {
	r.begun++
	return &suiteScratchFake{r: r}, nil
}

type suiteScratchFake struct{ r *suiteRepoFake }

func (s *suiteScratchFake) Path() string { return "/scratch" }
func (s *suiteScratchFake) Recipe(context.Context) ([]byte, error) {
	return s.r.recipe, s.r.recipeErr
}
func (s *suiteScratchFake) Observe(context.Context) (string, error) {
	i := s.r.observed
	s.r.observed++
	if i >= len(s.r.trees) {
		i = len(s.r.trees) - 1
	}
	return s.r.trees[i], nil
}
func (s *suiteScratchFake) Close(context.Context) error { s.r.closed++; return nil }

type suiteFixture struct {
	t        *testing.T
	db       *store.DB
	repo     *suiteRepoFake
	lockDir  string
	commands []string
	exits    map[string]int // command name to exit code, default 0
	stopped  func(string) (bool, error)
	b        ValidateSuiteBindings
}

var suiteClock = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

func newSuiteFixture(t *testing.T) *suiteFixture {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "litefactory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	f := &suiteFixture{t: t, db: db, repo: &suiteRepoFake{recipe: []byte(suiteRecipe), trees: []string{"tree-a"}}, lockDir: t.TempDir(), exits: map[string]int{}}
	f.b = ValidateSuiteBindings{
		DB: db, LockDir: f.lockDir, Repo: f.repo,
		Now: func() time.Time { return suiteClock },
		Bind: func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error) {
			return ValidateSuiteAuthority{Commit: suiteCommit}, nil
		},
		Run: func(_ context.Context, _ string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
			f.commands = append(f.commands, c.Argv[0])
			res := bugchecks.CommandResult{Started: suiteClock, Ended: suiteClock, CleanupConfirmed: true, CaptureComplete: true, ProcessIdentity: "identity-" + c.Argv[0], Output: "output of " + c.Argv[0]}
			if err := started(res); err != nil {
				res.Outcome, res.Reason = "blocked", err.Error()
				return res
			}
			code := f.exits[c.Argv[0]]
			res.Exit = &code
			switch code {
			case 0:
				res.Outcome = "passed"
			case 1:
				res.Outcome = "failed"
			default:
				res.Outcome, res.Reason = "blocked", "check infrastructure failure or signal"
			}
			return res
		},
	}
	f.b.Stopped = func(_ context.Context, id string) (bool, error) {
		if f.stopped == nil {
			return true, nil
		}
		return f.stopped(id)
	}
	return f
}

func (f *suiteFixture) exec(instance int64) (ActivityResult, validateSuiteOutput, error) {
	f.t.Helper()
	res, err := ValidateSuiteActivity{f.b}.Execute(f.t.Context(), Options{}, ActivityRequest{Mode: agent.ModeValidate, InstanceID: instance, Node: "suite"})
	var out validateSuiteOutput
	if err == nil {
		if e := json.Unmarshal(res.Output, &out); e != nil {
			f.t.Fatal(e)
		}
	}
	return res, out, err
}

func TestValidateSuiteSpecIsRegistered(t *testing.T) {
	f := newSuiteFixture(t)
	var specs graphexec.Registry
	var acts ActivityRegistry
	if err := RegisterValidateSuiteActivity(&specs, &acts, f.b); err != nil {
		t.Fatal(err)
	}
	if _, ok := specs.Resolve(graphexec.ValidateSuiteAction); !ok {
		t.Fatal("suite action is not registered")
	}
	if err := RegisterValidateSuiteActivity(&specs, &acts, ValidateSuiteBindings{}); err == nil {
		t.Fatal("registration without bindings must fail")
	}
}

func TestValidateSuitePassesInRecipeOrder(t *testing.T) { // AC-001, AC-003
	f := newSuiteFixture(t)
	res, out, err := f.exec(1)
	if err != nil || res.Outcome != "passed" || out.Outcome != "passed" {
		t.Fatalf("got %q %+v %v", res.Outcome, out, err)
	}
	if strings.Join(f.commands, ",") != "full-one,full-two,full-three" {
		t.Fatalf("commands %v", f.commands)
	}
	run, err := f.db.LatestSuiteRun(t.Context(), 1, suiteCommit)
	if err != nil || run.State != store.SuitePassed || run.Commit != suiteCommit || run.TreeBefore != "tree-a" || run.TreeAfter != "tree-a" || run.RecipeDigest != config.BugRecipeDigest([]byte(suiteRecipe)) {
		t.Fatalf("run %+v %v", run, err)
	}
	if out.RecipeDigest != run.RecipeDigest || out.TreeBefore != "tree-a" || out.TreeAfter != "tree-a" || out.Commit != suiteCommit {
		t.Fatalf("output %+v", out)
	}
	cmds, err := f.db.SuiteCommands(t.Context(), run.ID)
	if err != nil || len(cmds) != 3 || cmds[0].ProcessIdentity != "identity-full-one" || cmds[2].State != store.SuitePassed {
		t.Fatalf("commands %+v %v", cmds, err)
	}
	if f.repo.closed != 1 || f.repo.begun != 1 {
		t.Fatalf("scratch begun %d closed %d", f.repo.begun, f.repo.closed)
	}
}

func TestValidateSuiteStopsAtFirstFailure(t *testing.T) { // AC-001
	f := newSuiteFixture(t)
	f.exits["full-two"] = 1
	res, out, err := f.exec(1)
	if err != nil || res.Outcome != "failed" || out.Outcome != "failed" {
		t.Fatalf("got %q %+v %v", res.Outcome, out, err)
	}
	if strings.Join(f.commands, ",") != "full-one,full-two" {
		t.Fatalf("commands %v", f.commands)
	}
	if f.repo.closed != 1 {
		t.Fatal("scratch not closed")
	}
}

func TestValidateSuiteBlockedCases(t *testing.T) { // AC-002
	noFull := strings.Replace(suiteRecipe, "full:", "full-ignored:", 1)
	cases := map[string]func(f *suiteFixture){
		"missing recipe":  func(f *suiteFixture) { f.repo.recipe, f.repo.recipeErr = nil, git.ErrBugCheckRecipe },
		"invalid recipe":  func(f *suiteFixture) { f.repo.recipe = []byte("not: [a recipe") },
		"empty full":      func(f *suiteFixture) { f.repo.recipe = []byte(noFull) },
		"exit two":        func(f *suiteFixture) { f.exits["full-one"] = 2 },
		"snapshot drift":  func(f *suiteFixture) { f.repo.trees = []string{"tree-a", "tree-b"} },
		"drift on failed": func(f *suiteFixture) { f.repo.trees = []string{"tree-a", "tree-b"}; f.exits["full-one"] = 1 },
		"no sandbox": func(f *suiteFixture) {
			f.b.Run = nil
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newSuiteFixture(t)
			setup(f)
			res, out, err := f.exec(1)
			if err != nil || res.Outcome != "blocked" || out.Reason == "" {
				t.Fatalf("got %q %+v %v", res.Outcome, out, err)
			}
			if name == "exit two" && strings.Join(f.commands, ",") != "full-one" {
				t.Fatalf("commands %v", f.commands)
			}
			if f.repo.closed != 1 {
				t.Fatal("scratch not closed")
			}
			run, _ := f.db.LatestSuiteRun(t.Context(), 1, suiteCommit)
			if run.State != store.SuiteBlocked {
				t.Fatalf("state %s", run.State)
			}
		})
	}
}

func TestValidateSuiteUncleanCommandNeverPasses(t *testing.T) { // AC-002
	f := newSuiteFixture(t)
	run := f.b.Run
	f.b.Run = func(ctx context.Context, w string, c config.BugCheckCommand, s func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		r := run(ctx, w, c, s)
		r.CleanupConfirmed = false
		return r
	}
	res, _, err := f.exec(1)
	if err != nil || res.Outcome != "blocked" {
		t.Fatalf("got %q %v", res.Outcome, err)
	}
}

func TestValidateSuiteBusyLockRetriesThenRuns(t *testing.T) { // AC-004
	f := newSuiteFixture(t)
	unlock, err := f.db.LockSuite(t.Context(), f.lockDir)
	if err != nil {
		t.Fatal(err)
	}
	res, out, err := f.exec(2)
	if err != nil || res.Outcome != "retry" || out.Outcome != "retry" {
		t.Fatalf("got %q %+v %v", res.Outcome, out, err)
	}
	if len(f.commands) != 0 || f.repo.begun != 0 {
		t.Fatal("a busy lock must start nothing")
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	res, _, err = f.exec(2)
	if err != nil || res.Outcome != "passed" || len(f.commands) != 3 {
		t.Fatalf("got %q %v %v", res.Outcome, err, f.commands)
	}
}

func TestValidateSuiteReplaysFinishedResult(t *testing.T) { // AC-005
	for _, exit := range []int{0, 1, 2} {
		f := newSuiteFixture(t)
		f.exits["full-one"] = exit
		first, _, err := f.exec(3)
		if err != nil {
			t.Fatal(err)
		}
		ran := len(f.commands)
		second, _, err := f.exec(3)
		if err != nil || second.Outcome != first.Outcome || string(second.Output) != string(first.Output) {
			t.Fatalf("replay differs: %q %q %v", first.Output, second.Output, err)
		}
		if len(f.commands) != ran || f.repo.begun != 1 {
			t.Fatal("replay ran the suite again")
		}
		// A different graph instance is a different key; only a pass carries over.
		want := 2
		if exit == 0 {
			want = 1
		}
		if _, _, err := f.exec(4); err != nil || f.repo.begun != want {
			t.Fatalf("other instance after exit %d: begun %d, %v", exit, f.repo.begun, err)
		}
	}
}

func TestValidateSuiteReusesPassOfAnotherInstance(t *testing.T) {
	f := newSuiteFixture(t)
	_, first, err := f.exec(3)
	if err != nil || first.Outcome != "passed" {
		t.Fatalf("first run: %+v %v", first, err)
	}
	ran := len(f.commands)
	res, out, err := f.exec(4)
	if err != nil || res.Outcome != "passed" || out.Commit != suiteCommit || out.RecipeDigest != first.RecipeDigest || out.RunID == first.RunID {
		t.Fatalf("reuse: %+v %+v %v", res, out, err)
	}
	if len(f.commands) != ran || f.repo.begun != 1 {
		t.Fatal("reuse ran the suite again")
	}
	src, err := f.db.SuiteCommands(t.Context(), first.RunID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.db.SuiteCommands(t.Context(), out.RunID)
	if err != nil || len(got) != len(src) || len(got) == 0 {
		t.Fatalf("copied commands: %d of %d, %v", len(got), len(src), err)
	}
	for i := range got {
		if got[i].State != src[i].State || got[i].OutputDigest != src[i].OutputDigest || strings.Join(got[i].Argv, " ") != strings.Join(src[i].Argv, " ") {
			t.Fatalf("command %d differs: %+v %+v", i, got[i], src[i])
		}
	}
	// The copy replays for its own instance like a run of its own.
	if _, again, err := f.exec(4); err != nil || again.RunID != out.RunID {
		t.Fatalf("replay of reuse: %+v %v", again, err)
	}
}

func claimStaleRun(t *testing.T, f *suiteFixture, instance int64, identity string) store.SuiteRun {
	t.Helper()
	run, err := f.db.BeginSuiteRun(t.Context(), store.SuiteRun{InstanceID: instance, Node: "suite", Commit: suiteCommit, RecipeDigest: "old", TreeBefore: "t", StartedAt: suiteClock})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.ClaimSuiteCommand(t.Context(), store.SuiteCommand{RunID: run.ID, Position: 0, Argv: []string{"full-one"}, StartedAt: suiteClock, ProcessIdentity: identity}); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestValidateSuiteRestartConfirmedStoppedInterruptsOld(t *testing.T) { // AC-006
	f := newSuiteFixture(t)
	old := claimStaleRun(t, f, 5, "old-identity")
	var probed []string
	f.stopped = func(id string) (bool, error) { probed = append(probed, id); return true, nil }
	res, _, err := f.exec(5)
	if err != nil || res.Outcome != "passed" {
		t.Fatalf("got %q %v", res.Outcome, err)
	}
	if len(probed) != 1 || probed[0] != "old-identity" {
		t.Fatalf("probed %v", probed)
	}
	got, err := f.db.GetSuiteRun(t.Context(), old.ID)
	if err != nil || got.State != store.SuiteInterrupted {
		t.Fatalf("old run %+v %v", got, err)
	}
	latest, _ := f.db.LatestSuiteRun(t.Context(), 5, suiteCommit)
	if latest.ID == old.ID || latest.State != store.SuitePassed {
		t.Fatalf("new attempt missing: %+v", latest)
	}
	cmds, _ := f.db.SuiteCommands(t.Context(), old.ID)
	if cmds[0].State != store.SuiteInterrupted {
		t.Fatalf("old command %+v", cmds[0])
	}
}

func TestValidateSuiteRestartUnconfirmedStopBlocks(t *testing.T) { // AC-006
	for name, probe := range map[string]func(string) (bool, error){
		"still running": func(string) (bool, error) { return false, nil },
		"probe failed":  func(string) (bool, error) { return false, errors.New("unreadable") },
	} {
		t.Run(name, func(t *testing.T) {
			f := newSuiteFixture(t)
			old := claimStaleRun(t, f, 6, "old-identity")
			f.stopped = probe
			res, out, err := f.exec(7)
			if err != nil || res.Outcome != "blocked" || out.Reason == "" {
				t.Fatalf("got %q %+v %v", res.Outcome, out, err)
			}
			if len(f.commands) != 0 || f.repo.begun != 0 {
				t.Fatal("no new suite may start")
			}
			got, _ := f.db.GetSuiteRun(t.Context(), old.ID)
			if got.State != store.SuiteRunning {
				t.Fatalf("old run must stay unresolved, is %s", got.State)
			}
		})
	}
}

func TestValidateSuiteRestartWithoutIdentityBlocks(t *testing.T) { // AC-006
	f := newSuiteFixture(t)
	claimStaleRun(t, f, 8, "")
	f.b.Stopped = bugchecks.ProcessStopped
	res, _, err := f.exec(8)
	if err != nil || res.Outcome != "blocked" || len(f.commands) != 0 {
		t.Fatalf("got %q %v", res.Outcome, err)
	}
}

func TestValidateSuiteCancelledIsRetainedAndRetried(t *testing.T) {
	f := newSuiteFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	inner := f.b.Run
	f.b.Run = func(c context.Context, w string, cmd config.BugCheckCommand, s func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		r := inner(c, w, cmd, s)
		cancel()
		r.Outcome, r.Reason = "cancelled", "context canceled"
		return r
	}
	if _, err := (ValidateSuiteActivity{f.b}).Execute(ctx, Options{}, ActivityRequest{Mode: agent.ModeValidate, InstanceID: 9, Node: "suite"}); err == nil {
		t.Fatal("a cancelled suite is an error")
	}
	run, _ := f.db.LatestSuiteRun(t.Context(), 9, suiteCommit)
	if run.State != store.SuiteCancelled {
		t.Fatalf("state %s", run.State)
	}
	f.b.Run = inner
	res, _, err := f.exec(9)
	if err != nil || res.Outcome != "passed" {
		t.Fatalf("rerun %q %v", res.Outcome, err)
	}
	if again, _ := f.db.LatestSuiteRun(t.Context(), 9, suiteCommit); again.Attempt != 2 {
		t.Fatalf("attempt %d", again.Attempt)
	}
}

func TestValidateSuiteRetainsBoundedTailAndDigest(t *testing.T) { // AC-007
	f := newSuiteFixture(t)
	big := strings.Repeat("x", store.MaxSuiteOutputTail*3) + "END"
	inner := f.b.Run
	f.b.Run = func(c context.Context, w string, cmd config.BugCheckCommand, s func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		r := inner(c, w, cmd, s)
		r.Output = big
		return r
	}
	if _, _, err := f.exec(10); err != nil {
		t.Fatal(err)
	}
	run, _ := f.db.LatestSuiteRun(t.Context(), 10, suiteCommit)
	cmds, _ := f.db.SuiteCommands(t.Context(), run.ID)
	c := cmds[0]
	if len(c.OutputTail) != store.MaxSuiteOutputTail || !strings.HasSuffix(c.OutputTail, "END") || !c.OutputTruncated || len(c.OutputDigest) != 64 {
		t.Fatalf("tail %d truncated %v digest %q", len(c.OutputTail), c.OutputTruncated, c.OutputDigest)
	}
}

func TestValidateSuiteRejectsBadAuthority(t *testing.T) {
	f := newSuiteFixture(t)
	f.b.Bind = func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error) {
		return ValidateSuiteAuthority{Commit: "main"}, nil
	}
	if _, _, err := f.exec(1); err == nil {
		t.Fatal("a branch name is not an exact commit")
	}
	if _, err := (ValidateSuiteActivity{f.b}).Execute(t.Context(), Options{}, ActivityRequest{Mode: agent.ModeImplement}); err == nil {
		t.Fatal("a non-Validate request must be refused")
	}
}

// The real git repository: the recipe comes from the tested commit, not from
// the working files, and the scratch worktree is gone afterwards.
func TestGitValidateSuiteReadsRecipeFromCommitAndRemovesWorktree(t *testing.T) { // AC-003, AC-007
	repo := gittest.New(t)
	repo.WriteFile(".litefactory/bug-checks.yaml", suiteRecipe)
	commit := repo.Commit("add recipe")
	repo.Git("push", "--quiet", "origin", gittest.DefaultBranch)
	// The working file now differs from the committed recipe.
	repo.WriteFile(".litefactory/bug-checks.yaml", strings.Replace(suiteRecipe, "full-one", "tampered", 1))

	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	clone, err := binary.Open(t.Context(), repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	scratchRoot := config.NewPaths(filepath.Join(t.TempDir(), "home")).ValidateScratchDir()
	f := newSuiteFixture(t)
	f.b.Repo = GitValidateSuiteRepo{Clone: clone, ClonePath: repo.Dir, Remote: "origin", ScratchRoot: scratchRoot}
	f.b.Bind = func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error) {
		return ValidateSuiteAuthority{Commit: commit}, nil
	}
	var worktree string
	inner := f.b.Run
	f.b.Run = func(c context.Context, w string, cmd config.BugCheckCommand, s func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		worktree = w
		for _, dir := range []string{scratchRoot, filepath.Dir(w)} {
			info, statErr := os.Stat(dir)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if info.Mode().Perm() != 0o700 {
				t.Errorf("%s mode = %o", dir, info.Mode().Perm())
			}
		}
		if _, err := os.Stat(filepath.Join(w, "README.md")); err != nil {
			t.Errorf("the scratch checkout is not a checkout: %v", err)
		}
		return inner(c, w, cmd, s)
	}
	res, out, err := f.exec(11)
	if err != nil || res.Outcome != "passed" {
		t.Fatalf("got %q %+v %v", res.Outcome, out, err)
	}
	if strings.Join(f.commands, ",") != "full-one,full-two,full-three" {
		t.Fatalf("the committed recipe must run, got %v", f.commands)
	}
	if out.RecipeDigest != config.BugRecipeDigest([]byte(suiteRecipe)) || out.Commit != commit || out.TreeBefore == "" || out.TreeBefore != out.TreeAfter {
		t.Fatalf("output %+v", out)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("scratch worktree still exists: %v", err)
	}
}

// A full disk is the host's failure: a suite command that exits 1 with
// "no space left on device" is recorded blocked, never failed, while an
// ordinary exit 1 still fails.
func TestValidateSuiteNoSpaceLeftIsBlocked(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		want         string
	}{
		{"enospc", "TempDir: mkdir /tmp/x: no space left on device", store.SuiteBlocked},
		{"ordinary", "FAIL: TestSomething", store.SuiteFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			launcher := filepath.Join(root, "sandbox")
			if err := os.WriteFile(launcher, []byte("#!/bin/sh\necho '"+tc.output+"'\nexit 1\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			worktree := filepath.Join(root, "worktree")
			if err := os.Mkdir(worktree, 0o700); err != nil {
				t.Fatal(err)
			}
			isolation := bugchecks.Isolation{Binary: launcher, PATH: "/usr/bin:/bin", Home: root, Revision: "operator-v1"}
			f := newSuiteFixture(t)
			f.b.Run = func(ctx context.Context, _ string, _ config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
				return isolation.Run(ctx, worktree, config.BugCheckCommand{Argv: []string{"/bin/echo", "test"}, TimeoutSeconds: 10}, started)
			}
			if _, _, err := f.exec(1); err != nil {
				t.Fatal(err)
			}
			run, err := f.db.LatestSuiteRun(t.Context(), 1, suiteCommit)
			if err != nil {
				t.Fatal(err)
			}
			if run.State == store.SuiteBlocked && strings.Contains(run.Reason, "retain command identity: operation not permitted") {
				t.Skip("host refuses process identity observation required by isolated command executor")
			}
			if run.State != tc.want {
				t.Fatalf("suite state %s (%s), want %s", run.State, run.Reason, tc.want)
			}
		})
	}
}
