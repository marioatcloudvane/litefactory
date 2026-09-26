package graphexec_test

import (
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"testing"
)

func TestBugWorkflow_RegistryNamespace(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		owner agent.Workflow
		valid bool
	}{
		{"lf_bug_record", agent.WorkflowBug, true}, {"lf_bug_record", agent.WorkflowResearch, false},
		{"lf_bug_", agent.WorkflowBug, false}, {"lf_bug_Record", agent.WorkflowBug, false},
		{"lf_bug_record/path", agent.WorkflowBug, false}, {"lf_plan_publish", agent.WorkflowBug, false},
		{"lf_agent", agent.WorkflowBug, true}, {"lf_if", agent.WorkflowBug, true}, {"lf_loop", agent.WorkflowBug, true}, {"lf_ask", agent.WorkflowBug, true}, {"lf_comment", agent.WorkflowBug, true},
		{"lf_agent", agent.Workflow("unknown"), false},
	} {
		t.Run(tt.name+"/"+string(tt.owner), func(t *testing.T) {
			t.Parallel()
			var r graphexec.Registry
			err := r.Register(graphexec.ActionSpec{Name: tt.name, Workflow: tt.owner, Outcomes: []string{"done"}})
			if (err == nil) != tt.valid {
				t.Fatalf("registration = %v", err)
			}
			_, ok := r.Resolve(tt.name)
			if ok != tt.valid {
				t.Fatalf("registered=%v", ok)
			}
			if tt.valid {
				if err := r.Register(graphexec.ActionSpec{Name: tt.name, Workflow: tt.owner, Outcomes: []string{"other"}}); err == nil {
					t.Fatal("duplicate replaced action")
				}
				spec, _ := r.Resolve(tt.name)
				if len(spec.Outcomes) != 1 || spec.Outcomes[0] != "done" {
					t.Fatal("duplicate changed original spec")
				}
			}
			if _, ok := r.Resolve("lf_bug_unregistered"); ok {
				t.Fatal("namespace became wildcard")
			}
		})
	}
}

func TestBugWorkflow_GraphOwnership(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, decl, action string
		owner              agent.Workflow
		register, valid    bool
	}{
		{"bug", "workflow=bug", "lf_bug_record", agent.WorkflowBug, true, true},
		{"unknown suffix", "workflow=bug", "lf_bug_unknown", agent.WorkflowBug, false, false},
		{"generic missing policy", "workflow=bug", "lf_agent", agent.WorkflowBug, false, false},
		{"foreign generic policy", "workflow=bug", "lf_agent", agent.WorkflowPlan, true, false},
		{"conflicting declaration", "workflow=bug;mode=research", "lf_bug_record", agent.WorkflowBug, true, false},
		{"sixth mode", "mode=bug", "lf_bug_record", agent.WorkflowBug, true, false},
		{"foreign graph", "mode=research", "lf_bug_record", agent.WorkflowBug, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := graphexec.Registry{Policy: func(p *graphexec.Profile) error {
				if p.Workflow != agent.WorkflowBug || p.Mode != "" {
					return fmt.Errorf("mismatched Bug policy owner")
				}
				return nil
			}}
			if tt.register {
				if err := r.Register(graphexec.ActionSpec{Name: tt.action, Workflow: tt.owner, Outcomes: []string{"done"}}); err != nil {
					t.Fatal(err)
				}
			}
			p, err := graphexec.ParseProfile([]byte(fmt.Sprintf(`digraph {team=test;%s;profile_version=1;start=work;work [lf_action=%s,lf_outcomes=done];end [lf_terminal=true];work -> end [on=done];}`, tt.decl, tt.action)))
			if err == nil {
				err = graphexec.Validate(p, r)
			}
			if (err == nil) != tt.valid {
				t.Fatalf("graph validation=%v", err)
			}
			if tt.valid && (p.Workflow != agent.WorkflowBug || p.Mode != "") {
				t.Fatalf("lost standalone identity: %+v", p)
			}
		})
	}
}

func TestBugWorkflow_NamespaceAloneGrantsNoPolicy(t *testing.T) {
	t.Parallel()
	var registry graphexec.Registry
	if err := registry.Register(graphexec.ActionSpec{Name: "lf_bug_record", Workflow: agent.WorkflowBug, Outcomes: []string{"done"}}); err != nil {
		t.Fatal(err)
	}
	graph, err := graphexec.ParseProfile([]byte(`digraph {team=test;workflow=bug;profile_version=1;start=work;work [lf_action=lf_bug_record,lf_outcomes=done];end [lf_terminal=true];work -> end [on=done];}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := graphexec.Validate(graph, registry); err == nil {
		t.Fatal("namespace registration without workflow policy granted executable graph")
	}
}
