package graphexec

import (
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

const implementTestDOT = `digraph implement {
 graph [team="builtin", mode="implement", profile_version="1", start="select_path"];
 select_path [lf_action="lf_implement_select", lf_outcomes="coordinator,worker", lf_limit="4"];
 merge [lf_action="lf_implement_merge", lf_outcomes="merged,parked,none,retry", lf_limit="64"];
 ready [lf_action="lf_implement_ready", lf_outcomes="started,waiting,drained", lf_limit="64"];
 await [lf_action="lf_implement_await", lf_outcomes="go", lf_limit="64"];
 handoff [lf_action="lf_implement_handoff", lf_outcomes="posted", lf_limit="16"];
 any_parked [lf_action="lf_implement_any_parked", lf_outcomes="true,false", lf_limit="16"];
 ask_owner [lf_action="lf_implement_ask", lf_outcomes="published", lf_source="merge", lf_limit="16"];
 budget [lf_action="lf_implement_budget", lf_outcomes="run,park", lf_limit="8"];
 worker_agent [lf_action="lf_agent", lf_outcomes="success,failed", lf_agent_markdown="story-worker", lf_skills="implement-result", lf_limit="3"];
 worker_result [lf_action="lf_implement_result", lf_outcomes="completed,stopped", lf_source="worker_agent", lf_limit="3"];
 story_done [lf_action="lf_implement_story_done", lf_outcomes="done,retry", lf_limit="3"];
 park_story [lf_action="lf_implement_park", lf_outcomes="parked", lf_limit="4"];
 waiting [lf_terminal="true"];
 finished [lf_terminal="true"];
 worker_done [lf_terminal="true"];
 worker_parked [lf_terminal="true"];
 select_path -> merge [lf_on="coordinator"];
 select_path -> budget [lf_on="worker"];
 merge -> merge [lf_on="merged"];
 merge -> merge [lf_on="parked"];
 merge -> ready [lf_on="none"];
 merge -> await [lf_on="retry"];
 ready -> await [lf_on="started"];
 ready -> await [lf_on="waiting"];
 ready -> handoff [lf_on="drained"];
 await -> merge [lf_on="go"];
 handoff -> any_parked [lf_on="posted"];
 any_parked -> ask_owner [lf_on="true"];
 any_parked -> finished [lf_on="false"];
 ask_owner -> waiting [lf_on="published"];
 budget -> worker_agent [lf_on="run"];
 budget -> park_story [lf_on="park"];
 worker_agent -> worker_result [lf_on="success"];
 worker_agent -> budget [lf_on="failed"];
 worker_result -> story_done [lf_on="completed"];
 worker_result -> park_story [lf_on="stopped"];
 story_done -> worker_done [lf_on="done"];
 story_done -> budget [lf_on="retry"];
 park_story -> worker_parked [lf_on="parked"];
}
`

func implementTestRegistry(t *testing.T) Registry {
	t.Helper()
	var registry Registry
	for _, spec := range append(ImplementSpecs(), ActionSpec{Name: "lf_agent", Mode: agent.ModeImplement, Outcomes: []string{"success", "failed"}, Attributes: AgentAttributes()}) {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	registry.Policy = ValidateImplementGraph
	return registry
}

func validateImplementDOT(t *testing.T, dot string) error {
	t.Helper()
	profile, err := ParseProfile([]byte(dot))
	if err != nil {
		t.Fatal(err)
	}
	return Validate(profile, implementTestRegistry(t))
}

func TestImplementGraphValidates(t *testing.T) {
	if err := validateImplementDOT(t, implementTestDOT); err != nil {
		t.Fatal(err)
	}
}

func TestImplementSpecsOfTheWorkerAndTheBatchedQuestion(t *testing.T) {
	want := map[string][]string{
		ImplementSelectAction:    {"coordinator", "worker"},
		ImplementBudgetAction:    {"run", "park"},
		ImplementResultAction:    {"completed", "stopped"},
		ImplementStoryDoneAction: {"done", "retry"},
		ImplementParkAction:      {"parked"},
		ImplementAwaitAction:     {"go"},
		ImplementAnyParkedAction: {"true", "false"},
		ImplementAskAction:       {"published"},
	}
	got := map[string][]string{}
	for _, spec := range ImplementSpecs() {
		got[spec.Name] = spec.Outcomes
	}
	for name, outcomes := range want {
		if !slices.Equal(got[name], outcomes) {
			t.Errorf("%s outcomes = %v, want %v", name, got[name], outcomes)
		}
	}
	if len(got) != len(want)+3 {
		t.Errorf("specs = %d, want the story-002 three and %d more", len(got), len(want))
	}
}

func TestImplementGraphCheckerRefusesBrokenStructure(t *testing.T) {
	for name, tc := range map[string]struct{ old, new, want string }{
		"unbounded node": {`lf_outcomes="go", lf_limit="64"]`, `lf_outcomes="go"]`, "positive lf_limit"},
		"limit too big":  {`lf_outcomes="go", lf_limit="64"]`, `lf_outcomes="go", lf_limit="9999"]`, "positive lf_limit"},
		"wrong start":    {`start="select_path"`, `start="merge"`, "path selector"},
		"saved without the result decision": {
			` worker_result -> story_done [lf_on="completed"];`, ` worker_result -> park_story [lf_on="completed"];`, "must save the story",
		},
		"a failed attempt that ends the story quietly": {
			` worker_agent -> budget [lf_on="failed"];`, ` worker_agent -> worker_parked [lf_on="failed"];`, "budget decision",
		},
		"a failed save that skips the budget": {
			` story_done -> budget [lf_on="retry"];`, ` story_done -> worker_done [lf_on="retry"];`, "budget decision",
		},
		"a stopped answer that does not park": {
			` worker_result -> park_story [lf_on="stopped"];`, ` worker_result -> worker_done [lf_on="stopped"];`, "must park the story",
		},
		"the question does not end at a wait": {
			` ask_owner -> waiting [lf_on="published"];`, ` ask_owner -> merge [lf_on="published"];`, "terminal owner wait",
		},
		"the question names no merge step": {`lf_source="merge", lf_limit="16"`, `lf_source="ready", lf_limit="16"`, "owner reply resumes at"},
		"the await node is renamed":        {` await [lf_action="lf_implement_await"`, ` pause [lf_action="lf_implement_await"`, "undeclared"},
	} {
		dot := strings.Replace(implementTestDOT, tc.old, tc.new, 1)
		if dot == implementTestDOT {
			t.Fatalf("%s: the mutation changed nothing", name)
		}
		if name == "the await node is renamed" {
			dot = strings.ReplaceAll(dot, "-> await", "-> pause")
			dot = strings.ReplaceAll(dot, "await ->", "pause ->")
			if err := validateImplementDOT(t, dot); err == nil || !strings.Contains(err.Error(), "await node to run") {
				t.Errorf("%s: err = %v", name, err)
			}
			continue
		}
		if err := validateImplementDOT(t, dot); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, tc.want)
		}
	}
}

func TestImplementGraphCheckerRequiresExactlyOneWorker(t *testing.T) {
	dot := strings.Replace(implementTestDOT, ` park_story [lf_action="lf_implement_park"`, ` second [lf_action="lf_agent", lf_outcomes="success,failed", lf_agent_markdown="story-worker", lf_skills="implement-result", lf_limit="3"];
 second -> worker_result [lf_on="success"];
 second -> budget [lf_on="failed"];
 park_story [lf_action="lf_implement_park"`, 1)
	if err := validateImplementDOT(t, dot); err == nil || !strings.Contains(err.Error(), "exactly one worker agent") {
		t.Fatalf("%v", err)
	}
}

func TestImplementGraphCheckerRefusesForeignActions(t *testing.T) {
	var registry Registry
	for _, spec := range append(ImplementSpecs(), ActionSpec{Name: "lf_agent", Mode: agent.ModeImplement, Outcomes: []string{"success", "failed"}, Attributes: AgentAttributes()}, ControlSpec(ResearchIfAction, ImplementControlPolicy())) {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	dot := strings.Replace(implementTestDOT, `any_parked [lf_action="lf_implement_any_parked", lf_outcomes="true,false", lf_limit="16"]`, `any_parked [lf_action="lf_if", lf_field="response", lf_condition="completed", lf_outcomes="true,false", lf_limit="16"]`, 1)
	profile, err := ParseProfile([]byte(dot))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateImplementGraph(profile); err == nil || !strings.Contains(err.Error(), "does not implement") {
		t.Fatalf("%v", err)
	}
	if err := ValidateImplementGraph(nil); err == nil {
		t.Fatal("nil profile")
	}
	profile.Mode = agent.ModePlan
	if err := ValidateImplementGraph(profile); err == nil {
		t.Fatal("another mode")
	}
}

func TestImplementControlPolicyIsTheWorkersOutcomes(t *testing.T) {
	policy := ImplementControlPolicy()
	if policy.Mode != agent.ModeImplement || !slices.Equal(policy.Responses, []string{"completed", "incomplete", "blocked"}) {
		t.Fatalf("%+v", policy)
	}
}
