package graphexec_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

const controlDOT = `digraph {team=builtin;mode=research;profile_version=1;start=loop;
loop [lf_action=lf_loop,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_default=ask,lf_limit=3];
work [lf_action=lf_agent,lf_outcomes=success,lf_limit=3];
brief [lf_action=lf_research_brief,lf_source=work,lf_outcomes=success,lf_limit=3];
check [lf_action=lf_if,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_limit=3];
question [lf_action=lf_ask,lf_kind=question,lf_outcomes=published];
report [lf_action=lf_comment,lf_kind=final,lf_outcomes=published];
wait [lf_terminal=true];end [lf_terminal=true];
loop -> work [on=true];loop -> report [on=false];work -> brief [on=success];brief -> check [on=success];
check -> question [on=true];check -> loop [on=false];question -> wait [on=published];report -> end [on=published];}`

func TestResearchConditionValues(t *testing.T) {
	c, err := graphexec.DecodeResearchCondition(graphexec.ResearchLoopAction, map[string]string{"lf_field": "response", "lf_condition": "ask", "lf_default": "ask"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		response  string
		attempted bool
		want      string
	}{{"", false, "true"}, {"ask", true, "true"}, {"done", true, "false"}, {"", true, ""}, {"unknown", true, ""}, {"null", true, ""}} {
		got, err := c.Evaluate(tc.response, tc.attempted)
		if got != tc.want || (err != nil) != (tc.want == "") {
			t.Fatalf("%+v: %q %v", tc, got, err)
		}
	}
	for _, attrs := range []map[string]string{
		{"lf_field": "other", "lf_condition": "ask"}, {"lf_field": "response", "lf_condition": "ask || true"},
		{"lf_field": "response", "lf_condition": "ask", "lf_default": ""}, {"lf_field": "response", "lf_condition": "ask", "lf_default": "unknown"},
	} {
		if _, err := graphexec.DecodeResearchCondition(graphexec.ResearchLoopAction, attrs); err == nil {
			t.Fatalf("accepted %v", attrs)
		}
	}
	if _, err := graphexec.DecodeResearchCondition(graphexec.ResearchIfAction, map[string]string{"lf_field": "response", "lf_condition": "done", "lf_default": "ask"}); err == nil {
		t.Fatal("if accepted default")
	}
}

func TestResearchControlValidation(t *testing.T) {
	specs := graphexec.Registry{Policy: graphexec.ValidateResearchControlGraph}
	for _, spec := range []graphexec.ActionSpec{
		{Name: "lf_research_brief", Mode: agent.ModeResearch, Outcomes: []string{"success"}},
		graphexec.ControlSpec(graphexec.ResearchLoopAction, graphexec.ResearchControlPolicy()), graphexec.ControlSpec(graphexec.ResearchIfAction, graphexec.ResearchControlPolicy()),
		{Name: "lf_agent", Mode: agent.ModeResearch, Outcomes: []string{"success"}}, {Name: "lf_comment", Mode: agent.ModeResearch, Outcomes: []string{"published"}}, {Name: "lf_ask", Mode: agent.ModeResearch, Outcomes: []string{"published"}},
	} {
		if err := specs.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, from, to string
		valid          bool
	}{
		{name: "valid", valid: true},
		{name: "unknown field", from: "lf_field=response", to: "lf_field=unknown"},
		{name: "unknown condition", from: "lf_condition=ask", to: "lf_condition=unknown"},
		{name: "missing limit", from: "lf_limit=3", to: "lf_limit=0"},
		{name: "missing false edge", from: "loop -> report [on=false];", to: ""},
		{name: "undeclared branch", from: "loop -> report [on=false]", to: "loop -> report [on=unknown]"},
		{name: "duplicate branch", from: "loop -> report [on=false]", to: "loop -> report [on=true]"},
		{name: "open control outcomes", from: "lf_outcomes=\"true,false\"", to: "lf_outcomes=\"true,false,unknown\""},
		{name: "infinite limit", from: "lf_limit=3", to: "lf_limit=Inf"},
		{name: "negative limit", from: "lf_limit=3", to: "lf_limit=-1"},
		{name: "done retries", from: "check -> loop [on=false]", to: "check -> work [on=false]"},
		{name: "ask retries", from: "check -> question [on=true]", to: "check -> work [on=true]"},
		{name: "question retries", from: "question -> wait [on=published]", to: "question -> work [on=published]"},
		{name: "ask unreported", from: "check -> question [on=true]", to: "check -> wait [on=true]"},
		{name: "ask reports complete", from: "lf_kind=question", to: "lf_kind=final"},
		{name: "ask control cycle", from: "check -> question [on=true]", to: "check -> loop [on=true]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := controlDOT
			if tc.from != "" {
				raw = strings.Replace(raw, tc.from, tc.to, 1)
			}
			p, err := graphexec.ParseProfile([]byte(raw))
			if err != nil {
				if tc.valid {
					t.Fatal(err)
				}
				return
			}
			err = graphexec.Validate(p, specs)
			if (err == nil) != tc.valid {
				t.Fatalf("validation=%v", err)
			}
		})
	}
}

func TestResearchAttributeSchemaCloning(t *testing.T) {
	var r graphexec.Registry
	spec := graphexec.ControlSpec(graphexec.ResearchLoopAction, graphexec.ResearchControlPolicy())
	if err := r.Register(spec); err != nil {
		t.Fatal(err)
	}
	spec.Attributes["lf_field"].Values[0] = "mutated"
	got, _ := r.Resolve(spec.Name)
	if got.Attributes["lf_field"].Values[0] != "response" {
		t.Fatal("registration aliases schema")
	}
	got.Attributes["lf_field"].Values[0] = "mutated"
	next, _ := r.Resolve(spec.Name)
	if next.Attributes["lf_field"].Values[0] != "response" {
		t.Fatal("resolution aliases schema")
	}
}

func TestResearchConditionDoneComparison(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, action, response, defaultValue, want string
		attempted                                  bool
	}{
		{name: "loop initial ask/false", action: graphexec.ResearchLoopAction, defaultValue: "ask", want: "false"},
		{name: "loop initial done/true", action: graphexec.ResearchLoopAction, defaultValue: "done", want: "true"},
		{name: "loop saved ask overrides default/false", action: graphexec.ResearchLoopAction, defaultValue: "done", response: "ask", attempted: true, want: "false"},
		{name: "loop saved done overrides default/true", action: graphexec.ResearchLoopAction, defaultValue: "ask", response: "done", attempted: true, want: "true"},
		{name: "if saved ask/false", action: graphexec.ResearchIfAction, response: "ask", attempted: true, want: "false"},
		{name: "if saved done/true", action: graphexec.ResearchIfAction, response: "done", attempted: true, want: "true"},
		{name: "if without result/error", action: graphexec.ResearchIfAction},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			attrs := map[string]string{"lf_field": "response", "lf_condition": "done"}
			if tc.defaultValue != "" {
				attrs["lf_default"] = tc.defaultValue
			}
			condition, err := graphexec.DecodeResearchCondition(tc.action, attrs)
			if err != nil {
				t.Fatal(err)
			}
			got, err := condition.Evaluate(tc.response, tc.attempted)
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("Evaluate = %q, %v; want %q (error=%v)", got, err, tc.want, tc.want == "")
			}
		})
	}
}

// A declared failure edge is the only way the owner learns that Research broke.
// It must reach a failure publication that ends the graph and can never reach
// an agent: a failed attempt owns no response to authorize one with.
func TestValidateResearchFailurePathRejectsUnsafeTargets(t *testing.T) {
	t.Parallel()
	const base = `digraph {team=builtin;mode=research;profile_version=1;start=loop;
loop [lf_action=lf_loop,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_default=ask,lf_limit=3];
work [lf_action=lf_agent,lf_outcomes="success,failed",lf_limit=3];
brief [lf_action=lf_research_brief,lf_source=work,lf_outcomes=success,lf_limit=3];
check [lf_action=lf_if,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_limit=3];
question [lf_action=lf_ask,lf_kind=question,lf_outcomes=published];
report [lf_action=lf_comment,lf_kind=final,lf_outcomes=published];
wait [lf_terminal=true];end [lf_terminal=true];stopped [lf_terminal=true];
loop -> work [on=true];loop -> report [on=false];work -> brief [on=success];brief -> check [on=success];
check -> question [on=true];check -> loop [on=false];question -> wait [on=published];report -> end [on=published];
`
	for _, tc := range []struct{ name, tail, want string }{
		{"final comment", "work -> report [on=failed];}", "requires a failure comment"},
		{"agent target", "work -> work [on=failed];}", "requires a comment activity"},
		{"missing terminal", `broken [lf_action=lf_comment,lf_kind=failure,lf_outcomes=published];
work -> broken [on=failed];broken -> loop [on=published];}`, "must end at a terminal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, err := graphexec.ParseProfile([]byte(base + tc.tail))
			if err != nil {
				t.Skipf("fixture rejected earlier than the failure path: %v", err)
			}
			err = graphexec.ValidateResearchControlGraph(p)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

// The optional edge stays optional: a graph without one is unchanged.
func TestValidateResearchControlGraphAcceptsNoFailureEdge(t *testing.T) {
	t.Parallel()
	p, err := graphexec.ParseProfile([]byte(controlDOT))
	if err != nil {
		t.Fatal(err)
	}
	if err := graphexec.ValidateResearchControlGraph(p); err != nil {
		t.Fatalf("graph without a failure edge must stay valid: %v", err)
	}
}

const twoStageDOT = `digraph {team=builtin;mode=research;profile_version=1;start=loop;
loop [lf_action=lf_loop,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_default=ask,lf_limit=3,lf_source=work];
work [lf_action=lf_agent,lf_outcomes=success,lf_limit=3];
brief [lf_action=lf_research_brief,lf_source=work,lf_outcomes=success,lf_limit=3];
check [lf_action=lf_if,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_limit=3,lf_source=work];
question [lf_action=lf_ask,lf_kind=question,lf_outcomes=published];
report [lf_action=lf_comment,lf_kind=final,lf_outcomes=published];
shapeloop [lf_action=lf_loop,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_default=ask,lf_limit=3,lf_source=shape];
shape [lf_action=lf_agent,lf_outcomes=success,lf_limit=3];
shapebrief [lf_action=lf_research_brief,lf_source=shape,lf_outcomes=success,lf_limit=3];
shapecheck [lf_action=lf_if,lf_outcomes="true,false",lf_field=response,lf_condition=ask,lf_limit=3,lf_source=shape];
clarify [lf_action=lf_ask,lf_kind=question,lf_outcomes=published];
agreed [lf_action=lf_comment,lf_kind=final,lf_outcomes=published];
wait [lf_terminal=true];hold [lf_terminal=true];end [lf_terminal=true];
loop -> work [on=true];loop -> report [on=false];
work -> brief [on=success];brief -> check [on=success];check -> question [on=true];check -> loop [on=false];
question -> wait [on=published];report -> shapeloop [on=published];
shapeloop -> shape [on=true];shapeloop -> agreed [on=false];
shape -> shapebrief [on=success];shapebrief -> shapecheck [on=success];shapecheck -> clarify [on=true];shapecheck -> shapeloop [on=false];
clarify -> hold [on=published];agreed -> end [on=published];}`

// Publishing a report hands the workflow to the next stage instead of ending it.
// The owner sees the findings, then a loop scoped to the next agent bootstraps
// from its own default rather than inheriting the previous stage's response.
func TestValidateResearchControlGraphAcceptsAPublishedHandoff(t *testing.T) {
	t.Parallel()
	p, err := graphexec.ParseProfile([]byte(twoStageDOT))
	if err != nil {
		t.Fatal(err)
	}
	if err := graphexec.ValidateResearchControlGraph(p); err != nil {
		t.Fatalf("two-stage graph must validate: %v", err)
	}
}

func TestValidateResearchControlGraphKeepsTheOwnerBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, from, to, want string }{
		// A question that advances would run on without the answer it asked for.
		{"ask must rest", "question -> wait [on=published];", "question -> shapeloop [on=published];", "must end at a durable owner wait"},
		// Reaching an agent directly is still unauthorized; only a differently
		// scoped control node hands over, and only after a publication.
		{"no direct agent", "report -> shapeloop [on=published];", "report -> shape [on=published];", "invokes agent"},
		// A source that names nothing cannot scope anything.
		{"source must exist", "lf_source=shape];\nshape [", "lf_source=absent];\nshape [", "does not name an agent node"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := strings.Replace(twoStageDOT, tc.from, tc.to, 1)
			if raw == twoStageDOT {
				t.Fatalf("fixture edit %q did not apply", tc.name)
			}
			p, err := graphexec.ParseProfile([]byte(raw))
			if err != nil {
				t.Skipf("fixture rejected before control validation: %v", err)
			}
			err = graphexec.ValidateResearchControlGraph(p)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}
