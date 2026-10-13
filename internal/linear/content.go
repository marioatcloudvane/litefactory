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
	if metadata.Lines != len(prose)+len(metadata.Markers) {
		return "", invalid
	}
	previous := -1
	for _, marker := range metadata.Markers {
		if marker.Line <= previous || marker.Line >= metadata.Lines || strings.ContainsAny(marker.Value, "\r\n") || !managedMarker("<!-- "+marker.Value+" -->") {
			return "", invalid
		}
		previous = marker.Line
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

var contentTableRule = regexp.MustCompile(`^:?-{3,}:?$`)

func comparableContent(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	// Unknown HTML and inline code can span lines. Do not interpret their
	// whitespace or escapes with the plain-prose rules below.
	fence := ""
	for _, line := range lines {
		next := nextContentFence(fence, line)
		if fence == "" && next == "" && !managedMarker(line) &&
			(strings.ContainsAny(line, "`<") || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")) {
			return body
		}
		fence = next
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
							lines[i+j] = "|" + strings.Join(cells, "|") + "|"
						}
						i = end - 1
						continue
					}
				}
			}
		}
		// Two spaces encode a hard break; never trim those or code whitespace.
		if strings.HasSuffix(line, " ") && !strings.HasSuffix(line, "  ") && !strings.HasSuffix(line, "\\ ") {
			line = strings.TrimSuffix(line, " ")
		}
		lines[i] = redundantContentEscapes(line)
	}
	return strings.Join(lines, "\n")
}

func contentTableCells(line string) ([]string, bool) {
	line = strings.TrimRight(line, " ")
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
