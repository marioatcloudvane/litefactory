package teamsource

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

// Story-003 T-022 and T-023 gap tests (AC-022, AC-024, AC-025, AC-029, AC-030,
// AC-043, AC-048): the set stage is declared in the shipped graph file, the
// existing role walks pick its role up from the graph's own attributes, and the
// set examples in the shared result contract keep running through the validator.

// The walk that guards every shipped role finds the set role because the graph
// definition names it, with no role list kept in the guard.
func TestShippedPlanGraphWalkPicksUpTheSetRoleFromTheDefinition(t *testing.T) {
	t.Parallel()
	var plan *shippedWorkflow
	for _, w := range discoverShippedWorkflows(t) {
		if w.Name == goTeam+"/plan" {
			plan = &w
		}
	}
	if plan == nil {
		t.Fatal("no shipped plan workflow discovered")
	}
	dot, err := fs.ReadFile(plan.FS, plan.Dot)
	if err != nil {
		t.Fatal(err)
	}
	roles := dotAgentRoles(string(dot))
	want := []string{"plan-acceptance", "plan-set", "plan-story", "plan-technical"}
	if !slices.Equal(roles, want) {
		t.Fatalf("roles named by the plan graph: %v, want %v", roles, want)
	}
	for _, role := range roles {
		if _, err := fs.ReadFile(plan.FS, role+".md"); err != nil {
			t.Errorf("role %s is named by the graph but not shipped: %v", role, err)
		}
	}
}

// The role a graph node captures is the shipped file, byte for byte, and the set
// node reads the set skill.
func TestPlanSetAgentNodeCapturesTheShippedRoleAndSkill(t *testing.T) {
	t.Parallel()
	d, err := capturePlan(t.Context(), t, planPackageRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	node, ok := d.Profile.Node("set_agent")
	if !ok || node.Action != "lf_agent" {
		t.Fatalf("set_agent is not a declared agent: %+v %v", node, ok)
	}
	if node.Attrs["lf_skills"] != "plan-story-set" {
		t.Fatalf("set_agent skills %q", node.Attrs["lf_skills"])
	}
	if got, want := string(d.Files["plan-set.md"]), planEmbeddedText(t, "plan-set.md"); got != want || got == "" {
		t.Fatal("the captured plan-set.md differs from the shipped role")
	}
	if _, ok := d.Files["skills/plan-story-set/SKILL.md"]; !ok {
		t.Fatal("the set skill is outside the captured closure")
	}
	if d.Hashes["plan-set.md"] == "" || d.Hashes["skills/plan-story-set/SKILL.md"] == "" {
		t.Fatal("the set role or skill has no hash in the digest")
	}
}

// AC-029: the definition alone says a session that already has stories skips the
// set stage: the set decision's false outcome selects story reasoning, and the
// stage that follows the set is only reachable through the owner's promotion, not
// through a failure or exhausted loop.
func TestShippedPlanGraphSkipsTheSetStageWhenTheDecisionIsFalse(t *testing.T) {
	t.Parallel()
	d, err := capturePlan(t.Context(), t, planPackageRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	next, ok := graphexec.Decide(d.Profile, "select_set", "false")
	if !ok {
		t.Fatal("story scope lacks set bypass")
	}
	for _, facts := range []map[string]string{
		{"story.size": "S", "story.sizing_valid": "true", "story.fallback": "false"},
		{"story.size": "L", "story.sizing_valid": "true", "story.fallback": "false"},
		{"story.sizing_valid": "false", "story.fallback": "false"},
	} {
		firstStoryPlanningActivity(t, d.Profile, next, facts)
	}
	sel, _ := d.Profile.Node("select_set")
	if sel.Attrs["lf_condition"] != graphexec.PlanSetCondition || sel.Attrs["lf_source"] != "" {
		t.Fatalf("the set decision reads %v: it must read the factory's selection only", sel.Attrs)
	}
	// Nothing in the set stage reaches the creation cycle or another agent: the
	// owner's promotion ends the turn and the cycle is dispatched by the factory.
	for _, name := range []string{"set_loop", "set_agent", "set_question", "set_draft", "set_gate", "set_ask", "set_review"} {
		for _, e := range d.Profile.Edges {
			if e.From != name {
				continue
			}
			if strings.HasPrefix(e.To, "story_") || strings.HasPrefix(e.To, "technical_") || strings.HasPrefix(e.To, "acceptance_") {
				t.Errorf("set stage edge %s -> %s hands on inside the graph", e.From, e.To)
			}
		}
	}
}

// checkSetContractCoverage reports what the examples fail to show for the set
// stage: the ask and both done shapes, and a refusal for each way a set can be
// invalid.
func checkSetContractCoverage(examples []contractExample) []string {
	var fails []string
	positives := map[string]int{}
	codes := map[string]bool{}
	for _, ex := range examples {
		if ex.Attrs["stage"] != "set" {
			continue
		}
		if ex.Kind == "positive" {
			entries := setExampleEntries(ex.Body)
			switch {
			case ex.Attrs["response"] == "ask":
				positives["ask"]++
			case entries == 1:
				positives["single"]++
			case entries > 1:
				positives["multiple"]++
				if !setExampleHasDependency(ex.Body) {
					fails = append(fails, fmt.Sprintf("multiple-entry set example %q shows no dependency", ex.ID))
				}
			}
			continue
		}
		code, _, _ := strings.Cut(ex.Attrs["expect"], "@")
		codes[code] = true
	}
	for _, kind := range []string{"ask", "single", "multiple"} {
		if positives[kind] == 0 {
			fails = append(fails, "no positive set example: "+kind)
		}
	}
	for _, code := range []string{"duplicate", "blank", "cycle", "self_reference", "reference", "limit", "branch"} {
		if !codes[code] {
			fails = append(fails, "no negative set example expecting "+code)
		}
	}
	return fails
}

func setExampleEntries(body []byte) int {
	var doc struct {
		Result struct {
			Payload struct {
				Plan struct {
					StorySet struct {
						Entries []json.RawMessage `json:"entries"`
					} `json:"storyset"`
				} `json:"plan"`
			} `json:"payload"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &doc) != nil {
		return 0
	}
	return len(doc.Result.Payload.Plan.StorySet.Entries)
}

func setExampleHasDependency(body []byte) bool {
	var doc struct {
		Result struct {
			Payload struct {
				Plan struct {
					StorySet struct {
						Entries []struct {
							DependsOn []string `json:"depends_on"`
						} `json:"entries"`
					} `json:"storyset"`
				} `json:"plan"`
			} `json:"payload"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &doc) != nil {
		return false
	}
	for _, e := range doc.Result.Payload.Plan.StorySet.Entries {
		if len(e.DependsOn) > 0 {
			return true
		}
	}
	return false
}

// AC-030: the set fixtures in the shared contract are extracted and run through the
// real validator (the existing runner), and the set stage has positive and
// negative coverage of its own.
func TestPlanContractSetExamplesRunAndCoverTheSetStage(t *testing.T) {
	t.Parallel()
	examples, fails := verifyContractExamples(planContractDoc(t))
	for _, f := range fails {
		t.Error(f)
	}
	var setExamples []contractExample
	for _, ex := range examples {
		if ex.Attrs["stage"] == "set" {
			setExamples = append(setExamples, ex)
		}
	}
	if len(setExamples) < 10 {
		t.Fatalf("only %d set examples were extracted and run", len(setExamples))
	}
	for _, f := range checkSetContractCoverage(examples) {
		t.Error(f)
	}
}

// The coverage check itself fails when a set example goes missing, so it cannot
// pass vacuously.
func TestSetContractCoverageCheckerFailsOnGaps(t *testing.T) {
	t.Parallel()
	examples, problems := extractContractExamples(planContractDoc(t))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	without := func(keep func(contractExample) bool) []contractExample {
		var out []contractExample
		for _, ex := range examples {
			if keep(ex) {
				out = append(out, ex)
			}
		}
		return out
	}
	tests := []struct {
		name string
		set  []contractExample
		want string
	}{
		{"no set examples", without(func(ex contractExample) bool { return ex.Attrs["stage"] != "set" }), "no positive set example: ask"},
		{"no set ask", without(func(ex contractExample) bool { return ex.ID != "ask-set" }), "no positive set example: ask"},
		{"no single-story set", without(func(ex contractExample) bool { return ex.ID != "set-single" }), "no positive set example: single"},
		{"no multiple-story set", without(func(ex contractExample) bool { return ex.ID != "set-multiple" }), "no positive set example: multiple"},
		{"no cycle refusal", without(func(ex contractExample) bool { return ex.ID != "set-cycle" }), "expecting cycle"},
		{"no unknown dependency refusal", without(func(ex contractExample) bool { return ex.ID != "set-unknown-dependency" }), "expecting reference"},
		{"no too-many refusal", without(func(ex contractExample) bool { return ex.ID != "set-too-many" && ex.ID != "set-empty" }), "expecting limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := strings.Join(checkSetContractCoverage(tt.set), "; ")
			if !strings.Contains(got, tt.want) {
				t.Fatalf("gaps %q do not include %q", got, tt.want)
			}
		})
	}
	if got := checkSetContractCoverage(examples); len(got) != 0 {
		t.Fatalf("the shipped examples report gaps: %v", got)
	}
}
