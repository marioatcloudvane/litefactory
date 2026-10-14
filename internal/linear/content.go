package linear

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

// Representation versions the lossless visible factory metadata format.
const Representation = "linear-markdown/v1"
const metadataFence = "```litefactory-metadata-v1"

func managedMarker(line string) bool {
	return (strings.HasPrefix(line, "<!-- lf:") || strings.HasPrefix(line, "<!-- litefactory:")) && strings.HasSuffix(line, " -->")
}

const metadataFooter = "+++ Factory metadata"
const relocatedMetadataFence = "```litefactory-metadata-v2"

type relocatedMarker struct {
	Line  int    `json:"line"`
	Value string `json:"value"`
	CR    bool   `json:"cr,omitempty"`
}

type relocatedMetadata struct {
	Lines   int               `json:"lines"`
	Markers []relocatedMarker `json:"markers"`
}

// EncodeContent moves standalone markers into one collapsed footer. Line
// positions preserve the exact canonical body used by ownership and recovery.
func EncodeContent(body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	metadata := relocatedMetadata{Lines: len(lines)}
	fence := ""
	for index, line := range lines {
		marker := strings.TrimSuffix(line, "\r")
		if fence == "" && managedMarker(marker) {
			metadata.Markers = append(metadata.Markers, relocatedMarker{Line: index, Value: strings.TrimSuffix(strings.TrimPrefix(marker, "<!-- "), " -->"), CR: marker != line})
		} else {
			out = append(out, line)
			fence = nextContentFence(fence, line)
		}
	}
	if len(metadata.Markers) == 0 {
		return body
	}
	// This struct contains only integers, strings and booleans; Marshal cannot fail.
	encoded, _ := json.Marshal(metadata)
	return strings.Join(out, "\n") + "\n\n" + metadataFooter + "\n" + relocatedMetadataFence + "\n" + string(encoded) + "\n```\n+++"
}

// decodeRelocatedMetadata accepts only our footer, never an ordinary collapsible
// section. Counts and ordered positions reject truncation and ambiguous layouts.
func decodeRelocatedMetadata(lines []string, index int) (string, error) {
	invalid := errors.New("linear: invalid relocated provider metadata representation")
	start, end := index-1, index+3
	// Linear may add blank lines around fenced code in a collapsible section
	// and a final newline. These belong to the envelope, not canonical prose.
	if start >= 0 && strings.TrimSuffix(lines[start], "\r") == "" {
		start--
	}
	if end < len(lines) && strings.TrimSuffix(lines[end], "\r") == "" {
		end++
	}
	last := len(lines) - 1
	if last >= 0 && strings.TrimSuffix(lines[last], "\r") == "" {
		last--
	}
	if start < 2 || end != last || index+2 >= len(lines) || strings.TrimSuffix(lines[start], "\r") != metadataFooter ||
		strings.TrimSuffix(lines[start-1], "\r") != "" || strings.TrimSuffix(lines[index+2], "\r") != "```" || strings.TrimSuffix(lines[end], "\r") != "+++" {
		return "", invalid
	}
	var metadata relocatedMetadata
	payload := strings.TrimSuffix(lines[index+1], "\r")
	if err := json.Unmarshal([]byte(payload), &metadata); err != nil {
		return "", invalid
	}
	canonical, _ := json.Marshal(metadata)
	if string(canonical) != payload || len(metadata.Markers) == 0 {
		return "", invalid
	}
	prose := lines[:start-1]
	// An empty visible body has no lines, unlike a retained empty canonical line.
	if metadata.Lines == len(metadata.Markers) && len(prose) == 1 && prose[0] == "" {
		prose = nil
	}
	previous := -1
	for _, marker := range metadata.Markers {
		if marker.Line <= previous || marker.Line >= metadata.Lines || strings.ContainsAny(marker.Value, "\r\n") || !managedMarker("<!-- "+marker.Value+" -->") {
			return "", invalid
		}
		previous = marker.Line
	}
	if metadata.Lines != len(prose)+len(metadata.Markers) {
		// Linear normalizes Markdown (it drops blank lines, for example), so the
		// recorded line positions no longer fit. Factory markers only lead or
		// trail a body: restore the leading run in front and the rest after the
		// prose. Content comparison is Linear-equivalence, not byte equality.
		return relocateNormalized(prose, metadata.Markers), nil
	}
	out := make([]string, 0, metadata.Lines)
	m, p := 0, 0
	for line := 0; line < metadata.Lines; line++ {
		if m < len(metadata.Markers) && metadata.Markers[m].Line == line {
			marker := metadata.Markers[m]
			value := "<!-- " + marker.Value + " -->"
			if marker.CR {
				value += "\r"
			}
			out = append(out, value)
			m++
		} else {
			out = append(out, prose[p])
			p++
		}
	}
	return strings.Join(out, "\n"), nil
}

func relocateNormalized(prose []string, markers []relocatedMarker) string {
	render := func(m relocatedMarker) string {
		value := "<!-- " + m.Value + " -->"
		if m.CR {
			value += "\r"
		}
		return value
	}
	out := make([]string, 0, len(prose)+len(markers))
	leading := 0
	for leading < len(markers) && markers[leading].Line == leading {
		out = append(out, render(markers[leading]))
		leading++
	}
	out = append(out, prose...)
	for _, m := range markers[leading:] {
		out = append(out, render(m))
	}
	return strings.Join(out, "\n")
}

// DecodeContent reverses only the exact versioned metadata form. Arbitrary prose,
// spacing and owner edits remain byte-for-byte; malformed factory fences fail.
func DecodeContent(body string) (string, error) {
	lines := strings.Split(body, "\n")
	f := ""
	legacy := false
	for index, line := range lines {
		if f == "" && strings.TrimSuffix(line, "\r") == relocatedMetadataFence {
			if legacy {
				return "", errors.New("linear: mixed provider metadata representations")
			}
			return decodeRelocatedMetadata(lines, index)
		}
		if f == "" && strings.TrimSuffix(line, "\r") == metadataFence {
			legacy = true
		}
		f = nextContentFence(f, line)
	}
	out := make([]string, 0, len(lines))
	fence := ""
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		if fence != "" || strings.TrimSuffix(line, "\r") != metadataFence {
			out = append(out, line)
			fence = nextContentFence(fence, line)
			continue
		}
		if index+2 >= len(lines) || strings.TrimSuffix(lines[index+2], "\r") != "```" {
			return "", errors.New("linear: incomplete provider metadata representation")
		}
		marker := "<!-- " + strings.TrimSuffix(lines[index+1], "\r") + " -->"
		if !managedMarker(marker) {
			return "", errors.New("linear: invalid provider metadata representation")
		}
		cr := strings.TrimPrefix(lines[index+2], "```")
		out = append(out, marker+cr)
		index += 2
	}
	return strings.Join(out, "\n"), nil
}

func nextContentFence(current, line string) string {
	trimmed := strings.TrimSpace(line)
	if current != "" {
		if strings.HasPrefix(trimmed, current) && strings.Trim(trimmed, string(current[0])) == "" {
			return ""
		}
		return current
	}
	if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
		index := 0
		for index < len(trimmed) && trimmed[index] == trimmed[0] {
			index++
		}
		return trimmed[:index]
	}
	return ""
}

// ContentEquivalent accepts only bounded, understood Markdown presentation
// changes. Metadata stays exact; raw provider content still owns the revision.
func ContentEquivalent(expected, observed string) bool {
	if expected == observed {
		return true
	}
	if len(expected) > 1<<20 || len(observed) > 1<<20 {
		return false
	}
	return comparableContent(expected) == comparableContent(observed)
}

// withoutProseBlankLines normalizes blank-line layout outside fences the way
// Linear stores it: runs of blank lines collapse to one, leading and trailing
// blank lines go, and blank lines next to factory markers go (relocated markers
// lose their surroundings). A single blank line between prose stays, since it
// separates paragraphs. Blank lines between list items go: Linear
// stores lists tight.
func withoutProseBlankLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	fence := ""
	blank := false
	for _, line := range lines {
		next := nextContentFence(fence, line)
		if fence == "" && next == "" && strings.TrimSpace(line) == "" {
			blank = true
			fence = next
			continue
		}
		_, listed := contentListItem(line)
		previousListed := false
		if len(out) > 0 {
			_, previousListed = contentListItem(out[len(out)-1])
		}
		// Linear stores lists tight: a blank line between two list items is
		// dropped (seen on MAR-12's technical draft, 2026-10-14).
		if blank && len(out) > 0 && !managedMarker(out[len(out)-1]) && !managedMarker(line) && (!listed || !previousListed) {
			out = append(out, "")
		}
		blank = false
		out = append(out, line)
		fence = next
	}
	return out
}

var contentTableRule = regexp.MustCompile(`^:?-{2,}:?$`)

func comparableContent(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	// Unknown HTML and inline code can span lines. Do not interpret their
	// whitespace or escapes with the plain-prose rules below.
	fence := ""
	for i, line := range lines {
		next := nextContentFence(fence, line)
		// A table row is one line, so inline code or HTML inside its cells
		// cannot span lines; its cells keep those characters untouched.
		if fence == "" && next == "" && !managedMarker(line) && !strings.HasPrefix(line, "|") &&
			(strings.ContainsAny(line, "`<") || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")) {
			return body
		}
		// Classify scaffold headings after the already-supported single-space
		// normalization, while retaining hard breaks, metadata and code bytes.
		if fence == "" && next == "" && !managedMarker(line) && strings.HasSuffix(line, " ") && !strings.HasSuffix(line, "  ") && !strings.HasSuffix(line, "\\ ") {
			lines[i] = strings.TrimSuffix(line, " ")
		}
		fence = next
	}
	story := managedStoryDocument(lines)
	lines, scaffold := comparableScaffoldSpacing(lines)
	if !uniformContentLists(lines) {
		return body
	}
	fence = ""
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		next := nextContentFence(fence, line)
		if fence != "" || next != "" || managedMarker(line) {
			fence = next
			continue
		}
		// Like the owned Plan scaffold parser, accept only explicit outer pipes
		// and fixed column counts. Preserve alignment and reject ambiguous cells.
		if i+1 < len(lines) {
			header, ok := contentTableCells(line)
			rules, ruleOK := contentTableCells(lines[i+1])
			if ok && ruleOK && len(header) == len(rules) {
				valid := true
				for _, rule := range rules {
					valid = valid && contentTableRule.MatchString(rule)
				}
				if valid {
					end := i + 2
					rows := [][]string{header, rules}
					for end < len(lines) && strings.HasPrefix(lines[end], "|") {
						cells, cellOK := contentTableCells(lines[end])
						if !cellOK || len(cells) != len(header) {
							valid = false
							break
						}
						rows = append(rows, cells)
						end++
					}
					if valid {
						for j, rule := range rules {
							rules[j] = "---"
							if strings.HasPrefix(rule, ":") {
								rules[j] = ":" + rules[j]
							}
							if strings.HasSuffix(rule, ":") {
								rules[j] += ":"
							}
						}
						for j, cells := range rows {
							if j > 1 && story {
								for k := range cells {
									cells[k] = comparableTableCell(cells[k])
								}
							}
							lines[i+j] = "|" + strings.Join(cells, "|") + "|"
						}
						i = end - 1
						continue
					}
				}
			}
		}
		if depth, ok := contentListItem(line); ok {
			line = line[:depth] + "-" + line[depth+1:]
		}
		if scaffold {
			line = comparableScaffoldProse(line)
		} else {
			// Later Plan stages add headings, so they are not the scaffold, but
			// the renderer escapes the same punctuation as entities there and
			// Linear stores the plain characters. The decoder only accepts plain
			// prose, so it cannot manufacture Markdown.
			line = redundantContentEscapes(line)
			if story {
				line = comparableScaffoldProse(line)
			}
		}
		lines[i] = line
	}
	return strings.Join(withoutProseBlankLines(lines), "\n")
}

func contentTableCells(line string) ([]string, bool) {
	// Linear escapes a literal underscore as \_ ; it is the same character.
	line = strings.ReplaceAll(strings.TrimRight(line, " "), `\_`, "_")
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") || len(line) < 2 || strings.ContainsAny(line, "\\`") {
		return nil, false
	}
	cells := strings.Split(line[1:len(line)-1], "|")
	for i := range cells {
		cells[i] = strings.Trim(cells[i], " \t")
	}
	return cells, true
}

// Only punctuation that cannot introduce inline structure here is unescaped.
// Paired backslashes, links, code and block-leading punctuation stay literal.
func redundantContentEscapes(line string) string {
	if strings.ContainsAny(line, "[]:@") || (strings.Contains(line, "www.") || strings.Contains(line, `www\.`)) {
		return line
	}
	var out strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' && i+1 < len(line) {
			next := line[i+1]
			if i > 0 && strings.ContainsRune("!.,?", rune(next)) && ((line[i-1] >= 'a' && line[i-1] <= 'z') || (line[i-1] >= 'A' && line[i-1] <= 'Z')) {
				out.WriteByte(next)
			} else {
				out.WriteByte(line[i])
				out.WriteByte(next)
			}
			i++
		} else {
			out.WriteByte(line[i])
		}
	}
	return out.String()
}

// Recognize only unambiguous unordered items. Indentation is never rewritten.
func contentListItem(line string) (int, bool) {
	depth := len(line) - len(strings.TrimLeft(line, " "))
	if depth > 3 || len(line) < depth+3 || !strings.ContainsRune("-*+", rune(line[depth])) || line[depth+1] != ' ' || line[depth+2] == ' ' {
		return 0, false
	}
	if strings.Trim(line[depth:], " -*+") == "" {
		return 0, false
	}
	return depth, true
}

// Switching marker within a run can split Markdown lists; keep that grouping
// significant rather than flattening all bullet punctuation indiscriminately.
func uniformContentLists(lines []string) bool {
	markers := map[int]byte{}
	fence := ""
	for _, line := range lines {
		next := nextContentFence(fence, line)
		if fence != "" || next != "" {
			fence = next
			clear(markers)
			continue
		}
		if line == "" {
			continue
		}
		depth, ok := contentListItem(line)
		if !ok {
			clear(markers)
			continue
		}
		if prior, exists := markers[depth]; exists && prior != line[depth] {
			return false
		}
		for d := range markers {
			if d > depth {
				delete(markers, d)
			}
		}
		markers[depth] = line[depth]
	}
	return true
}

var scaffoldStoryMarker = regexp.MustCompile(`^<!-- lf:plan-story parent=[0-9]+ key=[a-zA-Z0-9_-]+ -->$`)
var scaffoldRequestMarker = regexp.MustCompile(`^<!-- lf:plan-request digest=[0-9a-f]{64} -->$`)

// Only the full factory scaffold permits the observed block spacing changes.
// Unknown Markdown keeps its paragraph and loose-list boundaries untouched.
func comparableScaffoldSpacing(lines []string) ([]string, bool) {
	if len(lines) < 8 || !scaffoldStoryMarker.MatchString(lines[0]) {
		return lines, false
	}
	request := 1
	if lines[request] == "" {
		request++
	}
	if !scaffoldRequestMarker.MatchString(lines[request]) {
		return lines, false
	}
	start := request + 1
	if lines[start] == "" {
		start++
	}
	if lines[start] != "# Userstory" {
		return lines, false
	}
	end := len(lines) - 1
	if lines[end] == "" {
		end--
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(lines[end], "<!-- lf:plan-content v=1 data="), " -->")
	if !strings.HasPrefix(lines[end], "<!-- lf:plan-content v=1 data=") || !strings.HasSuffix(lines[end], " -->") || !json.Valid([]byte(payload)) {
		return lines, false
	}
	headings := []string{"# Userstory", "## What this story solves", "## Scope", "# Task", "# Acceptance Criterias", "# Architecture"} //nolint:misspell // Exact heading emitted by the existing story renderer.
	positions := make([]int, 0, len(headings))
	for i := start; i < end; i++ {
		if strings.HasPrefix(lines[i], "#") {
			if len(positions) >= len(headings) || lines[i] != headings[len(positions)] {
				return lines, false
			}
			positions = append(positions, i)
		}
	}
	if len(positions) != len(headings) {
		return lines, false
	}
	// Notes in the initial scaffold are single plain items. Never collapse
	// paragraph boundaries for multiline, nested or inline-structured items.
	simpleNote := func(line string) bool {
		depth, ok := contentListItem(line)
		return ok && depth == 0 && !strings.ContainsAny(line[2:], "*_`[]<>\\") && !strings.HasSuffix(line, "  ")
	}
	out := []string{lines[0], lines[request], lines[start]}
	for i := start + 1; i <= end; i++ {
		if lines[i] == "" {
			j := i
			for j < end && lines[j] == "" {
				j++
			}
			count := j - i
			if count <= 2 && (strings.HasPrefix(lines[j], "#") || j == end) {
				out = append(out, "")
				i = j - 1
				continue
			}
			if count == 1 && i > start+1 && i < positions[1] && simpleNote(lines[i-1]) && simpleNote(lines[j]) {
				continue
			}
		}
		out = append(out, lines[i])
	}
	if end < len(lines)-1 {
		out = append(out, "")
	}
	return out, true
}

// Decode only the four observed entities in plain scaffold prose. Inspect both
// forms before replacement: decoding must never manufacture Markdown syntax.
func comparableScaffoldProse(line string) string {
	prefix := ""
	prose := line
	if depth, ok := contentListItem(line); ok {
		if depth != 0 {
			return line
		}
		prefix, prose = line[:2], line[2:]
		// A task-list checkbox belongs to the item prefix, not to its prose.
		for _, box := range []string{"[ ] ", "[x] ", "[X] "} {
			if strings.HasPrefix(prose, box) {
				prefix, prose = prefix+box, prose[len(box):]
				break
			}
		}
	}
	decoded := strings.NewReplacer("&#46;", ".", "&#45;", "-", "&#40;", "(", "&#41;", ")").Replace(prose)
	if !plainScaffoldProse(decoded) || strings.ContainsAny(prose, "\\`[]<>*_!|@") {
		return line
	}
	return prefix + decoded
}

// comparableTableCell decodes the renderer's four punctuation entities inside a
// table body cell, where Linear stores the plain characters (and \_ for _).
// A cell that could form links or code, or is only a rule, is left as it is.
// managedStoryDocument reports a factory Plan story body (story and request
// markers first), the only content whose punctuation entities are decoded.
func managedStoryDocument(lines []string) bool {
	if len(lines) < 2 || !scaffoldStoryMarker.MatchString(lines[0]) {
		return false
	}
	request := 1
	if lines[request] == "" && len(lines) > 2 {
		request++
	}
	return scaffoldRequestMarker.MatchString(lines[request])
}

func comparableTableCell(cell string) string {
	if strings.ContainsAny(cell, "[]`\\") {
		return cell
	}
	decoded := strings.NewReplacer("&#46;", ".", "&#45;", "-", "&#40;", "(", "&#41;", ")", "&#95;", "_", "&#60;", "<", "&#62;", ">").Replace(cell)
	if strings.Trim(decoded, "-:. ") == "" {
		return cell
	}
	return decoded
}

func plainScaffoldProse(s string) bool {
	letter := func(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }
	alnum := func(b byte) bool { return letter(b) || b >= '0' && b <= '9' }
	if s == "" || !letter(s[0]) || strings.ContainsAny(s, "&\\`[]<>*_!|@#") || strings.Contains(s, "://") || strings.Contains(s, "www.") {
		return false
	}
	opened := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '.':
			if i == 0 || !letter(s[i-1]) || (i+1 < len(s) && s[i+1] != ' ') {
				return false
			}
		case '-':
			// Between letters or digits (AC-001, outside-click): never Markdown.
			if i == 0 || i+1 == len(s) || !alnum(s[i-1]) || !alnum(s[i+1]) {
				return false
			}
		case '(':
			if opened || i == 0 || s[i-1] != ' ' || i+1 == len(s) || !letter(s[i+1]) {
				return false
			}
			opened = true
		case ')':
			if !opened || i == 0 || !letter(s[i-1]) || (i+1 < len(s) && s[i+1] != ' ' && s[i+1] != '.') {
				return false
			}
			opened = false
		}
	}
	return !opened
}
