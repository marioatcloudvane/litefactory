package graphexec_test

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

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
