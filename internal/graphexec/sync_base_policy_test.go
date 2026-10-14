package graphexec

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"strings"
	"testing"
)

func TestBugExplicitSyncPreservesConversationAuthority(t *testing.T) {
	registry := Registry{Policy: ValidateOwnerConversationGraph}
	for _, spec := range []ActionSpec{OwnerConversationSpec(), BugFinalizationSpec(), SyncBaseWorkflowSpec(agent.WorkflowBug, ""), {Name: "lf_agent", Workflow: agent.WorkflowBug, Outcomes: OwnerStopAgentOutcomes(), Attributes: AgentAttributes()}} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	graph := strings.Replace(conversationGraph, " talk -> investigate", ` sync [lf_action="lf_sync_base",lf_outcomes="synced,current,conflict,retry",lf_limit="3",lf_park="owner",lf_stop="conflict,retry,failed,invalid,exhausted"];
 sync -> repair [lf_on="synced"]; sync -> repair [lf_on="current"];
 talk -> investigate`, 1)
	graph = strings.Replace(graph, `talk -> repair [lf_on="approved"]`, `talk -> sync [lf_on="approved"]`, 1)
	for name, text := range map[string]string{"approved sync": graph, "bypass": strings.Replace(graph, `talk -> investigate [lf_on="analyse"]`, `talk -> sync [lf_on="analyse"]`, 1), "retry agent": strings.Replace(graph, `sync -> repair [lf_on="current"]`, `sync -> repair [lf_on="current"]; sync -> repair [lf_on="retry"]`, 1), "success differs": strings.Replace(graph, `sync -> repair [lf_on="current"]`, `sync -> investigate [lf_on="current"]`, 1)} {
		t.Run(name, func(t *testing.T) {
			profile, err := ParseProfile([]byte(text))
			if err != nil {
				t.Fatal(err)
			}
			err = Validate(profile, registry)
			if name == "approved sync" && err != nil {
				t.Fatal(err)
			}
			if name != "approved sync" && err == nil {
				t.Fatal("unsafe authority route admitted")
			}
		})
	}
}

func TestImplementWorkerExplicitSyncSuccessAndStopPaths(t *testing.T) {
	graph := strings.Replace(implementTestDOT, ` budget [lf_action=`, ` worker_sync [lf_action="lf_sync_base",lf_outcomes="synced,current,conflict,retry",lf_limit="3"];
 worker_sync -> worker_agent [lf_on="synced"]; worker_sync -> worker_agent [lf_on="current"];
 worker_sync -> worker_parked [lf_on="conflict"]; worker_sync -> worker_parked [lf_on="retry"];
 budget [lf_action=`, 1)
	graph = strings.Replace(graph, `budget -> worker_agent [lf_on="run"]`, `budget -> worker_sync [lf_on="run"]`, 1)
	for name, text := range map[string]string{"safe": graph, "retry worker": strings.Replace(graph, `worker_sync -> worker_parked [lf_on="retry"]`, `worker_sync -> worker_agent [lf_on="retry"]`, 1), "different success": strings.Replace(graph, `worker_sync -> worker_agent [lf_on="current"]`, `worker_sync -> park_story [lf_on="current"]`, 1)} {
		t.Run(name, func(t *testing.T) {
			profile, err := ParseProfile([]byte(text))
			if err != nil {
				t.Fatal(err)
			}
			err = ValidateImplementGraph(profile)
			if name == "safe" && err != nil {
				t.Fatal(err)
			}
			if name != "safe" && err == nil {
				t.Fatal("unsafe sync route admitted")
			}
		})
	}
}

func TestGenericControlPreservesImplementCoordinatorResolverSync(t *testing.T) {
	profile, err := ParseProfile([]byte(implementSyncDOT(t)))
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateControlGraph(profile, ImplementControlPolicy()); err != nil {
		t.Fatalf("generic control rejected coordinator resolver sync: %v", err)
	}
	if err = ValidateImplementGraph(profile); err != nil {
		t.Fatalf("native Implement sync policy: %v", err)
	}
}
