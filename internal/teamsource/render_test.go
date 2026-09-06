package teamsource

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/render"
	"github.com/cloudvane/litefactory/internal/team"
)

// validAgentSource is a minimal, valid agent markdown source: frontmatter
// with just name/description, no Extra fields, so it renders cleanly under
// both the claude and codex renderers.
func validAgentSource(name, description, body string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body + "\n"
}

func TestParseAgentSource(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantErr    string
		wantName   string
		wantBody   string
	}{
		{
			name:     "valid frontmatter and body/parses name and body",
			raw:      "---\nname: reviewer\ndescription: reviews code\n---\nDo the review.\n",
			wantName: "reviewer",
			wantBody: "Do the review.\n",
		},
		{
			name:    "missing leading delimiter/returns error",
			raw:     "name: reviewer\ndescription: reviews code\n---\nDo the review.\n",
			wantErr: `missing leading "---" frontmatter delimiter`,
		},
		{
			name:    "unterminated frontmatter block/returns error",
			raw:     "---\nname: reviewer\ndescription: reviews code\nDo the review.\n",
			wantErr: `unterminated frontmatter block`,
		},
		{
			name:    "invalid yaml in frontmatter/returns error",
			raw:     "---\nname: [reviewer\n---\nDo the review.\n",
			wantErr: "decode frontmatter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseAgentSource([]byte(tt.raw))

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("err = nil, want error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %q, want to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if got.Frontmatter.Name != tt.wantName {
				t.Errorf("Frontmatter.Name = %q, want %q", got.Frontmatter.Name, tt.wantName)
			}
			if got.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", got.Body, tt.wantBody)
			}
		})
	}
}

func TestRenderTeam_SingleHarnessSingleAgent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fsys := fstest.MapFS{
		"agents/reviewer.md": &fstest.MapFile{
			Data: []byte(validAgentSource("reviewer", "reviews code", "Review the code.")),
		},
	}
	roster := []RosterEntry{
		{Key: "reviewer", Agent: team.AgentEntry{Source: "agents/reviewer.md", Dest: ".claude/agents/reviewer.md"}},
	}

	got, err := RenderTeam(root, fsys, "team-a", roster, []string{"claude"})

	if err != nil {
		t.Fatalf("RenderTeam err = %v, want nil", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(renders) = %d, want 1", len(got))
	}
	ar := got[0]
	if ar.AgentKey != "reviewer" {
		t.Errorf("AgentKey = %q, want %q", ar.AgentKey, "reviewer")
	}
	if ar.Harness != "claude" {
		t.Errorf("Harness = %q, want %q", ar.Harness, "claude")
	}
	if got, want := ar.Teams, []string{"team-a"}; len(got) != len(want) || got[0] != want[0] {
		t.Errorf("Teams = %v, want %v", got, want)
	}
	if ar.SourcePath != "agents/reviewer.md" {
		t.Errorf("SourcePath = %q, want %q", ar.SourcePath, "agents/reviewer.md")
	}
	if ar.SourceHash != team.HashBytes([]byte(validAgentSource("reviewer", "reviews code", "Review the code."))) {
		t.Errorf("SourceHash does not match expected hash of source bytes")
	}
	// claude produces two files: the standalone agent file and a CLAUDE.md section.
	if len(ar.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2", len(ar.Files))
	}
}

func TestRenderTeam_MultipleHarnesses(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fsys := fstest.MapFS{
		"agents/reviewer.md": &fstest.MapFile{
			Data: []byte(validAgentSource("reviewer", "reviews code", "Review the code.")),
		},
	}
	roster := []RosterEntry{
		{Key: "reviewer", Agent: team.AgentEntry{Source: "agents/reviewer.md", Dest: ".claude/agents/reviewer.md"}},
	}

	got, err := RenderTeam(root, fsys, "team-a", roster, []string{"claude", "codex"})

	if err != nil {
		t.Fatalf("RenderTeam err = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(renders) = %d, want 2 (one per harness)", len(got))
	}
	byHarness := map[string]team.AgentRender{}
	for _, ar := range got {
		byHarness[ar.Harness] = ar
	}
	claudeRender, ok := byHarness["claude"]
	if !ok {
		t.Fatalf("no render found for harness claude")
	}
	if len(claudeRender.Files) != 2 {
		t.Errorf("claude Files count = %d, want 2", len(claudeRender.Files))
	}
	codexRender, ok := byHarness["codex"]
	if !ok {
		t.Fatalf("no render found for harness codex")
	}
	if len(codexRender.Files) != 1 {
		t.Errorf("codex Files count = %d, want 1", len(codexRender.Files))
	}
}

func TestRenderTeam_FirstEverAdd_TeamsIsJustCurrentTeam(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fsys := fstest.MapFS{
		"agents/shared.md": &fstest.MapFile{
			Data: []byte(validAgentSource("shared", "a shared agent", "Body.")),
		},
	}
	roster := []RosterEntry{
		{Key: "shared", Agent: team.AgentEntry{Source: "agents/shared.md", Dest: ".claude/agents/shared.md"}},
	}

	got, err := RenderTeam(root, fsys, "team-a", roster, []string{"claude"})

	if err != nil {
		t.Fatalf("RenderTeam err = %v, want nil", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(renders) = %d, want 1", len(got))
	}
	if teams := got[0].Teams; len(teams) != 1 || teams[0] != "team-a" {
		t.Errorf("Teams = %v, want [team-a] (no prior lockfile, only the current team contributes)", teams)
	}
}

func TestRenderTeam_SharedAgentWithExistingContributingTeam_UnionsTeams(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fsys := fstest.MapFS{
		"agents/shared.md": &fstest.MapFile{
			Data: []byte(validAgentSource("shared", "a shared agent", "Body.")),
		},
	}
	roster := []RosterEntry{
		{Key: "shared", Agent: team.AgentEntry{Source: "agents/shared.md", Dest: ".claude/agents/shared.md"}},
	}

	// Arrange: team-a adds the shared agent first, producing a real lockfile
	// on disk that records team-a as a contributor.
	firstRenders, err := RenderTeam(root, fsys, "team-a", roster, []string{"claude"})
	if err != nil {
		t.Fatalf("RenderTeam (team-a) err = %v, want nil", err)
	}
	if _, err := team.Add(root, firstRenders); err != nil {
		t.Fatalf("team.Add (team-a) err = %v, want nil", err)
	}

	// Act: team-b now adds the same shared agent.
	secondRenders, err := RenderTeam(root, fsys, "team-b", roster, []string{"claude"})

	// Assert: the union of contributing teams includes both, with team-b
	// (the current invocation) appended last, per RenderTeam's documented
	// ordering.
	if err != nil {
		t.Fatalf("RenderTeam (team-b) err = %v, want nil", err)
	}
	if len(secondRenders) != 1 {
		t.Fatalf("len(renders) = %d, want 1", len(secondRenders))
	}
	wantTeams := []string{"team-a", "team-b"}
	gotTeams := secondRenders[0].Teams
	if len(gotTeams) != len(wantTeams) {
		t.Fatalf("Teams = %v, want %v", gotTeams, wantTeams)
	}
	for i := range wantTeams {
		if gotTeams[i] != wantTeams[i] {
			t.Fatalf("Teams = %v, want %v", gotTeams, wantTeams)
		}
	}
}

func TestRenderTeam_MalformedFrontmatterSource_ReturnsWrappedError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fsys := fstest.MapFS{
		"agents/broken.md": &fstest.MapFile{
			Data: []byte("no frontmatter here, just prose.\n"),
		},
	}
	roster := []RosterEntry{
		{Key: "broken", Agent: team.AgentEntry{Source: "agents/broken.md", Dest: ".claude/agents/broken.md"}},
	}

	got, err := RenderTeam(root, fsys, "team-a", roster, []string{"claude"})

	if err == nil {
		t.Fatalf("err = nil, want error for malformed frontmatter")
	}
	if got != nil {
		t.Errorf("renders = %v, want nil on error", got)
	}
	if !strings.Contains(err.Error(), "agents/broken.md") {
		t.Errorf("err = %q, want to mention the source path %q", err.Error(), "agents/broken.md")
	}
	if !strings.Contains(err.Error(), "missing leading") {
		t.Errorf("err = %q, want to contain the underlying parse error", err.Error())
	}
}

func TestRenderTeam_UnknownHarness_ReturnsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fsys := fstest.MapFS{
		"agents/reviewer.md": &fstest.MapFile{
			Data: []byte(validAgentSource("reviewer", "reviews code", "Review the code.")),
		},
	}
	roster := []RosterEntry{
		{Key: "reviewer", Agent: team.AgentEntry{Source: "agents/reviewer.md", Dest: ".claude/agents/reviewer.md"}},
	}

	got, err := RenderTeam(root, fsys, "team-a", roster, []string{"bogus-harness"})

	if err == nil {
		t.Fatalf("err = nil, want error for unknown harness")
	}
	if got != nil {
		t.Errorf("renders = %v, want nil on error", got)
	}
	var unknownErr *render.UnknownHarnessError
	if !errors.As(err, &unknownErr) {
		t.Fatalf("err = %v, want to wrap *render.UnknownHarnessError", err)
	}
	if unknownErr.Harness != "bogus-harness" {
		t.Errorf("UnknownHarnessError.Harness = %q, want %q", unknownErr.Harness, "bogus-harness")
	}
	if !errors.Is(err, render.ErrUnknownHarness) {
		t.Errorf("errors.Is(err, render.ErrUnknownHarness) = false, want true")
	}
}
