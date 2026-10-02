package turn

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// readOnlyClaudeTools is the complete Claude tool surface of a read-only agent
// node. It can only narrow the operator's resolved profile, never widen it.
var readOnlyClaudeTools = []string{"Read", "Glob", "Grep"}

// applyNodeAccess maps the access level an agent node declares (lf_access) to
// the narrowed tool profile that node runs under. A node that declares none gets
// the policy's default; a policy with no default leaves the resolved profile
// untouched. The DOT names only a level: which tools it means is decided here,
// under the daemon's ceiling, and never by graph or agent text.
func applyNodeAccess(policy WorkflowPolicy, node graphexec.Node, req Request) (Request, error) {
	access := nodeAccessLevel(policy, node)
	switch access {
	case "":
		return req, nil
	case "write":
		return applyWriteAccess(node, req)
	case "read":
	default:
		return req, fmt.Errorf("node %q declares unsupported access %q", node.Name, access)
	}
	switch req.Agent {
	case agent.Kind(store.AgentCodex):
		req.CodexSandbox = "read-only"
	case agent.Kind(store.AgentClaude):
		tools, err := narrowTools(req.Tools)
		if err != nil {
			return req, fmt.Errorf("node %q read access: %w", node.Name, err)
		}
		allowed := make([]string, 0, len(req.AllowedTools))
		for _, tool := range req.AllowedTools {
			if slices.Contains(readOnlyClaudeTools, tool) {
				allowed = append(allowed, tool)
			}
		}
		req.Tools, req.AllowedTools = tools, allowed
	default:
		return req, fmt.Errorf("node %q read access is undefined for agent %q", node.Name, req.Agent)
	}
	return req, nil
}

// nodeAccessLevel is the access level an agent node runs under: what its graph
// declares, or the policy's default when it declares none.
func nodeAccessLevel(policy WorkflowPolicy, node graphexec.Node) string {
	if access := node.Attrs[graphexec.AccessAttribute]; access != "" {
		return access
	}
	return policy.DefaultAccess
}

// narrowTools intersects the resolved tools with the read-only surface. An
// unset list means the operator resolved no explicit surface, which the
// read-only surface is strictly inside. A non-empty list with nothing in common
// is refused rather than silently widened.
func narrowTools(resolved []string) ([]string, error) {
	if len(resolved) == 0 {
		return slices.Clone(readOnlyClaudeTools), nil
	}
	out := make([]string, 0, len(readOnlyClaudeTools))
	for _, tool := range readOnlyClaudeTools {
		if slices.Contains(resolved, tool) {
			out = append(out, tool)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the resolved profile grants none of %v", readOnlyClaudeTools)
	}
	return out, nil
}

// applyWriteAccess maps write access onto the tool surface a Spike fixer gets:
// it edits files, and has no shell, delegation or network. The set is the
// existing role mapping, not a copy. The result is capped by the mode's ceiling: a
// resolved profile that grants none of the editing tools, or a read-only Codex
// sandbox, refuses the node instead of widening the run.
func applyWriteAccess(node graphexec.Node, req Request) (Request, error) {
	switch req.Agent {
	case agent.Kind(store.AgentCodex):
		if req.CodexSandbox == "read-only" {
			return req, fmt.Errorf("node %q write access exceeds the read-only mode ceiling", node.Name)
		}
		req.CodexSandbox = "workspace-write"
	case agent.Kind(store.AgentClaude):
		fixer := config.BugRoleClaudeTools(config.SpikeFixerProfile)
		tools := slices.Clone(fixer)
		if len(req.Tools) > 0 {
			tools = tools[:0]
			for _, tool := range fixer {
				if slices.Contains(req.Tools, tool) {
					tools = append(tools, tool)
				}
			}
		}
		if !slices.ContainsFunc(tools, func(tool string) bool { return !slices.Contains(readOnlyClaudeTools, tool) }) {
			return req, fmt.Errorf("node %q write access exceeds the mode ceiling: the resolved profile grants no editing tool", node.Name)
		}
		allowed := make([]string, 0, len(req.AllowedTools))
		for _, rule := range req.AllowedTools {
			name, _, _ := strings.Cut(rule, "(")
			if slices.Contains(tools, name) {
				allowed = append(allowed, rule)
			}
		}
		req.Tools, req.AllowedTools = tools, allowed
	default:
		return req, fmt.Errorf("node %q write access is undefined for agent %q", node.Name, req.Agent)
	}
	req.nodeWrite = true
	return req, nil
}
