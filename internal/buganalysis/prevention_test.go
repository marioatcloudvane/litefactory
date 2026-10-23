package buganalysis_test

import (
	"encoding/json"
	"testing"

	"github.com/cloudvane/litefactory/internal/buganalysis"
)

func TestBugAnalysisPreventionIsOptional(t *testing.T) {
	t.Parallel()
	a := reviewableBugAnalysis()
	if _, err := a.Validate(true); err != nil {
		t.Fatalf("analysis without prevention rejected: %v", err)
	}
	encoded, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["prevention"]; ok {
		t.Fatalf("absent prevention is encoded and would change older digests: %s", encoded)
	}
}

func TestBugAnalysisPreventionShape(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		p    buganalysis.BugPrevention
		ok   bool
	}{
		{"missing standard", buganalysis.BugPrevention{Kind: "missing_standard", Note: "A rule to check lengths before indexing would have caught it."}, true},
		{"ignored standard", buganalysis.BugPrevention{Kind: "ignored_standard", StandardID: "go-bounds-check", Note: "The bounds rule was skipped."}, true},
		{"one-off", buganalysis.BugPrevention{Kind: "one_off", Note: "A typo in a test fixture."}, true},
		{"unknown kind", buganalysis.BugPrevention{Kind: "maybe", Note: "x"}, false},
		{"blank kind", buganalysis.BugPrevention{Note: "x"}, false},
		{"ignored standard without id", buganalysis.BugPrevention{Kind: "ignored_standard", Note: "x"}, false},
		{"standard id on missing standard", buganalysis.BugPrevention{Kind: "missing_standard", StandardID: "go-bounds-check", Note: "x"}, false},
		{"standard id on one-off", buganalysis.BugPrevention{Kind: "one_off", StandardID: "go-bounds-check", Note: "x"}, false},
		{"blank note", buganalysis.BugPrevention{Kind: "one_off", Note: "  "}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			a := reviewableBugAnalysis()
			p := c.p
			a.Prevention = &p
			for _, review := range []bool{false, true} {
				_, err := a.Validate(review)
				if c.ok && err != nil {
					t.Fatalf("review=%v rejected valid prevention: %v", review, err)
				}
				if !c.ok && err == nil {
					t.Fatalf("review=%v accepted invalid prevention %+v", review, p)
				}
			}
		})
	}
}

func TestBugAnalysisPreventionDecodes(t *testing.T) {
	t.Parallel()
	var a buganalysis.BugAnalysis
	if err := json.Unmarshal([]byte(`{"prevention":{"kind":"ignored_standard","standard_id":"go-bounds-check","note":"The bounds rule was skipped."}}`), &a); err != nil {
		t.Fatal(err)
	}
	if a.Prevention == nil || a.Prevention.Kind != buganalysis.PreventionIgnoredStandard || a.Prevention.StandardID != "go-bounds-check" || a.Prevention.Note == "" {
		t.Fatalf("prevention=%+v", a.Prevention)
	}
}
