package teamsource

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

func TestWorkflowJudgeResourceCapture(t *testing.T) {
	source := researchTestSource()
	replaceResearchFixture(source, "research.dot", `work -> end [lf_on="success"];`, `work -> challenge [lf_on="success"];
 challenge [lf_action="lf_judge",lf_source="work",lf_limit="1",lf_outcomes="success,failed"];
 challenge -> end [lf_on="success"];
 challenge -> end [lf_on="failed"];`)
	registry := researchTestRegistry(t)
	def, err := CaptureWorkflow(t.Context(), source, "research.dot", "builtin", agent.ModeResearch, registry)
	if err != nil {
		t.Fatal(err)
	}
	role, err := WorkflowJudgeInstructions(def)
	if err != nil {
		t.Fatal(err)
	}
	shipped, err := JudgeInstructions()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(role, shipped) || def.Hashes["judge/challenger.md"] == "" {
		t.Fatal("judge role absent from captured identity")
	}
	retained := RetainedFiles(def.Files)
	replay, err := CaptureWorkflow(t.Context(), retained, "research.dot", "builtin", agent.ModeResearch, registry)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Digest != def.Digest {
		t.Fatal("retained identity changed")
	}
	retained[judgeLegacyRolePath] = []byte("damaged role")
	if _, err := CaptureWorkflow(t.Context(), retained, "research.dot", "builtin", agent.ModeResearch, registry); err == nil {
		t.Fatal("damaged role accepted")
	}
	// Historical captures keep their exact identity but cannot launch again.
	for name := range retained {
		if strings.HasPrefix(name, "judge/") {
			delete(retained, name)
		}
	}
	retained[judgeLegacyRolePath] = []byte("previously captured challenger")
	replay, err = CaptureWorkflow(t.Context(), retained, "research.dot", "builtin", agent.ModeResearch, registry)
	if err != nil {
		t.Fatal(err)
	}
	role, err = WorkflowJudgeInstructions(replay)
	if err != nil || string(role) != "previously captured challenger" {
		t.Fatalf("historical role changed: %s, %v", role, err)
	}
	if _, err := WorkflowJudgeCapture(replay); err == nil {
		t.Fatal("historical capture allowed launch")
	}
	delete(retained, judgeLegacyRolePath)
	if _, err := CaptureWorkflow(t.Context(), retained, "research.dot", "builtin", agent.ModeResearch, registry); err == nil {
		t.Fatal("missing retained instructions silently replaced")
	}

}

func TestFreshJudgeCaptureFromRootRetainedFiles(t *testing.T) {
	for _, withJudge := range []bool{false, true} {
		name := "without judge"
		if withJudge {
			name = "with judge"
		}
		t.Run(name, func(t *testing.T) {
			fixture := researchTestSource()
			registry := researchTestRegistry(t)
			if withJudge {
				replaceResearchFixture(fixture, "research.dot", `work -> end [lf_on="success"];`, `work -> challenge [lf_on="success"];
 challenge [lf_action="lf_judge",lf_source="work",lf_limit="1",lf_outcomes="success,failed"];
 challenge -> end [lf_on="success"];
 challenge -> end [lf_on="failed"];`)
			}
			source := RetainedFiles{}
			for path, file := range fixture {
				source[path] = bytes.Clone(file.Data)
			}
			root, err := fs.Sub(source, ".")
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := root.(RetainedFiles); !ok {
				t.Fatal("fixture does not preserve root retained filesystem type")
			}
			paths := config.NewPaths(t.TempDir())
			if withJudge {
				if _, err := CaptureWorkflowIdentity(t.Context(), root, "research.dot", "builtin", agent.WorkflowResearch, registry, paths); err == nil {
					t.Fatal("retained source without judge closure accepted as fresh")
				}
			}
			freshSource := struct{ fs.FS }{root}
			fresh, err := CaptureWorkflowIdentity(t.Context(), freshSource, "research.dot", "builtin", agent.WorkflowResearch, registry, paths)
			if err != nil {
				t.Fatalf("fresh root capture: %v", err)
			}
			if withJudge {
				if _, err := WorkflowJudgeCapture(fresh); err != nil {
					t.Fatalf("fresh judge closure: %v", err)
				}
			} else {
				for path := range fresh.Files {
					if strings.HasPrefix(path, "judge/") {
						t.Fatalf("nonjudge workflow captured %s", path)
					}
				}
			}
			restored, err := CaptureWorkflowIdentity(t.Context(), RetainedFiles(fresh.Files), "research.dot", "builtin", agent.WorkflowResearch, registry)
			if err != nil {
				t.Fatalf("restored root capture: %v", err)
			}
			if fresh.Digest != restored.Digest {
				t.Fatal("restore changed root capture identity")
			}
		})
	}
}
