package buganalysis_test

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/buganalysis"
)

func reviewableBugAnalysis() buganalysis.BugAnalysis {
	return buganalysis.BugAnalysis{
		Expected: "An empty query returns zero matches.",
		Actual:   "An empty query panics.",
		Reproduction: buganalysis.BugReproduction{
			Steps:       []string{"Call Search with an empty query."},
			Environment: "Go test fixture with an empty index",
			Observation: "owner_reported",
		},
		Cause:      "Search indexes the first query byte before checking its length.",
		Evidence:   []string{"internal/search/search.go:21"},
		Remedy:     "Return zero matches before indexing an empty query.",
		Exclusions: []string{"Do not change matching for nonempty queries."},
		Criteria: []buganalysis.BugCriterion{{
			ID: "AC-001", Requirement: "Empty queries return no matches without panic.",
			Measurement: "A regression test calls Search with an empty query and checks an empty result.",
		}},
	}
}

func TestBugAnalysis_ValidateReviewCompleteness(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*buganalysis.BugAnalysis)
		valid  bool
	}{
		{"complete owner reproduction/accepted", func(*buganalysis.BugAnalysis) {}, true},
		{"observed reproduction/accepted", func(a *buganalysis.BugAnalysis) { a.Reproduction.Observation = "observed" }, true},
		{"explicitly unobserved reproduction/accepted", func(a *buganalysis.BugAnalysis) { a.Reproduction.Observation = "not_observed" }, true},
		{"missing expected behavior/refused", func(a *buganalysis.BugAnalysis) { a.Expected = " \n" }, false},
		{"missing actual behavior/refused", func(a *buganalysis.BugAnalysis) { a.Actual = "" }, false},
		{"missing environment/refused", func(a *buganalysis.BugAnalysis) { a.Reproduction.Environment = "" }, false},
		{"missing reproduction steps/refused", func(a *buganalysis.BugAnalysis) { a.Reproduction.Steps = nil }, false},
		{"missing observation status/refused", func(a *buganalysis.BugAnalysis) { a.Reproduction.Observation = "" }, false},
		{"missing cause/refused", func(a *buganalysis.BugAnalysis) { a.Cause = "" }, false},
		{"missing cause evidence/refused", func(a *buganalysis.BugAnalysis) { a.Evidence = nil }, false},
		{"missing remedy/refused", func(a *buganalysis.BugAnalysis) { a.Remedy = "" }, false},
		{"missing exclusions/refused", func(a *buganalysis.BugAnalysis) { a.Exclusions = nil }, false},
		{"missing success criteria/refused", func(a *buganalysis.BugAnalysis) { a.Criteria = nil }, false},
		{"missing criterion requirement/refused", func(a *buganalysis.BugAnalysis) { a.Criteria[0].Requirement = "" }, false},
		{"missing criterion measurement/refused", func(a *buganalysis.BugAnalysis) { a.Criteria[0].Measurement = "\t" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			analysis := reviewableBugAnalysis()
			tt.change(&analysis)
			err := analysis.Validate(true)
			if (err == nil) != tt.valid {
				t.Fatalf("Validate(review=true) error = %v; want valid %v", err, tt.valid)
			}
		})
	}
}

func TestBugAnalysis_ValidatePartialSnapshot(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		analysis buganalysis.BugAnalysis
		valid    bool
	}{
		{"empty findings/retained", buganalysis.BugAnalysis{}, true},
		{"owner question without known cause/retained", buganalysis.BugAnalysis{
			Actual:    "Owner reports an intermittent panic.",
			Questions: []buganalysis.BugQuestion{{ID: "Q-001", Text: "Which input caused the panic?", Material: true}},
		}, true},
		{"unknown observation/refused", buganalysis.BugAnalysis{Reproduction: buganalysis.BugReproduction{Observation: "probably"}}, false},
		{"blank evidence/refused", buganalysis.BugAnalysis{Evidence: []string{" "}}, false},
		{"blank reproduction step/refused", buganalysis.BugAnalysis{Reproduction: buganalysis.BugReproduction{Steps: []string{""}}}, false},
		{"blank exclusion/refused", buganalysis.BugAnalysis{Exclusions: []string{""}}, false},
		{"blank decision/refused", buganalysis.BugAnalysis{Decisions: []string{""}}, false},
		{"blank assumption/refused", buganalysis.BugAnalysis{Assumptions: []string{""}}, false},
		{"blank answer reference/refused", buganalysis.BugAnalysis{AnswerReferences: []string{""}}, false},
		{"blank question/refused", buganalysis.BugAnalysis{Questions: []buganalysis.BugQuestion{{ID: "Q-001", Text: " "}}}, false},
		{"criterion missing measurement/refused", buganalysis.BugAnalysis{Criteria: []buganalysis.BugCriterion{{ID: "AC-001", Requirement: "No panic"}}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.analysis.Validate(false)
			if (err == nil) != tt.valid {
				t.Fatalf("Validate(review=false) error = %v; want valid %v", err, tt.valid)
			}
		})
	}
}

func TestBugAnalysis_ValidateStableIdentifiers(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		ids   []string
		valid bool
	}{
		{"distinct stable IDs/accepted", []string{"AC-001", "AC-002"}, true},
		{"empty ID/refused", []string{""}, false},
		{"whitespace ID/refused", []string{"AC 001"}, false},
		{"duplicate ID/refused", []string{"AC-001", "AC-001"}, false},
		{"path ID/refused", []string{"../AC-001"}, false},
		{"control character ID/refused", []string{"AC-001\n"}, false},
	} {
		for _, kind := range []string{"criteria", "questions"} {
			t.Run(kind+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				analysis := buganalysis.BugAnalysis{}
				for _, id := range tt.ids {
					if kind == "criteria" {
						analysis.Criteria = append(analysis.Criteria, buganalysis.BugCriterion{ID: id, Requirement: "No panic", Measurement: "Empty-input regression test"})
					} else {
						analysis.Questions = append(analysis.Questions, buganalysis.BugQuestion{ID: id, Text: "What input failed?", Material: true})
					}
				}
				err := analysis.Validate(false)
				if (err == nil) != tt.valid {
					t.Fatalf("Validate error = %v; want valid %v", err, tt.valid)
				}
			})
		}
	}
}

func TestBugAnalysis_DigestBindsProposal(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		change func(*buganalysis.BugAnalysis)
	}{
		{"remedy change", func(a *buganalysis.BugAnalysis) { a.Remedy = "Reject the empty query with an error." }},
		{"criterion meaning change", func(a *buganalysis.BugAnalysis) { a.Criteria[0].Requirement = "Return an error for empty queries." }},
		{"measurement change", func(a *buganalysis.BugAnalysis) { a.Criteria[0].Measurement = "Assert a typed error." }},
		{"cause evidence change", func(a *buganalysis.BugAnalysis) { a.Evidence = []string{"internal/search/search.go:42"} }},
		{"exclusions change", func(a *buganalysis.BugAnalysis) { a.Exclusions = []string{"Do not alter the public API."} }},
		{"owner answer reference change", func(a *buganalysis.BugAnalysis) { a.AnswerReferences = []string{"comment-123/Q-001"} }},
		{"question change", func(a *buganalysis.BugAnalysis) {
			a.Questions = []buganalysis.BugQuestion{{ID: "Q-001", Text: "Which release?", Material: true}}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			original := reviewableBugAnalysis()
			before, err := original.Digest()
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := hex.DecodeString(before)
			if err != nil || len(decoded) != 32 {
				t.Fatalf("digest is not SHA256: %q (%v)", before, err)
			}
			replay, err := reviewableBugAnalysis().Digest()
			if err != nil || replay != before {
				t.Fatalf("identical proposal changed digest: %q, %v", replay, err)
			}
			changed := reviewableBugAnalysis()
			tt.change(&changed)
			after, err := changed.Digest()
			if err != nil {
				t.Fatal(err)
			}
			if strings.EqualFold(before, after) {
				t.Fatal("changed proposal retained original digest")
			}
		})
	}
}
