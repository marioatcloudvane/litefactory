package daemon

import (
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/turn"
)

// rolePermissions resolves a graph role's run policy from the operator's
// profiles once: the full tool set inside the role's sandbox. The Validate
// repair agent resolves this way.
func rolePermissions(profiles config.ToolProfiles) func(agent.Kind, string) (turn.RolePermissions, error) {
	profiles = cloneToolProfiles(profiles)
	return func(kind agent.Kind, role string) (turn.RolePermissions, error) {
		var out turn.RolePermissions
		if role != config.ValidateRepairProfile {
			return out, fmt.Errorf("unknown graph role %q", role)
		}
		profile, _ := profiles.Resolve(role)
		out.Sandbox = profile.Sandbox
		switch kind {
		case agent.Kind(config.KindCodex):
		case agent.Kind(config.KindClaude):
			out.Tools = config.AgentTools()
		default:
			return out, fmt.Errorf("unsupported agent %s", kind)
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
