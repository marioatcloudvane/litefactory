package planbrief

import (
	"fmt"
	"strings"
)

// FileName is the artefact a Plan run writes at the root of its session
// directory — [naming.AITeamSessionDir] plus this name is where the caller
// finds it.
const FileName = "plan_brief.md"

// storyIndexHeading is the exact heading plan-orchestrator.md's contract
// puts above the table.
const storyIndexHeading = "## Story Index"

// The Story Index columns plan-orchestrator.md's contract names, lower-cased
// for the header match [columnIndex] does.
const (
	columnID        = "id"
	columnTitle     = "title"
	columnSize      = "size"
	columnDependsOn = "depends on"
	columnSubIssue  = "sub-issue"
)

// StoryRow is one row of plan_brief.md's Story Index, as the contract shapes
// it: "| ID | Title | Size | Depends On | Sub-issue |".
type StoryRow struct {
	// ID is the story's id within the plan — "C1", "US-3" — and the value
	// every other artefact refers to it by. Never empty; a row whose ID cell
	// is empty is not returned as a row at all.
	ID string

	// Title is the story's one-line title, as it stands in the Story Index.
	Title string

	// Size is the story's size estimate, as the plan wrote it.
	Size string

	// DependsOn is the Depends On cell, verbatim.
	DependsOn string

	// SubIssue is the Sub-issue cell as it stands in the file — a link, an
	// em dash, or empty. It is read for completeness only: what a row's cell
	// currently says is not trusted to mean the sub-issue was actually filed
	// (that answer comes from GitHub), so nothing in this package or its
	// caller branches on it.
	SubIssue string
}

// ParseStoryIndex reads the Story Index table out of plan_brief.md and
// returns one [StoryRow] per data row, in the order they appear.
//
// It looks for the table by its exact section heading and its header row's
// column names, not by column position, so a table with its columns
// reordered from the contract's canonical order is still read correctly. A
// row whose ID cell is empty is refused with [ErrMalformedRow] rather than
// silently dropped, because a row a later rewrite cannot find by id is a row
// whose story never gets a sub-issue and nothing says so.
func ParseStoryIndex(data []byte) ([]StoryRow, error) {
	lines := strings.Split(string(data), "\n")
	_, columns, dataStart, err := locateStoryIndex(lines)
	if err != nil {
		return nil, err
	}

	var rows []StoryRow
	for i := dataStart; i < len(lines); i++ {
		line := lines[i]
		if !isTableRow(line) {
			break
		}
		cells := splitRow(line)
		row, err := rowFromCells(columns, cells)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// RewriteSubIssueCell rewrites the Sub-issue cell of the row whose ID cell is
// storyID to value, leaving every other row, every other column and
// everything outside the Story Index untouched.
//
// changed is false, and data is returned unmodified, when the cell already
// holds value — the AC-C1.4/AC-W3 no-op path a caller uses to decide whether
// there is anything left to commit. [ErrStoryNotFound] is returned, data
// unmodified, when no row carries storyID; the caller is expected to log this
// and move on to the next story rather than fail the whole rewrite, since one
// row going missing says nothing about its siblings.
func RewriteSubIssueCell(data []byte, storyID, value string) ([]byte, bool, error) {
	lines := strings.Split(string(data), "\n")
	_, columns, dataStart, err := locateStoryIndex(lines)
	if err != nil {
		return data, false, err
	}
	subIdx, ok := columns[columnSubIssue]
	if !ok {
		return data, false, fmt.Errorf("%w: no %s column", ErrNoStoryIndex, columnSubIssue)
	}
	idIdx, ok := columns[columnID]
	if !ok {
		return data, false, fmt.Errorf("%w: no %s column", ErrNoStoryIndex, columnID)
	}

	for i := dataStart; i < len(lines); i++ {
		line := lines[i]
		if !isTableRow(line) {
			break
		}
		cells := splitRow(line)
		if idIdx >= len(cells) || cells[idIdx] != storyID {
			continue
		}
		for len(cells) <= subIdx {
			cells = append(cells, "")
		}
		if cells[subIdx] == value {
			return data, false, nil
		}
		cells[subIdx] = value
		lines[i] = renderRow(cells)
		return []byte(strings.Join(lines, "\n")), true, nil
	}
	return data, false, fmt.Errorf("%w: %q", ErrStoryNotFound, storyID)
}

// locateStoryIndex finds the Story Index heading, reads its header row into a
// column-name index, and returns the line index the data rows start at — just
// past the header row and, when there is one, the "|---|---|" separator row
// beneath it.
func locateStoryIndex(lines []string) (header []string, columns map[string]int, dataStart int, err error) {
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == storyIndexHeading {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil, nil, 0, ErrNoStoryIndex
	}
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start >= len(lines) || !isTableRow(lines[start]) {
		return nil, nil, 0, ErrNoStoryIndex
	}

	header = splitRow(lines[start])
	columns = columnIndex(header)
	if _, ok := columns[columnID]; !ok {
		return nil, nil, 0, fmt.Errorf("%w: no %s column", ErrNoStoryIndex, columnID)
	}

	dataStart = start + 1
	if dataStart < len(lines) && isSeparatorRow(lines[dataStart]) {
		dataStart++
	}
	return header, columns, dataStart, nil
}

// rowFromCells builds one [StoryRow] from a data row's cells, addressed by
// the header's column positions rather than fixed offsets.
func rowFromCells(columns map[string]int, cells []string) (StoryRow, error) {
	get := func(name string) string {
		i, ok := columns[name]
		if !ok || i >= len(cells) {
			return ""
		}
		return cells[i]
	}
	id := get(columnID)
	if id == "" {
		return StoryRow{}, fmt.Errorf("%w: row has no story id", ErrMalformedRow)
	}
	return StoryRow{
		ID:        id,
		Title:     get(columnTitle),
		Size:      get(columnSize),
		DependsOn: get(columnDependsOn),
		SubIssue:  get(columnSubIssue),
	}, nil
}

// columnIndex maps a header row's cell text, lower-cased and trimmed, to its
// position — how [ParseStoryIndex] and [RewriteSubIssueCell] read a table
// whose columns are in the contract's order without depending on that order.
func columnIndex(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, cell := range header {
		idx[strings.ToLower(strings.TrimSpace(cell))] = i
	}
	return idx
}

// isTableRow reports whether line is a markdown table row: after trimming,
// it starts with "|".
func isTableRow(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "|")
}

// isSeparatorRow reports whether line is a markdown table's header separator
// — every cell built only from "-" and ":".
func isSeparatorRow(line string) bool {
	cells := splitRow(line)
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		if cell == "" {
			return false
		}
		for _, r := range cell {
			if r != '-' && r != ':' {
				return false
			}
		}
	}
	return true
}

// splitRow splits one markdown table row into its trimmed cells, dropping the
// row's leading and trailing "|".
func splitRow(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	parts := strings.Split(t, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}

// renderRow renders cells back into one markdown table row, "| a | b | c |".
// It does not attempt to reproduce the original line's column padding — a
// Story Index table renders identically to GitHub either way, and matching
// whitespace byte for byte would make every rewrite touch a table's alignment
// as well as its own cell.
func renderRow(cells []string) string {
	return "| " + strings.Join(cells, " | ") + " |"
}
