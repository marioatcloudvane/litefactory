package teamsource

import (
	"os"
	"strings"
	"testing"
)

// Like the existing shared-source rendering checks, this reads canonical input
// only. It neither requires installed sibling parity nor installs a bundle.
func TestAdoptedCommunicationSourceContract(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		path    string
		clauses []string
	}{
		{"teams/source/skills/agent-finalization/references/common.md", []string{
			"shared result descriptor", "permitted communication tools", "application JSON as the terminal response",
			"explicit empty baseline", "stable IDs, retired IDs and observed revision", "The complete final merge must validate",
			"Changed bytes require fresh correlation", "lost replies", "separate receipt observations",
			"Missing/malformed required output prevents nominal success", "failure, cancellation and interruption keep their original cause",
			"only on a new authorized attempt", "declared compatibility route", "Adoption and reader retirement are independent", "#274/#296",
		}},
		{"teams/source/skills/research-evidence/SKILL.md", []string{
			"shared result helper, permitted communication tools or terminal output", "only payload.research",
			"explicit empty baseline", "prior prose/provider sessions are not a baseline", "full merge, including untouched fields",
			"corrected input uses a new identity", "receipt states independently", "Progress remains advisory", "#274/#296",
		}},
		{"teams/source/skills/agent-finalization/references/validate.md", []string{
			"retained snapshot/check references", "Questions are disallowed", "No shell, edits, new checks, capture or delegation",
			"identical terminal/live findings reconcile", "conflicting findings reject", "complete frozen scope", "Historical reads grant no writes",
		}},
		{"AGENTS.md", []string{"`- Mode: X`", "shared result helper", "prior prose is not editable state", "Input, engineering, acceptance and remote effects remain independent", "Validate remains reference-only", "Adoption and retirement are separate"}},
		{"CLAUDE.md", []string{"`- Mode: X`", "shared result helper", "prior prose is not editable state", "Input, engineering, acceptance and remote effects remain independent", "Validate remains reference-only", "Adoption and retirement are separate"}},
		{"docs/story-contract-instruction-manifest.md", []string{
			"US-005 changes shared/root sources only", "US-006 (Codex writes Claude tree)", "US-007 (Claude writes Codex tree)", "US-010 owns aggregate parity",
			"teams/source/skills/agent-finalization/SKILL.md", ".claude/skills/shared/workflow.md", ".codex/skills/workflow/SKILL.md",
			".claude/modes/validate.md", ".codex/skills/review-evidence/SKILL.md", "immutable teams/releases",
		}},
		{"docs/agent-auxiliary-contracts.md", []string{"Mode classifier", "Schedule advisor", "Conflict advisor", "explicitly excludes", "typed payloads", "unknown fields", "#274/#296"}},
	} {
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile("../../" + test.path)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Join(strings.Fields(string(data)), " ")
			for _, clause := range test.clauses {
				if !strings.Contains(text, clause) {
					t.Errorf("missing contract clause %q", clause)
				}
			}
		})
	}
}

func TestAdoptedInstructionManifestExactRoleTargets(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../docs/story-contract-instruction-manifest.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, role := range []string{"requirements-engineer", "proxy-product-owner", "research-orchestrator", "plan-orchestrator", "implement-orchestrator", "go-architect", "go-implementation-engineer", "go-unit-test-engineer", "go-integration-test-engineer", "go-code-reviewer"} {
		source := "shared/"
		if strings.HasPrefix(role, "go-") {
			source = "go-team/"
		}
		row := "| " + source + role + ".md | .claude/agents/" + role + ".md | .codex/skills/" + role + "/SKILL.md |"
		if !strings.Contains(text, row) {
			t.Errorf("missing exact mapping %s", row)
		}
	}
	for _, obsolete := range []string{"US-009", "US-011", "US-012"} {
		if strings.Contains(text, obsolete) {
			t.Errorf("ambiguous previous-feature ownership %s", obsolete)
		}
	}
}
