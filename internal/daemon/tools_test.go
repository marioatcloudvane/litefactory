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

func researchProfile() config.ToolProfile {
	return config.DefaultToolProfiles().Permissions["research"]
}

func TestModesSelectCanonicalPermissionsForBothAgents(t *testing.T) {
	tests := []struct {
		name, command, profile, sandbox string
		mode                            agent.Mode
		kind                            agent.Kind
		attachments                     bool
	}{
		{"Claude research", "/research inspect", "research", "", agent.ModeResearch, agent.Kind(store.AgentClaude), true},
		{"Claude plan", "/plan design", "plan", "", agent.ModePlan, agent.Kind(store.AgentClaude), true},
		{"Claude implement", "/implement build", "implement", "", agent.ModeImplement, agent.Kind(store.AgentClaude), false},
		{"Claude validate", "/validate check", "validate", "", agent.ModeValidate, agent.Kind(store.AgentClaude), false},
		{"Claude spike", "/spike experiment", "spike", "", agent.ModeSpike, agent.Kind(store.AgentClaude), false},
		{"Codex research", "/research inspect", "research", "workspace-write", agent.ModeResearch, agent.Kind(store.AgentCodex), true},
		{"Codex plan", "/plan design", "plan", "workspace-write", agent.ModePlan, agent.Kind(store.AgentCodex), true},
		{"Codex implement", "/implement build", "implement", "workspace-write", agent.ModeImplement, agent.Kind(store.AgentCodex), false},
		{"Codex validate", "/validate check", "validate", "read-only", agent.ModeValidate, agent.Kind(store.AgentCodex), false},
		{"Codex spike", "/spike experiment", "spike", "workspace-write", agent.ModeSpike, agent.Kind(store.AgentCodex), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			agents := turn.RunnerSet{agent.Kind(store.AgentClaude): nil, agent.Kind(store.AgentCodex): nil}
			f.daemon = f.newDaemon(daemon.Options{Agents: agents})
			f.plantCursor()
			labels := []string{"Mode: Implement"}
			if tt.mode == agent.ModeImplement {
				labels = []string{"Mode: Plan"}
			}
			if tt.kind == agent.Kind(store.AgentCodex) {
				labels = append(labels, "Coding Agent: Codex")
			}
			f.issueWithLabels("Add a /health endpoint", labels...)
			f.settle()
			f.forge.addComment(testIssue, 501, owner(), tt.command)
			f.settle()

			calls := f.turns.calls()
			if len(calls) != 1 {
				t.Fatalf("executor calls = %d, want 1\nlogs:\n%s", len(calls), f.logs)
			}
			req := calls[0]
			selected := "Mode: " + strings.ToUpper(string(tt.mode[:1])) + string(tt.mode[1:])
			if tt.kind == agent.Kind(store.AgentCodex) {
				assertRetentionLabels(t, f, selected, "Coding Agent: Codex")
			} else {
				assertRetentionLabels(t, f, selected)
			}
			if req.Mode != tt.mode || req.Agent != tt.kind || req.ToolProfile != tt.profile || req.AttachmentsAllowed != tt.attachments {
				t.Fatalf("routing = mode %q agent %q profile %q attachments %t", req.Mode, req.Agent, req.ToolProfile, req.AttachmentsAllowed)
			}
			// Spike's ceiling is its own explicit fixer binding, never implement.
			source := tt.profile
			if source == "spike" {
				source = config.SpikeFixerProfile
			}
			profile := config.DefaultToolProfiles().Permissions[source]
			if tt.kind == agent.Kind(store.AgentClaude) {
				if !equalStringSlices(req.Tools, profile.Claude.Tools) || !equalStringSlices(req.AllowedTools, profile.Claude.AllowedTools) {
					t.Fatalf("Claude payload = tools %v allowed %v", req.Tools, req.AllowedTools)
				}
			} else if req.CodexSandbox != tt.sandbox {
				t.Fatalf("Codex sandbox = %q, want %q", req.CodexSandbox, tt.sandbox)
			}
		})
	}
}

func TestConfiguredAttachmentOverrideReachesTurnRequest(t *testing.T) {
	profiles := config.DefaultToolProfiles().Permissions
	implement := profiles["implement"]
	implement.Attachments = true
	profiles["implement"] = implement

	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{ToolProfiles: profiles})
	f.plantCursor()
	f.issueWithLabels("Add a /health endpoint")
	f.settle()
	f.forge.addComment(testIssue, 501, owner(), "/implement build")
	f.settle()

	calls := f.turns.calls()
	if len(calls) != 1 {
		t.Fatalf("executor calls = %d, want 1\nlogs:\n%s", len(calls), f.logs)
	}
	req := calls[0]
	if req.Mode != agent.ModeImplement || req.ToolProfile != "implement" || !req.AttachmentsAllowed {
		t.Fatalf("override routing = mode %q profile %q attachments %t", req.Mode, req.ToolProfile, req.AttachmentsAllowed)
	}
}

func TestLegacyToolsLabelsAlwaysRefuseBeforeDispatch(t *testing.T) {
	tests := [][]string{{"lf:tools=research"}, {"LF:TOOLS=implement"}, {"lf:tools="}, {"lf:tools=bad name"}, {"lf:tools=research", "lf:tools=implement"}}
	for _, labels := range tests {
		t.Run(strings.Join(labels, "+"), func(t *testing.T) {
			f := newFixture(t)
			f.plantCursor()
			f.issueWithLabels("Add a /health endpoint", labels...)
			f.settle()
			f.forge.addComment(testIssue, 900, owner(), "/implement build it")
			f.settle()

			if len(f.turns.calls()) != 0 || len(f.runs()) != 0 {
				t.Fatal("obsolete label reached dispatch")
			}
			posted, _, _ := f.forge.snapshot()
			if len(posted) != 1 || !strings.Contains(posted[0], "Remove every") || !strings.Contains(posted[0], "Nothing was run") {
				t.Fatalf("reply is not actionable: %v\nlogs:\n%s", posted, f.logs)
			}
			for _, label := range labels {
				value := strings.TrimPrefix(strings.ToLower(label), "lf:tools=")
				if strings.Contains(value, " ") && strings.Contains(strings.ToLower(posted[0]), value) {
					t.Fatalf("reply echoed untrusted label value %q", value)
				}
			}
			if !strings.HasPrefix(posted[0], turn.MarkerPrefix) || f.watermark() != 900 {
				t.Fatalf("reply/watermark = %q/%d", posted[0], f.watermark())
			}
		})
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
