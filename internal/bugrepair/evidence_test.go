package bugrepair_test

import (
	"github.com/cloudvane/litefactory/internal/bugrepair"
	"testing"
)

func TestRepairEvidence_CriterionCoverage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		change  func(*bugrepair.Evidence)
		done    bool
		wantErr bool
	}{
		{"complete authored coverage", func(*bugrepair.Evidence) {}, true, false},
		{"omitted criterion", func(e *bugrepair.Evidence) { e.Coverage = nil }, true, true},
		{"unknown criterion", func(e *bugrepair.Evidence) { e.Coverage[0].Criterion = "AC-999" }, true, true},
		{"duplicate criterion", func(e *bugrepair.Evidence) { e.Coverage = append(e.Coverage, e.Coverage[0]) }, true, true},
		{"gap prevents done", func(e *bugrepair.Evidence) { e.Coverage[0].Gap = "Not implemented" }, true, true},
		{"missing regression prevents done", func(e *bugrepair.Evidence) { e.Coverage[0].Tests = nil }, true, true},
		{"missing change prevents done", func(e *bugrepair.Evidence) { e.Coverage[0].Changes = nil }, true, true},
		{"partial gap retained", func(e *bugrepair.Evidence) {
			e.Coverage[0] = bugrepair.Coverage{Criterion: "AC-001", Gap: "Waiting for evidence"}
		}, false, false},
		{"empty partial coverage refused", func(e *bugrepair.Evidence) { e.Coverage[0] = bugrepair.Coverage{Criterion: "AC-001"} }, false, true},
		{"missing reservation", func(e *bugrepair.Evidence) { e.Reservation = "" }, true, true},
		{"missing analysis", func(e *bugrepair.Evidence) { e.AnalysisDigest = "" }, true, true},
		{"missing source", func(e *bugrepair.Evidence) { e.SourceCommit = "" }, true, true},
		{"missing tree", func(e *bugrepair.Evidence) { e.SourceTree = "" }, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := repairFixture()
			tt.change(&e)
			err := e.Validate([]string{"AC-001"}, tt.done)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate error=%v, want error=%v", err, tt.wantErr)
			}
		})
	}
}
func TestRepairEvidence_RejectsEscapingPaths(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"../escape.go", "/tmp/escape.go", "a/../../escape.go", "a\\escape.go", ".", "a\x00b", "a\nb", "a\rb", "a//b"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			for _, testPath := range []bool{false, true} {
				e := repairFixture()
				if testPath {
					e.Coverage[0].Tests = []string{path}
				} else {
					e.Coverage[0].Changes = []string{path}
				}
				if err := e.Validate([]string{"AC-001"}, true); err == nil {
					t.Fatalf("accepted escaping path %q (test=%v)", path, testPath)
				}
			}
		})
	}
}
func repairFixture() bugrepair.Evidence {
	return bugrepair.Evidence{Reservation: "repair-1", AnalysisDigest: "analysis-digest", SourceCommit: "commit", SourceTree: "tree", Summary: "Guard empty input", Coverage: []bugrepair.Coverage{{Criterion: "AC-001", Changes: []string{"search.go"}, Tests: []string{"search_test.go"}}}}
}
