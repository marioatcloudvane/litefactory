package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/team"
)

// TestTeamUpdate_MixedState covers AC-1: In-sync, Upstream-updated, and
// Missing records are updated automatically, while Locally-modified and
// Conflict records are left untouched and reported by name and
// classification, without --force.
func TestTeamUpdate_MixedState(t *testing.T) {
	tests := []struct {
		name           string
		mutate         func(t *testing.T, repoDir, fixtureDir string)
		wantStandalone string
		wantRoot       string
		wantAction     string // shared action expectation for both records
	}{
		{
			name:           "no mutation/both in sync",
			mutate:         func(*testing.T, string, string) {},
			wantStandalone: "in sync",
			wantRoot:       "in sync",
			wantAction:     teamUpdateActionNone,
		},
		{
			name:           "source moved on/both upstream updated",
			mutate:         mutateSource,
			wantStandalone: "upstream updated",
			wantRoot:       "upstream updated",
			wantAction:     teamUpdateActionUpdated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoDir, fixtureDir := setupTeamStatusFixture(t)
			tt.mutate(t, repoDir, fixtureDir)

			router, stdout, _ := newTestRouter(t)
			code := router.Run(t.Context(), []string{
				"team", "update", "--repo", repoDir, "--json",
			})
			if code != ExitOK {
				t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitOK, stdout)
			}

			var out teamUpdateJSON
			if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
				t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
			}
			if len(out.Records) != 2 {
				t.Fatalf("len(Records) = %d, want 2", len(out.Records))
			}

			byDest := make(map[string]teamUpdateRecordJSON, 2)
			for _, r := range out.Records {
				byDest[r.DestPath] = r
			}
			standalone, ok := byDest[teamStatusStandalonePath]
			if !ok {
				t.Fatalf("no record for %s", teamStatusStandalonePath)
			}
			root, ok := byDest[teamStatusRootPath]
			if !ok {
				t.Fatalf("no record for %s", teamStatusRootPath)
			}

			if standalone.Status != tt.wantStandalone {
				t.Errorf("standalone status = %q, want %q", standalone.Status, tt.wantStandalone)
			}
			if root.Status != tt.wantRoot {
				t.Errorf("root status = %q, want %q", root.Status, tt.wantRoot)
			}
			if standalone.Action != tt.wantAction {
				t.Errorf("standalone action = %q, want %q", standalone.Action, tt.wantAction)
			}
			if root.Action != tt.wantAction {
				t.Errorf("root action = %q, want %q", root.Action, tt.wantAction)
			}

			// After an update, the installed bytes must reflect the fresh
			// source, and the report's Note must be empty for a clean
			// action (teamUpdateNote only produces text for overwritten or
			// refused actions).
			if tt.wantAction == teamUpdateActionUpdated {
				body, err := os.ReadFile(filepath.Join(repoDir, teamStatusStandalonePath))
				if err != nil {
					t.Fatalf("read standalone file: %v", err)
				}
				if !containsBytes(body, "Review the updated code.") {
					t.Errorf("standalone file was not updated: %s", body)
				}
			}
			if standalone.Note != "" || root.Note != "" {
				t.Errorf("expected empty notes for action %q, got %q / %q", tt.wantAction, standalone.Note, root.Note)
			}
		})
	}
}

// TestTeamUpdate_RefusesLocalModificationsByDefault covers AC-1's refusal
// half: a hand-edited file is left untouched and reported, and the run
// reports a non-OK outcome because at least one file was refused.
func TestTeamUpdate_RefusesLocalModificationsByDefault(t *testing.T) {
	repoDir, fixtureDir := setupTeamStatusFixture(t)
	mutateStandaloneFile(t, repoDir, fixtureDir)

	before, err := os.ReadFile(filepath.Join(repoDir, teamStatusStandalonePath))
	if err != nil {
		t.Fatalf("read standalone file before update: %v", err)
	}

	router, stdout, _ := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "update", "--repo", repoDir, "--json",
	})
	if code != ExitFailure {
		t.Fatalf("exit code = %d, want %d (refusal reports non-OK)\nstdout: %s", code, ExitFailure, stdout)
	}

	var out teamUpdateJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
	}
	if out.Refused != 1 {
		t.Fatalf("Refused = %d, want 1", out.Refused)
	}

	var standalone teamUpdateRecordJSON
	found := false
	for _, r := range out.Records {
		if r.DestPath == teamStatusStandalonePath {
			standalone = r
			found = true
		}
	}
	if !found {
		t.Fatalf("no record for %s", teamStatusStandalonePath)
	}
	if standalone.Status != "locally modified" {
		t.Fatalf("status = %q, want %q", standalone.Status, "locally modified")
	}
	if standalone.Action != teamUpdateActionRefused {
		t.Fatalf("action = %q, want %q", standalone.Action, teamUpdateActionRefused)
	}
	if standalone.Note == "" {
		t.Fatalf("expected a non-empty note explaining the refusal")
	}

	after, err := os.ReadFile(filepath.Join(repoDir, teamStatusStandalonePath))
	if err != nil {
		t.Fatalf("read standalone file after update: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("refused file was modified:\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestTeamUpdate_ActiveOverride_SubstitutesLocalFile covers AC-1's "update"
// half: once an agent is already installed, recording an active override for
// its key and running `team update` classifies it as upstream updated
// against the override's content and installs the override's bytes, not the
// team index's original source.
func TestTeamUpdate_ActiveOverride_SubstitutesLocalFile(t *testing.T) {
	repoDir, _ := setupTeamStatusFixture(t)
	writeManageOverrideSource(t, repoDir, "local/reviewer.md")
	if err := team.SetOverride(repoDir, "reviewer", "local/reviewer.md"); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}

	router, stdout, _ := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "update", "--repo", repoDir, "--json",
	})
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitOK, stdout)
	}

	var out teamUpdateJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
	}
	found := false
	for _, r := range out.Records {
		if r.DestPath == teamStatusStandalonePath {
			found = true
			if r.Action != teamUpdateActionUpdated {
				t.Errorf("standalone action = %q, want %q", r.Action, teamUpdateActionUpdated)
			}
		}
	}
	if !found {
		t.Fatalf("no record for %s: %+v", teamStatusStandalonePath, out.Records)
	}

	body, err := os.ReadFile(filepath.Join(repoDir, teamStatusStandalonePath))
	if err != nil {
		t.Fatalf("read standalone file: %v", err)
	}
	if !containsBytes(body, "Patched body.") {
		t.Errorf("standalone file was not updated with the override's content: %s", body)
	}
	if containsBytes(body, "Review the code.") {
		t.Errorf("standalone file still contains the team index's original body: %s", body)
	}
}

// TestTeamUpdate_RestoresMissingFile confirms that a deleted standalone file
// (StatusMissing) is re-rendered and written by `team update`, exercising
// the existing-on-disk dedup fix in internal/team/write.go end-to-end
// through the CLI.
func TestTeamUpdate_RestoresMissingFile(t *testing.T) {
	repoDir, fixtureDir := setupTeamStatusFixture(t)
	deleteStandaloneFile(t, repoDir, fixtureDir)

	if _, err := os.Stat(filepath.Join(repoDir, teamStatusStandalonePath)); !os.IsNotExist(err) {
		t.Fatalf("fixture assumption broken: file still exists (err=%v)", err)
	}

	router, stdout, _ := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "update", "--repo", repoDir, "--json",
	})
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitOK, stdout)
	}

	var out teamUpdateJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
	}

	var standalone teamUpdateRecordJSON
	found := false
	for _, r := range out.Records {
		if r.DestPath == teamStatusStandalonePath {
			standalone = r
			found = true
		}
	}
	if !found {
		t.Fatalf("no record for %s", teamStatusStandalonePath)
	}
	if standalone.Status != "missing" {
		t.Fatalf("status = %q, want %q", standalone.Status, "missing")
	}
	if standalone.Action != teamUpdateActionUpdated {
		t.Fatalf("action = %q, want %q", standalone.Action, teamUpdateActionUpdated)
	}

	body, err := os.ReadFile(filepath.Join(repoDir, teamStatusStandalonePath))
	if err != nil {
		t.Fatalf("file was not restored: %v", err)
	}
	if !containsBytes(body, "Review the code.") {
		t.Errorf("restored file has unexpected content: %s", body)
	}
}

// TestTeamUpdate_ForceOverwritesAndNamesWhatItDiscarded covers AC-2:
// --force overwrites a Locally-modified file, and the report names exactly
// which file was overwritten and what it previously was.
func TestTeamUpdate_ForceOverwritesAndNamesWhatItDiscarded(t *testing.T) {
	repoDir, fixtureDir := setupTeamStatusFixture(t)
	mutateStandaloneFile(t, repoDir, fixtureDir)

	router, stdout, _ := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "update", "--repo", repoDir, "--force", "--json",
	})
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitOK, stdout)
	}

	var out teamUpdateJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
	}
	if !out.Force {
		t.Errorf("Force = false, want true")
	}
	if out.Refused != 0 {
		t.Errorf("Refused = %d, want 0", out.Refused)
	}

	var standalone teamUpdateRecordJSON
	found := false
	for _, r := range out.Records {
		if r.DestPath == teamStatusStandalonePath {
			standalone = r
			found = true
		}
	}
	if !found {
		t.Fatalf("no record for %s", teamStatusStandalonePath)
	}
	if standalone.Status != "locally modified" {
		t.Fatalf("status = %q, want %q", standalone.Status, "locally modified")
	}
	if standalone.Action != teamUpdateActionOverwritten {
		t.Fatalf("action = %q, want %q", standalone.Action, teamUpdateActionOverwritten)
	}
	wantNote := "was locally modified; overwritten because --force was passed"
	if standalone.Note != wantNote {
		t.Errorf("note = %q, want %q", standalone.Note, wantNote)
	}

	body, err := os.ReadFile(filepath.Join(repoDir, teamStatusStandalonePath))
	if err != nil {
		t.Fatalf("read standalone file: %v", err)
	}
	if containsBytes(body, "HAND EDITED") {
		t.Errorf("hand-edit was not overwritten: %s", body)
	}
	if !containsBytes(body, "Review the code.") {
		t.Errorf("overwritten file has unexpected content: %s", body)
	}
}

// TestTeamUpdate_NoLockfile mirrors TestTeamStatus_NoLockfile: a repo with no
// provenance lockfile at all reports a vacuous success.
func TestTeamUpdate_NoLockfile(t *testing.T) {
	fixtureDir := newTeamIndexFixture(t, "reviewers", "reviewer")
	newTeamHome(t, fixtureDir)
	repo := newTeamGitRepo(t)

	router, stdout, _ := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "update", "--repo", repo, "--json",
	})
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitOK, stdout)
	}

	var out teamUpdateJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
	}
	if out.Updated != 0 || out.Skipped != 0 || out.Refused != 0 {
		t.Errorf("Updated/Skipped/Refused = %d/%d/%d, want 0/0/0", out.Updated, out.Skipped, out.Refused)
	}
	if len(out.Records) != 0 {
		t.Errorf("len(Records) = %d, want 0", len(out.Records))
	}
}

// TestTeamUpdate_SingleWorktreeScope covers AC-3: running `team update`
// against one worktree never touches a sibling worktree checked out from
// the same repository. setupTeamStatusFixture cannot be reused here because
// it discards the *gittest.Repo handle (it returns only repo.Dir), and a
// *gittest.Repo cannot be reconstructed from a bare directory string since
// its tb/home fields are unexported — so this test rebuilds the fixture
// steps itself, keeping the *gittest.Repo value in scope long enough to
// create a real sibling worktree with git itself.
func TestTeamUpdate_SingleWorktreeScope(t *testing.T) {
	const teamName = "reviewers"
	const agentKey = "reviewer"

	fixtureDir := newTeamIndexFixture(t, teamName, agentKey)
	newTeamHome(t, fixtureDir)
	repo := gittest.New(t)

	seedRouter, _, seedStderr := newTestRouter(t)
	code := seedRouter.Run(t.Context(), []string{
		"team", "add", teamName,
		"--repo", repo.Dir,
		"--harness", "claude",
	})
	if code != ExitOK {
		t.Fatalf("seed team add: exit code = %d\nstderr: %s", code, seedStderr)
	}

	siblingDir := filepath.Join(filepath.Dir(repo.Dir), "worktrees", "sibling")
	repo.Git("worktree", "add", "-b", "sibling-branch", siblingDir)

	mutateStandaloneFile(t, repo.Dir, fixtureDir)
	mutateSource(t, repo.Dir, fixtureDir)

	before := gittest.Capture(t, siblingDir)

	router, stdout, _ := newTestRouter(t)
	router.Run(t.Context(), []string{
		"team", "update", "--repo", repo.Dir, "--force", "--json",
	})
	_ = stdout // outcome of the primary worktree's update is not under test here

	after := gittest.Capture(t, siblingDir)
	if diff := before.Diff(after); diff != "" {
		t.Fatalf("sibling worktree was touched by team update run against a different worktree:\n%s", diff)
	}
}

// TestTeamUpdate_MixedOutcomesInSingleRun covers AC-1 with a genuinely mixed
// fixture rather than two uniform records: combining mutateStandaloneFile and
// mutateSource drives the standalone record to Conflict (both sides changed)
// while the root record — sharing the same source but never hand-edited —
// lands on Upstream updated (per TestTeamStatus_Classification's own
// "hand-edit standalone and edit source" case). One run therefore both
// auto-updates a safe record and refuses a Conflict record side by side,
// which the counters and per-record actions must reflect simultaneously.
func TestTeamUpdate_MixedOutcomesInSingleRun(t *testing.T) {
	repoDir, fixtureDir := setupTeamStatusFixture(t)
	combine(mutateStandaloneFile, mutateSource)(t, repoDir, fixtureDir)

	router, stdout, _ := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "update", "--repo", repoDir, "--json",
	})
	if code != ExitFailure {
		t.Fatalf("exit code = %d, want %d (a Conflict record is refused)\nstdout: %s", code, ExitFailure, stdout)
	}

	var out teamUpdateJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
	}
	if out.Updated != 1 {
		t.Errorf("Updated = %d, want 1", out.Updated)
	}
	if out.Refused != 1 {
		t.Errorf("Refused = %d, want 1", out.Refused)
	}
	if out.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0", out.Skipped)
	}
	if len(out.Records) != 2 {
		t.Fatalf("len(Records) = %d, want 2", len(out.Records))
	}

	byDest := make(map[string]teamUpdateRecordJSON, 2)
	for _, r := range out.Records {
		byDest[r.DestPath] = r
	}
	standalone, ok := byDest[teamStatusStandalonePath]
	if !ok {
		t.Fatalf("no record for %s", teamStatusStandalonePath)
	}
	root, ok := byDest[teamStatusRootPath]
	if !ok {
		t.Fatalf("no record for %s", teamStatusRootPath)
	}

	if standalone.Status != "conflict" {
		t.Errorf("standalone status = %q, want %q", standalone.Status, "conflict")
	}
	if standalone.Action != teamUpdateActionRefused {
		t.Errorf("standalone action = %q, want %q", standalone.Action, teamUpdateActionRefused)
	}
	if standalone.Note == "" {
		t.Errorf("expected a non-empty refusal note for the conflict record")
	}
	if root.Status != "upstream updated" {
		t.Errorf("root status = %q, want %q", root.Status, "upstream updated")
	}
	if root.Action != teamUpdateActionUpdated {
		t.Errorf("root action = %q, want %q", root.Action, teamUpdateActionUpdated)
	}

	// The refused conflict file must retain the hand-edit on disk...
	standaloneBody, err := os.ReadFile(filepath.Join(repoDir, teamStatusStandalonePath))
	if err != nil {
		t.Fatalf("read standalone file: %v", err)
	}
	if !containsBytes(standaloneBody, "HAND EDITED") {
		t.Errorf("refused conflict file lost its hand-edit: %s", standaloneBody)
	}
	// ...while the safely-updated root file reflects the fresh source.
	rootBody, err := os.ReadFile(filepath.Join(repoDir, teamStatusRootPath))
	if err != nil {
		t.Fatalf("read root file: %v", err)
	}
	if !containsBytes(rootBody, "Review the updated code.") {
		t.Errorf("root file was not updated to the fresh source: %s", rootBody)
	}
}

// TestTeamUpdate_ForcePrecision covers AC-2's "only overwrites
// Locally-modified/Conflict records" half with a mixed fixture: the same
// combine(mutateStandaloneFile, mutateSource) setup as
// TestTeamUpdate_MixedOutcomesInSingleRun drives the standalone record to
// Conflict (force-eligible) and the root record to Upstream updated
// (already safe, never force-eligible). Passing --force must overwrite only
// the standalone record and report it as "overwritten" by name, while the
// root record is still reported as plain "updated" rather than folded into
// the same bucket.
func TestTeamUpdate_ForcePrecision(t *testing.T) {
	repoDir, fixtureDir := setupTeamStatusFixture(t)
	combine(mutateStandaloneFile, mutateSource)(t, repoDir, fixtureDir)

	router, stdout, _ := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "update", "--repo", repoDir, "--force", "--json",
	})
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitOK, stdout)
	}

	var out teamUpdateJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
	}
	if out.Refused != 0 {
		t.Errorf("Refused = %d, want 0 (--force was passed)", out.Refused)
	}
	if out.Updated != 2 {
		t.Errorf("Updated = %d, want 2 (both records written, one updated + one overwritten)", out.Updated)
	}

	byDest := make(map[string]teamUpdateRecordJSON, 2)
	for _, r := range out.Records {
		byDest[r.DestPath] = r
	}
	standalone, ok := byDest[teamStatusStandalonePath]
	if !ok {
		t.Fatalf("no record for %s", teamStatusStandalonePath)
	}
	root, ok := byDest[teamStatusRootPath]
	if !ok {
		t.Fatalf("no record for %s", teamStatusRootPath)
	}

	if standalone.Action != teamUpdateActionOverwritten {
		t.Errorf("standalone action = %q, want %q (force-eligible conflict)", standalone.Action, teamUpdateActionOverwritten)
	}
	if standalone.Note == "" || !containsBytes([]byte(standalone.Note), "--force") {
		t.Errorf("standalone note = %q, want it to mention --force", standalone.Note)
	}
	if root.Action != teamUpdateActionUpdated {
		t.Errorf("root action = %q, want %q (was never force-eligible)", root.Action, teamUpdateActionUpdated)
	}

	body, err := os.ReadFile(filepath.Join(repoDir, teamStatusStandalonePath))
	if err != nil {
		t.Fatalf("read standalone file: %v", err)
	}
	if containsBytes(body, "HAND EDITED") {
		t.Errorf("hand-edit was not overwritten despite --force: %s", body)
	}
}

// TestTeamUpdate_ForceWithNothingToForce covers AC-2's other half: --force
// on a repo where nothing is Locally-modified or Conflict must not report a
// single overwritten file, proving the flag only ever acts when there is
// something force-eligible to act on.
func TestTeamUpdate_ForceWithNothingToForce(t *testing.T) {
	repoDir, _ := setupTeamStatusFixture(t)

	router, stdout, _ := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "update", "--repo", repoDir, "--force", "--json",
	})
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitOK, stdout)
	}

	var out teamUpdateJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
	}
	if !out.Force {
		t.Errorf("Force = false, want true")
	}
	for _, r := range out.Records {
		if r.Action == teamUpdateActionOverwritten {
			t.Errorf("record %s reported as overwritten despite nothing being force-eligible", r.DestPath)
		}
		if r.Action != teamUpdateActionNone {
			t.Errorf("record %s action = %q, want %q (nothing tracked has drifted)", r.DestPath, r.Action, teamUpdateActionNone)
		}
	}
}

// TestTeamUpdate_TableOutput covers the human-readable rendering of AC-1 and
// AC-2's naming requirements: a refused record and a --force-overwritten
// record must both be identifiable by destination path and explanatory note
// in the non-JSON table, not only in --json output.
func TestTeamUpdate_TableOutput(t *testing.T) {
	t.Run("refused/names the file and its classification", func(t *testing.T) {
		repoDir, fixtureDir := setupTeamStatusFixture(t)
		mutateStandaloneFile(t, repoDir, fixtureDir)

		router, stdout, _ := newTestRouter(t)
		code := router.Run(t.Context(), []string{"team", "update", "--repo", repoDir})

		if code != ExitFailure {
			t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitFailure, stdout)
		}
		out := stdout.String()
		if !strings.Contains(out, teamStatusStandalonePath) {
			t.Errorf("table does not name %s:\n%s", teamStatusStandalonePath, out)
		}
		if !strings.Contains(out, "locally modified") {
			t.Errorf("table does not report the locally modified classification:\n%s", out)
		}
		if !strings.Contains(out, teamUpdateActionRefused) {
			t.Errorf("table does not report the refused action:\n%s", out)
		}
	})

	t.Run("forced overwrite/names exactly what it discarded", func(t *testing.T) {
		repoDir, fixtureDir := setupTeamStatusFixture(t)
		mutateStandaloneFile(t, repoDir, fixtureDir)

		router, stdout, _ := newTestRouter(t)
		code := router.Run(t.Context(), []string{"team", "update", "--repo", repoDir, "--force"})

		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitOK, stdout)
		}
		out := stdout.String()
		if !strings.Contains(out, teamStatusStandalonePath) {
			t.Errorf("table does not name %s:\n%s", teamStatusStandalonePath, out)
		}
		if !strings.Contains(out, "overwritten because --force was passed") {
			t.Errorf("table does not explain the forced overwrite:\n%s", out)
		}
	})
}

// containsBytes reports whether needle appears anywhere in haystack.
func containsBytes(haystack []byte, needle string) bool {
	return bytes.Contains(haystack, []byte(needle))
}
