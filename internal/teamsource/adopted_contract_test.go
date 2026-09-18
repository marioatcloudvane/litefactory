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
