package graphexec_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

func TestSharedActionsRequireExplicitModeAdmission(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"lf_agent", "lf_if", "lf_loop", "lf_comment", "lf_ask"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name       string
				registered bool
				mode       agent.Mode
				valid      bool
			}{
				{"registered admitted mode", true, agent.ModePlan, true},
				{"generic spelling alone", false, agent.ModePlan, false},
				{"registered other mode", true, agent.ModeResearch, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					var r graphexec.Registry
					if tc.registered {
						if err := r.Register(graphexec.ActionSpec{Name: action, Mode: tc.mode, Outcomes: []string{"success"}}); err != nil {
							t.Fatal(err)
						}
					}
					p, err := graphexec.ParseProfile([]byte(fmt.Sprintf(`digraph {team=test;mode=plan;profile_version=1;start=work;work [lf_action=%s,lf_outcomes=success];end [lf_terminal=true];work -> end [on=success];}`, action)))
					if err != nil {
						t.Fatal(err)
					}
					if err := graphexec.Validate(p, r); (err == nil) != tc.valid {
						t.Fatalf("Validate = %v, want valid %v", err, tc.valid)
					}
				})
			}
		})
	}
}

func TestSharedRegistryRejectsRemovedAndCrossModeActions(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"lf_research_agent", "lf_research_if", "lf_research_loop", "lf_research_comment", "lf_plan_publish", "lf_unknown"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var r graphexec.Registry
			if err := r.Register(graphexec.ActionSpec{Name: name, Mode: agent.ModeResearch, Outcomes: []string{"success"}}); err == nil {
				t.Fatalf("registered unauthorized action %q", name)
			}
			if _, ok := r.Resolve(name); ok {
				t.Fatalf("rejected action %q retained", name)
			}
		})
	}
}

func TestSharedControlsUseAdmittedVocabularyAndInitialDefault(t *testing.T) {
	t.Parallel()
	policy := graphexec.ControlPolicy{Mode: agent.ModePlan, Responses: []string{"question", "ready", "accepted"}, Defaults: []string{"question", "ready"}, Ask: "question", Approved: "accepted"}
	condition, err := graphexec.DecodeCondition("lf_loop", map[string]string{"lf_field": "response", "lf_condition": "question", "lf_default": "question"}, policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, response, want string
		attempted            bool
	}{
		{"initial default", "", "true", false},
		{"retained question", "question", "true", true},
		{"retained ready", "ready", "false", true},
		{"retained owner acceptance", "accepted", "false", true},
		{"missing retained response", "", "", true},
		{"research vocabulary rejected", "ask", "", true},
		{"expression rejected", "question || true", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := condition.Evaluate(tc.response, tc.attempted)
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("Evaluate = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	for _, tc := range []struct{ name, from, to string }{
		{"valid vocabulary", "", ""},
		{"owner wait bypass", "question -> wait [on=published]", "question -> work [on=published]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := strings.ReplaceAll(controlDOT, "mode=research", "mode=plan")
			raw = strings.ReplaceAll(raw, "brief [lf_action=lf_research_brief,lf_source=work,lf_outcomes=success,lf_limit=3];", "")
			raw = strings.ReplaceAll(raw, "work -> brief [on=success];brief -> check [on=success];", "work -> check [on=success];")
			raw = strings.ReplaceAll(raw, "=ask", "=question")
			if tc.from != "" {
				raw = strings.Replace(raw, tc.from, tc.to, 1)
			}
			p, err := graphexec.ParseProfile([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			err = graphexec.ValidateControlGraph(p, policy)
			if (err == nil) != (tc.from == "") {
				t.Fatalf("ValidateControlGraph = %v", err)
			}
			if tc.from != "" && !strings.Contains(err.Error(), "durable owner wait") {
				t.Fatalf("lost owner-wait diagnostic: %v", err)
			}
		})
	}
}
