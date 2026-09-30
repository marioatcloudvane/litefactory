package graphexec_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

const measureDOT = `digraph {team=builtin;mode=research;profile_version=1;start=loop;
loop [lf_action=lf_loop,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_default=ask,lf_limit=3,lf_source=work];
work [lf_action=lf_agent,lf_outcomes="success,failed",lf_limit=3,lf_access=read];
check [lf_action=lf_if,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_limit=3,lf_source=work];
gauge [lf_action=lf_research_measure,lf_source=work,lf_forbid="alpha,beta",lf_outcomes="one,two,failed",lf_limit=3];
question [lf_action=lf_ask,lf_kind=question,lf_source=work,lf_outcomes=published];
report [lf_action=lf_comment,lf_kind=final,lf_source=work,lf_outcomes=published];
oops [lf_action=lf_comment,lf_kind=failure,lf_outcomes=published];
wait [lf_terminal=true];end [lf_terminal=true];stop [lf_terminal=true];
loop -> work [lf_on=true];loop -> report [lf_on=false];
work -> check [lf_on=success];work -> oops [lf_on=failed];
check -> question [lf_on=true];check -> gauge [lf_on=false];
gauge -> report [lf_on=one];gauge -> report [lf_on=two];gauge -> oops [lf_on=failed];
question -> wait [lf_on=published];report -> end [lf_on=published];oops -> stop [lf_on=published];}`

func measureRegistry(t *testing.T, measure []string) graphexec.Registry {
	t.Helper()
	policy := graphexec.ResearchControlPolicy()
	policy.Generation = ""
	policy.MeasureActions = measure
	registry := graphexec.Registry{Policy: func(p *graphexec.Profile) error { return graphexec.ValidateControlGraph(p, policy) }}
	for _, spec := range []graphexec.ActionSpec{
		{Name: "lf_agent", Mode: agent.ModeResearch, Outcomes: []string{"success", "failed"}, Attributes: graphexec.AgentAttributes()},
		graphexec.ControlSpec(graphexec.ResearchLoopAction, policy), graphexec.ControlSpec(graphexec.ResearchIfAction, policy),
		{Name: "lf_comment", Mode: agent.ModeResearch, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: []string{"final", "failure"}}}},
		{Name: "lf_ask", Mode: agent.ModeResearch, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: []string{"question"}}}},
		{Name: "lf_research_measure", Mode: agent.ModeResearch, Outcomes: []string{"one", "two", "failed"}, Attributes: map[string]graphexec.AttributeSpec{"lf_forbid": {Required: true, List: true, Values: []string{"alpha", "beta", "gamma"}}}},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	return registry
}

func validateMeasure(t *testing.T, measure []string, edit func(string) string) error {
	t.Helper()
	profile, err := graphexec.ParseProfile([]byte(edit(measureDOT)))
	if err != nil {
		t.Fatal(err)
	}
	return graphexec.Validate(profile, measureRegistry(t, measure))
}

func identity(s string) string { return s }

func TestMeasureActionsValidateOnAnAnswerPath(t *testing.T) {
	if err := validateMeasure(t, []string{"lf_research_measure"}, identity); err != nil {
		t.Fatalf("declared measure step refused: %v", err)
	}
	// The list is data on the policy: without it the same step is not an
	// admitted activity on an answer path, exactly as before.
	err := validateMeasure(t, nil, identity)
	if err == nil || !strings.Contains(err.Error(), "unsupported activity") {
		t.Fatalf("an undeclared measure step was admitted on an answer path: %v", err)
	}
	for name, tc := range map[string]struct{ from, to, want string }{
		"failed edge must reach the failure comment": {"gauge -> oops [lf_on=failed]", "gauge -> report [lf_on=failed]", "failure path"},
		"source must name an agent":                  {"lf_source=work,lf_forbid", "lf_source=check,lf_forbid", "does not name an agent"},
		"missing source":                             {"lf_source=work,lf_forbid", "lf_forbid", "does not name an agent"},
		"source must be the current agent":           {"gauge [lf_action=lf_research_measure,lf_source=work", "gauge [lf_action=lf_research_measure,lf_source=other", "does not name an agent"},
		"every outcome needs an edge":                {"gauge -> report [lf_on=two];", "", "want exactly one"},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateMeasure(t, []string{"lf_research_measure"}, func(s string) string {
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

func TestListAttributeIsAClosedSet(t *testing.T) {
	for name, tc := range map[string]struct {
		value string
		want  string
	}{
		"single":             {"alpha", ""},
		"several":            {"alpha,gamma", ""},
		"spaces are ignored": {"alpha , beta", ""},
		"unknown element":    {"alpha,delta", "outside declared values"},
		"repeated element":   {"alpha,alpha", "repeats element"},
		"empty element":      {"alpha,", "outside declared values"},
		"empty value":        {"", "outside declared values"},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateMeasure(t, []string{"lf_research_measure"}, func(s string) string {
				return strings.Replace(s, `lf_forbid="alpha,beta"`, `lf_forbid="`+tc.value+`"`, 1)
			})
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("refused %q: %v", tc.value, err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("want %q for %q, got %v", tc.want, tc.value, err)
			}
		})
	}
}

func TestAgentAccessIsOptionalAndClosed(t *testing.T) {
	for _, access := range []string{"read", "write"} {
		if err := validateMeasure(t, []string{"lf_research_measure"}, func(s string) string { return strings.Replace(s, "lf_access=read", "lf_access="+access, 1) }); err != nil {
			t.Fatalf("access %s refused: %v", access, err)
		}
	}
	if err := validateMeasure(t, []string{"lf_research_measure"}, func(s string) string { return strings.Replace(s, ",lf_access=read", "", 1) }); err != nil {
		t.Fatalf("an agent without access refused: %v", err)
	}
	err := validateMeasure(t, []string{"lf_research_measure"}, func(s string) string { return strings.Replace(s, "lf_access=read", "lf_access=admin", 1) })
	if err == nil || !strings.Contains(err.Error(), "outside declared values") {
		t.Fatalf("undeclared access accepted: %v", err)
	}
}

func TestExistingControlPoliciesDeclareNoMeasureActions(t *testing.T) {
	for name, policy := range map[string]graphexec.ControlPolicy{"research": graphexec.ResearchControlPolicy(), "plan": graphexec.PlanControlPolicy()} {
		if len(policy.MeasureActions) != 0 {
			t.Errorf("%s policy gained measure actions: %v", name, policy.MeasureActions)
		}
	}
}
