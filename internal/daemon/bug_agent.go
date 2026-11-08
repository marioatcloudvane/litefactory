package daemon

import (
	"github.com/cloudvane/litefactory/internal/agent"
)

// effectiveAgent picks the coding agent: the issue's agent label, then for a
// Bug the configured bug agent, then the default agent.
func (d *Daemon) effectiveAgent(t *task, workflow agent.Workflow) (agentDecision, string) {
	choice := d.resolveAgent(t.issue.Labels)
	if choice.refused() || choice.Agent != "" {
		return choice, "label"
	}
	if workflow == agent.WorkflowBug && d.bugAgent != "" {
		return agentDecision{Agent: string(d.bugAgent)}, "bug_agent"
	}
	return agentDecision{Agent: string(d.defaultAgent)}, "default"
}
