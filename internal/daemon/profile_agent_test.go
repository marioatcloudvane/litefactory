package daemon_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// This file covers T-7.5 (Add Codex as second-party coding-agent tool,
// US-7): stage 3 of the four-stage refusal precedence, where a resolved
// lf:tools= profile is narrowed to the agent a run actually addresses
// (AC-7.1, AC-7.2). Membership in [turn.RunnerSet] is all resolveAgent asks
// of it, so nil Runner values are enough — see [testDaemonAgents] in
// agent_test.go, which this file's helper mirrors with a second, "codex"
// entry so an lf:agent=codex label can resolve at all.

// testTwoAgentRunners registers both a claude and a codex Kind, which is
// what lets an lf:agent=codex label pass stage 1 (resolveAgent) so this
// file's tests can reach stage 3. Neither Runner is ever invoked: every test
// here is a refusal, or is asserted only against the turn.Request the fake
// executor received — nothing here needs a real coding-agent-tool process.
func testTwoAgentRunners() turn.RunnerSet {
	return turn.RunnerSet{
		agent.Kind(store.AgentClaude): nil,
		agent.Kind(store.AgentCodex):  nil,
	}
}

// claudeOnlyProfile carries a claude: stanza and no codex: stanza.
func claudeOnlyProfile() config.ToolProfile {
	return config.ToolProfile{
		Claude: &config.ToolProfileClaude{Tools: []string{"Read", "Grep"}},
	}
}

// codexOnlyProfile carries a codex: stanza and no claude: stanza.
func codexOnlyProfile() config.ToolProfile {
	return config.ToolProfile{
		Codex: &config.ToolProfileCodex{Sandbox: "read-only"},
	}
}

// bothStanzasProfile carries both, so a run may address either agent.
func bothStanzasProfile() config.ToolProfile {
	return config.ToolProfile{
		Claude: &config.ToolProfileClaude{Tools: []string{"Read", "Grep"}},
		Codex:  &config.ToolProfileCodex{Sandbox: "read-only"},
	}
}

// TestProfileWithOnlyClaudeStanzaRefusesACodexRoutedRun is AC-7.1: a profile
// with only a claude: stanza must refuse a run addressed to Codex, naming
// both the profile and the agent, spawning nothing and never deriving a
// Codex sandbox value from the Claude stanza it does have (AC-7.2, checked
// separately in internal/config's own HasStanzaFor tests — this test only
// pins the observable behaviour: refused, not silently widened).
func TestProfileWithOnlyClaudeStanzaRefusesACodexRoutedRun(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{
		Agents:       testTwoAgentRunners(),
		ToolProfiles: map[string]config.ToolProfile{"research": claudeOnlyProfile()},
	})
	f.plantCursor()
	f.issueWithLabels("Add a /health endpoint", "lf:tools=research", "Coding Agent: Codex")
	f.settle() // first sight

	f.forge.addComment(testIssue, 1101, owner(), "/spike add a health endpoint")
	f.settle()

	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("the executor ran %d turns, want none: a profile-for-agent refusal must be run-less", len(calls))
	}
	if runs := f.runs(); len(runs) != 0 {
		t.Errorf("%d run rows exist, want none", len(runs))
	}
	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 {
		t.Fatalf("posted %d comments, want 1", len(posted))
	}
	for _, want := range []string{"research", "codex"} {
		if !strings.Contains(posted[0], want) {
			t.Errorf("reply does not name %q:\n%s", want, posted[0])
		}
	}
	if !strings.HasPrefix(posted[0], turn.MarkerPrefix) {
		t.Errorf("reply does not carry the litefactory marker:\n%s", posted[0])
	}
	if got := f.watermark(); got != 1101 {
		t.Errorf("watermark = %d, want 1101: a refusal still consumes the comment that asked for it", got)
	}
}

// TestProfileWithOnlyCodexStanzaRefusesTheDefaultClaudeRoutedRun is the
// mirror of AC-7.1: with no lf:agent= label at all, a run defaults to the
// configured default agent (Claude today), and a profile with only a
// codex: stanza refuses that just as loudly — a profile is never widened by
// falling back to the other agent's default surface either.
func TestProfileWithOnlyCodexStanzaRefusesTheDefaultClaudeRoutedRun(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{
		Agents:       testTwoAgentRunners(),
		ToolProfiles: map[string]config.ToolProfile{"codex-only": codexOnlyProfile()},
	})
	f.plantCursor()
	f.issueWithLabels("Add a /health endpoint", "lf:tools=codex-only")
	f.settle() // first sight

	f.forge.addComment(testIssue, 1102, owner(), "/spike add a health endpoint")
	f.settle()

	if calls := f.turns.calls(); len(calls) != 0 {
		t.Errorf("the executor ran %d turns, want none", len(calls))
	}
	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 {
		t.Fatalf("posted %d comments, want 1", len(posted))
	}
	if !strings.Contains(posted[0], "codex-only") {
		t.Errorf("reply does not name the profile %q:\n%s", "codex-only", posted[0])
	}
	if got := f.watermark(); got != 1102 {
		t.Errorf("watermark = %d, want 1102", got)
	}
}

// TestProfileWithBothStanzasResolvesForEitherAgent is the positive half of
// AC-7.1/AC-7.2: a profile that carries both stanzas is not refused for
// either agent, and the Claude-routed run's turn.Request still carries only
// the Claude stanza's own Tools/AllowedTools — never anything derived from
// the Codex stanza sitting right beside it in the same profile.
func TestProfileWithBothStanzasResolvesForEitherAgent(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{
		Agents:       testTwoAgentRunners(),
		ToolProfiles: map[string]config.ToolProfile{"both": bothStanzasProfile()},
	})
	f.plantCursor()
	f.issueWithLabels("Add a /health endpoint", "lf:tools=both", "Coding Agent: Codex")
	f.settle() // first sight

	f.forge.addComment(testIssue, 1103, owner(), "/spike add a health endpoint")
	f.settle()

	calls := f.turns.calls()
	if len(calls) != 1 {
		t.Fatalf("the executor was called %d times, want 1: a profile carrying a stanza for the "+
			"routed agent must not be refused", len(calls))
	}
	req := calls[0]
	want := bothStanzasProfile()
	if req.ToolProfile != "both" {
		t.Errorf("ToolProfile = %q, want %q", req.ToolProfile, "both")
	}
	if req.Agent != agent.Kind(store.AgentCodex) {
		t.Errorf("Agent = %q, want codex", req.Agent)
	}
	if req.CodexSandbox != want.Codex.Sandbox {
		t.Errorf("CodexSandbox = %q, want %q", req.CodexSandbox, want.Codex.Sandbox)
	}
	if !equalStringSlices(req.Tools, want.Claude.Tools) {
		t.Errorf("Tools = %v, want the claude stanza's %v — never the codex stanza's sandbox value", req.Tools, want.Claude.Tools)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
