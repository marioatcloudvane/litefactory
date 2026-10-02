package teamsource

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

// Story-003 AC-048 and AC-030: the set role and its skill. The nine-section and
// ask/done guards run over plan-set.md directly, so the role is covered even before
// a graph names it; once the graph does, the graph walk covers it as well.

func planEmbeddedText(t *testing.T, name string) string {
	t.Helper()
	raw, err := planAssets.ReadFile("plan/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestPlanSetRoleHasTheNineSectionsAndTheSharedGuidance(t *testing.T) {
	t.Parallel()
	role := planEmbeddedText(t, "plan-set.md")
	for _, p := range checkRoleSections(role) {
		t.Error(p)
	}
	for _, p := range checkPlanRoleGuidance(role) {
		t.Error(p)
	}
	if !strings.HasPrefix(role, "---\nname: plan-set\n") {
		t.Error("plan-set.md front matter must name the role plan-set")
	}
}

func TestPlanSetRoleExplainsDecompositionLimitsAndBoundaries(t *testing.T) {
	t.Parallel()
	names, bodies := roleSectionBodies(planEmbeddedText(t, "plan-set.md"))
	sections := map[string]string{}
	for i, n := range names {
		sections[n] = squash(bodies[i])
	}
	cases := []struct {
		section string
		terms   []string
	}{
		{"Goal", []string{"accounts for all the agreed scope", "independent outcome", "not one umbrella story", "one deliverable is one story", "the order between stories"}},
		{"How You Are Invoked", []string{"parent thread and the saved research scope", "only after the owner reviewed the set"}},
		{"When You Are Done", []string{"1 to 12 entries", "temporary id that is nonempty and unique", "at most 80 characters", "at most 280 characters", "never form a cycle", "one deliverable yields one entry", "split the parent into smaller parents"}},
		{"Must Do", []string{"account for all agreed scope", "propose several stories when several independent outcomes exist", "`depends_on`", "read plan-evidence and plan-story-set"}},
		{"Must Not Do", []string{"more than 12 stories", "umbrella story", "never write story drafts, architecture, tasks or criteria"}},
		{"Output", []string{"story set: `entries`", "between 1 and 12 entries", "unique nonempty temporary ids", "no cycles"}},
	}
	for _, tt := range cases {
		body, ok := sections[tt.section]
		if !ok {
			t.Errorf("no %s section", tt.section)
			continue
		}
		for _, term := range tt.terms {
			if !strings.Contains(body, term) {
				t.Errorf("%s does not say %q", tt.section, term)
			}
		}
	}
}

func TestPlanStorySetSkillIsSelfDescribingAndRequiresTheContract(t *testing.T) {
	t.Parallel()
	skill := planEmbeddedText(t, "skills/plan-story-set/SKILL.md")
	for _, term := range []string{"name: plan-story-set\n", "requires: [plan-evidence]", "independent outcome", "umbrella story", "Account for all agreed scope", "temporary id", "depends_on", "never forms a cycle", "1 to 12 entries", "split the parent", "280", "80 characters", "storyset"} {
		if !strings.Contains(skill, term) {
			t.Errorf("plan-story-set skill does not say %q", term)
		}
	}
	if strings.Contains(planEmbeddedText(t, "skills/plan-evidence/SKILL.md"), "plan-story-set") {
		t.Error("the shared evidence skill must not depend on a role-specific skill")
	}
}

func TestPlanSetRoleAndSkillCopiesAgree(t *testing.T) {
	t.Parallel()
	source, embedded := planSourceTree(t), planEmbeddedTree(t)
	for _, name := range []string{"plan-set.md", "skills/plan-story-set/SKILL.md", "skills/plan-evidence/node-result.md"} {
		s, e := source[name], embedded[name]
		if len(s) == 0 || len(e) == 0 {
			t.Errorf("%s is missing from the source or the embedded tree", name)
			continue
		}
		if string(s) != string(e) {
			t.Errorf("%s differs between source and embedded copies", name)
		}
	}
}

// setBody builds a done result over n entries whose names, ids and intents can be
// adjusted, judged by the same validator the examples run through.
func setBody(t *testing.T, entries []map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"result": map[string]any{
		"outcome": "completed", "comment": "A proposed set.",
		"payload": map[string]any{"plan": map[string]any{"storyset": map[string]any{"entries": entries}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func setEntries(n int) []map[string]any {
	out := make([]map[string]any, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, map[string]any{"id": fmt.Sprintf("s%d", i), "name": fmt.Sprintf("Story %d", i), "intent": "Deliver one outcome."})
	}
	return out
}

// The documented limits are the validator's: the boundary value passes and one
// beyond it is refused with the code the contract names.
func TestPlanSetContractLimitsMatchTheValidator(t *testing.T) {
	t.Parallel()
	const base = "$.result.payload.plan.storyset.entries"
	cases := []struct {
		name   string
		mutate func([]map[string]any) []map[string]any
		want   string // empty: accepted
	}{
		{"one entry", func([]map[string]any) []map[string]any { return setEntries(1) }, ""},
		{"twelve entries", func([]map[string]any) []map[string]any { return setEntries(12) }, ""},
		{"thirteen entries", func([]map[string]any) []map[string]any { return setEntries(13) }, "limit@" + base},
		{"intent of 280 characters", func(e []map[string]any) []map[string]any {
			e[0]["intent"] = strings.Repeat("a", 280)
			return e
		}, ""},
		{"intent of 281 characters", func(e []map[string]any) []map[string]any {
			e[0]["intent"] = strings.Repeat("a", 281)
			return e
		}, "intent@" + base + "[0].intent"},
		{"name of 80 characters", func(e []map[string]any) []map[string]any {
			e[0]["name"] = strings.Repeat("a", 80)
			return e
		}, ""},
		{"name of 81 characters", func(e []map[string]any) []map[string]any {
			e[0]["name"] = strings.Repeat("a", 81)
			return e
		}, "name@" + base + "[0].name"},
		{"multi-line intent", func(e []map[string]any) []map[string]any {
			e[0]["intent"] = "one\ntwo"
			return e
		}, "intent@" + base + "[0].intent"},
		{"dependency on an earlier entry", func(e []map[string]any) []map[string]any {
			e[1]["depends_on"] = []string{"s1"}
			return e
		}, ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, d := agentresult.Create(setBody(t, tt.mutate(setEntries(2))), factoryContext("set", ""))
			if tt.want == "" {
				if !d.Valid {
					t.Fatalf("refused: %s", diagList(d))
				}
				return
			}
			code, path, _ := strings.Cut(tt.want, "@")
			if d.Valid || !hasDiag(d, code, path) {
				t.Fatalf("want %s, valid=%v got %s", tt.want, d.Valid, diagList(d))
			}
		})
	}
}

// The extended stage vocabulary is closed and the set pair rule is exact: a set
// example has operation none, and no other stage may claim it for a done.
func TestContractMarkersAdmitTheSetStageOnly(t *testing.T) {
	t.Parallel()
	fence := "\n```json\n{}\n```\n"
	cases := []struct {
		name, marker, want string
	}{
		{"set ask", "<!-- example: positive id=a stage=set response=ask operation=none -->", ""},
		{"set done", "<!-- example: positive id=a stage=set response=done operation=none -->", ""},
		{"set done with an operation", "<!-- example: positive id=a stage=set response=done operation=create -->", "is not a valid pair"},
		{"story done with none", "<!-- example: positive id=a stage=story response=done operation=none -->", "is not a valid pair"},
		{"negative set", "<!-- example: negative id=a stage=set expect=cycle@$.x -->", ""},
		{"unknown stage", "<!-- example: negative id=a stage=sets expect=cycle@$.x -->", `stage "sets"`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, problems := extractContractExamples(tt.marker + fence)
			got := strings.Join(problems, "; ")
			if tt.want == "" && got != "" || tt.want != "" && !strings.Contains(got, tt.want) {
				t.Fatalf("problems %q, want %q", got, tt.want)
			}
		})
	}
}
