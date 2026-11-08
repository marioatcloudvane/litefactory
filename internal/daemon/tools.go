package daemon

import (
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/report"
)

const toolLabelPrefix = "lf:tools="

type toolDecision struct {
	Refusal report.ReplyKind
	Profile string
	// Tools is the Claude tool surface: always [config.AgentTools].
	Tools []string
	// AllowedTools is retired and stays empty; the runner allows the whole
	// surface.
	AllowedTools []string
	// Sandbox is the sandbox the mode's profile selects, for every agent.
	Sandbox string
	// Attachments is the agent-agnostic ingestion permission selected by mode.
	Attachments bool
}

// resolveWorkflowTools resolves the run policy of a matching workflow and mode.
func (d *Daemon) resolveWorkflowTools(workflow agent.Workflow, mode agent.Mode, kind agent.Kind) (toolDecision, bool) {
	resolved, err := agent.ResolveWorkflow(workflow, mode)
	if err != nil {
		return toolDecision{}, false
	}
	mapped, ok := resolved.Mode()
	if !ok {
		return toolDecision{}, false
	}
	return d.resolveTools(mapped, kind)
}

// resolveTools selects the run policy of the resolved workflow mode: the full
// tool set inside the sandbox the mode's profile chooses. Spike runs under its
// fixer's sandbox; its investigator nodes narrow to read-only per node.
func (d *Daemon) resolveTools(mode agent.Mode, kind agent.Kind) (toolDecision, bool) {
	if !mode.Valid() {
		return toolDecision{}, false
	}
	name := string(mode)
	switch mode {
	case agent.ModeSpike:
		name = config.SpikeFixerProfile
	case agent.ModeBug:
		name = config.BugProfile
	}
	profile, ok := d.toolProfiles.Resolve(name)
	if !ok {
		return toolDecision{}, false
	}
	decision := toolDecision{Profile: string(mode), Sandbox: profile.Sandbox, Attachments: profile.Attachments && mode != agent.ModeSpike}
	switch string(kind) {
	case string(config.KindClaude):
		decision.Tools = config.AgentTools()
	case string(config.KindCodex):
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
