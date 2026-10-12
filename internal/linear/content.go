package linear

import (
	"errors"
	"strings"
)

// Representation versions the lossless visible factory metadata format.
const Representation = "linear-markdown/v1"
const metadataFence = "```litefactory-metadata-v1"

func managedMarker(line string) bool {
	return (strings.HasPrefix(line, "<!-- lf:") || strings.HasPrefix(line, "<!-- litefactory:")) && strings.HasSuffix(line, " -->")
}

// EncodeContent preserves prose bytes and represents complete factory marker
// lines as visible fenced metadata so Linear does not discard HTML comments.
func EncodeContent(body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	fence := ""
	for _, line := range lines {
		if fence == "" && managedMarker(line) {
			out = append(out, metadataFence, strings.TrimSuffix(strings.TrimPrefix(line, "<!-- "), " -->"), "```")
		} else {
			out = append(out, line)
			fence = nextContentFence(fence, line)
		}
	}
	return strings.Join(out, "\n")
}

// DecodeContent reverses only the exact versioned metadata form. Arbitrary prose,
// spacing and owner edits remain byte-for-byte; malformed factory fences fail.
func DecodeContent(body string) (string, error) {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	fence := ""
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		if fence != "" || line != metadataFence {
			out = append(out, line)
			fence = nextContentFence(fence, line)
			continue
		}
		if index+2 >= len(lines) || lines[index+2] != "```" {
			return "", errors.New("linear: incomplete provider metadata representation")
		}
		marker := "<!-- " + lines[index+1] + " -->"
		if !managedMarker(marker) {
			return "", errors.New("linear: invalid provider metadata representation")
		}
		out = append(out, marker)
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
