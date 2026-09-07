package teamsource

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/team"
)

// forbiddenFlagFixture is claude's permission-skipping flag, assembled from
// fragments so this file does not itself trip internal/config's whole-tree
// TestForbiddenFlagIsAbsentFromTheTree scan (see internal/config/policy.go's
// forbiddenFlag and purity_test.go, and internal/team's own
// write_forbidden_flag_test.go, which follows the same convention).
const forbiddenFlagFixture = "--dangerously-" + "skip-permissions"

// secretFixture is a literal credential shape config.ClassifySecret
// recognizes (a GitHub token).
const secretFixture = "ghp_teamsourcefixture0123456789abcdef"

// The four tests below exercise US-14's AC-1 end to end, through the real
// production pipeline (RenderTeam + team.Add), for both places offending
// content can originate: the index-resolved source read from fsys, and a
// US-13 override pointing at a local file. In both cases the check runs
// against the same rendered bytes at the same call site (team.Add), so the
// write is refused identically regardless of origin, and no artifact from
// the run lands on disk.

func TestRenderTeamThenAdd_IndexSourcedForbiddenFlag_RefusesWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fsys := fstest.MapFS{
		"agents/reviewer.md": &fstest.MapFile{
			Data: []byte(validAgentSource("reviewer", "reviews code", "Run with "+forbiddenFlagFixture)),
		},
	}
	roster := []RosterEntry{
		{Key: "reviewer", Agent: team.AgentEntry{Source: "agents/reviewer.md", Dest: ".claude/agents/reviewer.md"}},
	}

	renders, err := RenderTeam(root, fsys, "team-a", roster, []string{"claude"})
	if err != nil {
		t.Fatalf("RenderTeam err = %v, want nil (rendering itself does not scan content)", err)
	}

	_, addErr := team.Add(root, renders)
	if !errors.Is(addErr, team.ErrForbiddenFlag) {
		t.Fatalf("team.Add err = %v, want team.ErrForbiddenFlag", addErr)
	}

	if _, statErr := os.Stat(filepath.Join(root, ".claude/agents/reviewer.md")); !os.IsNotExist(statErr) {
		t.Errorf("standalone file was written despite a forbidden flag in the index-resolved source")
	}
	if has, hasErr := team.HasLockfile(root); hasErr != nil || has {
		t.Errorf("HasLockfile = (%v, %v), want (false, nil): no lockfile written after a refused write", has, hasErr)
	}
}

func TestRenderTeamThenAdd_IndexSourcedSecret_RefusesWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fsys := fstest.MapFS{
		"agents/reviewer.md": &fstest.MapFile{
			Data: []byte(validAgentSource("reviewer", "reviews code", "token: "+secretFixture)),
		},
	}
	roster := []RosterEntry{
		{Key: "reviewer", Agent: team.AgentEntry{Source: "agents/reviewer.md", Dest: ".claude/agents/reviewer.md"}},
	}

	renders, err := RenderTeam(root, fsys, "team-a", roster, []string{"claude"})
	if err != nil {
		t.Fatalf("RenderTeam err = %v, want nil", err)
	}

	_, addErr := team.Add(root, renders)
	if !errors.Is(addErr, team.ErrSecretDetected) {
		t.Fatalf("team.Add err = %v, want team.ErrSecretDetected", addErr)
	}

	if _, statErr := os.Stat(filepath.Join(root, ".claude/agents/reviewer.md")); !os.IsNotExist(statErr) {
		t.Errorf("standalone file was written despite a secret in the index-resolved source")
	}
	if has, hasErr := team.HasLockfile(root); hasErr != nil || has {
		t.Errorf("HasLockfile = (%v, %v), want (false, nil): no lockfile written after a refused write", has, hasErr)
	}
}

func TestRenderTeamThenAdd_OverrideSourcedForbiddenFlag_RefusesWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fsys := fstest.MapFS{
		"agents/reviewer.md": &fstest.MapFile{
			Data: []byte(validAgentSource("reviewer", "reviews code", "Index-resolved, clean body.")),
		},
	}
	overrideContent := validAgentSource("reviewer", "reviews code", "Run with "+forbiddenFlagFixture)
	writeLocalOverrideFile(t, root, "local/reviewer.md", overrideContent)
	if err := team.SetOverride(root, "reviewer", "local/reviewer.md"); err != nil {
		t.Fatalf("SetOverride err = %v, want nil", err)
	}
	roster := []RosterEntry{
		{Key: "reviewer", Agent: team.AgentEntry{Source: "agents/reviewer.md", Dest: ".claude/agents/reviewer.md"}},
	}

	renders, err := RenderTeam(root, fsys, "team-a", roster, []string{"claude"})
	if err != nil {
		t.Fatalf("RenderTeam err = %v, want nil", err)
	}

	_, addErr := team.Add(root, renders)
	if !errors.Is(addErr, team.ErrForbiddenFlag) {
		t.Fatalf("team.Add err = %v, want team.ErrForbiddenFlag (override-introduced flag must be refused just like an index-sourced one)", addErr)
	}
	var ffErr *team.ForbiddenFlagError
	if !errors.As(addErr, &ffErr) {
		t.Fatalf("team.Add err = %v, want *team.ForbiddenFlagError", addErr)
	}
	if ffErr.AgentKey != "reviewer" {
		t.Errorf("ForbiddenFlagError.AgentKey = %q, want %q", ffErr.AgentKey, "reviewer")
	}
	if ffErr.Harness != "claude" {
		t.Errorf("ForbiddenFlagError.Harness = %q, want %q", ffErr.Harness, "claude")
	}

	if _, statErr := os.Stat(filepath.Join(root, ".claude/agents/reviewer.md")); !os.IsNotExist(statErr) {
		t.Errorf("standalone file was written despite a forbidden flag introduced via a US-13 override")
	}
	if has, hasErr := team.HasLockfile(root); hasErr != nil || has {
		t.Errorf("HasLockfile = (%v, %v), want (false, nil): no lockfile written after a refused write", has, hasErr)
	}
}

func TestRenderTeamThenAdd_OverrideSourcedSecret_RefusesWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fsys := fstest.MapFS{
		"agents/reviewer.md": &fstest.MapFile{
			Data: []byte(validAgentSource("reviewer", "reviews code", "Index-resolved, clean body.")),
		},
	}
	overrideContent := validAgentSource("reviewer", "reviews code", "token: "+secretFixture)
	writeLocalOverrideFile(t, root, "local/reviewer.md", overrideContent)
	if err := team.SetOverride(root, "reviewer", "local/reviewer.md"); err != nil {
		t.Fatalf("SetOverride err = %v, want nil", err)
	}
	roster := []RosterEntry{
		{Key: "reviewer", Agent: team.AgentEntry{Source: "agents/reviewer.md", Dest: ".claude/agents/reviewer.md"}},
	}

	renders, err := RenderTeam(root, fsys, "team-a", roster, []string{"claude"})
	if err != nil {
		t.Fatalf("RenderTeam err = %v, want nil", err)
	}

	_, addErr := team.Add(root, renders)
	if !errors.Is(addErr, team.ErrSecretDetected) {
		t.Fatalf("team.Add err = %v, want team.ErrSecretDetected (override-introduced secret must be refused just like an index-sourced one)", addErr)
	}
	var se *team.SecretDetectedError
	if !errors.As(addErr, &se) {
		t.Fatalf("team.Add err = %v, want *team.SecretDetectedError", addErr)
	}
	if se.AgentKey != "reviewer" {
		t.Errorf("SecretDetectedError.AgentKey = %q, want %q", se.AgentKey, "reviewer")
	}
	if se.Harness != "claude" {
		t.Errorf("SecretDetectedError.Harness = %q, want %q", se.Harness, "claude")
	}

	if _, statErr := os.Stat(filepath.Join(root, ".claude/agents/reviewer.md")); !os.IsNotExist(statErr) {
		t.Errorf("standalone file was written despite a secret introduced via a US-13 override")
	}
	if has, hasErr := team.HasLockfile(root); hasErr != nil || has {
		t.Errorf("HasLockfile = (%v, %v), want (false, nil): no lockfile written after a refused write", has, hasErr)
	}
}
