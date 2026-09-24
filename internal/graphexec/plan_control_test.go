package graphexec_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// stageBlock declares one stage's nodes and edges. limit bounds the nodes a
// creation cycle visits; review is where a published review goes next.
func stageBlock(stage, role string, limit int, review string) string {
	var b strings.Builder
	attrs := map[string]string{
		"loop":     fmt.Sprintf(`lf_action="lf_loop",lf_outcomes="true,false",lf_field="response",lf_condition="%s"`, stage),
		"agent":    fmt.Sprintf(`lf_action="lf_agent",lf_outcomes="success,failed",lf_agent_markdown="%s"`, role),
		"question": fmt.Sprintf(`lf_action="lf_if",lf_outcomes="true,false",lf_field="response",lf_condition="ask",lf_source="%s_agent"`, stage),
		"draft":    fmt.Sprintf(`lf_action="lf_plan_story",lf_outcomes="applied,conflict,failed,uncertain",lf_source="%s_agent"`, stage),
		"gate":     `lf_action="lf_if",lf_outcomes="true,false",lf_field="response",lf_condition="approved"`,
		"ask":      fmt.Sprintf(`lf_action="lf_ask",lf_outcomes="published",lf_kind="question",lf_source="%s_agent"`, stage),
		"review":   fmt.Sprintf(`lf_action="lf_ask",lf_outcomes="published",lf_kind="review",lf_source="%s_agent"`, stage),
	}
	for _, name := range []string{"loop", "agent", "question", "draft", "gate", "ask", "review"} {
		l := 8
		if limit > 0 && name != "loop" && name != "ask" {
			l = limit
		}
		fmt.Fprintf(&b, `%s_%s [%s,lf_limit="%d"];`, stage, name, attrs[name], l)
	}
	edges := [][3]string{{"loop", "true", stage + "_agent"}, {"loop", "false", "stopped"}, {"agent", "success", stage + "_question"}, {"agent", "failed", "failed"}, {"question", "true", stage + "_ask"}, {"question", "false", stage + "_draft"}, {"draft", "applied", stage + "_gate"}, {"draft", "conflict", "failed"}, {"draft", "failed", "failed"}, {"draft", "uncertain", "failed"}, {"gate", "true", "promoted"}, {"gate", "false", stage + "_review"}, {"ask", "published", "waiting"}, {"review", "published", review}}
	for _, e := range edges {
		fmt.Fprintf(&b, `%s_%s -> %s [lf_on="%s"];`, stage, e[0], e[2], e[1])
	}
	return b.String()
}

func selector(stage, condition, onTrue, onFalse string) string {
	return fmt.Sprintf(`select_%s [lf_action="lf_if",lf_outcomes="true,false",lf_field="response",lf_condition="%s",lf_limit="24"];select_%s -> %s [lf_on="true"];select_%s -> %s [lf_on="false"];`, stage, condition, stage, onTrue, stage, onFalse)
}

const planTail = `failed [lf_action="lf_comment",lf_outcomes="published",lf_kind="failure",lf_limit="1"];failed -> stopped [lf_on="published"];waiting [lf_terminal="true"];promoted [lf_terminal="true"];stopped [lf_terminal="true"];}`

// In-memory graph fixture: no repository/filesystem or installed-tree dependency.
// It declares the set stage, the creation cycle and the three shaping stages.
func planPolicyGraph() string {
	var b strings.Builder
	b.WriteString(`digraph plan { graph [team="builtin",mode="plan",profile_version="1",start="select_set"];`)
	b.WriteString(selector("set", "set", "set_loop", "select_story"))
	b.WriteString(selector("story", "story", "story_loop", "select_technical"))
	b.WriteString(selector("technical", "technical", "technical_loop", "select_acceptance"))
	b.WriteString(selector("acceptance", "acceptance", "acceptance_loop", "stopped"))
	b.WriteString(stageBlock("set", "plan-set", 0, "waiting"))
	b.WriteString(stageBlock("story", "plan-story", graphexec.PlanCreationBound, "story_pick"))
	fmt.Fprintf(&b, `story_pick [lf_action="lf_loop",lf_outcomes="true,false",lf_field="response",lf_condition="create",lf_limit="%d"];story_pick -> story_agent [lf_on="true"];story_pick -> waiting [lf_on="false"];`, graphexec.PlanCreationBound)
	b.WriteString(stageBlock("technical", "plan-technical", 0, "waiting"))
	b.WriteString(stageBlock("acceptance", "plan-acceptance", 0, "waiting"))
	b.WriteString(planTail)
	return b.String()
}

// minimalPlanGraph declares one stage and no set: the checker pins no node set.
func minimalPlanGraph() string {
	return `digraph plan { graph [team="builtin",mode="plan",profile_version="1",start="select_story"];` +
		selector("story", "story", "story_loop", "stopped") + stageBlock("story", "plan-story", 0, "waiting") + planTail
}

func checkPlan(t *testing.T, raw string) error {
	t.Helper()
	p, err := graphexec.ParseProfile([]byte(raw))
	if err != nil {
		return err
	}
	return graphexec.ValidatePlanControlGraph(p)
}

func TestPlanControl_OwnerGatesAndExhaustion(t *testing.T) {
	tests := []struct{ name, old, next string }{
		{"review ready cannot promote", `lf_condition="approved"`, `lf_condition="review_ready"`},
		{"loop exhaustion cannot promote", `story_loop -> stopped [lf_on="false"]`, `story_loop -> promoted [lf_on="false"]`},
		{"uncertain publication cannot promote", `story_draft -> failed [lf_on="uncertain"]`, `story_draft -> promoted [lf_on="uncertain"]`},
		{"uncertain publication cannot stop silently", `story_draft -> failed [lf_on="uncertain"]`, `story_draft -> stopped [lf_on="uncertain"]`},
		{"conflict cannot stop silently", `story_draft -> failed [lf_on="conflict"]`, `story_draft -> stopped [lf_on="conflict"]`},
		{"question cannot continue", `story_ask -> waiting`, `story_ask -> technical_loop`},
		{"wrong stage source", `lf_source="story_agent"`, `lf_source="technical_agent"`},
		{"unbounded node", `lf_limit="8"`, `lf_limit="0"`},
		{"nonresponse expression", `lf_field="response"`, `lf_field="owner.approved"`},
		{"set effect cannot promote", `set_draft -> failed [lf_on="uncertain"]`, `set_draft -> promoted [lf_on="uncertain"]`},
		{"set agent failure cannot stop silently", `set_agent -> failed [lf_on="failed"]`, `set_agent -> stopped [lf_on="failed"]`},
		{"set question cannot continue", `set_ask -> waiting`, `set_ask -> story_loop`},
		{"set effect needs an owner gate", `set_draft -> set_gate [lf_on="applied"]`, `set_draft -> set_review [lf_on="applied"]`},
		{"set review cannot reach an agent", `set_review -> waiting`, `set_review -> story_agent`},
		{"picking cannot promote", `story_pick -> waiting [lf_on="false"]`, `story_pick -> promoted [lf_on="false"]`},
		{"picking exhaustion cannot continue", `story_pick -> waiting [lf_on="false"]`, `story_pick -> story_review [lf_on="false"]`},
		{"creation cycle needs its bound", fmt.Sprintf(`story_pick [lf_action="lf_loop",lf_outcomes="true,false",lf_field="response",lf_condition="create",lf_limit="%d"]`, graphexec.PlanCreationBound), `story_pick [lf_action="lf_loop",lf_outcomes="true,false",lf_field="response",lf_condition="create",lf_limit="8"]`},
		{"cycle must pass through a loop", `story_review -> story_pick [lf_on="published"]`, `story_review -> story_question [lf_on="published"]`},
		{"bound has a ceiling", fmt.Sprintf(`lf_limit="%d"`, graphexec.PlanCreationBound), `lf_limit="1000"`},
		{"agent needs a role", `lf_agent_markdown="plan-set"`, `lf_agent_markdown="Plan Set"`},
		{"agent success reaches its own question", `set_agent -> set_question [lf_on="success"]`, `set_agent -> set_draft [lf_on="success"]`},
		{"gate reads no agent", `set_gate [lf_action="lf_if",lf_outcomes="true,false",lf_field="response",lf_condition="approved"`, `set_gate [lf_action="lf_if",lf_outcomes="true,false",lf_field="response",lf_condition="approved",lf_source="set_agent"`},
		{"start is a stage decision", `start="select_set"`, `start="set_loop"`},
		{"unknown action", `lf_action="lf_plan_story",lf_outcomes="applied,conflict,failed,uncertain",lf_source="set_agent"`, `lf_action="lf_plan_delete",lf_outcomes="applied,conflict,failed,uncertain",lf_source="set_agent"`},
		{"failure comment is required", `failed [lf_action="lf_comment",lf_outcomes="published",lf_kind="failure",lf_limit="1"]`, `failed [lf_action="lf_comment",lf_outcomes="published",lf_kind="review",lf_limit="1"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := planPolicyGraph()
			if !strings.Contains(base, tt.old) {
				t.Fatalf("fixture lacks %q", tt.old)
			}
			if err := checkPlan(t, strings.Replace(base, tt.old, tt.next, 1)); err == nil {
				t.Fatal("unsafe structure accepted")
			}
		})
	}
	t.Run("declared structure is accepted", func(t *testing.T) {
		if err := checkPlan(t, planPolicyGraph()); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("exhaustion of every loop stops without promotion", func(t *testing.T) {
		p, err := graphexec.ParseProfile([]byte(planPolicyGraph()))
		if err != nil {
			t.Fatal(err)
		}
		for _, stage := range []string{"set", "story", "technical", "acceptance"} {
			next, ok := graphexec.Decide(p, stage+"_loop", "false")
			if !ok || next != "stopped" {
				t.Fatalf("%s loop exhaustion reaches %q", stage, next)
			}
		}
		next, ok := graphexec.Decide(p, "story_pick", "false")
		if !ok || next != "waiting" {
			t.Fatalf("a finished creation cycle reaches %q, want the owner waits", next)
		}
	})
}

// The checker validates what the definition declares, not a Go-side node list.
func TestPlanControl_PinsNoNodeSet(t *testing.T) {
	t.Parallel()
	if err := checkPlan(t, minimalPlanGraph()); err != nil {
		t.Fatalf("a valid graph with one stage was refused: %v", err)
	}
	extended := strings.Replace(planPolicyGraph(), `waiting [lf_terminal="true"];`, `waiting [lf_terminal="true"];parked [lf_terminal="true"];`, 1)
	extended = strings.Replace(extended, `select_acceptance -> stopped`, `select_acceptance -> parked`, 1)
	if err := checkPlan(t, extended); err != nil {
		t.Fatalf("a graph declaring another terminal was refused: %v", err)
	}
}

func TestPlanControl_CreationBoundCoversAFullSet(t *testing.T) {
	t.Parallel()
	if graphexec.PlanCreationBound <= planbrief.MaxPlanStorySetEntries {
		t.Fatalf("creation bound %d cannot cover %d stories", graphexec.PlanCreationBound, planbrief.MaxPlanStorySetEntries)
	}
}
