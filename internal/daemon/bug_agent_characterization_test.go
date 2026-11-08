package daemon_test

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestBugAgentDispatchCharacterization(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels []string
		want   agent.Kind
	}{
		{"default", nil, agent.Kind("claude")},
		{"explicit Codex", []string{"Coding Agent: Codex"}, agent.Kind("codex")},
		{"explicit Claude", []string{"Coding Agent: Claude"}, agent.Kind("claude")},
		{"unknown", []string{"Coding Agent: Unknown"}, ""},
		{"ambiguous", []string{"Coding Agent: Claude", "Coding Agent: Codex"}, ""},
		{"malformed", []string{"Coding Agent: codex"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			runtime, err := turn.NewGraphBugRuntime(t.Context(), turn.Options{DB: f.db, Git: new(git.Binary), CloneGate: unusedRecoveryCloneGate{}, ResearchReporter: f.reporter, MCP: &turn.MCPRuntime{Redactor: redact.New(redact.Options{Logger: f.logger}), BotID: botID}}, turn.BugRuntimeConfig{Code: f.client, Tickets: f.client, OwnerID: ownerID, Permissions: func(agent.Kind, string) (turn.BugRolePermissions, error) { return turn.BugRolePermissions{}, nil }})
			if err != nil {
				t.Fatal(err)
			}
			f.daemon = f.newDaemon(daemon.Options{Bug: runtime, DefaultAgent: agent.Kind("claude"), Agents: turn.RunnerSet{agent.Kind("claude"): nil, agent.Kind("codex"): nil}})
			f.plantCursor()
			labels := []fakeLabel{{Name: "bug"}}
			for _, label := range tc.labels {
				labels = append(labels, fakeLabel{Name: label})
			}
			f.forge.addIssue(fakeIssue{Number: testIssue, Title: "Bug agent characterization", Body: "Investigate failure", User: owner(), Labels: labels})
			f.settle()
			calls := f.turns.calls()
			if tc.want == "" {
				if len(calls) != 0 {
					t.Fatalf("invalid labels dispatched: %+v", calls)
				}
				return
			}
			if len(calls) != 1 || calls[0].Agent != tc.want || calls[0].Workflow != agent.WorkflowBug {
				t.Fatalf("dispatch = %+v; want Bug agent %s; logs=%s", calls, tc.want, f.logs)
			}
		})
	}
}
