package daemon

import (
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/turn"
)

// TestLabelReaderAgreement keeps label setup's ReaderAcceptsLabel in step with
// the daemon's real label readers.
func TestLabelReaderAgreement(t *testing.T) {
	d := &Daemon{agents: turn.RunnerSet{agent.Kind("claude"): nil, agent.Kind("codex"): nil}}
	labels := []string{
		"Mode: Plan", "mode: plan", "Mode: Bogus", "Mode: Research", "MODE: Plan",
		"Coding Agent: Claude", "Coding Agent: CLAUDE", "Coding Agent: claude", "coding agent: Claude",
		"Coding Agent: Codex", "Coding Agent: Other", "Coding Agent: ",
		"bug", "Bug", "BUG", "mode: plan ",
	}
	for _, w := range WantedLabelsForTest() {
		for _, label := range labels {
			var daemonAccepts bool
			switch w.Family {
			case agent.LabelFamilyMode:
				mode, ok, err := resolveModeLabel([]string{label})
				daemonAccepts = err == nil && ok && mode.Label() == w.Name
			case agent.LabelFamilyBug:
				daemonAccepts = slices.Contains([]string{label}, "bug")
			case agent.LabelFamilyAgent:
				dec := d.resolveAgent([]string{label})
				daemonAccepts = dec.Refusal == report.ReplyInvalid &&
					agent.CodingAgentLabelPrefix+capitalise(dec.Agent) == w.Name
			}
			got, reason := agent.ReaderAcceptsLabel(w, label)
			// Case-insensitive value match is intended: only the daemon's own
			// verdict on "does this label select the wanted thing" is compared.
			if got != daemonAccepts {
				t.Errorf("wanted %q existing %q: ReaderAcceptsLabel=%v (%s), daemon=%v", w.Name, label, got, reason, daemonAccepts)
			}
		}
	}
}

func WantedLabelsForTest() []agent.WantedLabel {
	return agent.WantedLabels([]string{"claude", "codex"})
}

func capitalise(s string) string {
	if s == "" {
		return ""
	}
	return string(s[0]-32) + s[1:]
}
