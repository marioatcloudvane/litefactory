package bugrepair

import "testing"

func TestEvidenceNeedsOnlyASummary(t *testing.T) {
	if (Evidence{}).Validate() == nil {
		t.Fatal("empty summary accepted")
	}
	if e := (Evidence{Summary: "Guard empty input", Checks: []Check{{Command: "make test", Outcome: "passed"}}}).Validate(); e != nil {
		t.Fatal(e)
	}
}
