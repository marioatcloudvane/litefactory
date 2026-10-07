package graphexec_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

func rightsGraph(mode, tag string) string {
	return `digraph {team=builtin;mode=` + mode + `;profile_version=1;start=wait;
wait [lf_action=lf_ask,lf_kind=question,` + tag + `lf_outcomes=published];
done [lf_terminal=true];
wait -> done [lf_on=published];}`
}

func validateRights(t *testing.T, mode agent.Mode, dot string) error {
	t.Helper()
	profile, err := graphexec.ParseProfile([]byte(dot))
	if err != nil {
		t.Fatal(err)
	}
	var registry graphexec.Registry
	spec := graphexec.ActionSpec{Name: "lf_ask", Mode: mode, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: []string{"question"}}}}
	if err := registry.Register(spec); err != nil {
		t.Fatal(err)
	}
	return graphexec.Validate(profile, registry)
}

func TestRightsTagOnAskNodes(t *testing.T) {
	for _, tag := range []string{``, `lf_rights=approve,`, `lf_rights="answer,request_changes,approve",`} {
		if err := validateRights(t, agent.ModePlan, rightsGraph("plan", tag)); err != nil {
			t.Fatalf("tag %q refused: %v", tag, err)
		}
	}
}

func TestRightsTagRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		mode agent.Mode
		dot  string
		want string
	}{
		"unknown value":    {agent.ModePlan, rightsGraph("plan", `lf_rights=merge,`), "outside declared values"},
		"repeated value":   {agent.ModePlan, rightsGraph("plan", `lf_rights="approve,approve",`), "repeats"},
		"empty value":      {agent.ModePlan, rightsGraph("plan", `lf_rights="",`), "outside declared values"},
		"in Implement":     {agent.ModeImplement, rightsGraph("implement", `lf_rights=approve,`), "Implement graph"},
		"other node kinds": {agent.ModePlan, strings.Replace(rightsGraph("plan", ``), "done [lf_terminal=true]", "done [lf_terminal=true,lf_rights=approve]", 1), "only on lf_ask"},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateRights(t, tc.mode, tc.dot)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestRightsTagRefusedOnOtherActions(t *testing.T) {
	dot := `digraph {team=builtin;mode=research;profile_version=1;start=say;
say [lf_action=lf_comment,lf_kind=final,lf_rights=approve,lf_outcomes=published];
done [lf_terminal=true];
say -> done [lf_on=published];}`
	profile, err := graphexec.ParseProfile([]byte(dot))
	if err != nil {
		t.Fatal(err)
	}
	var registry graphexec.Registry
	if err := registry.Register(graphexec.ActionSpec{Name: "lf_comment", Mode: agent.ModeResearch, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: []string{"final"}}}}); err != nil {
		t.Fatal(err)
	}
	err = graphexec.Validate(profile, registry)
	if err == nil || !strings.Contains(err.Error(), "only on lf_ask") {
		t.Fatalf("lf_rights on lf_comment was not refused: %v", err)
	}
}
