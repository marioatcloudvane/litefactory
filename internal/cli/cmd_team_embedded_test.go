package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

// newTeamHomeEmbedded points $LF_HOME at a fresh temporary directory with an
// untouched default config.yaml — settings.team_config left at its default
// empty value — so that resolving the team index falls all the way through
// to tier 1, the embedded default (internal/teamsource.ResolveEmbedded).
// Unlike newTeamHome, this never writes a static-path override.
func newTeamHomeEmbedded(t *testing.T) config.Paths {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	paths := config.NewPaths(home)
	if _, err := config.WriteDefault(paths); err != nil {
		t.Fatalf("write default config: %v", err)
	}
	return paths
}

// wantGoTeamSpecialistKeys are the 5 Go-specialist agents US-17 names
// inline under go-team's own agents map in the embedded default index.
var wantGoTeamSpecialistKeys = []string{
	"go-architect",
	"go-implementation-engineer",
	"go-code-reviewer",
	"go-unit-test-engineer",
	"go-integration-test-engineer",
}

// wantSharedGenericKeys are the generic/orchestration agents US-17 places in
// the embedded default index's top-level shared map.
var wantSharedGenericKeys = []string{
	"codebase-tour",
	"web-researcher",
	"requirements-engineer",
	"proxy-product-owner",
	"research-orchestrator",
	"plan-orchestrator",
	"implement-orchestrator",
}

// TestTeamShow_Embedded_GoTeam covers T-63's CLI-facing half: `team show
// go-team` against the embedded default index (no static-path or
// registered-repo override configured) surfaces exactly the 5 Go-specialist
// agents as go-team's own, plus the shared agents unioned in, with none
// marked Shared incorrectly.
func TestTeamShow_Embedded_GoTeam(t *testing.T) {
	newTeamHomeEmbedded(t)
	repo := newTeamGitRepo(t)
	router, stdout, stderr := newTestRouter(t)

	code := router.Run(t.Context(), []string{"team", "show", "go-team", "--repo", repo, "--json"})

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	var got teamShowJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode json: %v\nstdout: %s", err, stdout)
	}
	if got.IndexSource != "embedded" {
		t.Errorf("IndexSource = %q, want %q", got.IndexSource, "embedded")
	}
	if got.Team != "go-team" {
		t.Errorf("Team = %q, want %q", got.Team, "go-team")
	}

	gotOwn := map[string]bool{}
	gotShared := map[string]bool{}
	for _, a := range got.Agents {
		if a.Shared {
			gotShared[a.Key] = true
		} else {
			gotOwn[a.Key] = true
		}
	}

	for _, key := range wantGoTeamSpecialistKeys {
		if !gotOwn[key] {
			t.Errorf("go-team's own agents missing %q (got own=%v)", key, gotOwn)
		}
	}
	if len(gotOwn) != len(wantGoTeamSpecialistKeys) {
		t.Errorf("go-team's own agents = %v, want exactly %v", gotOwn, wantGoTeamSpecialistKeys)
	}

	for _, key := range wantSharedGenericKeys {
		if !gotShared[key] {
			t.Errorf("shared agents missing %q (got shared=%v)", key, gotShared)
		}
	}
	if len(gotShared) != len(wantSharedGenericKeys) {
		t.Errorf("shared agents = %v, want exactly %v", gotShared, wantSharedGenericKeys)
	}

	for _, tool := range []string{"claude", "Explore", "general-purpose", "Plan", "statusline-setup"} {
		if gotOwn[tool] || gotShared[tool] {
			t.Errorf("host-level tooling entry %q leaked into resolved roster", tool)
		}
	}
}

// TestTeamList_Embedded covers `team list` (no --json) against the embedded
// default index: it must report "index source: embedded" and list go-team
// with its full merged roster.
func TestTeamList_Embedded(t *testing.T) {
	newTeamHomeEmbedded(t)
	repo := newTeamGitRepo(t)
	router, stdout, stderr := newTestRouter(t)

	code := router.Run(t.Context(), []string{"team", "list", "--repo", repo})

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	out := stdout.String()
	wantSubstrings := append([]string{"index source: embedded", "go-team"},
		append(append([]string{}, wantGoTeamSpecialistKeys...), wantSharedGenericKeys...)...)
	for _, want := range wantSubstrings {
		if !strings.Contains(out, want) {
			t.Errorf("stdout does not contain %q:\n%s", want, out)
		}
	}
}
