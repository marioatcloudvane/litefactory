package graphexec_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

const ownerStopDOT = `digraph {team=test;workflow=bug;profile_version=1;start=renamed_worker;
renamed_worker [lf_action=lf_agent,lf_response=result,lf_park=owner,lf_stop="blocked,failed,invalid,exhausted",lf_outcomes="succeeded,reshape,blocked,failed"];
finished [lf_terminal=true];
renamed_worker -> finished [on=succeeded];renamed_worker -> finished [on=reshape];}`

func ownerStopRegistry(t *testing.T, workflow agent.Workflow) graphexec.Registry {
	t.Helper()
	r := graphexec.Registry{}
	if err := r.Register(graphexec.ActionSpec{Name: "lf_agent", Workflow: workflow, Outcomes: graphexec.OwnerStopAgentOutcomes(), Attributes: graphexec.AgentAttributes()}); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestOwnerStops_DeclaredRoutesAndParking(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"renamed_worker", "investigator_42"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, err := graphexec.ParseProfile([]byte(strings.ReplaceAll(ownerStopDOT, "renamed_worker", name)))
			if err != nil {
				t.Fatal(err)
			}
			if err := graphexec.Validate(p, ownerStopRegistry(t, agent.WorkflowBug)); err != nil {
				t.Fatal(err)
			}
			n, ok := p.Node(name)
			if !ok {
				t.Fatal("origin missing")
			}
			for _, outcome := range []string{"blocked", "failed", "invalid", "exhausted"} {
				if !graphexec.ParksOutcome(n, outcome) {
					t.Fatalf("%s does not park at origin", outcome)
				}
				if target, ok := graphexec.Decide(p, name, outcome); ok || target != "" {
					t.Fatalf("parked %s advanced to %q", outcome, target)
				}
			}
			for _, outcome := range []string{"succeeded", "reshape"} {
				if graphexec.ParksOutcome(n, outcome) {
					t.Fatalf("%s unexpectedly parked", outcome)
				}
				if target, ok := graphexec.Decide(p, name, outcome); !ok || target != "finished" {
					t.Fatalf("%s target=%q ok=%v", outcome, target, ok)
				}
			}
		})
	}
}

func TestOwnerStops_RefusesInvalidContracts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, from, to string }{
		{"parked response has edge", "renamed_worker -> finished [on=reshape];", "renamed_worker -> finished [on=reshape];renamed_worker -> finished [on=blocked];"},
		{"ambiguous success", "renamed_worker -> finished [on=reshape];", "renamed_worker -> finished [on=succeeded];"},
		{"missing success route", "renamed_worker -> finished [on=succeeded];", ""},
		{"duplicate stop", "blocked,failed,invalid,exhausted", "blocked,failed,invalid,exhausted,blocked"},
		{"unknown stop", "blocked,failed,invalid,exhausted", "blocked,failed,invalid,exhausted,unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := strings.Replace(ownerStopDOT, tc.from, tc.to, 1)
			p, err := graphexec.ParseProfile([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			if err := graphexec.Validate(p, ownerStopRegistry(t, agent.WorkflowBug)); err == nil {
				t.Fatal("invalid contract admitted")
			}
		})
	}
}

func TestOwnerStops_UnchangedAgentPolicy(t *testing.T) {
	t.Parallel()
	raw := `digraph {team=test;mode=research;profile_version=1;start=worker;worker [lf_action=lf_agent,lf_outcomes="success,failed"];finished [lf_terminal=true];worker -> finished [on=success];worker -> finished [on=failed];}`
	p, err := graphexec.ParseProfile([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := graphexec.Validate(p, ownerStopRegistry(t, agent.WorkflowResearch)); err != nil {
		t.Fatal(err)
	}
	n, _ := p.Node("worker")
	if graphexec.ParksOwner(n) || graphexec.ParksOutcome(n, "failed") {
		t.Fatal("ordinary research activity opted into parking")
	}
	if target, ok := graphexec.Decide(p, "worker", "failed"); !ok || target != "finished" {
		t.Fatalf("ordinary failure route changed: %q %v", target, ok)
	}
}

func TestOwnerStops_DeclaredRetryAndOptionalStale(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, raw, outcome, target string }{
		{"declared retry", strings.Replace(ownerStopDOT, "renamed_worker -> finished [on=reshape]", "renamed_worker -> renamed_worker [on=reshape]", 1), "reshape", "renamed_worker"},
		{"optional stale parks", strings.Replace(strings.Replace(ownerStopDOT, "blocked,failed,invalid,exhausted", "blocked,failed,invalid,exhausted,stale", 1), `lf_outcomes="succeeded,reshape,blocked,failed"`, `lf_outcomes="succeeded,reshape,blocked,failed,stale"`, 1), "stale", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, err := graphexec.ParseProfile([]byte(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			r := ownerStopRegistry(t, agent.WorkflowBug)
			if tc.outcome == "stale" {
				r = graphexec.Registry{}
				if err := r.Register(graphexec.ActionSpec{Name: "lf_agent", Workflow: agent.WorkflowBug, Outcomes: append(graphexec.OwnerStopAgentOutcomes(), "stale"), Attributes: graphexec.AgentAttributes()}); err != nil {
					t.Fatal(err)
				}
			}
			if err := graphexec.Validate(p, r); err != nil {
				t.Fatal(err)
			}
			target, ok := graphexec.Decide(p, "renamed_worker", tc.outcome)
			if target != tc.target || ok != (tc.target != "") {
				t.Fatalf("target=%q ok=%v, want %q", target, ok, tc.target)
			}
		})
	}
}
