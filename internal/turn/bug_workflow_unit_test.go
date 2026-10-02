package turn

import (
	"context"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"strings"
	"testing"
)

type bugUnitHandler struct{}

func (*bugUnitHandler) Execute(context.Context, Options, ActivityRequest) (ActivityResult, error) {
	return ActivityResult{Outcome: "recorded"}, nil
}

func TestBugWorkflow_ConcreteHandlerRequired(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		handler ActivityHandler
		valid   bool
	}{{"missing", nil, false}, {"typed nil", (*bugUnitHandler)(nil), false}, {"concrete", &bugUnitHandler{}, true}} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var r ActivityRegistry
			err := r.Register("lf_bug_record", tt.handler)
			if (err == nil) != tt.valid {
				t.Fatalf("Register=%v", err)
			}
			_, ok := r.Resolve("lf_bug_record")
			if ok != tt.valid {
				t.Fatalf("Resolve=%v", ok)
			}
			if _, ok := r.Resolve("lf_bug_unknown"); ok {
				t.Fatal("unknown handler resolved")
			}
			if tt.valid {
				if err := r.Register("lf_bug_record", &bugUnitHandler{}); err == nil {
					t.Fatal("duplicate handler admitted")
				}
			}
		})
	}
}

func TestBugWorkflow_ReadinessRefusals(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, want string
		mutate     func(*WorkflowPolicy)
	}{
		{"missing decoder", "result decoder", func(p *WorkflowPolicy) { p.Decode = nil }},
		{"missing shared preparation", "shared result service", func(p *WorkflowPolicy) { p.Handlers = &WorkflowHandlers{} }},
		{"foreign control", "control", func(p *WorkflowPolicy) { p.Control = graphexec.ControlPolicy{Mode: agent.ModeResearch} }},
		{"missing guidance", "guidance", func(p *WorkflowPolicy) { p.Guidance = "" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			policy := ResearchWorkflowPolicy()
			policy.Mode = ""
			policy.Workflow = agent.WorkflowBug
			policy.Control.Mode = ""
			policy.Control.Workflow = agent.WorkflowBug
			tt.mutate(&policy)
			err := policy.Admit()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Admit=%v, want %q", err, tt.want)
			}
			if _, err := WorkflowSpecs(policy); err == nil {
				t.Fatal("unready policy produced executable specs")
			}
		})
	}
}

// Registry construction must preserve the identity boundary before any graph or
// invocation exists. The synthetic decoder supplies no production Bug policy.
func TestBugWorkflow_GenericSpecsPreserveIdentity(t *testing.T) {
	t.Parallel()
	bug := WorkflowPolicy{
		Workflow: agent.WorkflowBug,
		Control:  graphexec.ControlPolicy{Workflow: agent.WorkflowBug, Responses: []string{"ask", "done", "approved", "blocked"}, Defaults: []string{"ask"}, Ask: "ask", Approved: "approved"},
		Decode: func([]byte, agentresult.Context) (agentresult.ResearchNode, agentresult.Diagnostics) {
			return agentresult.ResearchNode{}, agentresult.Diagnostics{}
		},
		Guidance: "Synthetic test policy; results use the shared result helper.",
	}
	for _, tt := range []struct {
		name     string
		policy   WorkflowPolicy
		workflow agent.Workflow
		mode     agent.Mode
	}{
		{"standalone bug", bug, agent.WorkflowBug, ""},
		{"historical research", ResearchWorkflowPolicy(), "", agent.ModeResearch},
		{"historical plan", PlanWorkflowPolicy(), "", agent.ModePlan},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry, err := WorkflowSpecs(tt.policy)
			if err != nil {
				t.Fatalf("WorkflowSpecs: %v", err)
			}
			for _, name := range []string{"lf_agent", "lf_comment", "lf_ask", "lf_if", "lf_loop"} {
				spec, ok := registry.Resolve(name)
				if !ok {
					t.Fatalf("missing generic action %s", name)
				}
				if spec.Workflow != tt.workflow || spec.Mode != tt.mode {
					t.Errorf("%s identity = workflow %q mode %q, want workflow %q mode %q", name, spec.Workflow, spec.Mode, tt.workflow, tt.mode)
				}
				if len(spec.Outcomes) == 0 {
					t.Errorf("%s lost its outcome contract", name)
				}
			}
		})
	}
}
