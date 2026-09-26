package report_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/report"
)

func bugReviewFixture() report.BugDeliveryReview {
	digest, head, target := strings.Repeat("d", 64), strings.Repeat("a", 40), strings.Repeat("b", 40)
	return report.BugDeliveryReview{
		Task: "github:owner/repo#385", Digest: digest, Head: head, Target: target, Recipe: strings.Repeat("c", 64), PR: 42,
		Analysis:        "Expected: one result. Actual: duplicate result. Cause: retry. Remedy: deduplicate. Exclusions: routing. AC-001: exactly one result.",
		Repair:          "AC-001 covered by dedupe.go and dedupe_test.go",
		Phases:          [3]string{"regression evidence #11 passed", "focused evidence #12 passed", "full evidence #13 passed"},
		ApprovalCommand: "/approve-bug delivery " + digest + " 42 " + head + " main " + target,
	}
}

func TestRenderBugDeliveryReview_PreservesRequiredEvidence(t *testing.T) {
	t.Parallel()
	r := bugReviewFixture()
	got, err := report.RenderBugDeliveryReview(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{r.Task, r.Digest, r.Head, r.Target, r.Recipe, r.Analysis, r.Repair,
		r.Phases[0], r.Phases[1], r.Phases[2], r.ApprovalCommand, "/bug-review " + r.Digest} {
		if !strings.Contains(got, want) {
			t.Errorf("required review evidence/control %q missing from %q", want, got)
		}
	}
	for _, control := range []string{"clarify", "repair", "reshape"} {
		if !strings.Contains(got, control) {
			t.Errorf("owner feedback control %q missing", control)
		}
	}
	if strings.Index(got, r.Phases[0]) >= strings.Index(got, r.Phases[1]) || strings.Index(got, r.Phases[1]) >= strings.Index(got, r.Phases[2]) {
		t.Fatal("check evidence does not retain regression/focused/full order")
	}
}

func TestRenderBugDeliveryReview_RefusesIncompleteEvidence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*report.BugDeliveryReview)
	}{
		{"task", func(r *report.BugDeliveryReview) { r.Task = "" }},
		{"digest", func(r *report.BugDeliveryReview) { r.Digest = "" }},
		{"head", func(r *report.BugDeliveryReview) { r.Head = "" }},
		{"target", func(r *report.BugDeliveryReview) { r.Target = "" }},
		{"recipe", func(r *report.BugDeliveryReview) { r.Recipe = "" }},
		{"PR", func(r *report.BugDeliveryReview) { r.PR = 0 }},
		{"analysis", func(r *report.BugDeliveryReview) { r.Analysis = " \n" }},
		{"repair", func(r *report.BugDeliveryReview) { r.Repair = "" }},
		{"regression", func(r *report.BugDeliveryReview) { r.Phases[0] = "" }},
		{"focused", func(r *report.BugDeliveryReview) { r.Phases[1] = "" }},
		{"full", func(r *report.BugDeliveryReview) { r.Phases[2] = "" }},
		{"approval command", func(r *report.BugDeliveryReview) { r.ApprovalCommand = "" }},
		{"mixed approval and changes", func(r *report.BugDeliveryReview) { r.ApprovalCommand += " but change the remedy" }},
		{"changed approval head", func(r *report.BugDeliveryReview) { r.Head = strings.Repeat("e", 40) }},
		{"changed approval target", func(r *report.BugDeliveryReview) { r.Target = strings.Repeat("e", 40) }},
		{"changed approval PR", func(r *report.BugDeliveryReview) { r.PR++ }},
		{"malformed recipe digest", func(r *report.BugDeliveryReview) { r.Recipe = strings.Repeat("z", 64) }},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/cannot publish incomplete review", func(t *testing.T) {
			t.Parallel()
			r := bugReviewFixture()
			tc.change(&r)
			got, err := report.RenderBugDeliveryReview(r)
			if err == nil || got != "" {
				t.Fatalf("incomplete review rendered: %q, error %v", got, err)
			}
		})
	}
}
