package team

import (
	"bytes"
	"sort"
	"strings"
)

// Marker delimiters wrapping one agent's section inside a shared root file
// (AGENTS.md, CLAUDE.md). The BEGIN line's team= value lists every team
// contributing this agent's section, sorted and comma-separated, so a
// shared agent referenced by more than one team gets exactly one section
// rather than one per team (US-18's dedup rule). Identity for locating an
// existing section is the agent key alone: the team list can change
// (a team starts or stops contributing a shared agent) without changing
// which section it is.
const (
	markerBeginPrefix = "<!-- litefactory:team BEGIN "
	markerSuffix      = " -->"
	markerEndLineText = "<!-- litefactory:team END -->"
)

// sortedUniqueTeams returns teams deduplicated and sorted, for a
// deterministic BEGIN line regardless of the order AgentRender.Teams was
// built in.
func sortedUniqueTeams(teams []string) []string {
	set := make(map[string]struct{}, len(teams))
	for _, t := range teams {
		set[t] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// markerBeginLine builds one agent's BEGIN delimiter line.
func markerBeginLine(teams []string, agentKey string) string {
	return markerBeginPrefix + "team=" + strings.Join(sortedUniqueTeams(teams), ",") + " agent=" + agentKey + markerSuffix
}

// buildMarkerSection builds the full marker-enclosed span for one agent:
// its BEGIN line, its body trimmed to exactly one trailing newline (or
// omitted entirely for an empty body), and its END line. This is the
// "virtual file" markerSpanHash hashes and upsertMarkerSection places into
// a root file.
func buildMarkerSection(teams []string, agentKey string, body []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString(markerBeginLine(teams, agentKey))
	buf.WriteByte('\n')
	content := bytes.TrimRight(body, "\n")
	if len(content) > 0 {
		buf.Write(content)
		buf.WriteByte('\n')
	}
	buf.WriteString(markerEndLineText)
	buf.WriteByte('\n')
	return buf.Bytes()
}

// markerSpanHash hashes the exact bytes buildMarkerSection would place for
// this agent, including its delimiter lines. Including the BEGIN line
// means a change in the set of contributing teams — not just a change in
// body content — is enough to invalidate a previously recorded
// RenderedHash, so Add's no-op short-circuit never leaves a stale team
// list sitting in a root file.
func markerSpanHash(teams []string, agentKey string, body []byte) string {
	return HashBytes(buildMarkerSection(teams, agentKey, body))
}

// parseBeginLine extracts the team list and agent key from one BEGIN
// delimiter line. ok is false if line is not a well-formed BEGIN line.
func parseBeginLine(line string) (teams []string, agent string, ok bool) {
	if !strings.HasPrefix(line, markerBeginPrefix) || !strings.HasSuffix(line, markerSuffix) {
		return nil, "", false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(line, markerBeginPrefix), markerSuffix)
	var teamStr string
	for _, field := range strings.Fields(inner) {
		if v, cut := strings.CutPrefix(field, "team="); cut {
			teamStr = v
		} else if v, cut := strings.CutPrefix(field, "agent="); cut {
			agent = v
		}
	}
	if teamStr == "" || agent == "" {
		return nil, "", false
	}
	return strings.Split(teamStr, ","), agent, true
}

// locateMarkerSection finds the byte range [start, end) of the marker
// section identified by agentKey within content, including both delimiter
// lines and their trailing newlines. found is false if no such section is
// present, or if a BEGIN line for agentKey is present with no matching END
// line after it (a malformed file this package will not have produced
// itself, but must not corrupt further).
func locateMarkerSection(content []byte, agentKey string) (start, end int, found bool) {
	pos := 0
	for pos < len(content) {
		lineStart := pos
		lineEnd, next := nextLine(content, pos)
		line := strings.TrimRight(string(content[lineStart:lineEnd]), "\n")
		pos = next

		_, agent, ok := parseBeginLine(line)
		if !ok || agent != agentKey {
			continue
		}

		for pos < len(content) {
			eStart := pos
			eEnd, eNext := nextLine(content, pos)
			eLine := strings.TrimRight(string(content[eStart:eEnd]), "\n")
			pos = eNext
			if eLine == markerEndLineText {
				return lineStart, eEnd, true
			}
		}
		return 0, 0, false
	}
	return 0, 0, false
}

// nextLine returns the end offset of the line starting at pos (including
// its trailing newline, if any) and the offset to resume scanning from.
func nextLine(content []byte, pos int) (lineEnd, next int) {
	idx := bytes.IndexByte(content[pos:], '\n')
	if idx < 0 {
		return len(content), len(content)
	}
	return pos + idx + 1, pos + idx + 1
}

// upsertMarkerSection replaces agentKey's existing marker section in
// content in place, leaving every byte outside the span untouched, or
// appends a new section — separated from existing content by exactly one
// blank line — if no section for agentKey is present yet.
func upsertMarkerSection(content []byte, teams []string, agentKey string, body []byte) []byte {
	section := buildMarkerSection(teams, agentKey, body)

	if start, end, found := locateMarkerSection(content, agentKey); found {
		out := make([]byte, 0, len(content)-(end-start)+len(section))
		out = append(out, content[:start]...)
		out = append(out, section...)
		out = append(out, content[end:]...)
		return out
	}

	if len(content) == 0 {
		return section
	}
	trimmed := bytes.TrimRight(content, "\n")
	out := make([]byte, 0, len(trimmed)+2+len(section))
	out = append(out, trimmed...)
	out = append(out, '\n', '\n')
	out = append(out, section...)
	return out
}

// removeMarkerSection removes agentKey's marker section from content, for
// a future removal story (US-11) to call. It is a primitive: it deletes
// the identified span and collapses the resulting run of blank lines at
// the seam down to at most one, but does not otherwise attempt to make the
// surrounding file's whitespace look hand-edited. rootFile is used only to
// identify the file in the returned error.
func removeMarkerSection(content []byte, rootFile, agentKey string) ([]byte, error) {
	start, end, found := locateMarkerSection(content, agentKey)
	if !found {
		return nil, &MarkerNotFoundError{RootFile: rootFile, Agent: agentKey}
	}
	out := make([]byte, 0, len(content)-(end-start))
	out = append(out, content[:start]...)
	out = append(out, content[end:]...)
	for bytes.Contains(out, []byte("\n\n\n")) {
		out = bytes.ReplaceAll(out, []byte("\n\n\n"), []byte("\n\n"))
	}
	return out, nil
}
