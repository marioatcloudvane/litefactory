package turn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

func repaired(files ...string) agentresult.ValidateRepair {
	return agentresult.ValidateRepair{Response: "repaired", Failures: []agentresult.RepairFailure{{
		Command: "full-one", Observed: "TestTotal failed", Cause: "wrong sum", Locations: []agentresult.RepairLocation{{Path: "app/app.go", Line: 3}}, FilesChanged: files, Fixed: "Fixed the sum.",
	}}}
}

func handbackReport(cause string) agentresult.ValidateRepair {
	return agentresult.ValidateRepair{Response: "handback", Failures: []agentresult.RepairFailure{{
		Command: "full-one", Observed: "TestTotal failed", Cause: cause, Locations: []agentresult.RepairLocation{{Path: "app/app.go", Line: 3}},
		Expected: "Total returns 10.", Objective: "Change Total so it returns 10.",
	}}}
}

// AC-002: a passed suite skips repair; the review runs and the run ends assessed.
func TestValidatePassedSuiteSkipsRepairAndEndsAssessed(t *testing.T) {
	f := newFlowFixture(t)
	nodes := f.runToEnd()
	if got := joinNodes(nodes); got != "suite>review>outcome>reporter" {
		t.Fatalf("path %s", got)
	}
	if f.final() != "assessed" || f.agentRuns != 0 {
		t.Fatalf("final %s, agent runs %d", f.final(), f.agentRuns)
	}
	if len(f.suiteCommands) != 3 {
		t.Fatalf("the suite ran %d commands", len(f.suiteCommands))
	}
	o := f.outcome(f.base)
	if o.Outcome != store.ValidateAssessed || o.Handback != nil {
		t.Fatalf("outcome %+v", o)
	}
	var joined []ValidateJoinedStory
	if err := json.Unmarshal(o.Assessment, &joined); err != nil || len(joined) != 1 || len(joined[0].Criteria) != 2 || len(joined[0].Reviews) != 3 || len(joined[0].Unresolved) != 0 {
		t.Fatalf("joined %s %v", o.Assessment, err)
	}
	if joined[0].SnapshotID != "commit-"+f.base {
		t.Fatalf("snapshot %s", joined[0].SnapshotID)
	}
	for _, c := range joined[0].Criteria {
		if c.Status != "satisfied" || len(c.Evidence[0].CheckIDs) != 1 {
			t.Fatalf("a passed suite command must prove the criterion: %+v", c)
		}
	}
}

// AC-005: three reviewers, each in a run slot, read-only, on a checkout at the tested commit.
func TestValidateReviewStartsThreeReadOnlyReviewersInSlots(t *testing.T) {
	f := newFlowFixture(t)
	f.runToEnd()
	roles := f.reviewers.roles()
	if len(roles) != 3 || f.slots.took != 3 || f.suiteRepo.begun < 2 {
		t.Fatalf("roles %v slots taken %d checkouts %d", roles, f.slots.took, f.suiteRepo.begun)
	}
	for _, r := range f.reviewers.started {
		if r.Access != "read" || r.Commit != f.base || r.Checkout != "/scratch" || r.Scope.WriteAccess || r.Scope.RepairRole {
			t.Errorf("reviewer %s: %+v", r.Role, r)
		}
		if r.Scope.ReviewRole != r.Role {
			t.Errorf("reviewer %s has role scope %q", r.Role, r.Scope.ReviewRole)
		}
	}
	if f.suiteRepo.closed != f.suiteRepo.begun {
		t.Fatalf("checkouts opened %d closed %d", f.suiteRepo.begun, f.suiteRepo.closed)
	}
}

// AC-002: a blocked suite is not repaired, still gets reviews, and ends blocked with its reason.
func TestValidateBlockedSuiteSkipsRepairRunsReviewsAndEndsBlocked(t *testing.T) {
	f := newFlowFixture(t)
	f.suiteExit = func(string) int { return 2 }
	nodes := f.runToEnd()
	if f.final() != "blocked" || visits(nodes, "repair_agent") != 0 || visits(nodes, "review") != 1 {
		t.Fatalf("path %s final %s", joinNodes(nodes), f.final())
	}
	o := f.outcome(f.base)
	if o.Outcome != store.ValidateBlocked || !strings.Contains(o.Reason, "check infrastructure failure") {
		t.Fatalf("outcome %+v", o)
	}
	var joined []ValidateJoinedStory
	_ = json.Unmarshal(o.Assessment, &joined)
	for _, c := range joined[0].Criteria {
		if c.Status == "satisfied" {
			t.Fatalf("a blocked suite proved a criterion: %+v", c)
		}
	}
}

// AC-002: a finished suite result for the commit is reused without running again.
func TestValidateFinishedSuiteResultIsReusedWithoutRunning(t *testing.T) {
	f := newFlowFixture(t)
	run, err := f.db.BeginSuiteRun(t.Context(), store.SuiteRun{InstanceID: f.instance.ID, Node: "suite", Commit: f.base, RecipeDigest: "r", TreeBefore: "tree-a", StartedAt: f.clock})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.FinishSuiteRun(t.Context(), run.ID, store.SuitePassed, "tree-a", "", f.clock); err != nil {
		t.Fatal(err)
	}
	f.runToEnd()
	if len(f.suiteCommands) != 0 || f.suiteRepo.begun == 0 {
		t.Fatalf("the suite ran %d commands for a commit with a finished result", len(f.suiteCommands))
	}
	if f.final() != "assessed" {
		t.Fatalf("final %s", f.final())
	}
}

// AC-003: a failed suite starts a repair round; a kept change is committed and
// the suite runs again on the new commit.
func TestValidateKeptRepairIsCommittedAndTestedAgain(t *testing.T) {
	f := newFlowFixture(t)
	fixed := ""
	f.suiteExit = func(commit string) int {
		if commit == f.base {
			return 1
		}
		fixed = commit
		return 0
	}
	f.agent = func(round int, f *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		f.write("app/app.go", "package app\n\nfunc Total() int { return 10 }\n")
		return repaired("app/app.go"), ActivityResult{}
	}
	nodes := f.runToEnd()
	if f.final() != "assessed" || visits(nodes, "repair_agent") != 1 || visits(nodes, "suite") != 2 {
		t.Fatalf("path %s final %s", joinNodes(nodes), f.final())
	}
	rounds, err := f.db.ListRepairRounds(t.Context(), f.instance.ID)
	if err != nil || len(rounds) != 1 {
		t.Fatalf("rounds %+v %v", rounds, err)
	}
	r := rounds[0]
	if r.State != store.RepairKept || r.PreCommit != f.base || r.ResultCommit == "" || r.ResultCommit == f.base || r.ResultCommit != f.head() || r.ResultCommit != fixed {
		t.Fatalf("round %+v head %s tested %s", r, f.head(), fixed)
	}
	if len(r.Files) != 1 || r.Files[0] != "app/app.go" || r.DiffDigest == "" || !strings.Contains(r.Diff, "return 10") {
		t.Fatalf("measured change %+v", r)
	}
	o := f.outcome(r.ResultCommit)
	if o.Outcome != store.ValidateAssessed {
		t.Fatalf("outcome %+v", o)
	}
	for _, role := range f.reviewers.started {
		if role.Commit != r.ResultCommit {
			t.Fatalf("a reviewer read %s, not the final tested commit %s", role.Commit, r.ResultCommit)
		}
	}
}

// AC-003 and AC-008: a change over the limit is reset, its diff is retained and
// the run is handed back with what must change.
func TestValidateChangeOverTheLimitIsResetRetainedAndHandedBack(t *testing.T) {
	f := newFlowFixture(t)
	f.suiteExit = func(string) int { return 1 }
	f.agent = func(round int, f *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		for _, name := range []string{"a.go", "b.go", "c.go", "d.go"} {
			f.write("app/"+name, "package app\n")
		}
		return repaired("app/a.go", "app/b.go", "app/c.go", "app/d.go"), ActivityResult{}
	}
	nodes := f.runToEnd()
	if f.final() != "handed_back" || visits(nodes, "repair_agent") != 1 || visits(nodes, "review") != 1 {
		t.Fatalf("path %s", joinNodes(nodes))
	}
	if f.head() != f.base {
		t.Fatalf("the worktree is on %s, not the commit before the repair %s", f.head(), f.base)
	}
	for _, name := range []string{"a.go", "d.go"} {
		if _, err := os.Stat(filepath.Join(f.wt.Path(), "app", name)); err == nil {
			t.Fatalf("%s survived the reset", name)
		}
	}
	rounds, _ := f.db.ListRepairRounds(t.Context(), f.instance.ID)
	if len(rounds) != 1 || rounds[0].State != store.RepairTooBig || rounds[0].ResultCommit != "" || rounds[0].DiffDigest == "" || len(rounds[0].Files) != 4 || !strings.Contains(rounds[0].Reason, "4 files") {
		t.Fatalf("rounds %+v", rounds)
	}
	o := f.outcome(f.base)
	var h ValidateHandback
	if o.Outcome != store.ValidateHandedBack || json.Unmarshal(o.Handback, &h) != nil || h.Trigger != HandbackBoundaryExceeded {
		t.Fatalf("outcome %+v handback %+v", o, h)
	}
}

// AC-003: forbidden kinds of change are rejected: a dependency file, a deleted
// test, a skipped test and a changed acceptance criterion.
func TestValidateForbiddenKindsOfChangeAreRejected(t *testing.T) {
	cases := map[string]func(f *flowFixture){
		"dependency": func(f *flowFixture) { f.write("go.mod", "module x\n") },
		"deleted test": func(f *flowFixture) {
			f.write("app/app_test.go", "package app\n")
		},
		"skipped test": func(f *flowFixture) {
			f.write("app/app_test.go", "package app\n\nimport \"testing\"\n\nfunc TestTotal(t *testing.T) {\n\tt.Skip(\"later\")\n\tif Total() != 10 {\n\t\tt.Fatal(\"total\")\n\t}\n}\n")
		},
		"criterion": func(f *flowFixture) { f.write(flowSessionDir+"/plan_brief.md", "changed\n") },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFlowFixture(t)
			f.suiteExit = func(string) int { return 1 }
			f.agent = func(round int, f *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
				edit(f)
				return repaired("app/app.go"), ActivityResult{}
			}
			f.runToEnd()
			rounds, _ := f.db.ListRepairRounds(t.Context(), f.instance.ID)
			if len(rounds) == 0 || rounds[0].State != store.RepairTooBig || !strings.Contains(rounds[0].Reason, "forbidden kinds") {
				t.Fatalf("the change was kept: %+v", rounds)
			}
			if f.head() != f.base {
				t.Fatal("the worktree was not reset")
			}
			if f.final() != "handed_back" {
				t.Fatalf("final %s", f.final())
			}
		})
	}
}

// AC-003: at most two rounds per run; a suite that still fails after the last
// round is handed back.
func TestValidateAtMostTwoRepairRoundsThenHandback(t *testing.T) {
	f := newFlowFixture(t)
	f.suiteExit = func(string) int { return 1 }
	f.agent = func(round int, f *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		f.write("app/app.go", "package app\n\nfunc Total() int { return "+string(rune('0'+round))+" }\n")
		return repaired("app/app.go"), ActivityResult{}
	}
	nodes := f.runToEnd()
	if f.agentRuns != 2 || visits(nodes, "repair_agent") != 2 || f.final() != "handed_back" {
		t.Fatalf("agent runs %d path %s", f.agentRuns, joinNodes(nodes))
	}
	rounds, _ := f.db.ListRepairRounds(t.Context(), f.instance.ID)
	if len(rounds) != 2 || rounds[0].State != store.RepairKept || rounds[1].State != store.RepairKept {
		t.Fatalf("rounds %+v", rounds)
	}
	if _, err := f.db.BeginRepairRound(t.Context(), store.RepairRound{InstanceID: f.instance.ID, PreCommit: f.base, StartedAt: f.clock}); err == nil {
		t.Fatal("a third round was allowed")
	}
	o := f.outcome(rounds[1].ResultCommit)
	var h ValidateHandback
	if o.Outcome != store.ValidateHandedBack || json.Unmarshal(o.Handback, &h) != nil || h.Trigger != HandbackStillFailing {
		t.Fatalf("outcome %+v", o)
	}
	if len(h.RepairRecords) != 2 {
		t.Fatalf("repair records %v", h.RepairRecords)
	}
}

// AC-004: a repair that ran out of its time budget counts as used and its
// half-made edit is reset; the next round may still run.
func TestValidateTimedOutRepairCountsAsUsedAndIsReset(t *testing.T) {
	f := newFlowFixture(t)
	f.suiteExit = func(commit string) int {
		if commit == f.base {
			return 1
		}
		return 0
	}
	f.agent = func(round int, f *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		if round == 1 {
			f.write("app/half.go", "package app\n")
			f.write("app/app.go", "package app\n\n// half made\n")
			attempt, err := f.db.LatestGraphNodeAttempt(f.t.Context(), f.instance.ID, "repair_agent")
			if err != nil {
				f.t.Fatal(err)
			}
			if err := f.db.RecordAttemptTimeout(f.t.Context(), store.AttemptTimeout{NodeAttemptID: attempt.ID, Budget: 15 * 60e9, TimedOutAt: f.clock}); err != nil {
				f.t.Fatal(err)
			}
			return agentresult.ValidateRepair{}, ActivityResult{Outcome: "failed", Diagnostic: "timed out"}
		}
		f.write("app/app.go", "package app\n\nfunc Total() int { return 10 }\n")
		return repaired("app/app.go"), ActivityResult{}
	}
	nodes := f.runToEnd()
	rounds, _ := f.db.ListRepairRounds(t.Context(), f.instance.ID)
	if len(rounds) != 2 || rounds[0].State != store.RepairTimedOut || rounds[0].ResultCommit != "" || rounds[1].State != store.RepairKept {
		t.Fatalf("rounds %+v path %s", rounds, joinNodes(nodes))
	}
	if rounds[1].PreCommit != f.base {
		t.Fatalf("the second round started from %s, not the reset commit", rounds[1].PreCommit)
	}
	if visits(nodes, "repair_budget") != 2 || f.final() != "assessed" {
		t.Fatalf("path %s", joinNodes(nodes))
	}
}

// AC-004: an interrupted round counts as used and the worktree is reset before
// anything else runs.
func TestValidateInterruptedRoundIsUsedAndWorktreeIsResetFirst(t *testing.T) {
	f := newFlowFixture(t)
	f.suiteExit = func(string) int { return 1 }
	// Run to the budget step, start a round and leave edits behind, as a crash would.
	for f.currentNode() != "repair_budget" {
		if _, err := f.step(); err != nil {
			t.Fatal(err)
		}
	}
	round, err := f.db.BeginRepairRound(t.Context(), store.RepairRound{InstanceID: f.instance.ID, PreCommit: f.base, StartedAt: f.clock})
	if err != nil {
		t.Fatal(err)
	}
	f.write("app/half.go", "package app\n")
	f.write("app/app.go", "package app\n\n// half made\n")
	f.write(flowSessionDir+"/notes.md", "keep me\n")
	if _, err := f.step(); err != nil {
		t.Fatal(err)
	}
	got, err := f.db.GetRepairRound(t.Context(), round.ID)
	if err != nil || got.State != store.RepairInterrupted || got.ResultCommit != "" {
		t.Fatalf("round %+v %v", got, err)
	}
	status, err := f.wt.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range status {
		if !strings.HasPrefix(c.Path, flowSessionDir) {
			t.Fatalf("the half-made edit survived: %+v", status)
		}
	}
	if f.head() != f.base {
		t.Fatal("the worktree moved")
	}
	// The interrupted round was used, so only one round is left.
	rounds, _ := f.db.ListRepairRounds(t.Context(), f.instance.ID)
	if len(rounds) != 2 || rounds[1].Round != 2 {
		t.Fatalf("the budget did not count the interrupted round: %+v", rounds)
	}
}

func (f *flowFixture) currentNode() string {
	inst, err := f.db.GetGraphInstance(f.t.Context(), f.instance.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return inst.CurrentNode
}

// AC-008: a repair agent that reports the fix as too large or uncertain hands back.
func TestValidateRepairHandbackTriggersHandback(t *testing.T) {
	for _, cause := range []string{"the sum is wrong", agentresult.RepairUnknownCause} {
		t.Run(cause, func(t *testing.T) {
			f := newFlowFixture(t)
			f.suiteExit = func(string) int { return 1 }
			f.agent = func(int, *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
				return handbackReport(cause), ActivityResult{}
			}
			nodes := f.runToEnd()
			if f.final() != "handed_back" || f.agentRuns != 1 {
				t.Fatalf("path %s runs %d", joinNodes(nodes), f.agentRuns)
			}
			rounds, _ := f.db.ListRepairRounds(t.Context(), f.instance.ID)
			if len(rounds) != 1 || rounds[0].State != store.RepairHandback {
				t.Fatalf("rounds %+v", rounds)
			}
			o := f.outcome(f.base)
			var h ValidateHandback
			if err := json.Unmarshal(o.Handback, &h); err != nil || h.Trigger != HandbackRepairHandback || len(h.Failures) != 1 {
				t.Fatalf("handback %s %v", o.Handback, err)
			}
			x := h.Failures[0]
			if x.Command != "full-one" || len(x.Argv) != 1 || x.Argv[0] != "full-one" || x.Exit == nil || *x.Exit != 1 || !strings.Contains(x.OutputTail, "expected 10, got 9") {
				t.Fatalf("reproduction %+v", x)
			}
			if x.Expected != "Total returns 10." || len(x.Locations) != 1 || x.Locations[0].Path != "app/app.go" || x.Locations[0].Line != 3 || x.Objective != "Change Total so it returns 10." || !strings.HasPrefix(x.SuiteRecord, "suite run ") || !strings.HasPrefix(x.RepairRecord, "repair round 1") {
				t.Fatalf("handback failure %+v", x)
			}
			if x.Diagnostic != (cause == agentresult.RepairUnknownCause) {
				t.Fatalf("diagnostic flag %v for cause %q", x.Diagnostic, cause)
			}
			if !strings.Contains(h.OwnerInstruction, "full-one") || !strings.Contains(h.OwnerInstruction, "app/app.go:3") || len(h.Findings) != 1 || h.SuiteRecord == "" {
				t.Fatalf("instruction %q findings %d", h.OwnerInstruction, len(h.Findings))
			}
		})
	}
}

// AC-008: with no diagnosis the objective is a concrete diagnostic step, never a guess.
func TestValidateHandbackWithoutDiagnosisAsksForADiagnosticStep(t *testing.T) {
	f := newFlowFixture(t)
	f.suiteExit = func(string) int { return 1 }
	f.agent = func(int, *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		return agentresult.ValidateRepair{}, ActivityResult{Outcome: "failed", Diagnostic: "the agent crashed"}
	}
	f.runToEnd()
	if f.final() != "handed_back" {
		t.Fatalf("final %s", f.final())
	}
	var h ValidateHandback
	if err := json.Unmarshal(f.outcome(f.base).Handback, &h); err != nil || len(h.Failures) != 1 {
		t.Fatalf("handback %v", err)
	}
	x := h.Failures[0]
	if !x.Diagnostic || !strings.Contains(x.Objective, "find the cause") || x.Cause != "" || strings.Contains(x.Objective, "because") {
		t.Fatalf("failure %+v", x)
	}
	if h.Trigger != HandbackStillFailing {
		t.Fatalf("trigger %s", h.Trigger)
	}
}

// AC-003: a repair agent that cannot work ends blocked.
func TestValidateBlockedRepairEndsBlocked(t *testing.T) {
	f := newFlowFixture(t)
	f.suiteExit = func(string) int { return 1 }
	f.agent = func(int, *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		return agentresult.ValidateRepair{Response: "blocked", Reason: "the failing output was empty"}, ActivityResult{}
	}
	f.runToEnd()
	o := f.outcome(f.base)
	if f.final() != "blocked" || o.Outcome != store.ValidateBlocked || !strings.Contains(o.Reason, "failing output was empty") || o.Handback != nil {
		t.Fatalf("final %s outcome %+v", f.final(), o)
	}
}

// AC-007: the outcome never says accepted or approved, and it is recorded once
// per run and final commit.
func TestValidateOutcomeIsRecordedOnceAndNeverApproves(t *testing.T) {
	f := newFlowFixture(t)
	f.suiteExit = func(string) int { return 1 }
	f.agent = func(int, *flowFixture) (agentresult.ValidateRepair, ActivityResult) {
		return handbackReport("unknown"), ActivityResult{}
	}
	f.runToEnd()
	o := f.outcome(f.base)
	text := strings.ToLower(string(o.Assessment) + string(o.Handback) + o.Reason + o.Outcome)
	for _, word := range []string{"accepted", "approved"} {
		if strings.Contains(text, word) {
			t.Fatalf("the outcome says %q: %s", word, text)
		}
	}
	latest, err := f.db.LatestValidateOutcome(t.Context(), f.instance.ID)
	if err != nil || latest.ID != o.ID || latest.Commit != f.base {
		t.Fatalf("latest %+v %v", latest, err)
	}
}

var _ = agent.ModeValidate
