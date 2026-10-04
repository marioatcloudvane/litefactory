package buganalysis

import "testing"

func TestConversationAlternativeEvidenceAndMaterialQuestion(t *testing.T) {
	a := BugAnalysis{Expected: "Safe empty input", Actual: "Panic", Reproduction: BugReproduction{Environment: "Source inspection; execution unavailable", Observation: "not_observed"}, Cause: "Unchecked index", Evidence: []string{"search.go:12 indexes empty bytes"}, Remedy: "Guard length", Exclusions: []string{"Nonempty search"}, Criteria: []BugCriterion{{ID: "C1", Requirement: "No panic", Measurement: "Call empty input"}}}
	if _, e := a.Validate(true); e != nil {
		t.Fatalf("honest alternative evidence refused: %v", e)
	}
	a.Reproduction.Observation = "observed"
	if _, e := a.Validate(true); e == nil {
		t.Fatal("observed reproduction without steps accepted")
	}
	a.Reproduction.Observation = "not_observed"
	a.Evidence = nil
	if _, e := a.Validate(true); e == nil {
		t.Fatal("missing supporting evidence accepted")
	}
	a.Evidence = []string{"source evidence"}
	a.Questions = []BugQuestion{{ID: "Q1", Text: "Which expected output?", Material: true}}
	if _, e := a.Validate(true); e == nil {
		t.Fatal("unresolved material question reviewable")
	}
	if _, e := a.Validate(false); e != nil {
		t.Fatalf("partial discussion refused: %v", e)
	}
	before, e := a.Digest()
	if e != nil {
		t.Fatal(e)
	}
	a.Remedy = "Change input API"
	after, e := a.Digest()
	if e != nil {
		t.Fatal(e)
	}
	if before == after {
		t.Fatal("canonical remedy change kept digest")
	}
}
