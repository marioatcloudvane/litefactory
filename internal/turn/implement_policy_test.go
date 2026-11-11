package turn

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

type timedRunner time.Duration

func (r timedRunner) Timeout() time.Duration { return time.Duration(r) }

func TestImplementAttemptGuidanceAsksOnlyTheStoryWorkerToDelegateAndStatesTheLimit(t *testing.T) {
	implement := agent.Request{Mode: string(agent.ModeImplement)}
	worker := map[string]string{"lf_agent_markdown": "story-worker"}
	cases := []struct {
		name     string
		req      agent.Request
		attrs    map[string]string
		runner   any
		delegate bool
		limit    string
	}{
		{"worker with runner timeout", implement, worker, timedRunner(6 * time.Hour), true, "Wall-clock limit: 21600 seconds"},
		{"smaller node budget wins", implement, map[string]string{"lf_agent_markdown": "story-worker", "lf_budget": "45"}, timedRunner(6 * time.Hour), true, "Wall-clock limit: 2700 seconds"},
		{"larger node budget wins", implement, map[string]string{"lf_agent_markdown": "story-worker", "lf_budget": "50"}, timedRunner(20 * time.Minute), true, "Wall-clock limit: 3000 seconds"},
		{"budget without runner timeout", implement, map[string]string{"lf_agent_markdown": "story-worker", "lf_budget": "30"}, nil, true, "Wall-clock limit: 1800 seconds"},
		{"effective parent beats node", implement, map[string]string{"lf_agent_markdown": "story-worker", "lf_budget": "120"}, effectiveWorkerLimit(61 * time.Second), true, "Wall-clock limit: 61 seconds"},
		{"exact legacy seconds", implement, worker, effectiveWorkerLimit(121 * time.Second), true, "Wall-clock limit: 121 seconds"},
		{"no known limit", implement, worker, nil, true, "Wall-clock limit: set by the factory"},
		{"other implement node", implement, map[string]string{"lf_agent_markdown": "go-unit-test-engineer"}, timedRunner(time.Hour), false, "Wall-clock limit: 3600 seconds"},
		{"main role delivery decides the role", agent.Request{Mode: string(agent.ModeImplement), Delivery: &agent.Delivery{MainRole: "story-worker"}}, map[string]string{}, timedRunner(time.Hour), true, "Wall-clock limit: 3600 seconds"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := implementAttemptGuidance(c.req, c.attrs, c.runner)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(got, implementDelegationRequest) != c.delegate {
				t.Errorf("delegation request present=%v, want %v: %q", !c.delegate, c.delegate, got)
			}
			if !strings.Contains(got, c.limit) || !strings.Contains(got, "no token, context or cost budget") {
				t.Errorf("the limit sentence lacks %q: %q", c.limit, got)
			}
		})
	}
	for _, req := range []agent.Request{{Mode: string(agent.ModeResearch)}, {Mode: string(agent.ModeBug)}} {
		if got, err := implementAttemptGuidance(req, worker, timedRunner(time.Hour)); err != nil || got != "" {
			t.Errorf("%+v got guidance %q %v", req, got, err)
		}
	}
	if _, err := implementAttemptGuidance(implement, map[string]string{"lf_budget": "zero"}, nil); err == nil {
		t.Error("an invalid node budget was accepted")
	}
}
