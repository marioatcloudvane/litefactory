package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/team"
)

// newTeamGitRepo builds a bare-minimum git-shaped repo directory: just a
// .git directory, which is all resolveTeamRepoPath checks for. Tests that
// need harness auto-detection or a real commit history use gittest.New
// instead; tests here only need "this path looks like a repo" to get past
// the first resolution step.
func newTeamGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o777); err != nil {
		t.Fatalf("create .git: %v", err)
	}
	return dir
}

// validTeamAgentSource is a minimal, valid agent markdown source: frontmatter
// with just name/description, no Extra fields, rendering cleanly under the
// claude renderer.
func validTeamAgentSource(name, description, body string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body + "\n"
}

// newTeamIndexFixture writes a minimal, valid team index plus one agent
// markdown source under a fresh temporary directory laid out the way
// internal/teamsource expects a static-path override's root to look:
// <dir>/teams/index.json and <dir>/teams/agents/<file>.md. It returns dir,
// the static-path override value to put in config.yaml's settings.team_config.
func newTeamIndexFixture(t *testing.T, teamName, agentKey string) string {
	t.Helper()
	dir := t.TempDir()
	teamsDir := filepath.Join(dir, "teams")
	agentsDir := filepath.Join(teamsDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o777); err != nil {
		t.Fatalf("create agents dir: %v", err)
	}

	agentSource := validTeamAgentSource(agentKey, "a test reviewer agent", "Review the code.")
	if err := os.WriteFile(filepath.Join(agentsDir, agentKey+".md"), []byte(agentSource), 0o600); err != nil {
		t.Fatalf("write agent source: %v", err)
	}

	indexJSON := `{
  "schemaVersion": 1,
  "teams": [
    {
      "name": "` + teamName + `",
      "agents": {
        "` + agentKey + `": {
          "source": "agents/` + agentKey + `.md",
          "dest": ".claude/agents/` + agentKey + `.md"
        }
      }
    }
  ],
  "shared": {}
}
`
	if err := os.WriteFile(filepath.Join(teamsDir, "index.json"), []byte(indexJSON), 0o600); err != nil {
		t.Fatalf("write team index: %v", err)
	}
	return dir
}

// newTeamHome points $LF_HOME at a fresh temporary directory and writes a
// config.yaml overriding settings.team_config at fixtureDir, without going
// through config.WriteDefault (whose O_EXCL guard makes it unsuitable for
// pre-seeding an override).
// team add's happy path needs no database: openTeamRuntime only opens one
// when paths.DBPath() already exists on disk, and the static-path override
// resolves the team index without one.
func newTeamHome(t *testing.T, fixtureDir string) config.Paths {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	paths := config.NewPaths(home)
	if err := paths.EnsureHome(); err != nil {
		t.Fatalf("ensure home: %v", err)
	}
	// Build on the canonical default rather than a hand-rolled minimal file:
	// config validation requires whole sections (tool_profiles.permissions and
	// friends) that a two-line YAML cannot satisfy.
	yaml := strings.Replace(string(config.DefaultFileContents()),
		`team_config: ""`, `team_config: "`+fixtureDir+`"`, 1)
	if !strings.Contains(yaml, fixtureDir) {
		t.Fatalf("default config.yaml no longer carries an empty team_config key to override")
	}
	if err := os.WriteFile(paths.ConfigPath(), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return paths
}

// TestTeamAddRejections covers the failure paths that reach the user as an
// exit code and a message, without ever reaching a successful write.
func TestTeamAddRejections(t *testing.T) {
	tests := []struct {
		name     string
		args     func(t *testing.T) []string
		wantCode int
		wantHas  []string
	}{
		{
			name: "missing team argument",
			args: func(*testing.T) []string {
				return []string{"team", "add"}
			},
			wantCode: ExitUsage,
			wantHas:  []string{"missing argument"},
		},
		{
			name: "unknown --harness value",
			args: func(t *testing.T) []string {
				repo := newTeamGitRepo(t)
				return []string{"team", "add", "some-team", "--repo", repo, "--harness", "bogus-harness"}
			},
			wantCode: ExitUsage,
			wantHas:  []string{`unknown harness "bogus-harness"`, "claude, codex, opencode"},
		},
		{
			name: "repo path has no .git directory",
			args: func(t *testing.T) []string {
				return []string{"team", "add", "some-team", "--repo", t.TempDir()}
			},
			wantCode: ExitPrecondition,
			wantHas:  []string{"no git repository at"},
		},
		{
			name: "repo already has a team lockfile",
			args: func(t *testing.T) []string {
				repo := newTeamGitRepo(t)
				// Seed a real lockfile via a direct team.Add call, exactly the
				// artifact runTeamAdd checks for via team.HasLockfile before
				// ever opening a team runtime.
				renders := []team.AgentRender{
					{
						AgentKey:   "reviewer",
						Harness:    "claude",
						Teams:      []string{"other-team"},
						SourcePath: "agents/reviewer.md",
						SourceHash: "deadbeef",
						Files: []team.RenderedFile{
							{Path: ".claude/agents/reviewer.md", Bytes: []byte("reviewer body\n")},
						},
					},
				}
				if _, err := team.Add(repo, renders); err != nil {
					t.Fatalf("seed lockfile: %v", err)
				}
				return []string{"team", "add", "some-team", "--repo", repo, "--harness", "claude"}
			},
			wantCode: ExitFailure,
			wantHas:  []string{"already has a team lockfile", "litefactory team update", "team add"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.args(t)
			router, _, stderr := newTestRouter(t)

			if code := router.Run(t.Context(), args); code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d\nstderr: %s", code, tt.wantCode, stderr)
			}
			for _, want := range tt.wantHas {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr does not mention %q:\n%s", want, stderr)
				}
			}
		})
	}
}

// TestTeamAddSuccess covers the happy path: a fresh repo, a static-path team
// index override resolved through config.yaml, and a single-agent,
// single-harness team add. It asserts on the printed summary and on the real
// filesystem artifacts team.Add produced, not merely on a successful exit.
func TestTeamAddSuccess(t *testing.T) {
	const teamName = "reviewers"
	const agentKey = "reviewer"

	fixtureDir := newTeamIndexFixture(t, teamName, agentKey)
	paths := newTeamHome(t, fixtureDir)
	_ = paths
	repo := gittest.New(t)
	router, stdout, stderr := newTestRouter(t)

	code := router.Run(t.Context(), []string{
		"team", "add", teamName,
		"--repo", repo.Dir,
		"--harness", "claude",
	})

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}

	out := stdout.String()
	wantLines := []string{
		"repo: " + repo.Dir,
		"team: " + teamName,
		"index source: static-path",
		"harness: claude (explicit)",
		"written: 2",
		"skipped (unchanged): 0",
	}
	for _, want := range wantLines {
		if !strings.Contains(out, want) {
			t.Errorf("stdout does not contain %q:\n%s", want, out)
		}
	}

	// Assert on real filesystem effects: the standalone agent file landed...
	agentFile := filepath.Join(repo.Dir, ".claude", "agents", agentKey+".md")
	body, err := os.ReadFile(agentFile)
	if err != nil {
		t.Fatalf("read rendered agent file: %v", err)
	}
	if !strings.Contains(string(body), "Review the code.") {
		t.Errorf("rendered agent file does not contain the source body:\n%s", body)
	}

	// ...and the provenance lockfile now exists, so a second `team add`
	// against the same repo would be refused.
	hasLock, err := team.HasLockfile(repo.Dir)
	if err != nil {
		t.Fatalf("check lockfile: %v", err)
	}
	if !hasLock {
		t.Errorf("HasLockfile = false, want true after a successful add")
	}
}

// TestTeamAddSuccess_ActiveOverride_SubstitutesLocalFile covers AC-1: an
// active override recorded for an agent key is honored by `team add` itself
// (not merely by teamsource.RenderTeam in isolation) -- the installed file's
// bytes come from the override's local file, not from the team index's
// source, for that key.
func TestTeamAddSuccess_ActiveOverride_SubstitutesLocalFile(t *testing.T) {
	const teamName = "reviewers"
	const agentKey = "reviewer"

	fixtureDir := newTeamIndexFixture(t, teamName, agentKey)
	newTeamHome(t, fixtureDir)
	repo := gittest.New(t)
	writeManageOverrideSource(t, repo.Dir, "local/reviewer.md")
	if err := team.SetOverride(repo.Dir, agentKey, "local/reviewer.md"); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}

	router, _, stderr := newTestRouter(t)
	code := router.Run(t.Context(), []string{
		"team", "add", teamName,
		"--repo", repo.Dir,
		"--harness", "claude",
	})
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}

	agentFile := filepath.Join(repo.Dir, ".claude", "agents", agentKey+".md")
	body, err := os.ReadFile(agentFile)
	if err != nil {
		t.Fatalf("read rendered agent file: %v", err)
	}
	if !strings.Contains(string(body), "Patched body.") {
		t.Errorf("rendered agent file = %q, want it to contain the override's body", body)
	}
	if strings.Contains(string(body), "Review the code.") {
		t.Errorf("rendered agent file = %q, want it NOT to contain the team index's original body", body)
	}
}
