package agent_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func contributionFixture(stage string) (agent.ContributionScope, agent.Contribution) {
	revision := strings.Repeat("a", 64)
	digest := strings.Repeat("b", 64)
	scope := agent.ContributionScope{ContractVersion: 2, WorkflowVersion: 2, Stage: stage, Stories: map[string]agent.ContributionStory{
		"US-001": {Revision: revision, SnapshotID: "snapshot-1", ImplementationDigest: digest, SnapshotRetained: true, Criteria: []string{"AC-001"}, ExecutionRequired: []string{"AC-001"}, Checks: map[string]agent.RetainedContributionCheck{
			"check-1": {Story: "US-001", Revision: revision, SnapshotID: "snapshot-1", ImplementationDigest: digest, Executed: true, Passed: true, Retained: true},
		}},
	}}
	c := agent.Contribution{Stage: stage, Story: "US-001"}
	switch stage {
	case "engineering":
		c.Outcome = "engineering_complete"
		c.Engineering = &agent.EngineeringContribution{SnapshotID: "snapshot-1", CheckIDs: []string{"check-1"}}
	case "validate", "legacy":
		c.Outcome = "assessed"
		c.Assessment = &agent.AssessmentContribution{SnapshotID: "snapshot-1", Criteria: []agent.ContributionFinding{{Name: "AC-001", Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{"check-1"}, Summary: "Retained execution demonstrates the required behavior."}}}}, Reviews: []agent.ContributionFinding{}}
		for _, name := range []string{"code", "architecture", "plan"} {
			c.Assessment.Reviews = append(c.Assessment.Reviews, agent.ContributionFinding{Name: name, Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{}, Summary: "Reviewed the retained source."}}})
		}
		if stage == "legacy" {
			scope.WorkflowVersion = 1
			c.Outcome = "verified"
			c.Engineering = &agent.EngineeringContribution{SnapshotID: "snapshot-1", CheckIDs: []string{"check-1"}}
		}
	case "plan":
		c.Story = ""
		c.Outcome = "discussion"
		c.Context = &agent.PlanningDiscussion{Scope: "Typed submissions", Decisions: "Use retained references", Assumptions: "None", Questions: "None"}
	}
	return scope, c
}

func contributionJSON(t *testing.T, c agent.Contribution) []byte {
	t.Helper()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func requireContributionValid(t *testing.T, data []byte, scope agent.ContributionScope) {
	t.Helper()
	_, d := agent.ParseContribution(data, scope)
	if !d.Valid || !d.Complete || len(d.Diagnostics) != 0 || len(d.UncheckedPaths) != 0 {
		t.Fatalf("valid contribution rejected: %+v", d)
	}
}

func requireContributionInvalid(t *testing.T, data []byte, scope agent.ContributionScope, path string) agent.ContributionDiagnostics {
	t.Helper()
	c, d := agent.ParseContribution(data, scope)
	if d.Valid || !reflect.DeepEqual(c, agent.Contribution{}) {
		t.Fatalf("invalid contribution returned usable result: %+v %+v", c, d)
	}
	if len(d.Diagnostics) == 0 {
		t.Fatalf("invalid contribution has no explanation: %+v", d)
	}
	found := false
	for _, item := range d.Diagnostics {
		if item.Path == path {
			found = true
		}
		if item.Path == "" || item.Code == "" || item.Expected == "" || item.Actual == "" || item.Message == "" {
			t.Fatalf("incomplete diagnostic: %+v", item)
		}
	}
	if !found {
		t.Fatalf("missing diagnostic at %s: %+v", path, d)
	}
	return d
}

func TestContributionStageContracts(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"plan", "engineering", "validate", "legacy"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			scope, c := contributionFixture(stage)
			requireContributionValid(t, contributionJSON(t, c), scope)
		})
	}
	for _, stage := range []string{"engineering", "legacy"} {
		for _, outcome := range []string{"blocked", "invalidated"} {
			t.Run(stage+"/"+outcome, func(t *testing.T) {
				t.Parallel()
				scope, _ := contributionFixture(stage)
				c := agent.Contribution{Stage: stage, Story: "US-001", Outcome: outcome, Reason: "Required retained check is unavailable."}
				requireContributionValid(t, contributionJSON(t, c), scope)
			})
		}
	}
	for _, tc := range []struct {
		name   string
		stage  string
		mutate func(*agent.ContributionScope, *agent.Contribution)
	}{
		{"wrong stage", "engineering", func(s *agent.ContributionScope, _ *agent.Contribution) { s.Stage = "validate" }},
		{"engineering under legacy workflow", "engineering", func(s *agent.ContributionScope, _ *agent.Contribution) { s.WorkflowVersion = 1 }},
		{"legacy under separated workflow", "legacy", func(s *agent.ContributionScope, _ *agent.Contribution) { s.WorkflowVersion = 2 }},
		{"validate under legacy workflow", "validate", func(s *agent.ContributionScope, _ *agent.Contribution) { s.WorkflowVersion = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, c := contributionFixture(tc.stage)
			tc.mutate(&s, &c)
			requireContributionInvalid(t, contributionJSON(t, c), s, "$.stage")
		})
	}
}

func TestContributionRejectsEnvelopeOverrides(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"version", "parent", "parent_id", "run_id", "generation", "issue_id", "issue_number", "revision", "workflow_version", "sequence", "attempt", "submission_id", "receipt_id"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			s, c := contributionFixture("engineering")
			data := contributionJSON(t, c)
			data = append(append([]byte{}, data[:len(data)-1]...), []byte(`,"`+field+`":5399889036}`)...)
			requireContributionInvalid(t, data, s, "$."+field)
		})
	}
}

func TestContributionEvidenceListB(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		count   int
		summary string
		valid   bool
	}{
		{"one", 1, "one result", true}, {"sixteen", 16, "one result", true}, {"zero", 0, "unused", false}, {"seventeen", 17, "one result", false},
		{"exact UTF8 byte boundary", 1, strings.Repeat("é", 4096), true}, {"UTF8 boundary exceeded", 1, strings.Repeat("é", 4096) + "x", false}, {"blank summary", 1, " \n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, c := contributionFixture("validate")
			e := make([]agent.EvidenceExplanation, tc.count)
			for i := range e {
				e[i] = agent.EvidenceExplanation{CheckIDs: []string{"check-1"}, Summary: tc.summary}
			}
			c.Assessment.Criteria[0].Evidence = e
			if tc.valid {
				requireContributionValid(t, contributionJSON(t, c), s)
			} else {
				path := "$.assessment.criteria[0].evidence"
				if tc.count == 1 {
					path += "[0].summary"
				}
				requireContributionInvalid(t, contributionJSON(t, c), s, path)
			}
		})
	}
	for _, value := range []string{`"two successful tests"`, `["first test passed","second test passed"]`, `null`} {
		t.Run("wrong evidence "+value, func(t *testing.T) {
			t.Parallel()
			s, c := contributionFixture("validate")
			data := contributionJSON(t, c)
			old, err := json.Marshal(c.Assessment.Criteria[0].Evidence)
			if err != nil {
				t.Fatal(err)
			}
			data = []byte(strings.Replace(string(data), string(old), value, 1))
			path := "$.assessment.criteria[0].evidence"
			if strings.HasPrefix(value, "[") {
				path += "[0]"
			}
			requireContributionInvalid(t, data, s, path)
		})
	}
}

func TestContributionDiagnosticsAggregateAndIncomplete(t *testing.T) {
	t.Parallel()
	s, _ := contributionFixture("validate")
	data := []byte(`{"stage":"validate","outcome":"assessed","story":"US-001","parent":5399889036,"assessment":{"snapshot_id":"snapshot-1","criteria":[{"name":"AC-001","status":"satisfied","classification":"blocking","evidence":[{"check_ids":[],"summary":42}]}],"reviews":null}}`)
	d := requireContributionInvalid(t, data, s, "$.parent")
	for _, path := range []string{"$.assessment.criteria[0].evidence[0].summary", "$.assessment.reviews"} {
		found := false
		for _, item := range d.Diagnostics {
			if item.Path == path {
				found = true
			}
		}
		if !found {
			t.Fatalf("independent defect missing at %s: %+v", path, d)
		}
	}
	if d.Complete || len(d.UncheckedPaths) == 0 {
		t.Fatalf("uninspectable containers claimed complete: %+v", d)
	}
	for i := 0; i < 10; i++ {
		repeat := agent.PreflightContribution(data, s)
		if !reflect.DeepEqual(d, repeat) {
			t.Fatalf("unstable diagnostics: %+v vs %+v", d, repeat)
		}
	}
	for _, raw := range []string{`{`, `[]`, `{"stage":"engineering","outcome":"engineering_complete","story":"US-001","engineering":42}`, `{"stage":"engineering"} {}`} {
		t.Run(raw, func(t *testing.T) {
			scope, _ := contributionFixture("engineering")
			_, result := agent.ParseContribution([]byte(raw), scope)
			if result.Valid || result.Complete || len(result.Diagnostics) == 0 || len(result.UncheckedPaths) == 0 {
				t.Fatalf("malformed input claimed checked: %+v", result)
			}
		})
	}
}

func TestContributionPreflightMatchesSubmissionWithoutMutatingScope(t *testing.T) {
	t.Parallel()
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "invalid"}[invalid], func(t *testing.T) {
			t.Parallel()
			s, c := contributionFixture("engineering")
			before, err := json.Marshal(s.Stories)
			if err != nil {
				t.Fatal(err)
			}
			if invalid {
				c.Engineering.CheckIDs = []string{"unknown"}
			}
			data := contributionJSON(t, c)
			_, submit := agent.ParseContribution(data, s)
			preflight := agent.PreflightContribution(data, s)
			after, err := json.Marshal(s.Stories)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(submit, preflight) {
				t.Fatalf("preflight differs: %+v vs %+v", preflight, submit)
			}
			if string(before) != string(after) {
				t.Fatal("validation changed frozen scope or retained evidence")
			}
		})
	}
}

func TestContributionFrozenCoverageAndAttribution(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path string
		mutate     func(*agent.ContributionScope, *agent.Contribution)
	}{
		{"unknown criterion", "$.assessment.criteria[0].name", func(_ *agent.ContributionScope, c *agent.Contribution) { c.Assessment.Criteria[0].Name = "AC-999" }},
		{"missing criterion", "$.assessment.criteria", func(_ *agent.ContributionScope, c *agent.Contribution) {
			c.Assessment.Criteria = []agent.ContributionFinding{}
		}},
		{"duplicate criterion", "$.assessment.criteria[1].name", func(_ *agent.ContributionScope, c *agent.Contribution) {
			c.Assessment.Criteria = append(c.Assessment.Criteria, c.Assessment.Criteria[0])
		}},
		{"missing review", "$.assessment.reviews", func(_ *agent.ContributionScope, c *agent.Contribution) {
			c.Assessment.Reviews = c.Assessment.Reviews[:2]
		}},
		{"unknown check", "$.assessment.criteria[0].evidence[0].check_ids[0]", func(_ *agent.ContributionScope, c *agent.Contribution) {
			c.Assessment.Criteria[0].Evidence[0].CheckIDs = []string{"missing"}
		}},
		{"required execution absent", "$.assessment.criteria[0].evidence", func(_ *agent.ContributionScope, c *agent.Contribution) {
			c.Assessment.Criteria[0].Evidence[0].CheckIDs = []string{}
		}},
		{"snapshot mismatch", "$.assessment.snapshot_id", func(_ *agent.ContributionScope, c *agent.Contribution) { c.Assessment.SnapshotID = "another" }},
		{"owner waiver forbidden", "$.assessment.criteria[0].status", func(_ *agent.ContributionScope, c *agent.Contribution) { c.Assessment.Criteria[0].Status = "waived" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, c := contributionFixture("validate")
			tc.mutate(&s, &c)
			requireContributionInvalid(t, contributionJSON(t, c), s, tc.path)
		})
	}
	for _, field := range []string{"story", "revision", "snapshot", "digest", "retained", "executed", "passed"} {
		t.Run("check "+field, func(t *testing.T) {
			t.Parallel()
			s, c := contributionFixture("validate")
			story := s.Stories["US-001"]
			check := story.Checks["check-1"]
			switch field {
			case "story":
				check.Story = "US-002"
			case "revision":
				check.Revision = strings.Repeat("c", 64)
			case "snapshot":
				check.SnapshotID = "other"
			case "digest":
				check.ImplementationDigest = strings.Repeat("c", 64)
			case "retained":
				check.Retained = false
			case "executed":
				check.Executed = false
			case "passed":
				check.Passed = false
			}
			story.Checks["check-1"] = check
			s.Stories["US-001"] = story
			requireContributionInvalid(t, contributionJSON(t, c), s, "$.assessment.criteria[0].evidence[0].check_ids[0]")
		})
	}
}

func TestContributionMissingEvidenceRemainsUnverified(t *testing.T) {
	t.Parallel()
	s, c := contributionFixture("validate")
	c.Assessment.Criteria[0].Status = "unverified"
	c.Assessment.Criteria[0].Evidence = []agent.EvidenceExplanation{{CheckIDs: []string{}, Summary: "The required executed result is unavailable."}}
	requireContributionValid(t, contributionJSON(t, c), s)
	// Pure source findings can use no execution references when execution is not required.
	story := s.Stories["US-001"]
	story.ExecutionRequired = []string{}
	s.Stories["US-001"] = story
	c.Assessment.Criteria[0].Status = "satisfied"
	c.Assessment.Criteria[0].Evidence[0].Summary = "The retained source contains the required declaration."
	requireContributionValid(t, contributionJSON(t, c), s)
}

func TestContributionPlanConsumerDiagnostics(t *testing.T) {
	t.Parallel()
	s, _ := contributionFixture("plan")
	data := []byte(`{"stage":"plan","outcome":"completed","plan":{"summary":"Typed content","stories":[]}}`)
	requireContributionInvalid(t, data, s, "$.plan")
	s.ValidatePlan = func(data json.RawMessage) agent.ContributionDiagnostics {
		return agent.ContributionDiagnostics{Valid: false, Complete: true, Diagnostics: []agent.ContributionDiagnostic{{Path: "$.stories", Code: "count", Expected: "nonempty story array", Actual: "empty array", Message: "Supply the complete story set."}}, UncheckedPaths: []string{}}
	}
	requireContributionInvalid(t, data, s, "$.plan.stories")
}

func TestContributionStrictFieldsAndBounds(t *testing.T) {
	t.Parallel()
	s, c := contributionFixture("engineering")
	base := string(contributionJSON(t, c))
	for _, tc := range []struct{ name, raw, path string }{
		{"duplicate field", strings.Replace(base, `"story":"US-001"`, `"story":"US-001","story":"US-001"`, 1), "$.story"},
		{"missing field", strings.Replace(base, `"story":"US-001",`, "", 1), "$.story"},
		{"unknown field", strings.Replace(base, `"story":"US-001"`, `"story":"US-001","unrecognized":true`, 1), "$.unrecognized"},
		{"null check collection", strings.Replace(base, `["check-1"]`, `null`, 1), "$.engineering.check_ids"},
		{"wrong check type", strings.Replace(base, `["check-1"]`, `[99]`, 1), "$.engineering.check_ids[0]"},
		{"inactive assessment", strings.TrimSuffix(base, "}") + `,"assessment":{}}`, "$.assessment"},
		{"oversized stream", strings.Repeat(" ", agent.MaxContributionBytes+1), "$"},
	} {
		t.Run(tc.name, func(t *testing.T) { t.Parallel(); requireContributionInvalid(t, []byte(tc.raw), s, tc.path) })
	}
}

func TestContributionCapacity256CriteriaWithSharedProof(t *testing.T) {
	t.Parallel()
	s, c := contributionFixture("validate")
	story := s.Stories["US-001"]
	story.Criteria = []string{}
	story.ExecutionRequired = []string{}
	c.Assessment.Criteria = []agent.ContributionFinding{}
	for i := 1; i <= 256; i++ {
		id := fmt.Sprintf("AC-%03d", i)
		story.Criteria = append(story.Criteria, id)
		story.ExecutionRequired = append(story.ExecutionRequired, id)
		c.Assessment.Criteria = append(c.Assessment.Criteria, agent.ContributionFinding{Name: id, Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{"check-1"}, Summary: "The retained comprehensive execution covers this obligation."}}})
	}
	s.Stories["US-001"] = story
	data := contributionJSON(t, c)
	if len(data) > agent.MaxContributionBytes {
		t.Fatalf("256 short criteria exceed metadata budget: %d", len(data))
	}
	requireContributionValid(t, data, s)
}

func TestContributionLegacyTextEvidenceKeepsOriginalMeaning(t *testing.T) {
	t.Parallel()
	revision := strings.Repeat("a", 64)
	snapshot := strings.Repeat("b", 64)
	context := &agent.ValidationContext{Version: 1, Parent: "github:o/r#1", Generation: 1, Stories: []agent.ValidationStory{{Story: agent.ApprovedStory{Key: "US-1", IssueID: 12, Revision: revision}, Criteria: []string{"AC-1"}, Snapshot: snapshot, EvidenceStatus: "satisfied"}}}
	findings := []agent.ValidationFinding{{Name: "AC-1", Status: "satisfied", Classification: "blocking", Evidence: "First retained test passed. Second retained test passed."}}
	reviews := []agent.ValidationFinding{}
	for _, name := range []string{"code", "architecture", "plan"} {
		reviews = append(reviews, agent.ValidationFinding{Name: name, Status: "satisfied", Classification: "blocking", Evidence: "Reviewed retained source."})
	}
	out := agent.ValidationOutput{Version: 1, Parent: context.Parent, Generation: 1, Stories: []agent.ValidationResult{{Key: "US-1", IssueID: 12, Revision: revision, Snapshot: snapshot, Criteria: findings, Reviews: reviews}}}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := agent.ParseValidationOutput(data, context)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Stories[0].Criteria[0].Evidence != findings[0].Evidence {
		t.Fatal("legacy evidence text changed")
	}
	wrong := strings.Replace(string(data), `"First retained test passed. Second retained test passed."`, `["First retained test passed.","Second retained test passed."]`, 1)
	if _, err := agent.ParseValidationOutput([]byte(wrong), context); err == nil {
		t.Fatal("legacy text contract accepted list evidence")
	}
}

func TestContributionCompletedPlanUsesSuppliedValidator(t *testing.T) {
	t.Parallel()
	scope, _ := contributionFixture("plan")
	plan := `{"summary":"Approved scope","stories":[]}`
	data := []byte(`{"stage":"plan","outcome":"completed","plan":` + plan + `}`)
	observed := ""
	scope.ValidatePlan = func(raw json.RawMessage) agent.ContributionDiagnostics {
		observed = string(raw)
		return agent.ContributionDiagnostics{Valid: true, Complete: true, Diagnostics: []agent.ContributionDiagnostic{}, UncheckedPaths: []string{}}
	}
	requireContributionValid(t, data, scope)
	if observed != plan {
		t.Fatalf("validator received %s, want exact plan %s", observed, plan)
	}
	scope.ValidatePlan = func(raw json.RawMessage) agent.ContributionDiagnostics {
		return agent.ContributionDiagnostics{Complete: false, Diagnostics: []agent.ContributionDiagnostic{{Path: "$.stories", Code: "type", Expected: "array", Actual: "unavailable", Message: "Restore inspectable stories."}}, UncheckedPaths: []string{"$.stories"}}
	}
	result := requireContributionInvalid(t, data, scope, "$.plan.stories")
	if result.Complete || !reflect.DeepEqual(result.UncheckedPaths, []string{"$.plan.stories"}) {
		t.Fatalf("lost incomplete Plan diagnostic: %+v", result)
	}
}
