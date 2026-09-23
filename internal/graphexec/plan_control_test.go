package graphexec_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

// In-memory graph fixture: no repository/filesystem or installed-tree dependency.
func planPolicyGraph() string {
	var b strings.Builder
	b.WriteString(`digraph plan { graph [team="builtin",mode="plan",profile_version="1",start="select_story"];`)
	for _, stage := range []string{"story", "technical", "acceptance"} {
		fmt.Fprintf(&b, `select_%s [lf_action="lf_if",lf_outcomes="true,false",lf_field="response",lf_condition="%s",lf_limit="24"]; select_%s -> %s_loop [lf_on="true"];`, stage, stage, stage, stage)
		attrs := map[string]string{"loop": fmt.Sprintf(`lf_action="lf_loop",lf_outcomes="true,false",lf_field="response",lf_condition="%s"`, stage), "agent": fmt.Sprintf(`lf_action="lf_agent",lf_outcomes="success,failed",lf_agent_markdown="plan-%s"`, stage), "question": fmt.Sprintf(`lf_action="lf_if",lf_outcomes="true,false",lf_field="response",lf_condition="ask",lf_source="%s_agent"`, stage), "draft": fmt.Sprintf(`lf_action="lf_plan_story",lf_outcomes="applied,conflict,failed,uncertain",lf_source="%s_agent"`, stage), "gate": `lf_action="lf_if",lf_outcomes="true,false",lf_field="response",lf_condition="approved"`, "ask": fmt.Sprintf(`lf_action="lf_ask",lf_outcomes="published",lf_kind="question",lf_source="%s_agent"`, stage), "review": fmt.Sprintf(`lf_action="lf_ask",lf_outcomes="published",lf_kind="review",lf_source="%s_agent"`, stage)}
		for _, name := range []string{"loop", "agent", "question", "draft", "gate", "ask", "review"} {
			fmt.Fprintf(&b, `%s_%s [%s,lf_limit="8"];`, stage, name, attrs[name])
		}
		edges := [][3]string{{"loop", "true", stage + "_agent"}, {"loop", "false", "stopped"}, {"agent", "success", stage + "_question"}, {"agent", "failed", "failed"}, {"question", "true", stage + "_ask"}, {"question", "false", stage + "_draft"}, {"draft", "applied", stage + "_gate"}, {"draft", "conflict", "stopped"}, {"draft", "failed", "failed"}, {"draft", "uncertain", "stopped"}, {"gate", "true", "promoted"}, {"gate", "false", stage + "_review"}, {"ask", "published", "waiting"}, {"review", "published", "waiting"}}
		for _, e := range edges {
			fmt.Fprintf(&b, `%s_%s -> %s [lf_on="%s"];`, stage, e[0], e[2], e[1])
		}
	}
	b.WriteString(`select_story -> select_technical [lf_on="false"];select_technical -> select_acceptance [lf_on="false"];select_acceptance -> stopped [lf_on="false"];failed [lf_action="lf_comment",lf_outcomes="published",lf_kind="failure",lf_limit="1"];failed -> stopped [lf_on="published"];waiting [lf_terminal="true"];promoted [lf_terminal="true"];stopped [lf_terminal="true"];}`)
	return b.String()
}

func TestPlanControl_OwnerGatesAndExhaustion(t *testing.T) {
	tests := []struct{ name, old, next string }{
		{"review ready cannot promote", `lf_condition="approved"`, `lf_condition="review_ready"`},
		{"loop exhaustion cannot promote", `story_loop -> stopped [lf_on="false"]`, `story_loop -> promoted [lf_on="false"]`},
		{"uncertain publication cannot promote", `story_draft -> stopped [lf_on="uncertain"]`, `story_draft -> promoted [lf_on="uncertain"]`},
		{"question cannot continue", `story_ask -> waiting`, `story_ask -> technical_loop`},
		{"wrong stage source", `lf_source="story_agent"`, `lf_source="technical_agent"`},
		{"unbounded node", `lf_limit="8"`, `lf_limit="0"`},
		{"nonresponse expression", `lf_field="response"`, `lf_field="owner.approved"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw := strings.Replace(planPolicyGraph(), tt.old, tt.next, 1)
			p, err := graphexec.ParseProfile([]byte(raw))
			if err == nil {
				err = graphexec.ValidatePlanControlGraph(p)
			}
			if err == nil {
				t.Fatal("unsafe gate accepted")
			}
		})
	}
	t.Run("three stages bounded", func(t *testing.T) {
		p, err := graphexec.ParseProfile([]byte(planPolicyGraph()))
		if err != nil {
			t.Fatal(err)
		}
		if err = graphexec.ValidatePlanControlGraph(p); err != nil {
			t.Fatal(err)
		}
		for _, stage := range []string{"story", "technical", "acceptance"} {
			next, ok := graphexec.Decide(p, stage+"_loop", "false")
			if !ok || next != "stopped" {
				t.Fatal("exhaustion grants approval")
			}
		}
	})
}
