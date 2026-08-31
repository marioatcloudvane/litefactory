package planbrief_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// contractShapedIndex is the exact shape plan-orchestrator.md's contract
// describes, with two data rows and one unresolved Sub-issue cell — the
// realistic input every test in this file starts from.
const contractShapedIndex = `# Plan Brief — test

## Shared Decisions
- none

## Story Index
| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| C1 | The plan ships as one sub-issue | M | — |  |
| C2 | Follow-up wiring | S | C1 | https://github.com/acme/acme-api/issues/41 |

## Out of Scope
- none
`

func TestParseStoryIndex(t *testing.T) {
	t.Parallel()

	rows, err := planbrief.ParseStoryIndex([]byte(contractShapedIndex))
	if err != nil {
		t.Fatalf("ParseStoryIndex: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("parsed %d rows, want 2: %+v", len(rows), rows)
	}

	c1, c2 := rows[0], rows[1]
	switch {
	case c1.ID != "C1":
		t.Errorf("rows[0].ID = %q, want C1", c1.ID)
	case c1.Title != "The plan ships as one sub-issue":
		t.Errorf("rows[0].Title = %q", c1.Title)
	case c1.Size != "M":
		t.Errorf("rows[0].Size = %q, want M", c1.Size)
	case c1.DependsOn != "—":
		t.Errorf("rows[0].DependsOn = %q, want an em dash", c1.DependsOn)
	case c1.SubIssue != "":
		t.Errorf("rows[0].SubIssue = %q, want empty", c1.SubIssue)
	}
	switch {
	case c2.ID != "C2":
		t.Errorf("rows[1].ID = %q, want C2", c2.ID)
	case c2.DependsOn != "C1":
		t.Errorf("rows[1].DependsOn = %q, want C1", c2.DependsOn)
	case c2.SubIssue != "https://github.com/acme/acme-api/issues/41":
		t.Errorf("rows[1].SubIssue = %q", c2.SubIssue)
	}
}

// TestParseStoryIndexIsColumnOrderIndependent proves the table is read by its
// header's column names, not by fixed positions — a table with Size and Title
// swapped from the contract's canonical order still parses correctly.
func TestParseStoryIndexIsColumnOrderIndependent(t *testing.T) {
	t.Parallel()

	reordered := "## Story Index\n" +
		"| Size | ID | Sub-issue | Title | Depends On |\n" +
		"|------|----|-----------|-------|-----------|\n" +
		"| M | C1 |  | A reordered table | — |\n"

	rows, err := planbrief.ParseStoryIndex([]byte(reordered))
	if err != nil {
		t.Fatalf("ParseStoryIndex: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("parsed %d rows, want 1", len(rows))
	}
	row := rows[0]
	if row.ID != "C1" || row.Title != "A reordered table" || row.Size != "M" || row.DependsOn != "—" {
		t.Errorf("row = %+v, want ID=C1 Title=%q Size=M DependsOn=—", row, "A reordered table")
	}
}

func TestParseStoryIndexNoHeading(t *testing.T) {
	t.Parallel()

	_, err := planbrief.ParseStoryIndex([]byte("# Plan Brief\n\nNo table here.\n"))
	if !errors.Is(err, planbrief.ErrNoStoryIndex) {
		t.Errorf("err = %v, want ErrNoStoryIndex", err)
	}
}

func TestParseStoryIndexEmptyTable(t *testing.T) {
	t.Parallel()

	data := "## Story Index\n| ID | Title | Size | Depends On | Sub-issue |\n|----|-------|------|-----------|-----------|\n\n## Out of Scope\n"
	rows, err := planbrief.ParseStoryIndex([]byte(data))
	if err != nil {
		t.Fatalf("ParseStoryIndex: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("parsed %d rows, want 0", len(rows))
	}
}

func TestParseStoryIndexRefusesAnEmptyID(t *testing.T) {
	t.Parallel()

	data := "## Story Index\n| ID | Title | Size | Depends On | Sub-issue |\n|----|-------|------|-----------|-----------|\n|  | No id | M | — |  |\n"
	_, err := planbrief.ParseStoryIndex([]byte(data))
	if !errors.Is(err, planbrief.ErrMalformedRow) {
		t.Errorf("err = %v, want ErrMalformedRow", err)
	}
}

func TestRewriteSubIssueCell(t *testing.T) {
	t.Parallel()

	url := "https://github.com/acme/acme-api/issues/99"
	rewritten, changed, err := planbrief.RewriteSubIssueCell([]byte(contractShapedIndex), "C1", url)
	if err != nil {
		t.Fatalf("RewriteSubIssueCell: %v", err)
	}
	if !changed {
		t.Error("changed = false, want true")
	}

	rows, err := planbrief.ParseStoryIndex(rewritten)
	if err != nil {
		t.Fatalf("ParseStoryIndex on the rewritten bytes: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("parsed %d rows, want 2", len(rows))
	}
	if rows[0].SubIssue != url {
		t.Errorf("C1's Sub-issue cell = %q, want %q", rows[0].SubIssue, url)
	}
	// C2's row, and everything outside the Story Index, must survive
	// untouched.
	if rows[1].SubIssue != "https://github.com/acme/acme-api/issues/41" {
		t.Errorf("C2's row was disturbed: %+v", rows[1])
	}
	if !strings.Contains(string(rewritten), "## Shared Decisions") ||
		!strings.Contains(string(rewritten), "## Out of Scope") {
		t.Errorf("sections outside the Story Index were lost:\n%s", rewritten)
	}
}

// TestRewriteSubIssueCellIsIdempotent is the AC-W3/AC-C1.4 no-op shape: asking
// to rewrite a cell to the value it already holds changes nothing and reports
// changed=false, byte for byte.
func TestRewriteSubIssueCellIsIdempotent(t *testing.T) {
	t.Parallel()

	url := "https://github.com/acme/acme-api/issues/41"
	rewritten, changed, err := planbrief.RewriteSubIssueCell([]byte(contractShapedIndex), "C2", url)
	if err != nil {
		t.Fatalf("RewriteSubIssueCell: %v", err)
	}
	if changed {
		t.Error("changed = true, want false: the cell already held this value")
	}
	if string(rewritten) != contractShapedIndex {
		t.Errorf("rewritten bytes differ from the input on a no-op rewrite")
	}
}

func TestRewriteSubIssueCellStoryNotFound(t *testing.T) {
	t.Parallel()

	_, changed, err := planbrief.RewriteSubIssueCell([]byte(contractShapedIndex), "C9", "https://example.invalid/9")
	if !errors.Is(err, planbrief.ErrStoryNotFound) {
		t.Errorf("err = %v, want ErrStoryNotFound", err)
	}
	if changed {
		t.Error("changed = true, want false")
	}
}

// TestRewriteSubIssueCellGrowsAShortRow covers a row authored with fewer
// cells than the header names — a plan-orchestrator that left the trailing
// Sub-issue cell off the line entirely rather than writing it empty.
func TestRewriteSubIssueCellGrowsAShortRow(t *testing.T) {
	t.Parallel()

	data := "## Story Index\n| ID | Title | Size | Depends On | Sub-issue |\n" +
		"|----|-------|------|-----------|-----------|\n" +
		"| C1 | Short row | M | — |\n"

	rewritten, changed, err := planbrief.RewriteSubIssueCell([]byte(data), "C1", "https://example.invalid/1")
	if err != nil {
		t.Fatalf("RewriteSubIssueCell: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	rows, err := planbrief.ParseStoryIndex(rewritten)
	if err != nil {
		t.Fatalf("ParseStoryIndex: %v", err)
	}
	if len(rows) != 1 || rows[0].SubIssue != "https://example.invalid/1" {
		t.Errorf("rows = %+v, want one row with the new Sub-issue cell", rows)
	}
}
