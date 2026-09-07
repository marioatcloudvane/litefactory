package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/gittest"
)

// setupTeamUpdateWorktreeFixture builds the same fixture setupTeamStatusFixture
// does (a resolvable team index, one agent installed for claude via a real
// `team add`), but keeps the *gittest.Repo handle in scope so a test can add
// real sibling worktrees with git itself and mutate the fixture's source
// afterward. It mirrors TestTeamUpdate_SingleWorktreeScope's own setup, which
// exists for the same reason: a *gittest.Repo cannot be reconstructed from a
// bare directory string once discarded.
func setupTeamUpdateWorktreeFixture(t *testing.T) (repo *gittest.Repo, fixtureDir string) {
	t.Helper()
	const teamName = "reviewers"
	const agentKey = "reviewer"

	fixtureDir = newTeamIndexFixture(t, teamName, agentKey)
	newTeamHome(t, fixtureDir)
	repo = gittest.New(t)

	router, _, stderr := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "add", teamName,
		"--repo", repo.Dir,
		"--harness", "claude",
	})
	if code != ExitOK {
		t.Fatalf("seed team add: exit code = %d\nstderr: %s", code, stderr)
	}
	return repo, fixtureDir
}

// TestTeamUpdateAllWorktrees_UpdatesGitConfirmedSibling covers AC-1: with
// --all-worktrees, a real sibling worktree that "git worktree list" confirms
// is updated exactly as if `team update` had been run against it directly —
// its own Upstream-updated record is re-rendered and written.
func TestTeamUpdateAllWorktrees_UpdatesGitConfirmedSibling(t *testing.T) {
	repo, fixtureDir := setupTeamUpdateWorktreeFixture(t)

	siblingDir := filepath.Join(filepath.Dir(repo.Dir), "worktrees", "sibling")
	repo.Git("worktree", "add", "-b", "sibling-branch", siblingDir)

	// Seed the sibling's own .litefactory bookkeeping too: each worktree has
	// its own independent lockfile, so the sibling needs its own `team add`
	// before there is anything for `team update` to classify there.
	seedRouter, _, seedStderr := newTestRouter(t)
	code := seedRouter.Run(t.Context(), []string{
		"team", "add", "reviewers",
		"--repo", siblingDir,
		"--harness", "claude",
	})
	if code != ExitOK {
		t.Fatalf("seed sibling team add: exit code = %d\nstderr: %s", code, seedStderr)
	}

	mutateSource(t, "", fixtureDir)

	router, stdout, _ := newTestRouter(t)
	code = router.Run(t.Context(), []string{
		"team", "update", "--repo", repo.Dir, "--all-worktrees", "--json",
	})
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitOK, stdout)
	}

	var out teamUpdateAllJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
	}
	if len(out.Worktrees) != 2 {
		t.Fatalf("len(Worktrees) = %d, want 2 (primary + sibling): %+v", len(out.Worktrees), out.Worktrees)
	}

	byPath := make(map[string]teamUpdateWorktreeJSON, 2)
	for _, wt := range out.Worktrees {
		byPath[wt.Path] = wt
	}
	primary, ok := byPath[repo.Dir]
	if !ok {
		t.Fatalf("no report entry for primary worktree %s: %+v", repo.Dir, out.Worktrees)
	}
	sibling, ok := byPath[siblingDir]
	if !ok {
		t.Fatalf("no report entry for sibling worktree %s: %+v", siblingDir, out.Worktrees)
	}
	if sibling.Updated == 0 {
		t.Errorf("sibling Updated = %d, want > 0 (upstream-updated record should be applied)", sibling.Updated)
	}
	if sibling.Error != "" {
		t.Errorf("sibling Error = %q, want empty", sibling.Error)
	}

	body, err := os.ReadFile(filepath.Join(siblingDir, teamStatusStandalonePath))
	if err != nil {
		t.Fatalf("read sibling standalone file: %v", err)
	}
	if !containsBytes(body, "Review the updated code.") {
		t.Errorf("sibling worktree was not updated: %s", body)
	}

	// AC-1 covers both the sibling and "the current worktree" itself: with
	// --all-worktrees, the worktree the command was invoked against must be
	// updated too, not merely listed in the report.
	if primary.Updated == 0 {
		t.Errorf("primary Updated = %d, want > 0 (--all-worktrees must still update the worktree it was invoked against)", primary.Updated)
	}
	primaryBody, err := os.ReadFile(filepath.Join(repo.Dir, teamStatusStandalonePath))
	if err != nil {
		t.Fatalf("read primary standalone file: %v", err)
	}
	if !containsBytes(primaryBody, "Review the updated code.") {
		t.Errorf("primary worktree was not updated: %s", primaryBody)
	}
}

// TestTeamUpdateAllWorktrees_ExcludesStalePathSilently covers AC-2: a
// worktree whose directory has been removed from disk (so git itself marks
// its "git worktree list" entry prunable) is excluded from the run, not
// reported as an error and not touched.
func TestTeamUpdateAllWorktrees_ExcludesStalePathSilently(t *testing.T) {
	repo, _ := setupTeamUpdateWorktreeFixture(t)

	siblingDir := filepath.Join(filepath.Dir(repo.Dir), "worktrees", "stale")
	repo.Git("worktree", "add", "-b", "stale-branch", siblingDir)

	// Remove the directory out from under git without telling it: this is
	// exactly what leaves an entry in "git worktree list" marked prunable
	// rather than removing it from the registry outright.
	if err := os.RemoveAll(siblingDir); err != nil {
		t.Fatalf("remove sibling directory: %v", err)
	}

	router, stdout, _ := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "update", "--repo", repo.Dir, "--all-worktrees", "--json",
	})
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitOK, stdout)
	}

	var out teamUpdateAllJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
	}
	for _, wt := range out.Worktrees {
		if wt.Path == siblingDir {
			t.Fatalf("stale/removed worktree %s was reported at all, want silent exclusion: %+v", siblingDir, wt)
		}
	}
	if len(out.Worktrees) != 1 {
		t.Fatalf("len(Worktrees) = %d, want 1 (only the primary worktree): %+v", len(out.Worktrees), out.Worktrees)
	}
}

// TestTeamUpdateAllWorktrees_OneFailureDoesNotBlockOthers covers AC-4: a
// sibling worktree whose team index cannot be resolved (its own config.yaml
// is missing/corrupt) reports an error against that worktree specifically,
// while a second, healthy sibling is still updated in the same run.
func TestTeamUpdateAllWorktrees_OneFailureDoesNotBlockOthers(t *testing.T) {
	repo, fixtureDir := setupTeamUpdateWorktreeFixture(t)

	healthyDir := filepath.Join(filepath.Dir(repo.Dir), "worktrees", "healthy")
	repo.Git("worktree", "add", "-b", "healthy-branch", healthyDir)
	seedRouter, _, seedStderr := newTestRouter(t)
	code := seedRouter.Run(t.Context(), []string{
		"team", "add", "reviewers",
		"--repo", healthyDir,
		"--harness", "claude",
	})
	if code != ExitOK {
		t.Fatalf("seed healthy sibling team add: exit code = %d\nstderr: %s", code, seedStderr)
	}

	brokenDir := filepath.Join(filepath.Dir(repo.Dir), "worktrees", "broken")
	repo.Git("worktree", "add", "-b", "broken-branch", brokenDir)
	// A lockfile whose JSON does not parse makes team.LoadLockfile fail for
	// this worktree specifically, without touching the healthy sibling or the
	// primary worktree's own bookkeeping at all.
	litefactoryDir := filepath.Join(brokenDir, ".litefactory")
	if err := os.MkdirAll(litefactoryDir, 0o755); err != nil {
		t.Fatalf("create .litefactory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(litefactoryDir, "team.lock.json"), []byte("not valid json"), 0o600); err != nil {
		t.Fatalf("write broken lockfile: %v", err)
	}

	mutateSource(t, "", fixtureDir)

	router, stdout, stderr := newTestRouter(t)
	code = router.Run(t.Context(), []string{
		"team", "update", "--repo", repo.Dir, "--all-worktrees", "--json",
	})
	if code != ExitFailure {
		t.Fatalf("exit code = %d, want %d (the broken worktree's failure is reported)\nstdout: %s", code, ExitFailure, stdout)
	}
	// AC-4 requires the failure be reported against the broken worktree
	// specifically: the router prints the aggregated error to stderr, and it
	// must name the broken worktree's own path, not just say something failed
	// somewhere.
	if !containsBytes(stderr.Bytes(), brokenDir) {
		t.Errorf("stderr does not name the broken worktree %s specifically: %s", brokenDir, stderr)
	}
	if containsBytes(stderr.Bytes(), healthyDir) {
		t.Errorf("stderr names the healthy worktree %s as part of the failure, want only the broken one named: %s", healthyDir, stderr)
	}

	var out teamUpdateAllJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
	}
	if len(out.Worktrees) != 3 {
		t.Fatalf("len(Worktrees) = %d, want 3 (primary + healthy + broken): %+v", len(out.Worktrees), out.Worktrees)
	}

	byPath := make(map[string]teamUpdateWorktreeJSON, 3)
	for _, wt := range out.Worktrees {
		byPath[wt.Path] = wt
	}

	broken, ok := byPath[brokenDir]
	if !ok {
		t.Fatalf("no report entry for broken worktree %s: %+v", brokenDir, out.Worktrees)
	}
	if broken.Error == "" {
		t.Errorf("broken worktree Error = %q, want a non-empty explanation", broken.Error)
	}

	healthy, ok := byPath[healthyDir]
	if !ok {
		t.Fatalf("no report entry for healthy worktree %s: %+v", healthyDir, out.Worktrees)
	}
	if healthy.Error != "" {
		t.Errorf("healthy sibling Error = %q, want empty (its own update must not be blocked by the broken one)", healthy.Error)
	}
	if healthy.Updated == 0 {
		t.Errorf("healthy sibling Updated = %d, want > 0", healthy.Updated)
	}

	body, err := os.ReadFile(filepath.Join(healthyDir, teamStatusStandalonePath))
	if err != nil {
		t.Fatalf("read healthy sibling standalone file: %v", err)
	}
	if !containsBytes(body, "Review the updated code.") {
		t.Errorf("healthy sibling worktree was not updated: %s", body)
	}
}

// TestTeamUpdateAllWorktrees_DoesNotForceLocallyModifiedSibling covers the
// fourth T-43 case: a sibling worktree with a Locally-modified file is not
// force-updated by propagation. --all-worktrees applies the same refusal
// rule US-11 already applies in a single worktree — --force still has to be
// passed explicitly to overwrite it, and without --force the hand-edit
// survives untouched.
func TestTeamUpdateAllWorktrees_DoesNotForceLocallyModifiedSibling(t *testing.T) {
	repo, fixtureDir := setupTeamUpdateWorktreeFixture(t)

	siblingDir := filepath.Join(filepath.Dir(repo.Dir), "worktrees", "sibling")
	repo.Git("worktree", "add", "-b", "sibling-branch", siblingDir)
	seedRouter, _, seedStderr := newTestRouter(t)
	code := seedRouter.Run(t.Context(), []string{
		"team", "add", "reviewers",
		"--repo", siblingDir,
		"--harness", "claude",
	})
	if code != ExitOK {
		t.Fatalf("seed sibling team add: exit code = %d\nstderr: %s", code, seedStderr)
	}

	mutateStandaloneFile(t, siblingDir, fixtureDir)
	before, err := os.ReadFile(filepath.Join(siblingDir, teamStatusStandalonePath))
	if err != nil {
		t.Fatalf("read sibling standalone file before update: %v", err)
	}

	router, stdout, _ := newTestRouter(t)
	code = router.Run(t.Context(), []string{
		"team", "update", "--repo", repo.Dir, "--all-worktrees", "--json",
	})
	if code != ExitFailure {
		t.Fatalf("exit code = %d, want %d (the sibling's refusal is reported)\nstdout: %s", code, ExitFailure, stdout)
	}

	var out teamUpdateAllJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
	}

	var sibling teamUpdateWorktreeJSON
	found := false
	for _, wt := range out.Worktrees {
		if wt.Path == siblingDir {
			sibling = wt
			found = true
		}
	}
	if !found {
		t.Fatalf("no report entry for sibling worktree %s: %+v", siblingDir, out.Worktrees)
	}
	if sibling.Refused != 1 {
		t.Fatalf("sibling Refused = %d, want 1", sibling.Refused)
	}

	var standalone teamUpdateRecordJSON
	found = false
	for _, r := range sibling.Records {
		if r.DestPath == teamStatusStandalonePath {
			standalone = r
			found = true
		}
	}
	if !found {
		t.Fatalf("no record for %s in sibling report: %+v", teamStatusStandalonePath, sibling.Records)
	}
	if standalone.Action != teamUpdateActionRefused {
		t.Errorf("sibling standalone action = %q, want %q (--force was not passed)", standalone.Action, teamUpdateActionRefused)
	}

	after, err := os.ReadFile(filepath.Join(siblingDir, teamStatusStandalonePath))
	if err != nil {
		t.Fatalf("read sibling standalone file after update: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("sibling's locally modified file was overwritten by propagation without --force:\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestTeamUpdate_AllWorktreesFlagDefaultsFalse covers AC-3: without
// --all-worktrees, a sibling worktree is never discovered or touched, even
// when one exists and has a file that would otherwise need updating.
func TestTeamUpdate_AllWorktreesFlagDefaultsFalse(t *testing.T) {
	repo, fixtureDir := setupTeamUpdateWorktreeFixture(t)

	siblingDir := filepath.Join(filepath.Dir(repo.Dir), "worktrees", "sibling")
	repo.Git("worktree", "add", "-b", "sibling-branch", siblingDir)
	seedRouter, _, seedStderr := newTestRouter(t)
	code := seedRouter.Run(t.Context(), []string{
		"team", "add", "reviewers",
		"--repo", siblingDir,
		"--harness", "claude",
	})
	if code != ExitOK {
		t.Fatalf("seed sibling team add: exit code = %d\nstderr: %s", code, seedStderr)
	}

	mutateSource(t, "", fixtureDir)

	before := gittest.Capture(t, siblingDir)

	router, stdout, _ := newTestRouter(t)
	code = router.Run(t.Context(), []string{
		"team", "update", "--repo", repo.Dir, "--json",
	})
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitOK, stdout)
	}

	var out teamUpdateJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
	}
	if out.Repo != repo.Dir {
		t.Errorf("Repo = %q, want %q (single-worktree shape unchanged)", out.Repo, repo.Dir)
	}

	after := gittest.Capture(t, siblingDir)
	if diff := before.Diff(after); diff != "" {
		t.Fatalf("sibling worktree was touched by `team update` without --all-worktrees:\n%s", diff)
	}

	body, err := os.ReadFile(filepath.Join(siblingDir, teamStatusStandalonePath))
	if err != nil {
		t.Fatalf("read sibling standalone file: %v", err)
	}
	if containsBytes(body, "Review the updated code.") {
		t.Errorf("sibling worktree was updated despite --all-worktrees not being passed: %s", body)
	}
}

// TestTeamUpdateAllWorktrees_TableOutputNamesEachWorktree covers the
// human-readable (non-JSON) form of AC-1/AC-4's per-worktree report:
// writeTeamUpdateAllTable, exercised by every other test in this file only
// through --json, must still print one section per worktree naming that
// worktree's own path, so a reader can tell at a glance which outcome
// belongs to which checkout without needing --json.
func TestTeamUpdateAllWorktrees_TableOutputNamesEachWorktree(t *testing.T) {
	repo, fixtureDir := setupTeamUpdateWorktreeFixture(t)

	siblingDir := filepath.Join(filepath.Dir(repo.Dir), "worktrees", "sibling")
	repo.Git("worktree", "add", "-b", "sibling-branch", siblingDir)
	seedRouter, _, seedStderr := newTestRouter(t)
	code := seedRouter.Run(t.Context(), []string{
		"team", "add", "reviewers",
		"--repo", siblingDir,
		"--harness", "claude",
	})
	if code != ExitOK {
		t.Fatalf("seed sibling team add: exit code = %d\nstderr: %s", code, seedStderr)
	}

	mutateSource(t, "", fixtureDir)

	router, stdout, _ := newTestRouter(t)
	code = router.Run(t.Context(), []string{
		"team", "update", "--repo", repo.Dir, "--all-worktrees",
	})
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitOK, stdout)
	}

	out := stdout.String()
	if !containsBytes([]byte(out), "worktree: "+repo.Dir) {
		t.Errorf("table output does not name the primary worktree %s: %s", repo.Dir, out)
	}
	if !containsBytes([]byte(out), "worktree: "+siblingDir) {
		t.Errorf("table output does not name the sibling worktree %s: %s", siblingDir, out)
	}
	if !containsBytes([]byte(out), teamStatusStandalonePath) {
		t.Errorf("table output does not name the updated record's destination path %s: %s", teamStatusStandalonePath, out)
	}
	if !containsBytes([]byte(out), teamUpdateActionUpdated) {
		t.Errorf("table output does not report the %q action for the upstream-updated record: %s", teamUpdateActionUpdated, out)
	}
}
