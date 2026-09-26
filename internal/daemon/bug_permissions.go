package daemon

import (
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/turn"
)

// bugRolePermissions captures operator configuration once. Neither an issue
// label nor the mode profile map can substitute authority for a missing role.
func bugRolePermissions(profiles map[string]config.ToolProfile, ceiling []string) func(agent.Kind, string) (turn.BugRolePermissions, error) {
	profiles = cloneToolProfiles(profiles)
	ceiling = slices.Clone(ceiling)
	return func(kind agent.Kind, role string) (turn.BugRolePermissions, error) {
		var out turn.BugRolePermissions
		if role != "bug-analyst" && role != "bug-fixer" {
			return out, fmt.Errorf("unknown Bug role %q", role)
		}
		profile, ok := profiles[role]
		if !ok || !profile.HasStanzaFor(string(kind)) || profile.Attachments {
			return out, fmt.Errorf("missing explicit %s permission profile for %s", role, kind)
		}
		switch kind {
		case agent.Kind(config.KindCodex):
			want := "read-only"
			if role == "bug-fixer" {
				want = "workspace-write"
			}
			if profile.Codex.Sandbox != want {
				return out, fmt.Errorf("%s requires %s sandbox", role, want)
			}
			out.Sandbox = want
		case agent.Kind(config.KindClaude):
			if role != "bug-analyst" || len(profile.Claude.Tools) == 0 {
				return out, fmt.Errorf("%s enforcement is unavailable for Claude", role)
			}
			for _, tool := range append(slices.Clone(profile.Claude.Tools), profile.Claude.AllowedTools...) {
				if (tool != "Read" && tool != "Glob" && tool != "Grep") || !slices.Contains(ceiling, tool) {
					return out, fmt.Errorf("%s tool is outside the read-only operator ceiling", role)
				}
			}
			out.Tools = slices.Clone(profile.Claude.Tools)
			out.AllowedTools = slices.Clone(profile.Claude.AllowedTools)
		default:
			return out, fmt.Errorf("unsupported Bug agent %s", kind)
		}
		return out, nil
	}
}
