package git

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Safety ceilings remain independent of the configured aggregate input budget.
// MaxConflictRegionBytes matches the maximum supported input setting.
const (
	RegionProposalVersion    = 1
	MaxConflictOriginalBytes = 1 << 20
	MaxConflictRegionBytes   = 1 << 20
	MaxConflictProposalFiles = 100
	MaxConflictOutputBytes   = 1 << 20
)

// ConflictRefusal describes a policy failure without echoing source content.
type ConflictRefusal struct{ Path, Reason string }

func (e *ConflictRefusal) Error() string {
	return fmt.Sprintf("%q: %s; resolve manually or regenerate", e.Path, e.Reason)
}
func refuse(path, reason string) error { return &ConflictRefusal{Path: path, Reason: reason} }

// ConflictRegion offsets are factory-owned byte offsets, including marker terminators.
type ConflictRegion struct {
	ID                                                             string
	Start, End                                                     int
	Content, Ours, Theirs, OpeningTerm, SeparatorTerm, ClosingTerm string
}

// RegionResolution associates generated text with one factory identity.
type RegionResolution struct {
	ID         string `json:"id"`
	Resolution string `json:"resolution"`
}

// ConflictSnapshot holds bounded immutable originals transiently under the clone gate.
type ConflictSnapshot struct {
	Path, Original, Baseline, Mode, BranchSHA string
	Regions                                   []ConflictRegion
	ancestor, other                           string
	hasAncestor                               bool
}

// ConflictMetrics measure exact raw task-branch baseline and result bytes.
type ConflictMetrics struct{ BeforeBytes, AfterBytes, BeforeLines, AfterLines, Additions, Deletions int }

// ValidatedConflict binds a reconstructed result to canonical source and raw metrics.
type ValidatedConflict struct {
	Path, OriginalDigest, ResultDigest, Result, Mode string
	Metrics                                          ConflictMetrics
	Regions                                          []RegionResolution
	Diff                                             string
}

// ConflictDigest hashes exact bytes, without newline normalization.
func ConflictDigest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// ParseConflictRegions accepts only ordinary seven-character marker lines.
// Marker-looking malformed text is refused rather than silently salvaged.
func ParseConflictRegions(path, original string) ([]ConflictRegion, error) {
	if len(original) > MaxConflictOriginalBytes {
		return nil, refuse(path, "original exceeds 1 MiB")
	}
	if !utf8.ValidString(original) || strings.ContainsRune(original, 0) {
		return nil, refuse(path, "original is not UTF-8 text")
	}
	var regions []ConflictRegion
	state, pos, sideStart := 0, 0, 0
	var region ConflictRegion
	for pos < len(original) {
		start := pos
		end := strings.IndexByte(original[pos:], '\n')
		if end < 0 {
			pos = len(original)
		} else {
			pos += end + 1
		}
		raw := original[start:pos]
		line := strings.TrimSuffix(raw, "\n")
		term := raw[len(line):]
		if strings.HasSuffix(line, "\r") {
			line = strings.TrimSuffix(line, "\r")
			term = "\r" + term
		}
		kind := byte(0)
		for _, c := range []byte{'<', '=', '>', '|'} {
			if strings.HasPrefix(line, strings.Repeat(string(c), 7)) {
				kind = c
				break
			}
		}
		if kind == 0 {
			continue
		}
		valid := false
		switch kind {
		case '<':
			valid = state == 0 && (line == "<<<<<<<" || strings.HasPrefix(line, "<<<<<<< "))
			if valid {
				region = ConflictRegion{Start: start, OpeningTerm: term}
				sideStart = pos
				state = 1
			}
		case '=':
			valid = state == 1 && line == "======="
			if valid {
				region.Ours = original[sideStart:start]
				region.SeparatorTerm = term
				sideStart = pos
				state = 2
			}
		case '>':
			valid = state == 2 && (line == ">>>>>>>" || strings.HasPrefix(line, ">>>>>>> "))
			if valid {
				region.Theirs = original[sideStart:start]
				region.ClosingTerm = term
				region.End = pos
				region.Content = original[region.Start:pos]
				region.ID = ConflictDigest(path + "\x00" + strconv.Itoa(len(regions)) + "\x00" + region.Content)
				regions = append(regions, region)
				state = 0
			}
		}
		if !valid {
			return nil, refuse(path, "unsupported or malformed conflict markers")
		}
	}
	if state != 0 || len(regions) == 0 {
		return nil, refuse(path, "missing or unbalanced ordinary conflict regions")
	}
	return regions, nil
}

// ReconstructConflict uses only parsed spans, never model offsets or response order.
func ReconstructConflict(snapshot ConflictSnapshot, resolutions []RegionResolution) (string, error) {
	regions, err := ParseConflictRegions(snapshot.Path, snapshot.Original)
	if err != nil {
		return "", err
	}
	if len(regions) != len(resolutions) {
		return "", refuse(snapshot.Path, "incomplete region coverage")
	}
	byID := make(map[string]string, len(resolutions))
	total := 0
	for _, r := range resolutions {
		if _, ok := byID[r.ID]; ok {
			return "", refuse(snapshot.Path, "duplicate region")
		}
		byID[r.ID] = r.Resolution
		total += len(r.Resolution)
	}
	if total > MaxConflictOutputBytes {
		return "", refuse(snapshot.Path, "resolution exceeds 1 MiB")
	}
	var out strings.Builder
	pos := 0
	for _, r := range regions {
		text, ok := byID[r.ID]
		if !ok {
			return "", refuse(snapshot.Path, "unknown or missing region")
		}
		out.WriteString(snapshot.Original[pos:r.Start])
		out.WriteString(text)
		pos = r.End
	}
	out.WriteString(snapshot.Original[pos:])
	return out.String(), nil
}

// EquivalentConflictOriginals allows only opening/closing marker text labels to differ.
func EquivalentConflictOriginals(path, canonical, actual string) error {
	a, err := ParseConflictRegions(path, canonical)
	if err != nil {
		return err
	}
	b, err := ParseConflictRegions(path, actual)
	if err != nil {
		return err
	}
	if len(a) != len(b) {
		return refuse(path, "actual merge region count changed")
	}
	ap, bp := 0, 0
	for i, x := range a {
		y := b[i]
		if canonical[ap:x.Start] != actual[bp:y.Start] || x.Ours != y.Ours || x.Theirs != y.Theirs || x.OpeningTerm != y.OpeningTerm || x.SeparatorTerm != y.SeparatorTerm || x.ClosingTerm != y.ClosingTerm {
			return refuse(path, "actual merge bytes changed")
		}
		ap = x.End
		bp = y.End
	}
	if canonical[ap:] != actual[bp:] {
		return refuse(path, "actual merge suffix changed")
	}
	return nil
}

// LogicalLines counts terminated lines and a final unterminated line, if present.
func LogicalLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// ValidateConflictResult is deliberately usable independently of collection caps.
func ValidateConflictResult(path, baseline, result string) (ConflictMetrics, error) {
	m := ConflictMetrics{BeforeBytes: len(baseline), AfterBytes: len(result), BeforeLines: LogicalLines(baseline), AfterLines: LogicalLines(result)}
	var faults []error
	if !utf8.ValidString(result) || strings.ContainsRune(result, 0) {
		faults = append(faults, refuse(path, "result is not UTF-8 text"))
	}
	for _, marker := range []string{"<<<<<<<", "=======", ">>>>>>>", "|||||||"} {
		if strings.Contains(result, marker) {
			faults = append(faults, refuse(path, "result contains unresolved markers"))
			break
		}
	}
	if m.AfterBytes*2 < m.BeforeBytes || m.AfterLines*2 < m.BeforeLines {
		faults = append(faults, refuse(path, "result removes more than half baseline bytes or lines"))
	}
	if strings.HasSuffix(path, ".go") {
		if _, err := parser.ParseFile(token.NewFileSet(), path, result, parser.AllErrors); err != nil {
			faults = append(faults, refuse(path, "complete Go result fails syntax parsing"))
		}
	}
	if err := errors.Join(faults...); err != nil {
		return m, err
	}
	var err error
	m.Additions, m.Deletions, err = rawLineChanges(baseline, result)
	if err != nil {
		return m, refuse(path, "raw diff exceeds bounded comparison work; resolve manually")
	}
	return m, nil
}

// ValidateConflict reconstructs, validates and computes identities and raw statistics.
func ValidateConflict(snapshot ConflictSnapshot, resolutions []RegionResolution) (ValidatedConflict, error) {
	result, err := ReconstructConflict(snapshot, resolutions)
	if err != nil {
		return ValidatedConflict{}, err
	}
	ordered := make([]RegionResolution, 0, len(resolutions))
	byID := make(map[string]string, len(resolutions))
	for _, r := range resolutions {
		byID[r.ID] = r.Resolution
	}
	regions, err := ParseConflictRegions(snapshot.Path, snapshot.Original)
	if err != nil {
		return ValidatedConflict{}, err
	}
	for _, r := range regions {
		ordered = append(ordered, RegionResolution{ID: r.ID, Resolution: byID[r.ID]})
	}
	m, err := ValidateConflictResult(snapshot.Path, snapshot.Baseline, result)
	if err != nil {
		return ValidatedConflict{}, err
	}
	return ValidatedConflict{Path: snapshot.Path, Mode: snapshot.Mode, OriginalDigest: ConflictDigest(snapshot.Original), ResultDigest: ConflictDigest(result), Result: result, Metrics: m, Regions: ordered, Diff: rawConflictDiff(snapshot.Baseline, result)}, nil
}

func rawLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Myers' frontier counts a shortest raw-line edit script with linear memory.
func rawLineChanges(before, after string) (int, int, error) {
	a, b := rawLines(before), rawLines(after)
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		a = a[1:]
		b = b[1:]
	}
	for len(a) > 0 && len(b) > 0 && a[len(a)-1] == b[len(b)-1] {
		a = a[:len(a)-1]
		b = b[:len(b)-1]
	}
	n, m := len(a), len(b)
	maxD := n + m
	if maxD == 0 {
		return 0, 0, nil
	}
	v := make([]int, 2*maxD+3)
	off := maxD + 1
	work := 0
	for d := 0; d <= maxD; d++ {
		for k := -d; k <= d; k += 2 {
			work++
			if work > 4_000_000 {
				return 0, 0, errors.New("diff work limit exceeded")
			}
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				work++
				if work > 4_000_000 {
					return 0, 0, errors.New("diff work limit exceeded")
				}
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				return (d + m - n) / 2, (d + n - m) / 2, nil
			}
		}
	}
	return m, n, nil
}

// The preview is bounded independently of raw metrics. Report sanitizes it.
func rawConflictDiff(before, after string) string {
	if before == after {
		return ""
	}
	a, b := rawLines(before), rawLines(after)
	prefix := 0
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		a = a[1:]
		b = b[1:]
		prefix++
	}
	for len(a) > 0 && len(b) > 0 && a[len(a)-1] == b[len(b)-1] {
		a = a[:len(a)-1]
		b = b[:len(b)-1]
	}
	var out strings.Builder
	fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", prefix+1, len(a), prefix+1, len(b))
	for i, lines := range [][]string{a, b} {
		mark := "-"
		if i == 1 {
			mark = "+"
		}
		for _, line := range lines {
			if out.Len()+len(line)+1 > 8192 {
				out.WriteString("[diff omitted]\n")
				return out.String()
			}
			out.WriteString(mark)
			out.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				out.WriteString("\n\\ No newline at end of file\n")
			}
		}
	}
	return out.String()
}

// ResolveConflictAdditions proves same-gap additions using immutable side blobs.
// Exact ancestor prefixes prove EOF appends. Interior insertions additionally
// require unique ancestor embeddings. Other edits fall back to the advisor.
func ResolveConflictAdditions(s ConflictSnapshot) ([]RegionResolution, bool) {
	if !s.hasAncestor {
		return nil, false
	}
	gap, ours, ok := conflictAddition(s.ancestor, s.Baseline)
	otherGap, theirs, otherOK := conflictAddition(s.ancestor, s.other)
	if !ok || !otherOK || gap != otherGap {
		return nil, false
	}
	union := ours
	if ours != theirs {
		if overlappingAdditions(ours, theirs) {
			return nil, false
		}
		union += theirs
	}
	expected := s.ancestor[:gap] + union + s.ancestor[gap:]
	regions, err := ParseConflictRegions(s.Path, s.Original)
	if err != nil || len(regions) != 1 {
		return nil, false
	}
	// Git may move shared addition prefixes/suffixes outside its markers. Keep
	// those exact bytes and resolve only the identified span.
	r := regions[0]
	prefix, suffix := s.Original[:r.Start], s.Original[r.End:]
	if len(prefix)+len(suffix) > len(expected) || !strings.HasPrefix(expected, prefix) || !strings.HasSuffix(expected, suffix) {
		return nil, false
	}
	return []RegionResolution{{ID: r.ID, Resolution: expected[len(prefix) : len(expected)-len(suffix)]}}, true
}

func conflictAddition(ancestor, side string) (int, string, bool) {
	if len(side) <= len(ancestor) {
		return 0, "", false
	}
	// Appending after an unterminated line would replace that line.
	if strings.HasPrefix(side, ancestor) && (ancestor == "" || strings.HasSuffix(ancestor, "\n")) {
		return len(ancestor), side[len(ancestor):], true
	}
	a, b := rawLines(ancestor), rawLines(side)
	gap := 0
	for i := 0; i < len(a) && i < len(b) && a[i] == b[i]; i++ {
		gap += len(a[i])
	}
	end := len(side) - (len(ancestor) - gap)
	if end <= gap || side[end:] != ancestor[gap:] || !strings.HasSuffix(side[gap:end], "\n") || !uniqueAncestorEmbedding(ancestor, side) {
		return 0, "", false
	}
	return gap, side[gap:end], true
}

// The earliest and latest subsequence embeddings must agree at every raw line.
func uniqueAncestorEmbedding(ancestor, side string) bool {
	a, b := rawLines(ancestor), rawLines(side)
	first := make([]int, len(a))
	j := 0
	for i, line := range a {
		for j < len(b) && b[j] != line {
			j++
		}
		if j == len(b) {
			return false
		}
		first[i] = j
		j++
	}
	j = len(b) - 1
	for i := len(a) - 1; i >= 0; i-- {
		for j >= 0 && b[j] != a[i] {
			j--
		}
		if j != first[i] {
			return false
		}
		j--
	}
	return true
}

// Partial shared blocks need a human/model decision. Ignore overlap consisting
// only of whitespace separators. Prefix tables keep the comparison linear.
func overlappingAdditions(a, b string) bool {
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return true
	}
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		x, y := pair[0], pair[1]
		prefix := make([]int, len(y))
		for i, j := 1, 0; i < len(y); i++ {
			for j > 0 && y[i] != y[j] {
				j = prefix[j-1]
			}
			if y[i] == y[j] {
				j++
			}
			prefix[i] = j
		}
		j := 0
		for i := range len(x) {
			for j > 0 && x[i] != y[j] {
				j = prefix[j-1]
			}
			if x[i] == y[j] {
				j++
			}
			// Whole-block containment was handled above.
		}
		for j > 0 {
			if (len(x) == j || x[len(x)-j-1] == '\n') && strings.TrimSpace(y[:j]) != "" {
				return true
			}
			j = prefix[j-1]
		}
	}
	return false
}
