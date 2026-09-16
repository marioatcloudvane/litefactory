package teamsource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/render"
)

// These checks use the publishing source and an empty temporary root. Installed
// instruction trees deliberately are not inputs: each opposite-harness story
// must be independently verifiable before final cross-tree parity is possible.
func TestSharedWorkflowRendering(t *testing.T) {
	t.Parallel()
	for _, harness := range []string{"claude", "codex"} {
		t.Run(harness, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if err := os.CopyFS(root, os.DirFS("../../teams/source")); err != nil {
				t.Fatal(err)
			}
			for _, mode := range []string{"research", "plan", "implement", "validate"} {
				snapshot, err := CaptureRoot(t.Context(), root, "go-team", mode)
				if err != nil {
					t.Fatal(err)
				}
				for _, agent := range snapshot.Agents {
					sandbox := "workspace-write"
					if mode == "validate" {
						sandbox = "read-only"
					}
					file, err := render.Native(harness, render.NativeRole{Source: agent.Source, Model: "test-model", Tools: agent.Source.Frontmatter.Tools, Skills: agent.Definition.Skills}, sandbox)
					if err != nil {
						t.Fatal(err)
					}
					if len(file.Bytes) == 0 {
						t.Fatalf("empty render for %s", agent.Definition.ID)
					}
					if _, err := os.Lstat(filepath.Join(root, file.Path)); !os.IsNotExist(err) {
						t.Fatalf("render wrote %s: %v", file.Path, err)
					}
				}
				if mode == "implement" || mode == "validate" {
					workflow := string(snapshot.Files["skills/workflow/SKILL.md"])
					if !strings.Contains(workflow, "persisted workflow version") {
						t.Fatal("snapshot omitted shared lifecycle dependency")
					}
					for _, resource := range []string{"SKILL.md", "references/common.md", "references/implement.md", "references/validate.md"} {
						if len(snapshot.Files["skills/agent-finalization/"+resource]) == 0 {
							t.Errorf("%s snapshot omitted finalization dependency %s", mode, resource)
						}
					}
				}
			}
			for _, tree := range []string{".claude", ".codex", "plugin", "roles"} {
				if _, err := os.Lstat(filepath.Join(root, tree)); !os.IsNotExist(err) {
					t.Fatalf("unexpected installed output %s: %v", tree, err)
				}
			}
		})
	}
}

func TestSharedWorkflowContract(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../teams/source/skills/workflow/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, obligation := range []string{
		"persisted workflow version", "retain legacy behavior", "test execution, fixes and aggregate engineering/save gates",
		"Validate\nowns acceptance, code review, architecture consistency and approved-plan review",
		"never edit code, run tests or commission fresh checks", "Historical acceptance", "permanently",
		"Never automatically chain or dispatch modes", "override every substantive", "failed/unverified evidence",
		"every attached child", "link only by default", "changed head needs\nfresh approval",
		"successful integration into intended main before closing the parent", "Codex updates `.claude/**`",
		"Claude updates `.codex/**`", "cross-tree parity for the aggregate feature gate",
	} {
		if !strings.Contains(text, obligation) {
			t.Errorf("missing contract obligation %q", obligation)
		}
	}
}

// The root router must not impose a write prerequisite on its read-only mode.
func TestSharedWorkflowValidateRootRouting(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../CLAUDE.md")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(raw)), " ")
	for _, contract := range []string{
		"Validate reuses the supplied session and frozen scope read-only.",
		"Do not scaffold a directory or MANIFEST, write artifacts, or require artifact existence to route or finish Validate.",
		"Validate finishes with structured runner findings, not an artifact",
		"without writing or checking for a result file",
	} {
		if !strings.Contains(text, contract) {
			t.Errorf("root Validate routing omitted %q", contract)
		}
	}
}
