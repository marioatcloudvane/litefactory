package turn

import (
	"context"
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"os"
	"path/filepath"
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

// #1183: an owner /validate whose stage waited for run capacity resumes
// from a graph dispatch, under the factory's synthetic command. On a separated
// managed feature in its approved scope the repair agent's run still starts.
func TestValidateDispatchResumesIntoTheRepairAgent(t *testing.T) {
	a := newAdoptFlow(t)
	ctx := t.Context()
	// The repair round resets a real worktree to the tested commit.
	binary, err := git.Resolve(ctx)
	if err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	clone, err := binary.Open(ctx, a.f.repo.Dir)
	mustProgress(t, err)
	ensured, err := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: filepath.Join(t.TempDir(), "wt"), Branch: "lf/1183-repair", BaseBranch: gittest.DefaultBranch})
	mustProgress(t, err)
	mustProgress(t, os.MkdirAll(filepath.Join(ensured.Worktree.Path(), flowSessionDir), 0o700))
	a.head, err = ensured.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	mustProgress(t, err)
	realBind := a.f.opts.Validate.Bind
	a.f.opts.Validate.Bind = func(ctx context.Context, o Options, r Request, e store.ValidateEntry) (ValidateSuiteBindings, ValidateFlowBindings, error) {
		suite, flow, err := realBind(ctx, o, r, e)
		flow.Worktree = func() (*git.Worktree, error) { return ensured.Worktree, nil }
		run := suite.Run
		suite.Run = func(ctx context.Context, dir string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
			res := run(ctx, dir, c, started)
			code := 1
			res.Exit, res.Outcome = &code, "failed"
			return res
		}
		return suite, flow, err
	}
	a.f.opts.Validate.Permissions = validateRepairResolver(t)
	var agentRuns []store.Run
	var agentErrs []error
	a.f.opts.Runners = runnerSet(lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		runs, err := a.f.db.ListRuns(ctx, a.f.session.ID)
		if err == nil && len(runs) > 0 {
			agentRuns = append(agentRuns, runs[len(runs)-1])
		}
		agentErrs = append(agentErrs, err)
		return nil, errValidateStop
	}))
	feature, err := a.f.db.GetManagedFeature(ctx, a.f.session.TaskRef)
	mustProgress(t, err)
	if feature.WorkflowVersion != store.WorkflowSeparated || feature.State != "implementing" {
		t.Fatalf("fixture feature %+v", feature)
	}
	// No run capacity is free: the owner's start parks the owed stage.
	a.f.opts.GraphRetryDelay = time.Minute
	if _, err := Execute(WithStepCapacity(ctx, fullCapacity{}), a.f.opts, a.request(6001, validateOwnerID, "/validate check the finished work")); err != nil {
		t.Fatal(err)
	}
	inst, err := a.f.db.LatestWorkflowInstance(ctx, a.f.session.ID, "validate")
	mustProgress(t, err)
	dispatch, err := a.f.db.LatestGraphDispatch(ctx, inst.ID)
	mustProgress(t, err)
	// As on #998 the deferred stage is the suite; the dispatch's run then goes
	// on through the failed suite to the repair agent.
	if dispatch.Status != "pending" || dispatch.Node != inst.CurrentNode || (inst.CurrentNode != "suite" && inst.CurrentNode != "repair_agent") || len(agentRuns) != 0 {
		t.Fatalf("instance at %s, dispatch %+v, agent runs %d", inst.CurrentNode, dispatch, len(agentRuns))
	}
	reserved, ok, err := a.f.db.ReserveGraphDispatch(ctx, dispatch.ID, dispatch.DueAt)
	mustProgress(t, err)
	if !ok {
		t.Fatal("reserve refused")
	}
	// The daemon's dispatch request: a graph trigger and a synthetic command.
	instruction := "Continue the workflow at its " + reserved.Node + " stage."
	req := a.f.request()
	req.Mode = agent.ModeValidate
	req.Trigger = Trigger{CommentID: reserved.TriggerID(), Body: instruction, Author: "owner"}
	req.Command = Command{Kind: KindFreeText, Text: instruction}
	out, err := Execute(ctx, a.f.opts, req)
	if err != nil && strings.Contains(err.Error(), "managed Validate requires") {
		t.Fatalf("the resumed repair stage was refused: %v", err)
	}
	if len(agentRuns) != 1 || agentErrs[0] != nil {
		t.Fatalf("the repair agent did not start: runs=%+v errs=%v out=%+v err=%v", agentRuns, agentErrs, out, err)
	}
	if got := agentRuns[0]; got.Status != store.RunRunning || got.TriggerCommentID != reserved.TriggerID() {
		t.Fatalf("repair run %+v", got)
	}
}

// fullCapacity admits nothing, as a worker pool with every slot taken.
type fullCapacity struct{}

func (fullCapacity) Replace(context.Context, int) (bool, error) { return false, nil }
func (fullCapacity) Release()                                   {}
func (fullCapacity) Capacity() (int, int)                       { return 4, 4 }
