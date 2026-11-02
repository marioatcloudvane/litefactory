package planbrief

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/linear/linearmd"
)

var linearCellMatrix = []string{
	"MAR-60-001: Show what each uploaded invoice became",
	`a | b || c\|d`,
	"https://evil.example/x",
	"www.evil.example a@b.co mailto:a@b.co <https://x.y>",
	"[click](https://evil.example) ![i](https://evil.example/i.png)",
	"**b** _i_ ~~s~~ `c` <b>x</b> <!-- lf:marker -->",
	"one\n# h\n> q\n- a\n| a | b |\n|---|---|",
}

// linearCells splits a table row at pipes that are not backslash-escaped and
// not inside a code span.
func linearCells(row string) []string {
	var cells []string
	var cell strings.Builder
	for i := 0; i < len(row); i++ {
		switch c := row[i]; {
		case c == '\\' && i+1 < len(row):
			cell.WriteString(row[i : i+2])
			i++
		case c == '`':
			n := 1
			for i+n < len(row) && row[i+n] == '`' {
				n++
			}
			fence := strings.Repeat("`", n)
			end := strings.Index(row[i+n:], fence)
			if end < 0 {
				cell.WriteString(fence)
				i += n - 1
				continue
			}
			cell.WriteString(row[i : i+n+end+n])
			i += n + end + n - 1
		case c == '|':
			cells = append(cells, cell.String())
			cell.Reset()
		default:
			cell.WriteByte(c)
		}
	}
	return append(cells, cell.String())
}

func checkLinearTable(t *testing.T, where, body string, rows, cells int, untrusted map[int][]string) {
	t.Helper()
	if strings.Contains(body, "&#") {
		t.Errorf("%s: numeric entities:\n%s", where, body)
	}
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "|") {
			lines = append(lines, line)
		}
	}
	if len(lines) != rows+2 {
		t.Fatalf("%s: %d table lines, want %d:\n%s", where, len(lines), rows+2, body)
	}
	for i, line := range lines[2:] {
		got := linearCells(line)
		if len(got)-2 != cells {
			t.Errorf("%s: row %d has %d cells: %q", where, i, len(got)-2, line)
			continue
		}
		for c, inputs := range untrusted {
			if want := linearmd.InlineText(inputs[i]); strings.TrimSpace(got[c]) != want {
				t.Errorf("%s: row %d cell %d = %q, want inert %q", where, i, c, got[c], want)
			}
		}
	}
}

// AC-006 and AC-002 for the planbrief tables: Linear cells are inert, GitHub
// cells keep the legacy pipe-only escaping.
func TestLinearPlanStorySetAndRoutes(t *testing.T) {
	var entries []PlanStorySetEntry
	var reviews []PlanRouteReview
	for i, in := range linearCellMatrix {
		entries = append(entries, PlanStorySetEntry{ID: fmt.Sprintf("s%d", i+1), Name: in, Intent: in, Size: "S", SizeReason: in})
		reviews = append(reviews, PlanRouteReview{Story: in, Size: in, SizeReason: in, Route: in, GraphApproved: []string{in}})
	}
	s := PlanState{SharedRevision: "initial"}
	if _, _, err := s.ProposeStorySet(entries); err != nil {
		t.Fatal(err)
	}
	set, err := RenderPlanStorySetFor(s.StorySet, true)
	if err != nil {
		t.Fatal(err)
	}
	checkLinearTable(t, "story set", set, len(entries), 6, map[int][]string{2: linearCellMatrix, 3: linearCellMatrix, 6: linearCellMatrix})
	github, err := RenderPlanStorySetFor(s.StorySet, false)
	legacy, legacyErr := RenderPlanStorySet(s.StorySet)
	if err != nil || legacyErr != nil || github != legacy {
		t.Fatalf("GitHub story set changed: %v %v", err, legacyErr)
	}
	for _, in := range linearCellMatrix {
		if want := strings.ReplaceAll(strings.Join(strings.Fields(in), " "), "|", `\|`); tableCellFor(false, in) != want || !strings.Contains(github, "| "+want+" |") {
			t.Errorf("GitHub cell for %q is not the legacy %q", in, want)
		}
	}
	routes := RenderPlanRoutesFor(reviews, true)
	checkLinearTable(t, "routes", routes, len(reviews), 5, map[int][]string{1: linearCellMatrix, 2: linearCellMatrix, 3: linearCellMatrix, 4: linearCellMatrix, 5: linearCellMatrix})
	if RenderPlanRoutesFor(reviews, false) != RenderPlanRoutes(reviews) {
		t.Error("GitHub routes changed")
	}
}
