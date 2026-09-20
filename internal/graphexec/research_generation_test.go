package graphexec_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

func TestResearchGenerationRequiredOnSuccessfulPaths(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, from, to, want string }{
		{name: "both candidates generated/accepted"},
		{"investigation bypass/rejected", "work -> brief [on=success]", "work -> check [on=success]", "requires matching successful lf_research_brief generation"},
		{"shaping bypass/rejected", "shape -> shapebrief [on=success]", "shape -> shapecheck [on=success]", "requires matching successful lf_research_brief generation"},
		{"another candidate/rejected", "lf_source=work,lf_outcomes=success,lf_limit=3", "lf_source=shape,lf_outcomes=success,lf_limit=3", "source does not match"},
		{"missing source/rejected", "lf_source=work,lf_outcomes=success,lf_limit=3", "lf_source=absent,lf_outcomes=success,lf_limit=3", "requires named agent source"},
		{"control as source/rejected", "lf_source=work,lf_outcomes=success,lf_limit=3", "lf_source=check,lf_outcomes=success,lf_limit=3", "requires named agent source"},
		{"failure is not generation success/rejected", "brief -> check [on=success]", "brief -> check [on=failed]", "lacks success edge"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := twoStageDOT
			if tc.from != "" {
				raw = strings.Replace(raw, tc.from, tc.to, 1)
				if raw == twoStageDOT {
					t.Fatal("fixture replacement did not apply")
				}
			}
			p, err := graphexec.ParseProfile([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			err = graphexec.ValidateResearchControlGraph(p)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want diagnostic %q, got %v", tc.want, err)
			}
		})
	}
}

func TestResearchGenerationAllowsFailedAgentDiagnostic(t *testing.T) {
	t.Parallel()
	raw := strings.Replace(controlDOT, "lf_action=lf_agent,lf_outcomes=success", `lf_action=lf_agent,lf_outcomes="success,failed"`, 1)
	raw = strings.TrimSuffix(raw, "}") + `
broken [lf_action=lf_comment,lf_kind=failure,lf_outcomes=published];
work -> broken [on=failed];broken -> end [on=published];}`
	p, err := graphexec.ParseProfile([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := graphexec.ValidateResearchControlGraph(p); err != nil {
		t.Fatalf("failed agent diagnostic must not require generation: %v", err)
	}
}
