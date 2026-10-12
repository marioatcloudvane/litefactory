package turn

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestImplementHardRulesStopOnlyForTimeOrAnOutsideCauseAndBlockForDecisions(t *testing.T) {
	guidance := implementNodeGuidance()
	if strings.Contains(guidance, "when you cannot finish") {
		t.Fatalf("the hard rules still invite incomplete when the worker cannot finish: %q", guidance)
	}
	for _, want := range []string{
		"result.outcome incomplete with a nonblank result.reason in plain language only when this attempt's time is about to run out or something outside the story stops the work",
		"result.outcome blocked with a nonblank result.reason in plain language when you need a decision",
	} {
		if !strings.Contains(guidance, want) {
			t.Errorf("the hard rules lack %q", want)
		}
	}
}

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
		{"worker with runner timeout", implement, worker, timedRunner(6 * time.Hour), true, "wall-clock limit of 360 minutes"},
		{"smaller node budget wins", implement, map[string]string{"lf_agent_markdown": "story-worker", "lf_budget": "45"}, timedRunner(6 * time.Hour), true, "wall-clock limit of 45 minutes"},
		{"larger node budget loses", implement, map[string]string{"lf_agent_markdown": "story-worker", "lf_budget": "50"}, timedRunner(20 * time.Minute), true, "wall-clock limit of 20 minutes"},
		{"budget without runner timeout", implement, map[string]string{"lf_agent_markdown": "story-worker", "lf_budget": "30"}, nil, true, "wall-clock limit of 30 minutes"},
		{"no known limit", implement, worker, nil, true, "This attempt has a wall-clock limit; there is no other budget"},
		{"other implement node", implement, map[string]string{"lf_agent_markdown": "go-unit-test-engineer"}, timedRunner(time.Hour), false, "wall-clock limit of 60 minutes"},
		{"main role delivery decides the role", agent.Request{Mode: string(agent.ModeImplement), Delivery: &agent.Delivery{MainRole: "story-worker"}}, map[string]string{}, timedRunner(time.Hour), true, "wall-clock limit of 60 minutes"},
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
			if !strings.Contains(got, c.limit) || !strings.Contains(got, "there is no other budget (no token, context or cost budget)") {
				t.Errorf("the limit sentence lacks %q: %q", c.limit, got)
			}
		})
	}
	for _, req := range []agent.Request{{Mode: string(agent.ModeResearch)}, {Mode: string(agent.ModeImplement), Workflow: agent.WorkflowBug}} {
		if got, err := implementAttemptGuidance(req, worker, timedRunner(time.Hour)); err != nil || got != "" {
			t.Errorf("%+v got guidance %q %v", req, got, err)
		}
	}
	if _, err := implementAttemptGuidance(implement, map[string]string{"lf_budget": "zero"}, nil); err == nil {
		t.Error("an invalid node budget was accepted")
	}
}
