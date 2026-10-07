package agentresult

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

var roleDigest = strings.Repeat("ab", 32)

func roleContext(role string) Context {
	checks := map[string]agent.RetainedContributionCheck{
		"pass": {Story: "s1", Revision: roleDigest, SnapshotID: "snap", ImplementationDigest: roleDigest, Executed: true, Passed: true, Retained: true},
		"fail": {Story: "s1", Revision: roleDigest, SnapshotID: "snap", ImplementationDigest: roleDigest, Executed: true, Passed: false, Retained: true},
	}
	return Context{SchemaVersion: SchemaVersion, Mode: "validate", WorkflowVersion: 2, ReviewRole: role, Stories: map[string]agent.ContributionStory{
		"s1": {Revision: roleDigest, SnapshotID: "snap", ImplementationDigest: roleDigest, SnapshotRetained: true, Criteria: []string{"AC-001"}, ExecutionRequired: []string{"AC-001"}, Checks: checks},
	}}
}

func finding(name, status, class string, ids ...string) agent.ContributionFinding {
	if ids == nil {
		ids = []string{}
	}
	return agent.ContributionFinding{Name: name, Status: status, Classification: class, Evidence: []agent.EvidenceExplanation{{Summary: "seen", CheckIDs: ids}}}
}

func assessment(snapshot string, criteria, reviews []agent.ContributionFinding) Result {
	if criteria == nil {
		criteria = []agent.ContributionFinding{}
	}
	return Result{Outcome: "completed", Comment: "Reviewed.", Payload: Payload{Validate: &Validate{Stories: []AssessmentStory{{Story: "s1", SnapshotID: snapshot, Criteria: criteria, Reviews: reviews}}}}}
}

func valid(t *testing.T, r Result, ctx Context) Diagnostics {
	t.Helper()
	return ValidateResult(r, ctx)
}

// AC-006: each role is required to cover only its own part.
func TestReviewerCoverageIsScopedPerRole(t *testing.T) {
	acceptance := assessment("snap", []agent.ContributionFinding{finding("AC-001", "satisfied", "blocking", "pass")}, []agent.ContributionFinding{finding("plan", "satisfied", "advisory")})
	code := assessment("snap", nil, []agent.ContributionFinding{finding("code", "satisfied", "advisory")})
	architecture := assessment("snap", nil, []agent.ContributionFinding{finding("architecture", "unverified", "advisory")})
	for role, r := range map[string]Result{"acceptance": acceptance, "code": code, "architecture": architecture} {
		if d := valid(t, r, roleContext(role)); !d.Valid || !d.Complete {
			t.Errorf("%s refused its own coverage: %+v", role, d.Diagnostics)
		}
	}
	// A role that leaves out a part it must cover is refused.
	for role, r := range map[string]Result{
		"acceptance":   assessment("snap", nil, []agent.ContributionFinding{finding("plan", "satisfied", "advisory")}),
		"code":         assessment("snap", nil, nil),
		"architecture": code,
	} {
		if d := valid(t, r, roleContext(role)); d.Valid {
			t.Errorf("%s was accepted with missing or foreign coverage", role)
		}
	}
	// The acceptance role may not cover another role's reviews.
	extra := assessment("snap", []agent.ContributionFinding{finding("AC-001", "satisfied", "blocking", "pass")}, []agent.ContributionFinding{finding("plan", "satisfied", "advisory"), finding("code", "satisfied", "advisory")})
	if d := valid(t, extra, roleContext("acceptance")); d.Valid {
		t.Error("the acceptance role covered the code review")
	}
	// With no role the full coverage is still required, as before.
	if d := valid(t, acceptance, roleContext("")); d.Valid {
		t.Error("an unscoped assessment with only the acceptance part was accepted")
	}
	full := assessment("snap", []agent.ContributionFinding{finding("AC-001", "satisfied", "blocking", "pass")}, []agent.ContributionFinding{finding("code", "satisfied", "advisory"), finding("architecture", "satisfied", "advisory"), finding("plan", "satisfied", "advisory")})
	if d := valid(t, full, roleContext("")); !d.Valid {
		t.Errorf("unscoped full coverage refused: %+v", d.Diagnostics)
	}
}

// AC-006: a criterion is satisfied only by citing a passed command of the same
// snapshot; the snapshot must be the tested commit's.
func TestReviewerProofRules(t *testing.T) {
	ctx := roleContext("acceptance")
	reviews := []agent.ContributionFinding{finding("plan", "unverified", "advisory")}
	cases := map[string]struct {
		r    Result
		want bool
	}{
		"passed command":        {assessment("snap", []agent.ContributionFinding{finding("AC-001", "satisfied", "blocking", "pass")}, reviews), true},
		"failed command":        {assessment("snap", []agent.ContributionFinding{finding("AC-001", "satisfied", "blocking", "fail")}, reviews), false},
		"no command":            {assessment("snap", []agent.ContributionFinding{finding("AC-001", "satisfied", "blocking")}, reviews), false},
		"invented command":      {assessment("snap", []agent.ContributionFinding{finding("AC-001", "satisfied", "blocking", "made-up")}, reviews), false},
		"unverified":            {assessment("snap", []agent.ContributionFinding{finding("AC-001", "unverified", "blocking")}, reviews), true},
		"unmet":                 {assessment("snap", []agent.ContributionFinding{finding("AC-001", "unmet", "blocking", "fail")}, reviews), true},
		"another snapshot":      {assessment("other", []agent.ContributionFinding{finding("AC-001", "unverified", "blocking")}, reviews), false},
		"an advisory criterion": {assessment("snap", []agent.ContributionFinding{finding("AC-001", "unverified", "advisory")}, reviews), false},
	}
	for name, c := range cases {
		if d := valid(t, c.r, ctx); d.Valid != c.want {
			t.Errorf("%s: valid=%v want %v: %+v", name, d.Valid, c.want, d.Diagnostics)
		}
	}
}

func repairResult(outcome string, r *ValidateRepair) Result {
	return Result{Outcome: outcome, Comment: "Repair.", Reason: map[bool]string{true: "stopped"}[outcome != "completed"], Payload: Payload{Validate: &Validate{Repair: r}}}
}

func repairContext() Context {
	return Context{SchemaVersion: SchemaVersion, Mode: "validate", WorkflowVersion: 2, RepairRole: true, WriteAccess: true}
}

// The repair report is valid only under the repair role binding.
func TestRepairReportContract(t *testing.T) {
	failure := RepairFailure{Command: "go test ./...", Observed: "fails", Cause: "wrong sum", Locations: []RepairLocation{{Path: "a.go", Line: 3}}, FilesChanged: []string{"a.go"}, Fixed: "Fixed."}
	handed := RepairFailure{Command: "go test ./...", Observed: "fails", Cause: RepairUnknownCause, Expected: "passes", Objective: "Find the cause."}
	cases := map[string]struct {
		r    Result
		ctx  Context
		want bool
	}{
		"repaired":                   {repairResult("completed", &ValidateRepair{Response: "repaired", Failures: []RepairFailure{failure}}), repairContext(), true},
		"handback":                   {repairResult("completed", &ValidateRepair{Response: "handback", Failures: []RepairFailure{handed}}), repairContext(), true},
		"blocked":                    {repairResult("blocked", &ValidateRepair{Response: "blocked", Reason: "no output"}), repairContext(), true},
		"outside the repair role":    {repairResult("completed", &ValidateRepair{Response: "repaired", Failures: []RepairFailure{failure}}), roleContext("code"), false},
		"missing report":             {repairResult("completed", nil), repairContext(), false},
		"unknown response":           {repairResult("completed", &ValidateRepair{Response: "approved", Failures: []RepairFailure{failure}}), repairContext(), false},
		"repaired with no files":     {repairResult("completed", &ValidateRepair{Response: "repaired", Failures: []RepairFailure{{Command: "c", Observed: "o", Cause: "x", Fixed: "f"}}}), repairContext(), false},
		"handback with no objective": {repairResult("completed", &ValidateRepair{Response: "handback", Failures: []RepairFailure{{Command: "c", Observed: "o", Cause: "x", Expected: "e"}}}), repairContext(), false},
		"blocked with no reason":     {repairResult("blocked", &ValidateRepair{Response: "blocked"}), repairContext(), false},
		"blocked but completed":      {repairResult("completed", &ValidateRepair{Response: "blocked", Reason: "r"}), repairContext(), false},
		"no failures":                {repairResult("completed", &ValidateRepair{Response: "repaired"}), repairContext(), false},
		"unsafe path":                {repairResult("completed", &ValidateRepair{Response: "repaired", Failures: []RepairFailure{{Command: "c", Observed: "o", Cause: "x", Fixed: "f", FilesChanged: []string{"../a.go"}}}}), repairContext(), false},
		"unknown change kind":        {repairResult("completed", &ValidateRepair{Response: "repaired", Failures: []RepairFailure{{Command: "c", Observed: "o", Cause: "x", Fixed: "f", FilesChanged: []string{"a.go"}, ChangeKinds: []string{"magic"}}}}), repairContext(), false},
		"stories with the report":    {Result{Outcome: "completed", Comment: "x", Payload: Payload{Validate: &Validate{Repair: &ValidateRepair{Response: "repaired", Failures: []RepairFailure{failure}}, Stories: []AssessmentStory{{Story: "s1"}}}}}, repairContext(), false},
	}
	for name, c := range cases {
		if d := ValidateResult(c.r, c.ctx); d.Valid != c.want {
			t.Errorf("%s: valid=%v want %v: %+v", name, d.Valid, c.want, d.Diagnostics)
		}
	}
	// A repair role must send a report, not an assessment.
	if d := ValidateResult(assessment("snap", nil, nil), repairContext()); d.Valid {
		t.Error("an assessment was accepted from the repair role")
	}
}

func nodeJSON(t *testing.T, response string, r Result) []byte {
	t.Helper()
	raw, err := json.Marshal(ResearchNode{Version: 1, Response: response, Result: r})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDecodeValidateNode(t *testing.T) {
	rep := repairResult("completed", &ValidateRepair{Response: "repaired", Failures: []RepairFailure{{Command: "c", Observed: "o", Cause: "x", Fixed: "f", FilesChanged: []string{"a.go"}}}})
	if _, d := DecodeValidateNode(nodeJSON(t, "repaired", rep), repairContext()); !d.Valid {
		t.Fatalf("a repaired node was refused: %+v", d.Diagnostics)
	}
	for _, response := range []string{"handback", "done", "approved", ""} {
		if _, d := DecodeValidateNode(nodeJSON(t, response, rep), repairContext()); d.Valid {
			t.Errorf("response %q was accepted for a repaired report", response)
		}
	}
	review := assessment("snap", nil, []agent.ContributionFinding{finding("code", "satisfied", "advisory")})
	if _, d := DecodeValidateNode(nodeJSON(t, "done", review), roleContext("code")); !d.Valid {
		t.Fatalf("a finished review was refused: %+v", d.Diagnostics)
	}
	if _, d := DecodeValidateNode(nodeJSON(t, "approved", review), roleContext("code")); d.Valid {
		t.Error("approved was accepted as a reviewer response")
	}
	unfinished := Result{Outcome: "blocked", Comment: "x", Reason: "r", Payload: Payload{Validate: &Validate{}}}
	if _, d := DecodeValidateNode(nodeJSON(t, "blocked", unfinished), roleContext("code")); !d.Valid {
		t.Fatalf("a blocked review was refused: %+v", d.Diagnostics)
	}
	if _, d := DecodeValidateNode(nodeJSON(t, "done", unfinished), roleContext("code")); d.Valid {
		t.Error("done was accepted for a blocked review")
	}
	other := roleContext("code")
	other.Mode = "spike"
	if _, d := DecodeValidateNode(nodeJSON(t, "done", review), other); d.Valid {
		t.Error("a Spike invocation decoded as Validate")
	}
}
