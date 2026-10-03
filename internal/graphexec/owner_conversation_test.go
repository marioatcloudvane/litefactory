package graphexec

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

const conversationGraph = `digraph renamed {
 graph [team="builtin",workflow="bug",profile_version="2",start="talk"];
 talk [lf_action="lf_loop",lf_contract="owner_conversation",lf_outcomes="analyse,approved,blocked",lf_limit="64",lf_park="owner",lf_stop="blocked,failed,invalid,exhausted"];
 investigate [lf_action="lf_agent",lf_agent_markdown="bug-analyst",lf_model_tier="flagship",lf_access="read",lf_response="result",lf_outcomes="ask,done,blocked",lf_limit="8",lf_park="owner",lf_stop="blocked,failed,invalid,exhausted"];
 repair [lf_action="lf_agent",lf_agent_markdown="bug-fixer",lf_response="result",lf_outcomes="succeeded,reshape,blocked",lf_limit="3",lf_park="owner",lf_stop="blocked,failed,invalid,exhausted"];
 deliver [lf_action="lf_bug_finalize",lf_owner_continuation="required",lf_outcomes="completed,revalidate,stale,blocked",lf_limit="16",lf_park="owner",lf_stop="blocked,failed,invalid,exhausted,stale"];
 finish [lf_terminal="true"];
 talk -> investigate [lf_on="analyse"]; talk -> repair [lf_on="approved"];
 investigate -> talk [lf_on="ask"]; investigate -> talk [lf_on="done"];
 repair -> talk [lf_on="reshape"]; repair -> deliver [lf_on="succeeded"];
 deliver -> repair [lf_on="revalidate"]; deliver -> finish [lf_on="completed"];
}`

func TestOwnerConversationDeclaredRoutesAndNegativeTopology(t *testing.T) {
	registry := Registry{Policy: ValidateOwnerConversationGraph}
	for _, s := range []ActionSpec{OwnerConversationSpec(), BugFinalizationSpec(), {Name: "lf_agent", Workflow: agent.WorkflowBug, Outcomes: OwnerStopAgentOutcomes(), Attributes: AgentAttributes()}} {
		if e := registry.Register(s); e != nil {
			t.Fatal(e)
		}
	}
	profile, e := ParseProfile([]byte(conversationGraph))
	if e != nil {
		t.Fatal(e)
	}
	if e = Validate(profile, registry); e != nil {
		t.Fatalf("renamed graph refused: %v", e)
	}
	for name, rewrite := range map[string]func(string) string{
		"approval bypass": func(s string) string {
			return strings.Replace(s, "talk -> investigate [lf_on=\"analyse\"]", "talk -> repair [lf_on=\"analyse\"]", 1)
		},
		"automatic repair retry": func(s string) string {
			return strings.Replace(s, "repair -> talk [lf_on=\"reshape\"]", "repair -> repair [lf_on=\"reshape\"]", 1)
		},
		"unbounded discussion": func(s string) string { return strings.Replace(s, "lf_limit=\"64\"", "lf_limit=\"0\"", 1) },
		"wrong model": func(s string) string {
			return strings.Replace(s, "lf_model_tier=\"flagship\"", "lf_model_tier=\"fast\"", 1)
		},
		"analyst mutation":      func(s string) string { return strings.Replace(s, "lf_access=\"read\"", "lf_access=\"write\"", 1) },
		"unguarded stale retry": func(s string) string { return strings.Replace(s, "lf_owner_continuation=\"required\"", "", 1) },
	} {
		t.Run(name, func(t *testing.T) {
			p, e := ParseProfile([]byte(rewrite(conversationGraph)))
			if e == nil && Validate(p, registry) == nil {
				t.Fatal("unsafe graph admitted")
			}
		})
	}
}
