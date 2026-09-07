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

// setupTeamStatusFixture builds a repo with a resolvable team index (a
// static-path override, via newTeamIndexFixture/newTeamHome) and a single
// agent ("reviewer") already installed for the claude harness via a real
// `team add` run. That add produces exactly two lockfile records for the
// agent: the standalone file (.claude/agents/reviewer.md) and its marker
// section inside the shared root file (CLAUDE.md). It returns the repo
// directory and the fixture directory holding the team index and agent
// source, so a caller can mutate either side afterward to manufacture a
// specific drift state.
func setupTeamStatusFixture(t *testing.T) (repoDir, fixtureDir string) {
	t.Helper()
	const teamName = "reviewers"
	const agentKey = "reviewer"

	fixtureDir = newTeamIndexFixture(t, teamName, agentKey)
	newTeamHome(t, fixtureDir)
	repo := gittest.New(t)

	router, _, stderr := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "add", teamName,
		"--repo", repo.Dir,
		"--harness", "claude",
	})
	if code != ExitOK {
		t.Fatalf("seed team add: exit code = %d\nstderr: %s", code, stderr)
	}
	return repo.Dir, fixtureDir
}

const (
	teamStatusStandalonePath = ".claude/agents/reviewer.md"
	teamStatusRootPath       = "CLAUDE.md"
)

// replaceInFile rewrites path, replacing the first occurrence of old with
// new. It fails the test if old is not found, so a fixture assumption that
// silently stops holding is caught rather than producing a false pass.
func replaceInFile(t *testing.T, path, old, new string) {
	t.Helper()
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Contains(original, []byte(old)) {
		t.Fatalf("%s does not contain %q, fixture assumption broken:\n%s", path, old, original)
	}
	edited := bytes.Replace(original, []byte(old), []byte(new), 1)
	if err := os.WriteFile(path, edited, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// mutateStandaloneFile hand-edits the installed standalone agent file, as if
// an operator had edited it directly after `team add`.
func mutateStandaloneFile(t *testing.T, repoDir, _ string) {
	t.Helper()
	path := filepath.Join(repoDir, teamStatusStandalonePath)
	replaceInFile(t, path, "Review the code.", "Review the code, HAND EDITED.")
}

// mutateRootFile hand-edits the reviewer agent's marker section inside
// CLAUDE.md in place, leaving the BEGIN/END delimiter lines untouched so the
// section is still locatable.
func mutateRootFile(t *testing.T, repoDir, _ string) {
	t.Helper()
	path := filepath.Join(repoDir, teamStatusRootPath)
	replaceInFile(t, path, "Review the code.", "Review the code, HAND EDITED IN ROOT.")
}

// mutateSource edits the agent's current source markdown, as if the team
// index upstream had moved on since install.
func mutateSource(t *testing.T, _, fixtureDir string) {
	t.Helper()
	path := filepath.Join(fixtureDir, "teams", "agents", "reviewer.md")
	replaceInFile(t, path, "Review the code.", "Review the updated code.")
}

// deleteStandaloneFile removes the installed standalone agent file outright.
func deleteStandaloneFile(t *testing.T, repoDir, _ string) {
	t.Helper()
	if err := os.Remove(filepath.Join(repoDir, teamStatusStandalonePath)); err != nil {
		t.Fatalf("remove standalone file: %v", err)
	}
}

// combine runs every mutate func in order.
func combine(fns ...func(t *testing.T, repoDir, fixtureDir string)) func(t *testing.T, repoDir, fixtureDir string) {
	return func(t *testing.T, repoDir, fixtureDir string) {
		for _, fn := range fns {
			fn(t, repoDir, fixtureDir)
		}
	}
}

// TestTeamStatus_Classification covers AC-1 (every tracked file classified
// into exactly one of the five states, by byte comparison against the
// lockfile's recorded hashes and a fresh render) and AC-2 (Locally modified
// and Conflict records carry the hand-edit-risk note; every other state does
// not), exercised independently across both a standalone target and a
// root-file (marker-section) target sharing the same lockfile.
func TestTeamStatus_Classification(t *testing.T) {
	tests := []struct {
		name           string
		mutate         func(t *testing.T, repoDir, fixtureDir string)
		wantStandalone string
		wantRoot       string
	}{
		{
			name:           "no mutation/both in sync",
			mutate:         func(*testing.T, string, string) {},
			wantStandalone: "in sync",
			wantRoot:       "in sync",
		},
		{
			name:           "hand-edit standalone file only/standalone locally modified",
			mutate:         mutateStandaloneFile,
			wantStandalone: "locally modified",
			wantRoot:       "in sync",
		},
		{
			name:           "edit source body only/both upstream updated",
			mutate:         mutateSource,
			wantStandalone: "upstream updated",
			wantRoot:       "upstream updated",
		},
		{
			name:           "hand-edit standalone and edit source/standalone conflict",
			mutate:         combine(mutateStandaloneFile, mutateSource),
			wantStandalone: "conflict",
			wantRoot:       "upstream updated",
		},
		{
			name:           "hand-edit root section and edit source/root conflict",
			mutate:         combine(mutateRootFile, mutateSource),
			wantStandalone: "upstream updated",
			wantRoot:       "conflict",
		},
		{
			name:           "delete standalone file/standalone missing, not skipped",
			mutate:         deleteStandaloneFile,
			wantStandalone: "missing",
			wantRoot:       "in sync",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoDir, fixtureDir := setupTeamStatusFixture(t)
			tt.mutate(t, repoDir, fixtureDir)
			router, stdout, _ := newTestRouter(t)

			code := router.Run(t.Context(), []string{"team", "status", "--repo", repoDir, "--json"})

			wantInSync := tt.wantStandalone == "in sync" && tt.wantRoot == "in sync"
			wantCode := ExitOK
			if !wantInSync {
				wantCode = ExitFailure
			}
			if code != wantCode {
				t.Fatalf("exit code = %d, want %d\nstdout: %s", code, wantCode, stdout)
			}

			var out teamStatusJSON
			if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
				t.Fatalf("unmarshal json: %v\nstdout: %s", err, stdout)
			}
			if len(out.Records) != 2 {
				t.Fatalf("len(records) = %d, want 2 (a Missing record must be reported, not skipped): %+v", len(out.Records), out.Records)
			}
			if out.InSync != wantInSync {
				t.Errorf("in_sync = %v, want %v", out.InSync, wantInSync)
			}

			byDest := make(map[string]teamStatusRecordJSON, 2)
			for _, r := range out.Records {
				byDest[r.DestPath] = r
			}
			standalone, ok := byDest[teamStatusStandalonePath]
			if !ok {
				t.Fatalf("no record for %s: %+v", teamStatusStandalonePath, out.Records)
			}
			if standalone.Status != tt.wantStandalone {
				t.Errorf("standalone status = %q, want %q", standalone.Status, tt.wantStandalone)
			}
			root, ok := byDest[teamStatusRootPath]
			if !ok {
				t.Fatalf("no record for %s: %+v", teamStatusRootPath, out.Records)
			}
			if root.Status != tt.wantRoot {
				t.Errorf("root status = %q, want %q", root.Status, tt.wantRoot)
			}

			wantNote := "hand-edited since install; a future team update could discard this change"
			for _, r := range []teamStatusRecordJSON{standalone, root} {
				if r.Status == "locally modified" || r.Status == "conflict" {
					if r.Note != wantNote {
						t.Errorf("%s (%s) note = %q, want %q", r.DestPath, r.Status, r.Note, wantNote)
					}
				} else if r.Note != "" {
					t.Errorf("%s (%s) note = %q, want empty", r.DestPath, r.Status, r.Note)
				}
			}
		})
	}
}

// TestTeamStatus_ActiveOverride_ClassifiesAgainstOverrideFile covers AC-3: an
// overridden agent's drift is classified against the override's local file,
// not the team index's original source, with no special-cased behavior
// required in the classification logic itself -- changing only the
// override's file (and leaving the index source untouched) is what flips the
// status, and an override whose content is still byte-identical to the
// original source introduces no false drift.
func TestTeamStatus_ActiveOverride_ClassifiesAgainstOverrideFile(t *testing.T) {
	repoDir, _ := setupTeamStatusFixture(t)
	const overrideRel = "local/reviewer.md"
	overridePath := filepath.Join(repoDir, overrideRel)
	if err := os.MkdirAll(filepath.Dir(overridePath), 0o777); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Byte-identical to the original index source, so recording the override
	// alone introduces no false drift.
	content := validTeamAgentSource("reviewer", "a test reviewer agent", "Review the code.")
	if err := os.WriteFile(overridePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write override source: %v", err)
	}
	if err := team.SetOverride(repoDir, "reviewer", overrideRel); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}

	router, stdout, _ := newTestRouter(t)
	code := router.Run(t.Context(), []string{"team", "status", "--repo", repoDir, "--json"})
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitOK, stdout)
	}
	var out teamStatusJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal json: %v\nstdout: %s", err, stdout)
	}
	if !out.InSync {
		t.Fatalf("in_sync = false right after recording an identical-content override, want true: %+v", out.Records)
	}

	// Mutate only the override's local file; the index source under
	// fixtureDir is left untouched.
	replaceInFile(t, overridePath, "Review the code.", "Review the code, OVERRIDDEN UPSTREAM CHANGE.")

	router, stdout, _ = newTestRouter(t)
	code = router.Run(t.Context(), []string{"team", "status", "--repo", repoDir, "--json"})
	if code != ExitFailure {
		t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitFailure, stdout)
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal json: %v\nstdout: %s", err, stdout)
	}
	if out.InSync {
		t.Errorf("in_sync = true after mutating only the override's file, want false")
	}
	if len(out.Records) != 2 {
		t.Fatalf("len(records) = %d, want 2: %+v", len(out.Records), out.Records)
	}
	for _, r := range out.Records {
		if r.Status != "upstream updated" {
			t.Errorf("%s status = %q, want %q (classification should follow the override's file, not the index source)", r.DestPath, r.Status, "upstream updated")
		}
	}
}

// TestTeamStatus_SortOrder pins the report's deterministic ordering by
// DestPath: the standalone file (".claude/...") sorts before the root file
// ("CLAUDE.md") byte-wise ('.' < 'C'), so a stable ordering does not depend
// on lockfile record insertion order.
func TestTeamStatus_SortOrder(t *testing.T) {
	repoDir, _ := setupTeamStatusFixture(t)
	router, stdout, _ := newTestRouter(t)

	code := router.Run(t.Context(), []string{"team", "status", "--repo", repoDir, "--json"})

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitOK, stdout)
	}
	var out teamStatusJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal json: %v\nstdout: %s", err, stdout)
	}
	if len(out.Records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(out.Records))
	}
	if out.Records[0].DestPath != teamStatusStandalonePath {
		t.Errorf("Records[0].DestPath = %q, want %q", out.Records[0].DestPath, teamStatusStandalonePath)
	}
	if out.Records[1].DestPath != teamStatusRootPath {
		t.Errorf("Records[1].DestPath = %q, want %q", out.Records[1].DestPath, teamStatusRootPath)
	}
}

// TestTeamStatus_NoLockfile covers the vacuous-success case: a repo with a
// resolvable team index but no lockfile yet has nothing tracked, so AC-1
// holds trivially and the command succeeds with an empty report rather than
// erroring.
func TestTeamStatus_NoLockfile(t *testing.T) {
	fixtureDir := newTeamIndexFixture(t, "reviewers", "reviewer")
	newTeamHome(t, fixtureDir)
	repo := newTeamGitRepo(t)
	router, stdout, stderr := newTestRouter(t)

	code := router.Run(t.Context(), []string{"team", "status", "--repo", repo, "--json"})

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	var out teamStatusJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal json: %v\nstdout: %s", err, stdout)
	}
	if !out.InSync {
		t.Errorf("in_sync = false, want true when nothing is tracked")
	}
	if len(out.Records) != 0 {
		t.Errorf("len(records) = %d, want 0", len(out.Records))
	}
}

// TestTeamStatus_RenderError covers the re-render failure path: a lockfile
// record whose SourcePath no longer parses is reported as an "error" status
// with an explanatory note, for both records sharing that source, rather
// than aborting the whole report or silently dropping them.
func TestTeamStatus_RenderError(t *testing.T) {
	repoDir, fixtureDir := setupTeamStatusFixture(t)
	sourcePath := filepath.Join(fixtureDir, "teams", "agents", "reviewer.md")
	// Corrupt the frontmatter delimiter so teamsource.parseAgentSource fails
	// deterministically (RenderCurrent's "%s: %w" wrap around parseAgentSource).
	if err := os.WriteFile(sourcePath, []byte("not a valid frontmatter block\n"), 0o600); err != nil {
		t.Fatalf("corrupt source: %v", err)
	}
	router, stdout, _ := newTestRouter(t)

	code := router.Run(t.Context(), []string{"team", "status", "--repo", repoDir, "--json"})

	if code != ExitFailure {
		t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitFailure, stdout)
	}
	var out teamStatusJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal json: %v\nstdout: %s", err, stdout)
	}
	if out.InSync {
		t.Errorf("in_sync = true, want false when every record failed to re-render")
	}
	if len(out.Records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(out.Records))
	}
	for _, r := range out.Records {
		if r.Status != "error" {
			t.Errorf("%s status = %q, want %q", r.DestPath, r.Status, "error")
		}
		if !strings.Contains(r.Note, "re-render reviewer (claude)") {
			t.Errorf("%s note = %q, want it to mention the re-render failure", r.DestPath, r.Note)
		}
	}
}

// TestTeamStatus_BadRepo covers --repo resolution: a path with no .git
// directory is a precondition failure, matching resolveTeamRepoPath's
// contract for every other team command.
func TestTeamStatus_BadRepo(t *testing.T) {
	router, _, stderr := newTestRouter(t)

	code := router.Run(t.Context(), []string{"team", "status", "--repo", t.TempDir()})

	if code != ExitPrecondition {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitPrecondition, stderr)
	}
	if !strings.Contains(stderr.String(), "no git repository at") {
		t.Errorf("stderr does not mention the missing .git directory:\n%s", stderr)
	}
}

// TestTeamStatus_TableOutput covers the non-JSON table rendering: the
// in-sync trailer, the hand-edit note text next to a drifted row, and the
// empty-lockfile message, none of which the JSON-mode tests above exercise.
func TestTeamStatus_TableOutput(t *testing.T) {
	t.Run("in sync/prints the all-in-sync trailer", func(t *testing.T) {
		repoDir, _ := setupTeamStatusFixture(t)
		router, stdout, _ := newTestRouter(t)

		code := router.Run(t.Context(), []string{"team", "status", "--repo", repoDir})

		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitOK, stdout)
		}
		if !strings.Contains(stdout.String(), "all tracked files are in sync") {
			t.Errorf("stdout does not contain the in-sync trailer:\n%s", stdout)
		}
	})

	t.Run("locally modified/prints the hand-edit note next to the row", func(t *testing.T) {
		repoDir, fixtureDir := setupTeamStatusFixture(t)
		mutateStandaloneFile(t, repoDir, fixtureDir)
		router, stdout, _ := newTestRouter(t)

		code := router.Run(t.Context(), []string{"team", "status", "--repo", repoDir})

		if code != ExitFailure {
			t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitFailure, stdout)
		}
		out := stdout.String()
		if !strings.Contains(out, "locally modified") {
			t.Errorf("stdout does not mention the locally modified status:\n%s", out)
		}
		if !strings.Contains(out, "hand-edited since install; a future team update could discard this change") {
			t.Errorf("stdout does not contain the hand-edit-risk note:\n%s", out)
		}
		if strings.Contains(out, "all tracked files are in sync") {
			t.Errorf("stdout contains the in-sync trailer despite drift:\n%s", out)
		}
	})

	t.Run("no lockfile/prints the empty report message", func(t *testing.T) {
		fixtureDir := newTeamIndexFixture(t, "reviewers", "reviewer")
		newTeamHome(t, fixtureDir)
		repo := newTeamGitRepo(t)
		router, stdout, _ := newTestRouter(t)

		code := router.Run(t.Context(), []string{"team", "status", "--repo", repo})

		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d\nstdout: %s", code, ExitOK, stdout)
		}
		if !strings.Contains(stdout.String(), "no files tracked by this repo's team lockfile") {
			t.Errorf("stdout does not contain the empty-lockfile message:\n%s", stdout)
		}
	})
}
