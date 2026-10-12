package turn

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestRepairChangesTestsReadsTheDiff(t *testing.T) {
	cases := []struct {
		name string
		diff string
		want bool
	}{
		{"a source edit", "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n-func A() int { return 1 }\n+func A() int { return 2 }\n", false},
		{"a test fixed in place", "diff --git a/a_test.go b/a_test.go\n--- a/a_test.go\n+++ b/a_test.go\n-func TestA(t *testing.T) { want := 1 }\n+func TestA(t *testing.T) { want := 2 }\n", false},
		{"a test removed", "diff --git a/a_test.go b/a_test.go\n--- a/a_test.go\n+++ b/a_test.go\n-func TestA(t *testing.T) {}\n", true},
		{"a test file deleted", "diff --git a/a_test.go b/a_test.go\ndeleted file mode 100644\n--- a/a_test.go\n+++ /dev/null\n-package a\n", true},
		{"a Go skip", "diff --git a/a_test.go b/a_test.go\n+\tt.Skip(\"later\")\n", true},
		{"a skip in another language", "diff --git a/x.test.js b/x.test.js\n+it.skip('works', () => {})\n", true},
		{"a python skip", "diff --git a/test_x.py b/test_x.py\n+@pytest.mark.skip\n", true},
		{"a source file deleted", "diff --git a/a.go b/a.go\ndeleted file mode 100644\n-package a\n", false},
		{"a test added", "diff --git a/a_test.go b/a_test.go\n+func TestB(t *testing.T) {}\n", false},
		{"nothing", "", false},
	}
	for _, c := range cases {
		if got := repairChangesTests(c.diff); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestMeasureValidateChange(t *testing.T) {
	limits := []string{"package", "dependency", "schema", "api", "tests", "criteria"}
	files, kinds, forbidden := measureValidateChange([]string{"b.go", "a.go", ".aiteam/s/x.md"}, "", ".aiteam/s", nil, limits)
	if !reflect.DeepEqual(files, []string{"a.go", "b.go"}) || !reflect.DeepEqual(kinds, []string{"criteria"}) || !reflect.DeepEqual(forbidden, []string{"criteria"}) {
		t.Fatalf("session change: %v %v %v", files, kinds, forbidden)
	}
	files, kinds, forbidden = measureValidateChange([]string{"go.mod", "db/migrations/001.sql", "a.go"}, "", "", []string{"api"}, limits)
	if len(files) != 3 || !reflect.DeepEqual(kinds, []string{"api", "dependency", "schema"}) || len(forbidden) != 3 {
		t.Fatalf("path kinds: %v %v %v", files, kinds, forbidden)
	}
	_, kinds, forbidden = measureValidateChange([]string{"docs/plan_brief.md"}, "", "", nil, limits)
	if !reflect.DeepEqual(kinds, []string{"criteria"}) || len(forbidden) != 1 {
		t.Fatalf("criteria file: %v %v", kinds, forbidden)
	}
	// A kind that is not forbidden is reported but not refused.
	_, kinds, forbidden = measureValidateChange([]string{"go.mod"}, "", "", nil, []string{"api"})
	if !reflect.DeepEqual(kinds, []string{"dependency"}) || len(forbidden) != 0 {
		t.Fatalf("unforbidden kind: %v %v", kinds, forbidden)
	}
}

func TestDecodeValidateLimits(t *testing.T) {
	if n, kinds, err := decodeValidateLimits(map[string]string{"lf_max_files": "3", "lf_forbid": "package,tests"}); err != nil || n != 3 || len(kinds) != 2 {
		t.Fatalf("%d %v %v", n, kinds, err)
	}
	for _, attrs := range []map[string]string{{"lf_max_files": "9", "lf_forbid": "api"}, {"lf_max_files": "x", "lf_forbid": "api"}, {"lf_max_files": "2", "lf_forbid": "everything"}} {
		if _, _, err := decodeValidateLimits(attrs); err == nil {
			t.Errorf("accepted %v", attrs)
		}
	}
}

func TestDecideValidateEnd(t *testing.T) {
	rounds := func(states ...string) []store.RepairRound {
		var out []store.RepairRound
		for i, s := range states {
			out = append(out, store.RepairRound{Round: i + 1, State: s, Reason: "why " + s})
		}
		return out
	}
	cases := []struct {
		name    string
		suite   validateSuiteOutput
		rounds  []store.RepairRound
		end     string
		trigger string
	}{
		{"passed", validateSuiteOutput{Outcome: "passed"}, nil, store.ValidateAssessed, ""},
		{"passed after a kept repair", validateSuiteOutput{Outcome: "passed"}, rounds(store.RepairKept), store.ValidateAssessed, ""},
		{"blocked suite", validateSuiteOutput{Outcome: "blocked", Reason: "no recipe"}, nil, store.ValidateBlocked, ""},
		{"failed after too big", validateSuiteOutput{Outcome: "failed"}, rounds(store.RepairTooBig), store.ValidateHandedBack, HandbackBoundaryExceeded},
		{"failed after a handback", validateSuiteOutput{Outcome: "failed"}, rounds(store.RepairHandback), store.ValidateHandedBack, HandbackRepairHandback},
		{"still failing after kept rounds", validateSuiteOutput{Outcome: "failed"}, rounds(store.RepairKept, store.RepairKept), store.ValidateHandedBack, HandbackStillFailing},
		{"still failing after timeouts", validateSuiteOutput{Outcome: "failed"}, rounds(store.RepairTimedOut, store.RepairInterrupted), store.ValidateHandedBack, HandbackStillFailing},
		{"repair blocked", validateSuiteOutput{Outcome: "failed"}, rounds(store.RepairBlocked), store.ValidateBlocked, ""},
		{"failed with no round", validateSuiteOutput{Outcome: "failed"}, nil, store.ValidateHandedBack, HandbackStillFailing},
	}
	for _, c := range cases {
		end, reason, trigger := decideValidateEnd(c.suite, c.rounds)
		if end != c.end || trigger != c.trigger {
			t.Errorf("%s: got %s/%s want %s/%s", c.name, end, trigger, c.end, c.trigger)
		}
		if end == store.ValidateBlocked && strings.TrimSpace(reason) == "" {
			t.Errorf("%s: blocked without a reason", c.name)
		}
	}
	if _, reason, _ := decideValidateEnd(validateSuiteOutput{Outcome: "blocked", Reason: "no recipe"}, nil); reason != "no recipe" {
		t.Errorf("a blocked suite must carry its own reason, got %q", reason)
	}
}

func accepted(role string, story agentresult.AssessmentStory) store.ValidateReview {
	raw, _ := json.Marshal([]agentresult.AssessmentStory{story})
	return store.ValidateReview{Role: role, State: store.ReviewAccepted, Findings: raw}
}

// AC-007: the three parts are joined into one assessment per story; a missing
// part stays unresolved and is never satisfied.
func TestJoinValidateAssessments(t *testing.T) {
	stories := []ValidateStory{{Key: "s1", Criteria: []string{"AC-001"}}, {Key: "s2", Criteria: []string{"AC-001"}}}
	sat := func(name, class string) agent.ContributionFinding {
		return agent.ContributionFinding{Name: name, Status: "satisfied", Classification: class, Evidence: []agent.EvidenceExplanation{{Summary: "ok", CheckIDs: []string{"suite-1-0"}}}}
	}
	reviews := []store.ValidateReview{
		accepted("acceptance", agentresult.AssessmentStory{Story: "s1", Criteria: []agent.ContributionFinding{sat("AC-001", "blocking")}, Reviews: []agent.ContributionFinding{sat("plan", "advisory")}}),
		accepted("code", agentresult.AssessmentStory{Story: "s1", Reviews: []agent.ContributionFinding{sat("code", "advisory")}}),
		{Role: "architecture", State: store.ReviewFailed, Reason: "crashed"},
	}
	joined := JoinValidateAssessments(stories, "commit-x", reviews)
	if len(joined) != 2 || joined[0].Story != "s1" || joined[1].Story != "s2" || joined[0].SnapshotID != "commit-x" {
		t.Fatalf("%+v", joined)
	}
	s1 := joined[0]
	if s1.Criteria[0].Status != "satisfied" || s1.Reviews[0].Name != "plan" || s1.Reviews[1].Name != "code" || s1.Reviews[2].Name != "architecture" {
		t.Fatalf("s1 %+v", s1)
	}
	if s1.Reviews[0].Status != "satisfied" || s1.Reviews[1].Status != "satisfied" || s1.Reviews[2].Status != "unverified" || !reflect.DeepEqual(s1.Unresolved, []string{"architecture"}) {
		t.Fatalf("s1 reviews %+v unresolved %v", s1.Reviews, s1.Unresolved)
	}
	if !strings.Contains(s1.Reviews[2].Evidence[0].Summary, "crashed") {
		t.Fatalf("the reason of the missing part is not kept: %+v", s1.Reviews[2])
	}
	// A story the accepted parts do not cover stays unresolved in full.
	s2 := joined[1]
	if s2.Criteria[0].Status != "unverified" || len(s2.Unresolved) != 3 {
		t.Fatalf("s2 %+v", s2)
	}
	// With nothing at all, nothing is satisfied.
	for _, s := range JoinValidateAssessments(stories, "c", nil) {
		for _, f := range append(append([]agent.ContributionFinding{}, s.Criteria...), s.Reviews...) {
			if f.Status != "unverified" {
				t.Fatalf("a missing part is %s", f.Status)
			}
		}
	}
	// A stored findings blob that cannot be read is unresolved, not a crash.
	bad := JoinValidateAssessments(stories[:1], "c", []store.ValidateReview{{Role: "code", State: store.ReviewAccepted, Findings: json.RawMessage("not json")}})
	if bad[0].Reviews[1].Status != "unverified" {
		t.Fatalf("%+v", bad[0].Reviews)
	}
}

func TestOwnerHandbackInstructionNamesEveryFailure(t *testing.T) {
	h := ValidateHandback{Commit: strings.Repeat("a", 40), Failures: []HandbackFailure{
		{Command: "go test ./...", Objective: "Fix Total.", Locations: []agentresult.RepairLocation{{Path: "a.go", Line: 4}, {Path: "b.go"}}},
		{Command: "make lint", Objective: "Find the cause."},
	}}
	text := ownerHandbackInstruction(h)
	for _, want := range []string{h.Commit, "1. `go test ./...`: Fix Total.", "a.go:4, b.go", "2. `make lint`: Find the cause.", "Validate again"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	if strings.Contains(strings.ToLower(text), "approved") || strings.Contains(strings.ToLower(text), "accepted") {
		t.Errorf("instruction approves: %q", text)
	}
	if strings.Contains(text, "Implement must") || !strings.Contains(text, ValidateStartCommand) {
		t.Errorf("instruction names a step the factory lacks: %q", text)
	}
}

func TestValidateRespondMapsTheRepairReportAndReviews(t *testing.T) {
	for _, response := range agentresult.RepairResponses {
		got, err := validateRespond(agentresult.Result{Outcome: "completed", Payload: agentresult.Payload{Validate: &agentresult.Validate{Repair: &agentresult.ValidateRepair{Response: response}}}})
		if err != nil || got != response {
			t.Errorf("%s: %q %v", response, got, err)
		}
	}
	for outcome, want := range map[string]string{"completed": "done", "incomplete": "blocked", "blocked": "blocked"} {
		if got, err := validateRespond(agentresult.Result{Outcome: outcome}); err != nil || got != want {
			t.Errorf("%s: %q %v", outcome, got, err)
		}
	}
	if _, err := validateRespond(agentresult.Result{Outcome: "approved"}); err == nil {
		t.Error("an unknown outcome was routed")
	}
}

func TestValidateReviewScopeRequiresPassedExecutedProofForEveryCriterion(t *testing.T) {
	stories := []ValidateStory{{Key: "s1", Revision: strings.Repeat("ab", 32), Criteria: []string{"AC-001"}, ExecutionRequired: []string{"AC-001"}}}
	scope := validateReviewScope("code", stories, "commit-x", strings.Repeat("cd", 32), []ValidateEvidence{{ID: "suite-1-0", State: store.SuitePassed}, {ID: "suite-1-1", State: store.SuiteFailed}, {ID: "suite-1-2", State: store.SuiteBlocked}})
	s := scope.Stories["s1"]
	if scope.ReviewRole != "code" || !s.SnapshotRetained || s.SnapshotID != "commit-x" || !reflect.DeepEqual(s.ExecutionRequired, []string{"AC-001"}) {
		t.Fatalf("%+v", scope)
	}
	if c := s.Checks["suite-1-0"]; !c.Executed || !c.Passed {
		t.Errorf("passed %+v", c)
	}
	if c := s.Checks["suite-1-1"]; !c.Executed || c.Passed {
		t.Errorf("failed %+v", c)
	}
	if c := s.Checks["suite-1-2"]; c.Executed || c.Passed {
		t.Errorf("blocked %+v", c)
	}
}

// A (Manual) criterion may be judged by reading; an (Auto) one still needs a
// passing executed check, and any check a (Manual) finding cites still counts.
func TestValidateReviewScopeLetsManualCriteriaBeJudgedByReading(t *testing.T) {
	stories := []ValidateStory{{Key: "s1", Revision: strings.Repeat("ab", 32), Criteria: []string{"AC-001", "AC-002"}, ExecutionRequired: []string{"AC-002"}}}
	ctx := validateReviewScope(agent.ReviewRoleAcceptance, stories, "commit-x", strings.Repeat("cd", 32), []ValidateEvidence{{ID: "suite-1-0", State: store.SuitePassed}, {ID: "suite-1-1", State: store.SuiteFailed}})
	scope := agent.ContributionScope{ContractVersion: agent.SubmissionContractVersion, WorkflowVersion: ctx.WorkflowVersion, Stage: "validate", ReviewRole: ctx.ReviewRole, Stories: ctx.Stories}
	finding := func(name, status string, checks ...string) agent.ContributionFinding {
		return agent.ContributionFinding{Name: name, Status: status, Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: append([]string{}, checks...), Summary: "internal/x.go:12 implements the behavior."}}}
	}
	parse := func(manual, auto agent.ContributionFinding) agent.ContributionDiagnostics {
		t.Helper()
		c := agent.Contribution{Stage: "validate", Story: "s1", Outcome: "assessed", Assessment: &agent.AssessmentContribution{SnapshotID: "commit-x", Criteria: []agent.ContributionFinding{manual, auto}, Reviews: []agent.ContributionFinding{finding("plan", "satisfied")}}}
		data, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		_, d := agent.ParseContribution(data, scope)
		return d
	}
	has := func(d agent.ContributionDiagnostics, path, code string) bool {
		for _, item := range d.Diagnostics {
			if item.Path == path && item.Code == code {
				return true
			}
		}
		return false
	}
	for _, status := range []string{"satisfied", "unmet"} {
		if d := parse(finding("AC-001", status), finding("AC-002", "satisfied", "suite-1-0")); !d.Valid || len(d.Diagnostics) != 0 {
			t.Errorf("manual %s by reading refused: %+v", status, d)
		}
	}
	if d := parse(finding("AC-001", "satisfied"), finding("AC-002", "satisfied")); d.Valid || !has(d, "$.assessment.criteria[1].evidence", "missing_execution") {
		t.Errorf("auto satisfied without a check: %+v", d)
	}
	if d := parse(finding("AC-001", "satisfied"), finding("AC-002", "satisfied", "suite-1-1")); d.Valid || !has(d, "$.assessment.criteria[1].evidence[0].check_ids[0]", "unproven") {
		t.Errorf("auto satisfied by a failed check: %+v", d)
	}
	if d := parse(finding("AC-001", "satisfied", "suite-9-9"), finding("AC-002", "satisfied", "suite-1-0")); d.Valid || !has(d, "$.assessment.criteria[0].evidence[0].check_ids[0]", "attribution") {
		t.Errorf("manual cited an unknown check: %+v", d)
	}
	if d := parse(finding("AC-001", "satisfied", "suite-1-1"), finding("AC-002", "satisfied", "suite-1-0")); d.Valid || !has(d, "$.assessment.criteria[0].evidence[0].check_ids[0]", "unproven") {
		t.Errorf("manual cited a failed check: %+v", d)
	}
}

func TestRepairOutcomeOfMapsEverySettledState(t *testing.T) {
	want := map[string]string{store.RepairKept: "kept", store.RepairTooBig: "too_big", store.RepairHandback: "handback", store.RepairBlocked: "blocked", store.RepairTimedOut: "unfinished", store.RepairInterrupted: "unfinished", store.RepairUnusable: "unfinished"}
	for state, outcome := range want {
		if got, ok := repairOutcomeOf(store.RepairRound{State: state}); !ok || got != outcome {
			t.Errorf("%s: %q %v", state, got, ok)
		}
	}
	if _, ok := repairOutcomeOf(store.RepairRound{State: store.RepairRunning}); ok {
		t.Error("a running round has no outcome yet")
	}
}

// A (Manual) criterion the acceptance reviewer satisfied by reading counts as
// satisfied in the report and leaves no owner check behind.
func TestRenderValidateReportCountsReviewerSatisfiedManualCriterion(t *testing.T) {
	st := twoStories()
	for i := range st.Findings {
		if st.Findings[i].ID == "b:AC-002" {
			st.Findings[i].State, st.Findings[i].Status = findingSatisfied, "satisfied"
		}
	}
	st.computeReadiness()
	out := renderValidateReport(st, twoTargets, "")
	row := rowOf(t, out, "b")
	if !strings.Contains(row, "2 of 2 satisfied") || strings.Contains(row, findingAwaitingCheck) || strings.Contains(out, ValidateCheckCommand) {
		t.Errorf("reviewer-satisfied criterion not counted as satisfied:\n%s", out)
	}
	if checks, _ := ownerItemCounts(st, st.Findings); checks != 0 {
		t.Errorf("owner checks %d", checks)
	}
}
