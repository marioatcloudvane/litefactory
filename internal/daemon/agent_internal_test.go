package daemon

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/turn"
)

// testAgents is the daemon's registered coding-agent-tool set every
// resolveAgent test in this file reads: one implementation, "claude", which
// is every [agent.Kind] this tree can run today — internal/codex has not
// landed. Membership is all resolveAgent asks of [Daemon.agents], so a nil
// Runner value is enough to populate it.
func testAgents() turn.RunnerSet {
	return turn.RunnerSet{agent.Kind("claude"): nil}
}

// TestResolveAgent_DecisionTable pins the resolver's decision table (AC-2.1,
// AC-2.2): every shape of label a maintainer can put on an issue, and exactly
// one of the outcomes resolveAgent may reach for it. It mirrors
// TestResolveTools_DecisionTable rung for rung, because resolveAgent reuses
// resolveTools's ladder byte for byte with one substitution — the last rung
// checks [Daemon.agents] instead of a tool profile map.
//
// It calls resolveAgent directly, on a *Daemon built with nothing but the
// registered agent set, because the decision is a pure function of the
// labels and that set — nothing else about the daemon's wiring is relevant
// to it.
func TestResolveAgent_DecisionTable(t *testing.T) {
	d := &Daemon{agents: testAgents()}

	tests := []struct {
		name         string
		labels       []string
		wantRefusal  report.ReplyKind
		wantAgent    string
		wantMiscased bool
	}{
		{
			name:      "one valid label/resolves the agent",
			labels:    []string{"Coding Agent: Claude"},
			wantAgent: "claude",
		},
		{
			name:      "an unrelated label beside a valid one/resolves, ignoring the unrelated label",
			labels:    []string{"bug", "Coding Agent: Claude"},
			wantAgent: "claude",
		},
		{
			name:         "mis-cased prefix, lowercase value/malformed, prefix flagged",
			labels:       []string{"coding agent: Claude"},
			wantRefusal:  report.ReplyMalformedAgentLabel,
			wantMiscased: true,
		},
		{
			name:         "fully uppercase prefix/malformed, prefix flagged",
			labels:       []string{"CODING AGENT: Claude"},
			wantRefusal:  report.ReplyMalformedAgentLabel,
			wantMiscased: true,
		},
		{
			name:         "mis-cased prefix over an unregistered value/still the prefix failure, not unknown",
			labels:       []string{"coding agent: Codex"},
			wantRefusal:  report.ReplyMalformedAgentLabel,
			wantMiscased: true,
		},
		{
			name:        "byte-exact prefix, uppercase value/malformed on the value shape, prefix not flagged",
			labels:      []string{"Coding Agent: claude"},
			wantRefusal: report.ReplyMalformedAgentLabel,
		},
		{
			name:        "empty value/malformed, prefix not flagged",
			labels:      []string{"Coding Agent: "},
			wantRefusal: report.ReplyMalformedAgentLabel,
		},
		{
			name:        "value with a space/malformed, prefix not flagged",
			labels:      []string{"Coding Agent: bad name"},
			wantRefusal: report.ReplyMalformedAgentLabel,
		},
		{
			name:        "well-formed value naming no registered implementation/unknown, names the agent",
			labels:      []string{"Coding Agent: Codex"},
			wantRefusal: report.ReplyUnknownAgent,
			wantAgent:   "codex",
		},
		{
			name: "no labels at all/the zero value: no refusal, no agent",
		},
		{
			name:   "labels present, none of them an agent label/the zero value",
			labels: []string{"bug", "lf:reply", "priority:high", "lf:agent=codex"},
		},
		{
			name:   "the wrong key, lf:agnt=/too short to carry the prefix, ignored entirely",
			labels: []string{"Coding Agnt: Claude"},
		},
		{
			name:   "no equals sign at all/too short to carry the prefix, ignored entirely",
			labels: []string{"Coding Agent"},
		},
		{
			name:        "two valid labels, same casing/ambiguous, neither is chosen",
			labels:      []string{"Coding Agent: Claude", "Coding Agent: Codex"},
			wantRefusal: report.ReplyAmbiguousAgentLabels,
		},
		{
			name:        "two labels, mixed casing and reversed order/ambiguous regardless of order or casing",
			labels:      []string{"coding agent: Codex", "Coding Agent: Claude"},
			wantRefusal: report.ReplyAmbiguousAgentLabels,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision := d.resolveAgent(tc.labels)

			if decision.Refusal != tc.wantRefusal {
				t.Errorf("Refusal = %s, want %s", decision.Refusal, tc.wantRefusal)
			}
			if decision.refused() != (tc.wantRefusal != report.ReplyInvalid) {
				t.Errorf("refused() = %t, disagrees with Refusal = %s", decision.refused(), decision.Refusal)
			}
			if decision.MiscasedPrefix != tc.wantMiscased {
				t.Errorf("MiscasedPrefix = %t, want %t", decision.MiscasedPrefix, tc.wantMiscased)
			}
			// S-9: MiscasedPrefix is set on exactly the prefix-caused malformed
			// refusal, never on the value-caused one and never on ambiguous.
			if tc.wantMiscased && tc.wantRefusal != report.ReplyMalformedAgentLabel {
				t.Fatalf("test table error: %s wants MiscasedPrefix without wanting the malformed refusal", tc.name)
			}
			if decision.Agent != tc.wantAgent {
				t.Errorf("Agent = %q, want %q", decision.Agent, tc.wantAgent)
			}

			// AC-2.2: Agent stays empty on the ambiguous and malformed rungs —
			// there is no field for label text to travel in on those paths
			// (S-9). It is populated only by a resolution and by the unknown
			// refusal, which is allowed to name what it could not find.
			switch tc.wantRefusal {
			case report.ReplyAmbiguousAgentLabels, report.ReplyMalformedAgentLabel:
				if decision.Agent != "" {
					t.Errorf("Agent = %q, want empty: this decision must not name a label", decision.Agent)
				}
			}
		})
	}
}

// TestResolveAgent_NoRunnersRegisteredRefusesAnyLabel is the agent-selection
// counterpart of resolveTools's D-5: a daemon with no coding-agent-tool
// implementation registered at all treats every well-formed lf:agent= label
// as unknown rather than as a no-op, and still leaves an unlabelled issue
// alone.
func TestResolveAgent_NoRunnersRegisteredRefusesAnyLabel(t *testing.T) {
	d := &Daemon{}

	decision := d.resolveAgent([]string{"Coding Agent: Claude"})
	if decision.Refusal != report.ReplyUnknownAgent || decision.Agent != "claude" {
		t.Errorf("decision = %+v, want an unknown-agent refusal naming %q", decision, "claude")
	}

	if d.resolveAgent(nil).refused() {
		t.Error("no labels at all must resolve to the zero value even with no agents registered")
	}
}
