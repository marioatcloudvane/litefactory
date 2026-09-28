package agent

import "slices"

// ValidateBugAnalyst guards the provider-neutral capability boundary. Adapter
// validation additionally enforces read-only native tools or filesystem access.
// No other Bug role is admitted through the analyst surface.
func (req Request) ValidateBugAnalyst() error {
	if req.Workflow != WorkflowBug {
		return nil
	}
	if req.Mode != "" || req.Invocation != InvocationResearchNode || (req.Permissions.Profile != "bug-analyst" && req.Permissions.Profile != "bug-fixer") {
		return invalidRequest("Bug analyst requires its factory role, graph invocation and no effective mode")
	}
	if req.MCPLaunch == nil {
		return invalidRequest("Bug analyst requires attempt-scoped shared result MCP access")
	}
	if err := req.MCPLaunch.Validate(); err != nil {
		return err
	}
	tools := req.MCPLaunch.PermittedTools()
	for _, required := range []string{"communication_preflight", "communication_submit", "communication_receipt"} {
		if !slices.Contains(tools, required) {
			return invalidRequest("Bug analyst is missing scoped result operation %q", required)
		}
	}
	// The two self-check tools are optional and, when present, permitted only
	// for the bug-fixer profile; the analyst's surface stays exactly the three
	// shared result operations above.
	allowed := []string{"communication_preflight", "communication_submit", "communication_receipt"}
	if req.Permissions.Profile == "bug-fixer" {
		allowed = append(allowed, "bug_self_check_start", "bug_self_check_status")
	}
	for _, tool := range tools {
		if !slices.Contains(allowed, tool) {
			return invalidRequest("Bug analyst cannot receive capability %q", tool)
		}
	}
	return nil
}
