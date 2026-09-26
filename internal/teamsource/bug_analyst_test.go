package teamsource

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"
)

var bugAnalystSections = []string{"Role", "Voice", "Goal", "How You Are Invoked", "When You Are Done", "Must Do", "Must Not Do", "Writing the Report", "Output"}

func requireBugAnalystSections(t *testing.T, body string) {
	t.Helper()
	var got []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "# ") {
			got = append(got, strings.TrimPrefix(line, "# "))
		}
	}
	if !reflect.DeepEqual(got, bugAnalystSections) {
		t.Fatalf("role sections=%v; want %v", got, bugAnalystSections)
	}
}

func TestBugAnalyst_CapturedResourceClosure(t *testing.T) {
	t.Parallel()
	node, err := CaptureBugAnalyst(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if node.Role.Frontmatter.Name != "bug-analyst" || node.Role.Frontmatter.Tools != nil {
		t.Fatalf("analyst role or tools changed: %+v", node.Role.Frontmatter)
	}
	if !reflect.DeepEqual(node.Skills, []string{"bug-analysis-result"}) {
		t.Fatalf("skills=%v", node.Skills)
	}
	requireBugAnalystSections(t, node.Role.Body)
	expected := []string{"bug-analyst.md", "skills/bug-analysis-result/SKILL.md", "skills/bug-analysis-result/result.md"}
	if len(node.Files) != len(expected) {
		t.Fatalf("captured %d files, want %d", len(node.Files), len(expected))
	}
	for _, name := range expected {
		if len(node.Files[name]) == 0 {
			t.Errorf("missing captured resource %s", name)
		}
	}
	// Each capture owns its bytes; mutating one cannot rewrite later role context.
	node.Files[expected[0]][0] = '!'
	again, err := CaptureBugAnalyst(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(node.Files[expected[0]], again.Files[expected[0]]) {
		t.Fatal("capture exposes shared mutable resource bytes")
	}
}

func TestBugAnalyst_SourceEmbeddedParity(t *testing.T) {
	t.Parallel()
	// Source reads compare repository fixtures only; no runtime or installed tree is modified.
	node, err := CaptureBugAnalyst(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for name, captured := range node.Files {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := os.ReadFile(path.Join("../../teams/source/bug", name))
			if err != nil {
				t.Fatal(err)
			}
			embedded, err := bugAssets.ReadFile(path.Join("bug", name))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(source, embedded) || !bytes.Equal(source, captured) {
				t.Fatal("source, embedded and captured bytes differ")
			}
		})
	}
}

func TestBugAnalyst_NativePackage(t *testing.T) {
	t.Parallel()
	for _, adapter := range []string{"claude", "codex"} {
		t.Run(adapter, func(t *testing.T) {
			t.Parallel()
			node, err := CaptureBugAnalyst(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			files, err := NativeWorkflow(adapter, node, "test-model", []string{"Read", "Glob", "Grep"}, "read-only")
			if err != nil {
				t.Fatal(err)
			}
			// Model installation into an isolated in-memory filesystem. No user's .codex/.claude is touched.
			installed := fstest.MapFS{}
			for _, file := range files {
				if !fs.ValidPath(file.Path) || len(file.Bytes) == 0 {
					t.Fatalf("unsafe or empty rendered resource %q", file.Path)
				}
				if _, exists := installed[file.Path]; exists {
					t.Fatalf("duplicate resource %q", file.Path)
				}
				installed[file.Path] = &fstest.MapFile{Data: bytes.Clone(file.Bytes), Mode: 0600}
			}
			for _, name := range []string{"SKILL.md", "result.md"} {
				actual, err := fs.ReadFile(installed, "plugin/skills/bug-analysis-result/"+name)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(actual, node.Files["skills/bug-analysis-result/"+name]) {
					t.Fatalf("installed result resource changed: %s", name)
				}
			}
			var roleBody string
			if adapter == "claude" {
				raw, err := fs.ReadFile(installed, "plugin/agents/bug-analyst.md")
				if err != nil {
					t.Fatal(err)
				}
				parts := strings.SplitN(string(raw), "---", 3)
				if len(parts) != 3 {
					t.Fatal("missing native role frontmatter")
				}
				var header struct {
					Name, Model string
					Tools       *string
					Skills      []string
				}
				if err := yaml.Unmarshal([]byte(parts[1]), &header); err != nil {
					t.Fatal(err)
				}
				if header.Name != "bug-analyst" || header.Model != "test-model" || header.Tools != nil || !reflect.DeepEqual(header.Skills, []string{"bug-analysis-result"}) {
					t.Fatalf("native analyst settings=%+v", header)
				}
				roleBody = strings.TrimSpace(parts[2])
			} else {
				raw, err := fs.ReadFile(installed, "roles/bug-analyst.toml")
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(raw), "sandbox_mode = \"read-only\"\n") {
					t.Fatal("Codex role lacks read-only sandbox")
				}
				for _, line := range strings.Split(string(raw), "\n") {
					if value, ok := strings.CutPrefix(line, "developer_instructions = "); ok {
						if err := json.Unmarshal([]byte(value), &roleBody); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			requireBugAnalystSections(t, roleBody)
			if strings.TrimSpace(roleBody) != strings.TrimSpace(node.Role.Body) {
				t.Fatal("native installation changed role duties")
			}
		})
	}
}

func TestBugAnalyst_NativeRoleInheritsFactorySurface(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		ceiling []string
	}{
		{"read only", []string{"Read"}},
		{"no native tools", []string{}},
		{"read surface and result service", []string{"Read", "Glob", "Grep", "mcp__litefactory__communication_submit"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			node, err := CaptureBugAnalyst(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			files, err := NativeWorkflow("claude", node, "test-model", tt.ceiling, "read-only")
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				if file.Path == "plugin/agents/bug-analyst.md" {
					parts := strings.SplitN(string(file.Bytes), "---", 3)
					var header struct{ Tools *string }
					if err := yaml.Unmarshal([]byte(parts[1]), &header); err != nil {
						t.Fatal(err)
					}
					if header.Tools != nil {
						t.Fatalf("role supplied its own tools list %q; expected factory-session inheritance", *header.Tools)
					}
					return
				}
			}
			t.Fatal("native role missing")
		})
	}
}

func TestBugAnalyst_CancelledCapture(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := CaptureBugAnalyst(ctx); err == nil {
		t.Fatal("cancelled capture succeeded")
	}
}
