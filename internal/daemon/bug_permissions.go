package daemon

import (
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/turn"
)

// bugRolePermissions resolves a graph role's run policy from the operator's
// profiles once: the full tool set inside the role's sandbox. The Bug analyst
// and fixer and the Validate repair agent resolve this way.
func bugRolePermissions(profiles config.ToolProfiles) func(agent.Kind, string) (turn.BugRolePermissions, error) {
	profiles = cloneToolProfiles(profiles)
	return func(kind agent.Kind, role string) (turn.BugRolePermissions, error) {
		var out turn.BugRolePermissions
		if role != "bug-analyst" && role != "bug-fixer" && role != config.ValidateRepairProfile {
			return out, fmt.Errorf("unknown Bug role %q", role)
		}
		profile, _ := profiles.Resolve(role)
		out.Sandbox = profile.Sandbox
		switch kind {
		case agent.Kind(config.KindCodex):
		case agent.Kind(config.KindClaude):
			out.Tools = config.AgentTools()
		default:
			return out, fmt.Errorf("unsupported Bug agent %s", kind)
		}
		return out, nil
	}
}

// cloneToolProfiles gives the daemon its own copy of the operator's profiles.
func cloneToolProfiles(profiles config.ToolProfiles) config.ToolProfiles {
	cloned := config.ToolProfiles{Permissions: make(map[string]config.ToolProfile, len(profiles.Permissions))}
	for name, profile := range profiles.Permissions {
		if profile.Codex != nil {
			codex := *profile.Codex
			profile.Codex = &codex
		}
		profile.Claude = nil
		cloned.Permissions[name] = profile
	}
	return cloned
}
