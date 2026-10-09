package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// liveValidateRoute prepares the live result route a Validate agent node gets,
// the way the runtime prepares it for a write (repair) or read (reviewer) node.
func liveValidateRoute(t *testing.T, write bool) *preparedMCP {
	t.Helper()
	db, session, options := agentResultFixture(t, "validate")
	authority, err := NewAttemptAuthority(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := authority.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	prep := &preparation{ref: ref, session: session, nodeWrite: write, run: store.Run{ID: options.Attempt.RunID}, mcpRuntime: &MCPRuntime{Authority: authority, Paths: config.NewPaths(launchFixtureDirectory(t)), Executable: "/factory/litefactory", Now: time.Now}}
	p, _, err := prepareMCP(t.Context(), prep, agent.Request{Mode: "validate", Invocation: agent.InvocationResearchNode}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.close(context.WithoutCancel(t.Context())) })
	return p
}

func repairSubmission(t *testing.T, report agentresult.ValidateRepair) []byte {
	t.Helper()
	r := agentresult.Result{Outcome: "completed", Comment: "Repair.", Payload: agentresult.Payload{Validate: &agentresult.Validate{Repair: &report}}}
	raw, err := json.Marshal(map[string]any{"result": r})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func forbiddenRepair(d agentresult.Diagnostics) bool {
	for _, item := range d.Diagnostics {
		if item.Path == "$.result.payload.validate.repair" && item.Code == "forbidden" {
			return true
		}
	}
	return false
}

// #555: the repair node's own live result route accepts its repair report; a
// reviewer's route still refuses one.
func TestValidateRepairNodeReportsThroughItsLiveRoute(t *testing.T) {
	for name, report := range map[string]agentresult.ValidateRepair{"repaired": repaired("app/app.go"), "handback": handbackReport("wrong sum")} {
		t.Run(name, func(t *testing.T) {
			p := liveValidateRoute(t, true)
			d, err := p.service.Preflight(t.Context(), repairSubmission(t, report))
			if err != nil || !d.Valid || forbiddenRepair(d.Diagnostics) {
				t.Fatalf("preflight %+v %v", d, err)
			}
			sub, err := p.service.Submit(t.Context(), "create", repairSubmission(t, report))
			if err != nil || !sub.Validation.Valid {
				t.Fatalf("submit %+v %v", sub, err)
			}
			node, _, err := AgentActivity{Policy: ValidateWorkflowPolicy()}.acceptRetained(t.Context(), p.service)
			if err != nil || node.Response != report.Response {
				t.Fatalf("accepted %+v %v", node, err)
			}
			scope, err := p.service.trustedContext(t.Context())
			if err != nil || !scope.RepairRole {
				t.Fatalf("trusted scope %+v %v", scope, err)
			}
			raw, err := json.Marshal(node)
			if err != nil {
				t.Fatal(err)
			}
			if _, d := agentresult.DecodeValidateNode(raw, scope); !d.Valid || !d.Complete {
				t.Fatalf("node decode %+v", d)
			}
		})
	}
	t.Run("reviewer", func(t *testing.T) {
		p := liveValidateRoute(t, false)
		d, err := p.service.Preflight(t.Context(), repairSubmission(t, repaired("app/app.go")))
		if err != nil || d.Valid || !forbiddenRepair(d.Diagnostics) {
			t.Fatalf("a reviewer's repair report was not refused: %+v %v", d, err)
		}
	})
}

// #555: a failing suite runs the repair agent, whose report goes through the
// live result route, and repair_check settles the round as kept.
func TestValidateRepairRoundUsesTheLiveRepairReport(t *testing.T) {
	f := newFlowFixture(t)
	f.suiteExit = func(commit string) int {
		if commit == f.base {
			return 1
		}
		return 0
	}
	f.agent = func(round int, f *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		f.write("app/app.go", "package app\n\nfunc Total() int { return 10 }\n")
		p := liveValidateRoute(f.t, true)
		if sub, err := p.service.Submit(f.t.Context(), "create", repairSubmission(f.t, repaired("app/app.go"))); err != nil || !sub.Validation.Valid {
			f.t.Fatalf("submit %+v %v", sub, err)
		}
		node, _, err := AgentActivity{Policy: ValidateWorkflowPolicy()}.acceptRetained(f.t.Context(), p.service)
		if err != nil || node.Result.Payload.Validate == nil || node.Result.Payload.Validate.Repair == nil {
			f.t.Fatalf("accepted %+v %v", node, err)
		}
		return *node.Result.Payload.Validate.Repair, ActivityResult{}
	}
	nodes := f.runToEnd()
	if visits(nodes, "repair_agent") != 1 || visits(nodes, "repair_check") != 1 {
		t.Fatalf("path %s", joinNodes(nodes))
	}
	rounds, err := f.db.ListRepairRounds(t.Context(), f.instance.ID)
	if err != nil || len(rounds) != 1 || rounds[0].State != store.RepairKept || !strings.Contains(rounds[0].Diff, "return 10") {
		t.Fatalf("rounds %+v %v", rounds, err)
	}
}
