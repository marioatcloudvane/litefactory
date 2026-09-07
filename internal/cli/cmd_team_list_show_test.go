package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newEmptyTeamIndexFixture writes a minimal, valid team index with no teams
// at all, laid out the way a static-path override's root must look
// (<dir>/teams/index.json), for exercising `team list`'s empty-index case.
func newEmptyTeamIndexFixture(t *testing.T) string {
	t.Helper()
	dir := newTeamIndexFixture(t, "placeholder", "placeholder")
	// newTeamIndexFixture always seeds one team; overwrite its index with an
	// empty teams list, keeping the same directory layout.
	indexPath := filepath.Join(dir, "teams", "index.json")
	emptyIndexJSON := `{
  "schemaVersion": 1,
  "teams": [],
  "shared": {}
}
`
	if err := os.WriteFile(indexPath, []byte(emptyIndexJSON), 0o600); err != nil {
		t.Fatalf("overwrite team index with empty teams: %v", err)
	}
	return dir
}

// TestTeamList_HappyPath covers the populated-index case: one team, one
// agent, resolved via a static-path override, with the default (fallback)
// harness list since the repo has none of the harness directories present.
func TestTeamList_HappyPath(t *testing.T) {
	const teamName = "reviewers"
	const agentKey = "reviewer"
	fixtureDir := newTeamIndexFixture(t, teamName, agentKey)
	newTeamHome(t, fixtureDir)
	repo := newTeamGitRepo(t)
	router, stdout, stderr := newTestRouter(t)

	code := router.Run(t.Context(), []string{"team", "list", "--repo", repo})

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	out := stdout.String()
	wantLines := []string{
		"repo: " + repo,
		"index source: static-path",
		"harness: claude, codex (fallback)",
		teamName,
		agentKey,
		"agents/" + agentKey + ".md",
		".claude/agents/" + agentKey + ".md",
	}
	for _, want := range wantLines {
		if !strings.Contains(out, want) {
			t.Errorf("stdout does not contain %q:\n%s", want, out)
		}
	}
}

// TestTeamList_EmptyIndex covers a resolvable index with zero teams: the
// command must still succeed and say so explicitly, not print an empty table.
func TestTeamList_EmptyIndex(t *testing.T) {
	fixtureDir := newEmptyTeamIndexFixture(t)
	newTeamHome(t, fixtureDir)
	repo := newTeamGitRepo(t)
	router, stdout, stderr := newTestRouter(t)

	code := router.Run(t.Context(), []string{"team", "list", "--repo", repo})

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	out := stdout.String()
	if !strings.Contains(out, "no teams in this index") {
		t.Errorf("stdout does not contain empty-index message:\n%s", out)
	}
}

// TestTeamList_JSON covers --json mode: the top-level shape and every field
// team list documents, decoded into the real DTO type rather than matched by
// substring.
func TestTeamList_JSON(t *testing.T) {
	const teamName = "reviewers"
	const agentKey = "reviewer"
	fixtureDir := newTeamIndexFixture(t, teamName, agentKey)
	newTeamHome(t, fixtureDir)
	repo := newTeamGitRepo(t)
	router, stdout, stderr := newTestRouter(t)

	code := router.Run(t.Context(), []string{"team", "list", "--repo", repo, "--json"})

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	var got teamListJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
	}
	if got.Repo != repo {
		t.Errorf("Repo = %q, want %q", got.Repo, repo)
	}
	if got.IndexSource != "static-path" {
		t.Errorf("IndexSource = %q, want %q", got.IndexSource, "static-path")
	}
	if got.HarnessSource != "fallback" {
		t.Errorf("HarnessSource = %q, want %q", got.HarnessSource, "fallback")
	}
	if len(got.Teams) != 1 {
		t.Fatalf("len(Teams) = %d, want 1", len(got.Teams))
	}
	team := got.Teams[0]
	if team.Name != teamName {
		t.Errorf("Teams[0].Name = %q, want %q", team.Name, teamName)
	}
	if len(team.Agents) != 1 {
		t.Fatalf("len(Teams[0].Agents) = %d, want 1", len(team.Agents))
	}
	agent := team.Agents[0]
	if agent.Key != agentKey {
		t.Errorf("Agents[0].Key = %q, want %q", agent.Key, agentKey)
	}
	if agent.Source != "agents/"+agentKey+".md" {
		t.Errorf("Agents[0].Source = %q, want %q", agent.Source, "agents/"+agentKey+".md")
	}
	if agent.Dest != ".claude/agents/"+agentKey+".md" {
		t.Errorf("Agents[0].Dest = %q, want %q", agent.Dest, ".claude/agents/"+agentKey+".md")
	}
	if agent.Shared {
		t.Errorf("Agents[0].Shared = true, want false (team's own agent, not a shared one)")
	}
}

// TestTeamList_BadRepo covers the shared error path every team subcommand
// exercises identically via resolveTeamRepoPath: a --repo with no .git
// directory is a precondition failure, not a usage or generic error.
func TestTeamList_BadRepo(t *testing.T) {
	router, _, stderr := newTestRouter(t)

	code := router.Run(t.Context(), []string{"team", "list", "--repo", t.TempDir()})

	if code != ExitPrecondition {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitPrecondition, stderr)
	}
	if !strings.Contains(stderr.String(), "no git repository at") {
		t.Errorf("stderr does not mention %q:\n%s", "no git repository at", stderr)
	}
}

// TestTeamShow_HappyPath covers a single named team's resolved agents.
func TestTeamShow_HappyPath(t *testing.T) {
	const teamName = "reviewers"
	const agentKey = "reviewer"
	fixtureDir := newTeamIndexFixture(t, teamName, agentKey)
	newTeamHome(t, fixtureDir)
	repo := newTeamGitRepo(t)
	router, stdout, stderr := newTestRouter(t)

	code := router.Run(t.Context(), []string{"team", "show", teamName, "--repo", repo})

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	out := stdout.String()
	wantLines := []string{
		"repo: " + repo,
		"team: " + teamName,
		"index source: static-path",
		"harness: claude, codex (fallback)",
		agentKey,
		"agents/" + agentKey + ".md",
		".claude/agents/" + agentKey + ".md",
	}
	for _, want := range wantLines {
		if !strings.Contains(out, want) {
			t.Errorf("stdout does not contain %q:\n%s", want, out)
		}
	}
}

// TestTeamShow_UnknownTeam covers findTeam's error path: an unresolvable
// team name is a usage error (bad command-line input), not a generic
// runtime failure or precondition failure.
func TestTeamShow_UnknownTeam(t *testing.T) {
	const teamName = "reviewers"
	const agentKey = "reviewer"
	fixtureDir := newTeamIndexFixture(t, teamName, agentKey)
	newTeamHome(t, fixtureDir)
	repo := newTeamGitRepo(t)
	router, _, stderr := newTestRouter(t)

	code := router.Run(t.Context(), []string{"team", "show", "no-such-team", "--repo", repo})

	if code != ExitUsage {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitUsage, stderr)
	}
	if !strings.Contains(stderr.String(), `unknown team "no-such-team"`) {
		t.Errorf("stderr does not mention the unknown team name:\n%s", stderr)
	}
}

// TestTeamShow_JSON covers --json mode: the top-level shape and every field
// team show documents, decoded into the real DTO type rather than matched by
// substring.
func TestTeamShow_JSON(t *testing.T) {
	const teamName = "reviewers"
	const agentKey = "reviewer"
	fixtureDir := newTeamIndexFixture(t, teamName, agentKey)
	newTeamHome(t, fixtureDir)
	repo := newTeamGitRepo(t)
	router, stdout, stderr := newTestRouter(t)

	code := router.Run(t.Context(), []string{"team", "show", teamName, "--repo", repo, "--json"})

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	var got teamShowJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
	}
	if got.Repo != repo {
		t.Errorf("Repo = %q, want %q", got.Repo, repo)
	}
	if got.Team != teamName {
		t.Errorf("Team = %q, want %q", got.Team, teamName)
	}
	if got.IndexSource != "static-path" {
		t.Errorf("IndexSource = %q, want %q", got.IndexSource, "static-path")
	}
	if got.HarnessSource != "fallback" {
		t.Errorf("HarnessSource = %q, want %q", got.HarnessSource, "fallback")
	}
	if len(got.Agents) != 1 {
		t.Fatalf("len(Agents) = %d, want 1", len(got.Agents))
	}
	agent := got.Agents[0]
	if agent.Key != agentKey {
		t.Errorf("Agents[0].Key = %q, want %q", agent.Key, agentKey)
	}
	if agent.IndexSource != "static-path" {
		t.Errorf("Agents[0].IndexSource = %q, want %q", agent.IndexSource, "static-path")
	}
	if agent.Shared {
		t.Errorf("Agents[0].Shared = true, want false (team's own agent, not a shared one)")
	}
}
