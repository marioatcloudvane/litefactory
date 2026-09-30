package bugrepair_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/bugrepair"
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
		// Policy downgrade (REFUSAL_REVIEW.md bugrepair.coverage_gap): a
		// criterion with no changed file, test reference or explicit gap is
		// no longer a hard error when done=false; it continues on the normal
		// success edge and the caller records a Notice as a fact instead.
		{"empty partial coverage continues as a notice", func(e *bugrepair.Evidence) { e.Coverage[0] = bugrepair.Coverage{Criterion: "AC-001"} }, false, false},
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
			_, err := e.Validate([]string{"AC-001"}, tt.done)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate error=%v, want error=%v", err, tt.wantErr)
			}
		})
	}
}

// AC-004/AC-006 (story-002): the downgraded coverage-gap site continues and
// records exactly one Notice, with a stable reference and plain-language text,
// per occurrence -- never an error, and never touching more than that one
// criterion's gap.
func TestRepairEvidence_CoverageGapRecordsExactlyOneNoticePerOccurrence(t *testing.T) {
	t.Parallel()
	e := repairFixture()
	e.Coverage[0] = bugrepair.Coverage{Criterion: "AC-001"}
	notices, err := e.Validate([]string{"AC-001"}, false)
	if err != nil {
		t.Fatalf("policy downgrade returned a hard error: %v", err)
	}
	if len(notices) != 1 {
		t.Fatalf("want exactly one notice, got %d: %+v", len(notices), notices)
	}
	if notices[0].Reference != "bugrepair.coverage_gap:AC-001" {
		t.Fatalf("unstable/wrong reference: %q", notices[0].Reference)
	}
	if !strings.Contains(notices[0].Text, "AC-001") || !strings.Contains(notices[0].Text, "no changed file") {
		t.Fatalf("notice text is not plain-language and criterion-specific: %q", notices[0].Text)
	}

	// Two criteria left without evidence each get their own notice, and a
	// criterion that does carry a gap/changes/tests is not renotified.
	multi := repairFixture()
	multi.Coverage = []bugrepair.Coverage{
		{Criterion: "AC-001"},
		{Criterion: "AC-002"},
		{Criterion: "AC-003", Gap: "Documented gap"},
	}
	notices, err = multi.Validate([]string{"AC-001", "AC-002", "AC-003"}, false)
	if err != nil {
		t.Fatalf("policy downgrade returned a hard error: %v", err)
	}
	if len(notices) != 2 {
		t.Fatalf("want one notice per bare criterion, got %d: %+v", len(notices), notices)
	}
	refs := map[string]bool{notices[0].Reference: true, notices[1].Reference: true}
	if !refs["bugrepair.coverage_gap:AC-001"] || !refs["bugrepair.coverage_gap:AC-002"] {
		t.Fatalf("unexpected notice references: %+v", notices)
	}
}

// A criterion with a change or test reference never records a notice: only the
// no-evidence-at-all case is policy.
func TestRepairEvidence_PartialEvidenceRecordsNoNotice(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		cov  bugrepair.Coverage
	}{
		{"change only", bugrepair.Coverage{Criterion: "AC-001", Changes: []string{"search.go"}}},
		{"test only", bugrepair.Coverage{Criterion: "AC-001", Tests: []string{"search_test.go"}}},
		{"gap only", bugrepair.Coverage{Criterion: "AC-001", Gap: "Blocked on owner decision"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := repairFixture()
			e.Coverage[0] = tt.cov
			notices, err := e.Validate([]string{"AC-001"}, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(notices) != 0 {
				t.Fatalf("unexpected notice for partial evidence: %+v", notices)
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
				if _, err := e.Validate([]string{"AC-001"}, true); err == nil {
					t.Fatalf("accepted escaping path %q (test=%v)", path, testPath)
				}
			}
		})
	}
}
func repairFixture() bugrepair.Evidence {
	return bugrepair.Evidence{Reservation: "repair-1", AnalysisDigest: "analysis-digest", SourceCommit: "commit", SourceTree: "tree", Summary: "Guard empty input", Coverage: []bugrepair.Coverage{{Criterion: "AC-001", Changes: []string{"search.go"}, Tests: []string{"search_test.go"}}}}
}
