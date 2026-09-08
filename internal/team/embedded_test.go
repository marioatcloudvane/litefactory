package team

import (
	"io/fs"
	"sort"
	"testing"
)

// goTeamSpecialistKeys are the 5 Go-specialist agents US-17's worked example
// names inline under go-team's own agents map.
var goTeamSpecialistKeys = []string{
	"go-architect",
	"go-implementation-engineer",
	"go-code-reviewer",
	"go-unit-test-engineer",
	"go-integration-test-engineer",
}

// sharedGenericKeys are the generic/orchestration agents that actually still
// exist as agent definitions in this repo. app-context-updater was retired
// (its .claude/agents/app-context-updater.md source no longer exists) and is
// deliberately excluded — see internal/team/embedded.go's package comment.
var sharedGenericKeys = []string{
	"codebase-tour",
	"web-researcher",
	"requirements-engineer",
	"proxy-product-owner",
	"research-orchestrator",
	"plan-orchestrator",
	"implement-orchestrator",
}

// hostLevelToolingKeys are the tooling entries that appear in the same agent
// listing but are not part of litefactory's own team roster (AC-2): host
// utilities, never installed into a repo by this feature.
var hostLevelToolingKeys = []string{
	"claude",
	"Explore",
	"general-purpose",
	"Plan",
	"statusline-setup",
}

// TestEmbeddedDecodesUnderStrictDecoder exercises US-1's strict two-pass
// decoder against the real shipped snapshot, not a synthetic fixture: this
// uses the same strict decoder as production; these historical bytes are test-only.
func TestEmbeddedDecodesUnderStrictDecoder(t *testing.T) {
	idx, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded() = _, %v; want a clean decode of the shipped snapshot", err)
	}
	if errs := Validate(idx); len(errs) != 0 {
		t.Fatalf("Validate(Embedded()) = %v; want no faults", errs)
	}
}

// TestEmbeddedGoTeamHasExactlyTheGoSpecialistAgents covers AC-1's go-team
// half: the inline agents map names exactly the 5 Go-specialist agents, no
// more, no fewer.
func TestEmbeddedGoTeamHasExactlyTheGoSpecialistAgents(t *testing.T) {
	idx, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded(): %v", err)
	}
	if len(idx.Teams) != 1 {
		t.Fatalf("len(Teams) = %d, want 1 (go-team)", len(idx.Teams))
	}
	goTeam, ok := idx.Teams["go-team"]
	if !ok {
		t.Fatal("missing go-team")
	}
	wantKeys := append([]string(nil), goTeamSpecialistKeys...)
	gotKeys := make([]string, 0, len(inlineAgents(goTeam)))
	for k := range inlineAgents(goTeam) {
		gotKeys = append(gotKeys, k)
	}
	sort.Strings(wantKeys)
	sort.Strings(gotKeys)
	if !equalStrings(gotKeys, wantKeys) {
		t.Fatalf("go-team agent keys = %v, want %v", gotKeys, wantKeys)
	}
}

// TestEmbeddedSharedHasExactlyTheSurvivingGenericAgents covers AC-1's shared
// half: the top-level shared map contains exactly the generic/orchestration
// agents that still exist as agent definitions in this repo (7, not the
// originally-scoped 8 — app-context-updater was retired) and AC-2: none of
// the 5 host-level tooling entries.
func TestEmbeddedSharedHasExactlyTheSurvivingGenericAgents(t *testing.T) {
	idx, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded(): %v", err)
	}
	wantKeys := append([]string(nil), sharedGenericKeys...)
	gotKeys := make([]string, 0, len(idx.Shared))
	for k := range idx.Shared {
		gotKeys = append(gotKeys, k)
	}
	sort.Strings(wantKeys)
	sort.Strings(gotKeys)
	if !equalStrings(gotKeys, wantKeys) {
		t.Fatalf("shared agent keys = %v, want %v", gotKeys, wantKeys)
	}
	if _, ok := idx.Shared["app-context-updater"]; ok {
		t.Error("shared map still names app-context-updater, which has no surviving agent definition")
	}
	for _, tool := range hostLevelToolingKeys {
		if _, ok := idx.Shared[tool]; ok {
			t.Errorf("shared map names host-level tooling entry %q (AC-2)", tool)
		}
		for name, team := range idx.Teams {
			if _, ok := inlineAgents(team)[tool]; ok {
				t.Errorf("team %q names host-level tooling entry %q (AC-2)", name, tool)
			}
		}
	}
}

// TestEmbeddedSourcesAllResolve covers AC-3: every source path in the
// go-team worked example (and the shared map it draws on) resolves to a
// markdown file that actually exists in the shipped snapshot.
func TestEmbeddedSourcesAllResolve(t *testing.T) {
	idx, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded(): %v", err)
	}
	fsys, err := EmbeddedFS()
	if err != nil {
		t.Fatalf("EmbeddedFS(): %v", err)
	}

	checked := 0
	for name, team := range idx.Teams {
		for key, entry := range inlineAgents(team) {
			assertSourceReadable(t, fsys, name+"."+key, entry)
			checked++
		}
	}
	for key, entry := range idx.Shared {
		assertSourceReadable(t, fsys, "shared."+key, entry)
		checked++
	}
	if want := len(goTeamSpecialistKeys) + len(sharedGenericKeys); checked != want {
		t.Fatalf("checked %d agent entries, want %d", checked, want)
	}
}

// assertSourceReadable fails t if entry.Path cannot be read from fsys, or
// reads as empty.
func assertSourceReadable(t *testing.T, fsys fs.FS, label string, entry AgentEntry) {
	t.Helper()
	data, err := fs.ReadFile(fsys, entry.Path)
	if err != nil {
		t.Errorf("%s: read Source %q: %v", label, entry.Path, err)
		return
	}
	if len(data) == 0 {
		t.Errorf("%s: Source %q read as empty", label, entry.Path)
	}
}

// equalStrings reports whether a and b hold the same elements in the same
// order (both are expected pre-sorted by the caller).
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func inlineAgents(t Entry) map[string]AgentEntry {
	agents := map[string]AgentEntry{}
	for _, mode := range Modes() {
		entries, _ := t.Mode(mode)
		for _, a := range entries {
			if a.Ref == "" {
				agents[a.ID] = a
			}
		}
	}
	return agents
}
