package agent_test

import (
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
)

func TestRequest_GraphNodeIdentity(t *testing.T) {
	cases := []struct {
		name   string
		change func(*agent.Request)
		valid  bool
	}{
		{"bound research node", func(*agent.Request) {}, true},
		{"legacy request", func(r *agent.Request) { r.Invocation = ""; r.Graph = nil }, true},
		{"missing identity", func(r *agent.Request) { r.Graph = nil }, false},
		{"missing instance", func(r *agent.Request) { r.Graph.InstanceID = 0 }, false},
		{"negative instance", func(r *agent.Request) { r.Graph.InstanceID = -1 }, false},
		{"missing attempt", func(r *agent.Request) { r.Graph.AttemptID = 0 }, false},
		{"negative attempt", func(r *agent.Request) { r.Graph.AttemptID = -1 }, false},
		{"missing run", func(r *agent.Request) { r.Graph.RunID = 0 }, false},
		{"negative run", func(r *agent.Request) { r.Graph.RunID = -1 }, false},
		{"missing node", func(r *agent.Request) { r.Graph.Node = "" }, false},
		{"second mode node", func(r *agent.Request) { r.Mode = "plan" }, true},
		{"invalid mode", func(r *agent.Request) { r.Mode = "unregistered" }, false},
		{"unknown kind", func(r *agent.Request) { r.Invocation = "unrecognized" }, false},
		{"legacy with graph identity", func(r *agent.Request) { r.Invocation = "" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := agent.Request{Invocation: agent.InvocationResearchNode, Graph: &agent.GraphInvocation{InstanceID: 1, AttemptID: 2, RunID: 3, Node: "investigate"}, Ref: naming.Ref{Forge: "github", Owner: "owner", Repo: "repo", Issue: 1}, RunOrdinal: 1, SegmentOrdinal: 1, Session: agent.SessionRef{Ref: "session"}, Mode: "research", WorktreePath: "/fixture/worktree", Prompt: "Investigate the supplied request."}
			tc.change(&r)
			err := r.Validate()
			if tc.valid {
				if err != nil {
					t.Fatalf("valid request refused: %v", err)
				}
			} else if !errors.Is(err, agent.ErrInvalidRequest) {
				t.Fatalf("got %v, want ErrInvalidRequest", err)
			}
		})
	}
}
