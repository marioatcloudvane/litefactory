package daemon

import (
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/report"
)

const toolLabelPrefix = "lf:tools="

type toolDecision struct {
	Refusal      report.ReplyKind
	Profile      string
	Tools        []string
	AllowedTools []string
	Sandbox      string
}

func (d toolDecision) refused() bool { return d.Refusal != report.ReplyInvalid }

// resolveTools selects permissions from the already-resolved workflow mode and
// coding agent. Spike remains its own mode while borrowing implement authority.
func (d *Daemon) resolveTools(mode agent.Mode, kind agent.Kind) (toolDecision, bool) {
	profileName := string(mode)
	if mode == agent.ModeSpike {
		profileName = string(agent.ModeImplement)
	}
	if profileName == "" || !mode.Valid() {
		return toolDecision{}, false
	}
	profile, ok := d.toolProfiles[profileName]
	if !ok || !profile.HasStanzaFor(string(kind)) {
		return toolDecision{}, false
	}
	decision := toolDecision{Profile: profileName}
	switch string(kind) {
	case string(config.KindClaude):
		decision.Tools = append([]string(nil), profile.Claude.Tools...)
		decision.AllowedTools = append([]string(nil), profile.Claude.AllowedTools...)
	case string(config.KindCodex):
		decision.Sandbox = profile.Codex.Sandbox
	default:
		return toolDecision{}, false
	}
	return decision, true
}

// legacyToolsLabel reports only whether an obsolete permission label exists.
// Its value is deliberately neither returned nor logged.
func legacyToolsLabel(labels []string) bool {
	for _, label := range labels {
		if len(label) >= len(toolLabelPrefix) &&
			strings.EqualFold(label[:len(toolLabelPrefix)], toolLabelPrefix) {
			return true
		}
	}
	return false
}

// cloneToolProfiles gives the daemon ownership of all nested permission slices.
func cloneToolProfiles(profiles map[string]config.ToolProfile) map[string]config.ToolProfile {
	cloned := make(map[string]config.ToolProfile, len(profiles))
	for name, profile := range profiles {
		if profile.Claude != nil {
			claude := *profile.Claude
			claude.Tools = append([]string(nil), profile.Claude.Tools...)
			claude.AllowedTools = append([]string(nil), profile.Claude.AllowedTools...)
			profile.Claude = &claude
		}
		if profile.Codex != nil {
			codex := *profile.Codex
			profile.Codex = &codex
		}
		cloned[name] = profile
	}
	return cloned
}
