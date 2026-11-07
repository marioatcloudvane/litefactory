package graphexec

import "github.com/cloudvane/litefactory/internal/agent"

// OwnerConversationSpec opts one loop into a factory-authenticated conversation.
// Ordinary comparison loops keep their existing specifications and semantics.
func OwnerConversationSpec() ActionSpec {
	attrs := OwnerStopAttributes()
	attrs["lf_contract"] = AttributeSpec{Required: true, Values: []string{"owner_conversation"}}
	return ActionSpec{Name: ResearchLoopAction, Workflow: agent.WorkflowBug, Outcomes: []string{"analyse", "approved", "blocked"}, Attributes: attrs}
}

// BugFinalizationSpec describes the downstream capability without providing it.
// Admission still requires its concrete factory implementation.
func BugFinalizationSpec() ActionSpec {
	attrs := OwnerStopAttributes()
	attrs["lf_owner_continuation"] = AttributeSpec{Required: true, Values: []string{"required"}}
	return ActionSpec{Name: "lf_bug_finalize", Workflow: agent.WorkflowBug, Outcomes: []string{"completed", "revalidate", "stale", "blocked"}, Attributes: attrs}
}

// BugFixerSyncGate finds the explicit retrying prerequisite in a captured Bug
// profile. Legacy success-only synchronization keeps its established behavior.
func BugFixerSyncGate(p *Profile) (Node, bool) {
	if p == nil || p.Workflow != agent.WorkflowBug || p.Mode != "" {
		return Node{}, false
	}
	var gate Node
	found := false
	for _, n := range p.Nodes {
		if n.Action != SyncBaseAction {
			continue
		}
		next, ok := Decide(p, n.Name, "retry")
		if !ok || next != n.Name {
			continue
		}
		if found {
			return Node{}, false
		}
		gate, found = n, true
	}
	return gate, found
}
