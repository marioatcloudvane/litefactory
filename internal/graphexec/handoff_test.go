package graphexec_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

// handoffDOT has a first agent whose measured proposal is handed to a second,
// write-capable agent. The second agent's ask is settled by a measure step
// (nobody is asked); its done is measured, noted and reported.
const handoffDOT = `digraph {team=builtin;mode=research;profile_version=1;start=loop;
loop [lf_action=lf_loop,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_default=ask,lf_limit=2,lf_source=work];
work [lf_action=lf_agent,lf_outcomes="success,failed",lf_limit=2,lf_access=read];
wants [lf_action=lf_if,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_limit=2,lf_source=work];
gauge [lf_action=lf_research_measure,lf_source=work,lf_forbid="alpha",lf_outcomes="one,two,failed",lf_limit=1];
question [lf_action=lf_ask,lf_kind=question,lf_source=work,lf_outcomes=published];
report [lf_action=lf_comment,lf_kind=final,lf_source=work,lf_outcomes=published];
worker [lf_action=lf_agent,lf_outcomes="success,failed",lf_limit=1,lf_access=write];
wworker [lf_action=lf_if,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_limit=1,lf_source=worker];
verify [lf_action=lf_research_finding,lf_source=worker,lf_forbid="alpha",lf_outcomes="one,two,failed",lf_limit=1];
noteb [lf_action=lf_research_measure,lf_source=worker,lf_forbid="alpha",lf_outcomes="one,two,failed",lf_limit=1];
report2 [lf_action=lf_comment,lf_kind=final,lf_source=worker,lf_outcomes=published];
oops [lf_action=lf_comment,lf_kind=failure,lf_outcomes=published];
wait [lf_terminal=true];end [lf_terminal=true];stop [lf_terminal=true];
loop -> work [lf_on=true];loop -> gauge [lf_on=false];
work -> wants [lf_on=success];work -> oops [lf_on=failed];
wants -> question [lf_on=true];wants -> gauge [lf_on=false];
gauge -> worker [lf_on=one];gauge -> report [lf_on=two];gauge -> oops [lf_on=failed];
worker -> wworker [lf_on=success];worker -> oops [lf_on=failed];
wworker -> noteb [lf_on=true];wworker -> verify [lf_on=false];
verify -> report2 [lf_on=one];verify -> report2 [lf_on=two];verify -> oops [lf_on=failed];
noteb -> report2 [lf_on=one];noteb -> report2 [lf_on=two];noteb -> oops [lf_on=failed];
question -> wait [lf_on=published];report -> end [lf_on=published];report2 -> end [lf_on=published];oops -> stop [lf_on=published];}`

func handoffRegistry(t *testing.T, findings []string) graphexec.Registry {
	t.Helper()
	policy := graphexec.ResearchControlPolicy()
	policy.Generation = ""
	policy.MeasureActions = []string{"lf_research_measure", "lf_research_finding"}
	policy.FindingActions = findings
	registry := graphexec.Registry{Policy: func(p *graphexec.Profile) error { return graphexec.ValidateControlGraph(p, policy) }}
	for _, spec := range []graphexec.ActionSpec{
		{Name: "lf_agent", Mode: agent.ModeResearch, Outcomes: []string{"success", "failed"}, Attributes: graphexec.AgentAttributes()},
		graphexec.ControlSpec(graphexec.ResearchLoopAction, policy), graphexec.ControlSpec(graphexec.ResearchIfAction, policy),
		{Name: "lf_comment", Mode: agent.ModeResearch, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: []string{"final", "failure"}}}},
		{Name: "lf_ask", Mode: agent.ModeResearch, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: []string{"question"}}}},
		{Name: "lf_research_measure", Mode: agent.ModeResearch, Outcomes: []string{"one", "two", "failed"}, Attributes: map[string]graphexec.AttributeSpec{"lf_forbid": {Required: true, List: true, Values: []string{"alpha"}}}},
		{Name: "lf_research_finding", Mode: agent.ModeResearch, Outcomes: []string{"one", "two", "failed"}, Attributes: map[string]graphexec.AttributeSpec{"lf_forbid": {Required: true, List: true, Values: []string{"alpha"}}}},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	return registry
}

func validateHandoff(t *testing.T, findings []string, edit func(string) string) error {
	t.Helper()
	profile, err := graphexec.ParseProfile([]byte(edit(handoffDOT)))
	if err != nil {
		t.Fatal(err)
	}
	return graphexec.Validate(profile, handoffRegistry(t, findings))
}

func TestMeasureMayHandOffToADifferentAgentAndValidatesItsPaths(t *testing.T) {
	if err := validateHandoff(t, nil, identity); err != nil {
		t.Fatalf("a hand-off to a different agent was refused: %v", err)
	}
	for name, tc := range map[string]struct{ from, to, want string }{
		"source agent recurrence":   {"gauge -> worker [lf_on=one]", "gauge -> work [lf_on=one]", "own source agent"},
		"missing ask edge":          {"wworker -> noteb [lf_on=true];", "", "want exactly one"},
		"done reaches no report":    {"verify -> report2 [lf_on=one]", "verify -> end [lf_on=one]", "without required publication"},
		"ask reaches no settlement": {"wworker -> noteb [lf_on=true]", "wworker -> oops [lf_on=true]", "failure"},
		"handed agent may not loop": {"noteb -> report2 [lf_on=one]", "noteb -> worker [lf_on=one]", "own source agent"},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateHandoff(t, nil, func(s string) string {
				if !strings.Contains(s, tc.from) {
					t.Fatalf("fixture does not contain %q", tc.from)
				}
				return strings.Replace(s, tc.from, tc.to, 1)
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestAnAskCannotEndAtAFinalReportWithoutAMeasureStep(t *testing.T) {
	// The ask edge of the handed-off agent goes straight to its report: nothing
	// recorded the outcome that replaces the owner wait.
	err := validateHandoff(t, nil, func(s string) string {
		return strings.Replace(s, "wworker -> noteb [lf_on=true]", "wworker -> report2 [lf_on=true]", 1)
	})
	if err == nil || !strings.Contains(err.Error(), "question comment") {
		t.Fatalf("an unmeasured ask reached a final report: %v", err)
	}
}

func TestFailedOutcomeIsAFindingOnlyWhereThePolicyDeclaresIt(t *testing.T) {
	edit := func(s string) string {
		return strings.Replace(s, "verify -> oops [lf_on=failed]", "verify -> noteb [lf_on=failed]", 1)
	}
	err := validateHandoff(t, nil, edit)
	if err == nil || !strings.Contains(err.Error(), "failure path") {
		t.Fatalf("a failed edge left the failure path without a declaration: %v", err)
	}
	if err := validateHandoff(t, []string{"lf_research_finding"}, edit); err != nil {
		t.Fatalf("a declared finding action was refused: %v", err)
	}
}

func TestResponseReachesOwnerWaitFollowsTheEdges(t *testing.T) {
	policy := graphexec.ResearchControlPolicy()
	policy.Generation = ""
	policy.MeasureActions = []string{"lf_research_measure", "lf_research_finding"}
	profile, err := graphexec.ParseProfile([]byte(handoffDOT))
	if err != nil {
		t.Fatal(err)
	}
	if !graphexec.ResponseReachesOwnerWait(profile, "work", policy) {
		t.Fatal("the first agent's ask ends at an owner question")
	}
	if graphexec.ResponseReachesOwnerWait(profile, "worker", policy) {
		t.Fatal("the handed-off agent's ask has no owner to wait for")
	}
	if graphexec.ResponseReachesOwnerWait(nil, "work", policy) || graphexec.ResponseReachesOwnerWait(profile, "missing", policy) {
		t.Fatal("an absent graph or agent reaches an owner wait")
	}
}

func TestOtherModesKeepFailureAndAgentRules(t *testing.T) {
	for name, policy := range map[string]graphexec.ControlPolicy{"research": graphexec.ResearchControlPolicy(), "plan": graphexec.PlanControlPolicy()} {
		if len(policy.MeasureActions) != 0 || len(policy.FindingActions) != 0 {
			t.Errorf("%s policy gained measure behaviour", name)
		}
	}
}
