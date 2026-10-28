package coverage

import (
	"slices"
	"testing"
)

func plannerFixture() PlanInput {
	units := []Unit{{Key: "production", Files: []string{"src/service.go"}}, {Key: "affected", Dependencies: []string{"production"}, Files: []string{"tests/affected_test.go"}}, {Key: "unchanged", Files: []string{"tests/unchanged_test.go"}}, {Key: "unfinished", Files: []string{"tests/unfinished_test.go"}}, {Key: "failed", Files: []string{"tests/failed_test.go"}}}
	previous := Manifest{Version: Version, Commit: "previous", Recipe: "recipe", Complete: true, Inventory: Inventory{Commit: "previous", Conditions: "conditions", Complete: true, Units: units}}
	for _, u := range units {
		previous.Results = append(previous.Results, Result{Unit: u.Key, Kind: Executed, Origin: Origin{Unit: u.Key, Commit: "original", Evidence: "evidence-" + u.Key, Conditions: "conditions"}})
	}
	previous.Results[3].Kind = Unresolved
	previous.Results[4].Kind = Unresolved
	previous.Results[4].Cases = []Observation{{Unit: "failed", Case: "TestRepair/parameter[1]", Status: "fail", Complete: true}}
	return PlanInput{Current: Inventory{Commit: "current", Conditions: "conditions", Complete: true, Units: units}, Previous: &previous, Recipe: "recipe", Changes: map[string]Changes{"original": {Anchor: "original", Paths: []string{"src/service.go"}, Proven: true}}}
}

func TestPlannerFailureAffectedUnfinishedAndInherited(t *testing.T) {
	in := plannerFixture()
	plan := Build(in)
	if plan.Full {
		t.Fatalf("unexpected full: %+v", plan)
	}
	if len(plan.Work) == 0 || plan.Work[0].Purpose != "prior-failure" || !slices.Equal(plan.Work[0].Cases, []string{"TestRepair/parameter[1]"}) {
		t.Fatalf("failures not first: %+v", plan.Work)
	}
	for _, key := range []string{"production", "affected", "unfinished", "failed"} {
		if !slices.ContainsFunc(plan.Work, func(w Work) bool { return w.Unit.Key == key }) {
			t.Errorf("missing obligation %s", key)
		}
	}
	if len(plan.Inherited) != 1 || plan.Inherited[0].Unit != "unchanged" || plan.Inherited[0].Origin.Commit != "original" {
		t.Fatalf("invalid inherited coverage: %+v", plan.Inherited)
	}
	if slices.ContainsFunc(plan.Work, func(w Work) bool { return w.Unit.Key == "unchanged" }) {
		t.Fatal("unaffected unit executed")
	}
}

func TestPlannerUsesOriginalAnchorAndOldDependencies(t *testing.T) {
	in := plannerFixture()
	// The dependency was removed in the current snapshot, but its old source
	// still invalidates the dependent test after a merged-base deletion.
	in.Current.Units = slices.Clone(in.Current.Units)
	in.Current.Units[1].Dependencies = nil
	in.Current.Units[0].Files = []string{"src/replacement.go"}
	in.Changes["original"] = Changes{Anchor: "original", Paths: []string{"src/service.go"}, Proven: true}
	plan := Build(in)
	if !slices.ContainsFunc(plan.Work, func(w Work) bool { return w.Unit.Key == "affected" && w.Purpose == "affected" }) {
		t.Fatalf("old graph ignored: %+v", plan)
	}
	// Changing only the immediate previous commit cannot establish the original
	// unchanged unit's coverage.
	in.Changes = map[string]Changes{"previous": {Anchor: "previous", Proven: true}}
	if plan := Build(in); !plan.Full {
		t.Fatalf("missing original ancestry did not force full: %+v", plan)
	}
}

func TestPlannerConservativeFullFallback(t *testing.T) {
	cases := []struct {
		name   string
		change func(*PlanInput)
	}{
		{"fresh full", func(in *PlanInput) { in.Full = true }},
		{"old record", func(in *PlanInput) { in.Previous.Version = 0 }},
		{"unfinished capture", func(in *PlanInput) { in.Previous.Complete = false }},
		{"unknown inventory", func(in *PlanInput) { in.Current.Complete = false }},
		{"changed recipe", func(in *PlanInput) { in.Recipe = "new" }},
		{"changed conditions", func(in *PlanInput) { in.Current.Conditions = "race" }},
		{"shared fixture", func(in *PlanInput) {
			in.Changes["original"] = Changes{Anchor: "original", Proven: true, Paths: []string{"tests/conftest.py"}}
		}},
		{"unproven ancestry", func(in *PlanInput) { in.Changes["original"] = Changes{Anchor: "original", Proven: false} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := plannerFixture()
			tc.change(&in)
			plan := Build(in)
			if !plan.Full || len(plan.Inherited) != 0 || countWorkPurpose(plan.Work, "full") != len(in.Current.Units) {
				t.Fatalf("unsafe fallback: %+v", plan)
			}
		})
	}
}

func assessmentFixture() Manifest {
	observation := Observation{Unit: "unit", Case: "Tests/parameter[1]", Status: "fail", Diagnostic: "assertion: expected 1 at test.go:12", Complete: true}
	attempt := func(id, purpose, commit string) Attempt {
		return Attempt{ID: id, Purpose: purpose, Commit: commit, Conditions: "conditions", Complete: true, Fresh: true, Observations: []Observation{observation}}
	}
	return Manifest{Commit: "feature", BaseCommit: "pinned-base", Inventory: Inventory{Commit: "feature", Conditions: "conditions", Complete: true, Units: []Unit{{Key: "unit"}}}, Attempts: []Attempt{attempt("original", "affected", "feature"), attempt("retry", "retry", "feature"), attempt("base", "base", "pinned-base")}}
}

func TestAssessmentStrictPinnedBaseMatching(t *testing.T) {
	m := assessmentFixture()
	got := Assess(m)
	if !got.Complete || got.Outcome != "covered" || len(got.Results) != 1 || got.Results[0].Kind != PreExisting || len(got.Comparisons) != 1 || got.Comparisons[0].BaseCommit != "pinned-base" {
		t.Fatalf("matching base: %+v", got)
	}
	if got.Attempts[0].Observations[0].Status != "fail" {
		t.Fatal("raw failure history changed")
	}
	for _, tc := range []struct {
		name   string
		change func(*Manifest)
	}{
		{"wrong parameter", func(m *Manifest) { m.Attempts[2].Observations[0].Case = "Tests/parameter[2]" }},
		{"different diagnostic", func(m *Manifest) { m.Attempts[2].Observations[0].Diagnostic = "another assertion" }},
		{"truncated base", func(m *Manifest) { m.Attempts[2].Complete = false }},
		{"moving base", func(m *Manifest) { m.Attempts[2].Commit = "new-base" }},
		{"different conditions", func(m *Manifest) { m.Attempts[2].Conditions = "other-environment" }},
		{"cached retry", func(m *Manifest) { m.Attempts[1].Fresh = false }},
		{"skipped retry", func(m *Manifest) { m.Attempts[1].Observations[0].Status = "skip" }},
		{"deleted retry", func(m *Manifest) { m.Attempts[1].Observations = nil }},
		{"skipped original", func(m *Manifest) { m.Attempts[0].Observations[0].Status = "skip" }},
		{"unobserved original", func(m *Manifest) { m.Attempts[0].Observations = nil }},
		{"incomplete original case", func(m *Manifest) { m.Attempts[0].Observations[0].Complete = false }},
		{"wrong feature commit", func(m *Manifest) { m.Attempts[0].Commit = "older-feature" }},
		{"unknown original status", func(m *Manifest) { m.Attempts[0].Observations[0].Status = "not-run" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := assessmentFixture()
			tc.change(&m)
			got := Assess(m)
			if got.Outcome != "blocked" || got.Complete || got.Results[0].Kind != Unresolved {
				t.Fatalf("uncertain evidence settled: %+v", got)
			}
		})
	}
}

func TestAssessmentTransientAndFeatureFailure(t *testing.T) {
	m := assessmentFixture()
	m.Attempts[1].Observations[0].Status = "pass"
	m.Attempts = m.Attempts[:2]
	got := Assess(m)
	if got.Outcome != "covered" || !got.Complete || got.Results[0].Kind != Transient || len(got.Attempts) != 2 {
		t.Fatalf("transient: %+v", got)
	}
	m = assessmentFixture()
	m.Attempts[2].Observations[0].Status = "pass"
	got = Assess(m)
	if got.Outcome != "failed" || got.Results[0].Kind != Failed {
		t.Fatalf("persistent feature failure with passing base: %+v", got)
	}
}

func TestAssessmentExecutedAndInheritedCoverage(t *testing.T) {
	m := assessmentFixture()
	m.Attempts = m.Attempts[:1]
	m.Attempts[0].Observations[0].Status = "pass"
	got := Assess(m)
	if got.Outcome != "passed" || !got.Complete || got.Results[0].Kind != Executed {
		t.Fatalf("fresh pass: %+v", got)
	}
	m.Inventory.Units = append(m.Inventory.Units, Unit{Key: "inherited"})
	m.Results = []Result{{Unit: "inherited", Kind: Inherited, Origin: Origin{Unit: "inherited", Commit: "original-anchor", Evidence: "retained-observation", Conditions: "conditions"}}}
	got = Assess(m)
	if got.Outcome != "covered" || !got.Complete || got.Results[1].Kind != Inherited {
		t.Fatalf("inherited pass: %+v", got)
	}
}

func countWorkPurpose(work []Work, purpose string) int {
	count := 0
	for _, w := range work {
		if w.Purpose == purpose {
			count++
		}
	}
	return count
}

func TestPlannerPreservesFailuresDuringFullFallbackAndDeletion(t *testing.T) {
	in := plannerFixture()
	in.Previous.Complete = false
	plan := Build(in)
	if !plan.Full || len(plan.Work) == 0 || plan.Work[0].Purpose != "prior-failure" {
		t.Fatalf("full fallback lost prior failure: %+v", plan)
	}
	in.Current.Units = slices.Clone(in.Current.Units[:4])
	plan = Build(in)
	if len(plan.Unresolved) != 1 || plan.Unresolved[0].Unit != "failed" || plan.Unresolved[0].Cases[0].Case != "TestRepair/parameter[1]" {
		t.Fatalf("deleted failure disappeared: %+v", plan)
	}
}

func TestAssessmentRequiredFailureDeletionAndSkip(t *testing.T) {
	for _, status := range []string{"skip", "not-run", ""} {
		t.Run(status, func(t *testing.T) {
			m := assessmentFixture()
			m.Attempts = m.Attempts[:1]
			m.RequiredFailures = []Observation{m.Attempts[0].Observations[0]}
			m.Attempts[0].Observations[0].Status = status
			if got := Assess(m); got.Complete || got.Outcome != "blocked" {
				t.Fatalf("missing failure repaired: %+v", got)
			}
		})
	}
	m := assessmentFixture()
	m.Attempts = m.Attempts[:1]
	m.Attempts[0].Observations[0].Status = "pass"
	m.Results = []Result{{Unit: "removed-unit", Kind: Unresolved, Cases: []Observation{{Unit: "removed-unit", Case: "TestDeleted", Status: "fail", Complete: true}}}}
	if got := Assess(m); got.Complete || got.Outcome != "blocked" || len(got.Results) != 2 {
		t.Fatalf("removed unit disappeared: %+v", got)
	}
}

func TestAssessmentPhaseIdentityAndUnresolvedPrecedence(t *testing.T) {
	m := assessmentFixture()
	m.Attempts[0].Observations[0].Phase = "teardown"
	m.Attempts[1].Observations[0].Phase = "call"
	m.Attempts[1].Observations[0].Status = "pass"
	if got := Assess(m); got.Complete || got.Outcome != "blocked" {
		t.Fatalf("different pytest phase settled failure: %+v", got)
	}
	m = assessmentFixture()
	for i := range m.Attempts {
		m.Attempts[i].Observations = append([]Observation{{Unit: "unit", Case: "TestSkipped", Status: "skip", Complete: true}}, m.Attempts[i].Observations...)
	}
	m.Attempts[2].Observations[1].Status = "pass"
	got := Assess(m)
	if got.Outcome != "blocked" || got.Complete || got.Results[0].Kind != Unresolved {
		t.Fatalf("later feature failure erased unresolved coverage: %+v", got)
	}
}

func TestAssessmentNewFailureDiscoveredInContainingRetry(t *testing.T) {
	m := assessmentFixture()
	m.Attempts[1].Observations[0].Status = "pass"
	m.Attempts[1].Observations = append(m.Attempts[1].Observations, Observation{Unit: "unit", Case: "Tests/other-parameter[2]", Status: "fail", Diagnostic: "new assertion", Complete: true})
	if got := Assess(m); got.Outcome != "blocked" || got.Complete || got.Results[0].Kind != Unresolved {
		t.Fatalf("new retry failure hidden: %+v", got)
	}
}

func TestAssessmentLaterFullPassRetainsEarlierFailure(t *testing.T) {
	for _, status := range []string{"missing", "pass", "fail"} {
		t.Run(status, func(t *testing.T) {
			m := assessmentFixture()
			m.Attempts[0].Purpose = "prior-failure"
			later := m.Attempts[0]
			later.ID = "later-full"
			later.Purpose = "full"
			later.Observations = slices.Clone(later.Observations)
			later.Observations[0].Status = "pass"
			m.Attempts = append([]Attempt{m.Attempts[0], later}, m.Attempts[1:]...)
			m.Attempts[2].Observations[0].Status = status
			if status == "missing" {
				m.Attempts = m.Attempts[:2]
			}
			got := Assess(m)
			if len(got.Comparisons) != 1 || got.Comparisons[0].Original != "original" {
				t.Fatalf("original failure erased: %+v", got)
			}
			expected := map[string]string{"missing": "blocked", "pass": "covered", "fail": "covered"}[status]
			if got.Outcome != expected {
				t.Fatalf("outcome=%s expected=%s: %+v", got.Outcome, expected, got)
			}
			if status == "pass" && got.Results[0].Kind != Transient {
				t.Fatalf("passing retry not transient: %+v", got)
			}
		})
	}
}

func TestAssessmentNewRetryFailureGetsOneLaterRetry(t *testing.T) {
	m := assessmentFixture()
	m.Attempts[1].Observations[0].Status = "pass"
	newFailure := Observation{Unit: "unit", Case: "Tests/new", Status: "fail", Diagnostic: "new failure", Complete: true}
	m.Attempts[1].Observations = append(m.Attempts[1].Observations, newFailure)
	later := m.Attempts[1]
	later.ID = "new-case-retry"
	later.Observations = []Observation{newFailure}
	later.Observations[0].Status = "pass"
	m.Attempts = append(m.Attempts, later)
	got := Assess(m)
	if got.Outcome != "covered" || !got.Complete || got.Results[0].Kind != Transient || len(got.Comparisons) != 2 {
		t.Fatalf("new failure retry not retained: %+v", got)
	}
	if got.Comparisons[1].Original != "retry" || got.Comparisons[1].Retry != "new-case-retry" {
		t.Fatalf("retry lineage: %+v", got.Comparisons)
	}
}

func TestAssessmentAggregateCasePassSettlesPhaseFailure(t *testing.T) {
	m := assessmentFixture()
	m.Attempts[0].Observations[0].Phase = "call"
	m.Attempts[1].Observations[0].Phase = ""
	m.Attempts[1].Observations[0].Status = "pass"
	got := Assess(m)
	if got.Outcome != "covered" || !got.Complete || got.Results[0].Kind != Transient {
		t.Fatalf("complete aggregate pass rejected: %+v", got)
	}
}

func TestPlannerUnknownChangedInputRequiresFull(t *testing.T) {
	for _, file := range []string{"assets/generated.bin", "other-language/bridge.swift", "unknown/project.csproj", "docs/guide.md"} {
		t.Run(file, func(t *testing.T) {
			in := plannerFixture()
			in.Changes["original"] = Changes{Anchor: "original", Proven: true, Paths: []string{file}}
			plan := Build(in)
			if !plan.Full || len(plan.Inherited) != 0 || countWorkPurpose(plan.Work, "full") != len(in.Current.Units) {
				t.Fatalf("unknown path ignored: %+v", plan)
			}
		})
	}
}

func TestAssessmentRequiredPhaseFailureSettledByWholeCasePass(t *testing.T) {
	m := assessmentFixture()
	m.Attempts = m.Attempts[:1]
	m.RequiredFailures = []Observation{{Unit: "unit", Case: m.Attempts[0].Observations[0].Case, Phase: "call", Status: "fail", Complete: true}}
	m.Attempts[0].Observations[0].Status = "pass"
	if got := Assess(m); !got.Complete || got.Outcome != "passed" {
		t.Fatalf("complete whole-case pass failed reconciliation: %+v", got)
	}
}

func TestAssessmentLossyDiagnosticCannotEarnBaseExemption(t *testing.T) {
	for _, diagnostic := range []string{"expected [redacted]", "expected [REDACTED]", "invalid \uFFFD diagnostic"} {
		m := assessmentFixture()
		for i := range m.Attempts {
			m.Attempts[i].Observations[0].Diagnostic = diagnostic
		}
		actual := Assess(m)
		if actual.Outcome != "blocked" || actual.Complete {
			t.Fatalf("lossy diagnostics granted exemption: %+v", actual)
		}
	}
}
