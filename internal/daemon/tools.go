package daemon

import (
	"slices"
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
	// Attachments is the agent-agnostic ingestion permission selected by mode.
	Attachments bool
}

// resolveWorkflowTools preserves mode profiles only for a matching mode-backed
// workflow. Standalone Bug needs factory role-specific bindings supplied by its
// analyst/fixer increments; a label or an operator profile named bug cannot
// substitute a mode profile or create that authority.
func (d *Daemon) resolveWorkflowTools(workflow agent.Workflow, mode agent.Mode, kind agent.Kind) (toolDecision, bool) {
	resolved, err := agent.ResolveWorkflow(workflow, mode)
	if err != nil || resolved == agent.WorkflowBug {
		return toolDecision{}, false
	}
	mapped, ok := resolved.Mode()
	if !ok {
		return toolDecision{}, false
	}
	return d.resolveTools(mapped, kind)
}

// resolveTools selects permissions from the already-resolved workflow mode and
// coding agent. Spike borrows no other mode's authority: its graph roles have
// their own explicit bindings.
func (d *Daemon) resolveTools(mode agent.Mode, kind agent.Kind) (toolDecision, bool) {
	if mode == agent.ModeSpike {
		return d.resolveSpikeTools(kind)
	}
	profileName := string(mode)
	if profileName == "" || !mode.Valid() {
		return toolDecision{}, false
	}
	profile, ok := d.toolProfiles[profileName]
	if !ok || !profile.HasStanzaFor(string(kind)) {
		return toolDecision{}, false
	}
	decision := toolDecision{Profile: profileName, Attachments: profile.Attachments}
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

// resolveSpikeTools builds the Spike mode ceiling from the two explicit graph
// role bindings. The fixer grant is the ceiling each node is narrowed from; the
// investigator grant must not exceed it, and every Claude tool must sit inside
// the operator's tool ceiling. A missing, attachment-granting or inconsistent
// binding refuses the mode rather than falling back to another profile.
func (d *Daemon) resolveSpikeTools(kind agent.Kind) (toolDecision, bool) {
	investigator, ok := d.toolProfiles[config.SpikeInvestigatorProfile]
	if !ok || !investigator.HasStanzaFor(string(kind)) || investigator.Attachments {
		return toolDecision{}, false
	}
	fixer, ok := d.toolProfiles[config.SpikeFixerProfile]
	if !ok || !fixer.HasStanzaFor(string(kind)) || fixer.Attachments {
		return toolDecision{}, false
	}
	decision := toolDecision{Profile: string(agent.ModeSpike)}
	switch string(kind) {
	case string(config.KindClaude):
		if len(fixer.Claude.Tools) == 0 || len(investigator.Claude.Tools) == 0 {
			return toolDecision{}, false
		}
		for _, tool := range fixer.Claude.Tools {
			if len(d.toolCeiling) > 0 && !slices.Contains(d.toolCeiling, tool) {
				return toolDecision{}, false
			}
		}
		for _, tool := range investigator.Claude.Tools {
			if !slices.Contains(fixer.Claude.Tools, tool) {
				return toolDecision{}, false
			}
		}
		decision.Tools = slices.Clone(fixer.Claude.Tools)
		decision.AllowedTools = slices.Clone(fixer.Claude.AllowedTools)
	case string(config.KindCodex):
		if investigator.Codex.Sandbox != "read-only" || fixer.Codex.Sandbox != "workspace-write" {
			return toolDecision{}, false
		}
		decision.Sandbox = fixer.Codex.Sandbox
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
