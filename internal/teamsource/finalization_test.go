package teamsource

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/render"
)

// A temporary role selects the new source skill without changing any active role,
// installed provider tree, release or source catalog in the repository.
func TestFinalization_TemporaryDiscoveryAndBothRenderings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skill := "skills/agent-finalization"
	if err := os.MkdirAll(filepath.Join(root, skill), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(filepath.Join(root, skill), os.DirFS("../../teams/source/"+skill)); err != nil {
		t.Fatal(err)
	}
	role := map[string]any{"id": "finalizer", "desc": "Serialize assigned results.", "path": "agents/finalizer.md", "tier": "balanced", "skills": []string{"agent-finalization"}}
	index := map[string]any{"schema_version": 2, "shared": map[string]any{}, "teams": map[string]any{"fixture": map[string]any{"desc": "Isolated finalization fixture", "research": []any{role}, "plan": []any{}, "implement": []any{}, "validate": []any{}, "spike": []any{}}}}
	raw, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	writeLocalOverrideFile(t, root, "index.json", string(raw))
	writeLocalOverrideFile(t, root, "agents/finalizer.md", "---\nname: finalizer\ndescription: Serialize assigned results.\ntier: balanced\n---\nKeep assigned scope and evidence obligations available early. Use agent-finalization before every exit.\n")
	snapshot, err := CaptureRoot(t.Context(), root, "fixture", "research")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Agents) != 1 {
		t.Fatal("temporary role not discovered")
	}
	err = fs.WalkDir(os.DirFS(filepath.Join(root, skill)), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		source, err := os.ReadFile(filepath.Join(root, skill, p))
		if err != nil {
			return err
		}
		if !bytes.Equal(source, snapshot.Files[skill+"/"+p]) {
			t.Errorf("capture omitted/changed reference %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := snapshot.Agents[0]
	for _, flavor := range []string{"claude", "codex"} {
		file, err := render.Native(flavor, render.NativeRole{Source: agent.Source, Model: "fixture-model", Tools: agent.Source.Frontmatter.Tools, Skills: agent.Definition.Skills}, "workspace-write")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(file.Bytes), "agent-finalization") || !strings.Contains(string(file.Bytes), "evidence obligations available early") {
			t.Fatalf("%s render lost discovery/context", flavor)
		}
		if strings.Contains(string(file.Bytes), "expected_revision") {
			t.Fatalf("%s eagerly embedded late serialization references", flavor)
		}
	}
	// A captured dependency stays immutable after a source reference changes.
	before := append([]byte(nil), snapshot.Files[skill+"/references/plan.md"]...)
	writeLocalOverrideFile(t, root, skill+"/references/plan.md", "changed temporary reference")
	if !bytes.Equal(before, snapshot.Files[skill+"/references/plan.md"]) {
		t.Fatal("captured skill aliases filesystem")
	}
}
