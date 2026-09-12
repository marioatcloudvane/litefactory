package teamsource

import (
	"os"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/render"
)

func TestResearchStandaloneContract(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../teams/source")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureRoot(t.Context(), root, "go-team", "research")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Agents) != 1 {
		t.Fatalf("Research roster = %d, want one direct worker", len(snapshot.Agents))
	}
	if _, ok := snapshot.Files["skills/workflow/SKILL.md"]; ok {
		t.Fatal("Research captured unrelated story and maintenance protocols")
	}
	contract := string(snapshot.Files["skills/research-evidence/SKILL.md"])
	for _, obligation := range []string{"Before substantial investigation", "ask before choosing", "expected behavior, actual", "mark Research incomplete", "Never wait or poll", "do not re-ask", "first run", "MANIFEST.md", "RESEARCH_BRIEF.md"} {
		if !strings.Contains(contract, obligation) {
			t.Errorf("missing Research obligation %q", obligation)
		}
	}
	for path, raw := range snapshot.Files {
		// index.json describes other teams; it is not a Research instruction.
		if path == "index.json" {
			continue
		}
		for _, forbidden := range []string{".claude/", ".codex/", "Claude", "Codex", "story-contract-instruction-manifest"} {
			if strings.Contains(string(raw), forbidden) {
				t.Errorf("Research dependency contains %q", forbidden)
			}
		}
	}
	role := snapshot.Agents[0]
	for _, provider := range []string{"claude", "codex"} {
		file, err := render.Native(provider, render.NativeRole{Source: role.Source, Model: "test-model", Tools: role.Source.Frontmatter.Tools, Skills: role.Definition.Skills}, "workspace-write")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(file.Bytes), "research-evidence") {
			t.Fatalf("%s render lost the neutral contract", provider)
		}
	}
	var installedBody string
	for _, path := range []string{".claude/agents/research-orchestrator.md", ".codex/skills/research-orchestrator/SKILL.md"} {
		body := stripFrontmatter(readRepoFile(t, path))
		if !strings.Contains(body, "teams/source/skills/research-evidence/SKILL.md") {
			t.Errorf("%s lacks neutral contract", path)
		}
		if installedBody != "" && body != installedBody {
			t.Fatal("installed Research behavior differs")
		}
		installedBody = body
		nativeBody := strings.ReplaceAll(role.Source.Body, "`../../skills/research-evidence/SKILL.md` relative to this source definition.", "`teams/source/skills/research-evidence/SKILL.md`.")
		if strings.TrimSpace(nativeBody) != strings.TrimSpace(body) {
			t.Errorf("%s differs from native Research behavior", path)
		}
		for _, forbidden := range []string{".claude/", ".codex/", "Claude", "Codex", "workflow/SKILL.md"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s depends on %q", path, forbidden)
			}
		}
	}
}
