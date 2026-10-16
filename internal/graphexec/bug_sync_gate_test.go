package graphexec

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestBugRetryingSyncGateRequiresEveryRepairEntry(t *testing.T) {
	registry := Registry{Policy: ValidateOwnerConversationGraph}
	for _, spec := range []ActionSpec{OwnerConversationSpec(), BugFinalizationSpec(), SyncBaseWorkflowSpec(agent.WorkflowBug, ""), {Name: "lf_agent", Workflow: agent.WorkflowBug, Outcomes: OwnerStopAgentOutcomes(), Attributes: AgentAttributes()}} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	graph := strings.Replace(conversationGraph, " talk -> investigate", ` refresh [lf_action="lf_sync_base",lf_outcomes="synced,current,conflict,retry",lf_limit="8",lf_park="owner",lf_stop="conflict,failed,invalid,exhausted"];
 refresh -> repair [lf_on="synced"]; refresh -> repair [lf_on="current"]; refresh -> refresh [lf_on="retry"];
 talk -> investigate`, 1)
	graph = strings.Replace(graph, `talk -> repair [lf_on="approved"]`, `talk -> refresh [lf_on="approved"]`, 1)
	graph = strings.Replace(graph, `deliver -> repair [lf_on="revalidate"]`, `deliver -> refresh [lf_on="revalidate"]`, 1)
	cases := map[string]string{
		"safe":                graph,
		"approval bypass":     strings.Replace(graph, `talk -> refresh [lf_on="approved"]`, `talk -> repair [lf_on="approved"]`, 1),
		"revalidation bypass": strings.Replace(graph, `deliver -> refresh [lf_on="revalidate"]`, `deliver -> repair [lf_on="revalidate"]`, 1),
		"retry repair":        strings.Replace(graph, `refresh -> refresh [lf_on="retry"]`, `refresh -> repair [lf_on="retry"]`, 1),
		"conflict repair":     strings.Replace(graph, `refresh -> refresh [lf_on="retry"]`, `refresh -> refresh [lf_on="retry"]; refresh -> repair [lf_on="conflict"]`, 1),
		"success mismatch":    strings.Replace(graph, `refresh -> repair [lf_on="current"]`, `refresh -> investigate [lf_on="current"]`, 1),
		"unbounded":           strings.Replace(graph, `lf_limit="8",lf_park="owner",lf_stop="conflict`, `lf_limit="0",lf_park="owner",lf_stop="conflict`, 1),
	}
	for name, dot := range cases {
		t.Run(name, func(t *testing.T) {
			profile, err := ParseProfile([]byte(dot))
			if err != nil {
				t.Fatal(err)
			}
			err = Validate(profile, registry)
			if name == "safe" {
				if err != nil {
					t.Fatal(err)
				}
				gate, ok := BugFixerSyncGate(profile)
				if !ok || gate.Name != "refresh" {
					t.Fatal("renamed gate not recognized")
				}
			} else if err == nil {
				t.Fatal("unsafe gate admitted")
			}
		})
	}
}
