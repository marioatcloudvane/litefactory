package report_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/report"
)

func TestAcceptanceReportPreservesFactualOutcomesAndAttribution(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		n := report.AcceptanceNotice{Parent: testRef, ID: 15, RunID: 31, Generation: 1, Story: "US-1", Revision: strings.Repeat("a", 64), Snapshot: strings.Repeat("b", 64), Accepted: accepted, Detail: "AC-1: unmet; OWNER-WAIVED: irrelevant requirement (original finding retained)\nAC-2: unverified: missing test evidence\ncode: advisory: naming improvement"}
		body, err := newReporter(t).RenderAcceptance(t.Context(), n)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"AC-1: unmet", "OWNER-WAIVED", "AC-2: unverified", "missing test evidence", "code: advisory", n.Revision, n.Snapshot, n.Marker()} {
			if !strings.Contains(body.String(), want) {
				t.Fatalf("missing %q in %s", want, body.String())
			}
		}
		if !strings.HasSuffix(body.String(), n.Marker()) {
			t.Fatal("reconciliation marker lost")
		}
	}
}
func TestAcceptanceProjectionRefusesChangingOrTruncatingRequirementText(t *testing.T) {
	rep := newReporter(t)
	for _, tc := range []struct {
		name, title, body string
		reject            bool
	}{
		{"preserve", "Title", "## Acceptance Criteria\n- [x] AC-1: normal text", false},
		{"oversized title", strings.Repeat("t", 300), "Body", true},
		{"oversized body", "Title", strings.Repeat("b", 70000), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			title, body, err := rep.RenderAcceptanceProjection(t.Context(), tc.title, tc.body)
			if tc.reject {
				if err == nil {
					t.Fatal("unsafe projection allowed")
				}
				return
			}
			if err != nil || title.String() != tc.title || body.String() != tc.body {
				t.Fatalf("changed live text: %q %q %v", title.String(), body.String(), err)
			}
		})
	}
}
