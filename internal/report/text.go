package report

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// cutbackWindow is how far back [cutAt] looks for a line break, and failing
// that for any whitespace, when choosing a byte to cut at. 512 bytes of a
// 65536-byte comment is under 1% of the body.
const cutbackWindow = 512

// ansiEscape matches an ANSI CSI sequence — the colour and cursor codes a tool
// writes when it thinks it is talking to a terminal.
//
// They are stripped rather than left alone because the alternative is worse
// than either extreme: sanitize drops the ESC byte as a control character, and
// what survives is the visible junk "[0;31m" scattered through the log tail.
// Removing the whole sequence is the only version that reads.
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

// sanitize makes untrusted text safe to interpolate into a comment body.
//
// Run output and result text come from a model that read third-party issue
// comments, by way of a subprocess whose stderr is whatever its tools printed.
// Three things in there break a comment rather than merely look bad: invalid
// UTF-8 and NUL bytes, which GitHub rejects with a 422 that gives no hint what
// was wrong; and ANSI escapes, which render as noise.
//
// Tabs and newlines survive — the tail of a log is worth nothing without its
// line structure. Carriage returns are dropped so CRLF output does not render
// as double-spaced text.
func sanitize(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ToValidUTF8(s, "�")
	if strings.IndexByte(s, 0x1b) >= 0 {
		s = ansiEscape.ReplaceAllString(s, "")
	}

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r == '\r':
			// Dropped; see above.
		case r < 0x20 || r == 0x7f:
			// Remaining C0 controls and DEL: NUL, BEL, a stray ESC that was
			// not part of a full sequence.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// oneLine renders untrusted text as a single trimmed line, for a place where a
// newline would end the construct it sits in — a bullet, a table cell, a
// <summary>.
func oneLine(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(sanitize(s), "\n", " "))
}

// fenceFor returns a code fence long enough to quote content verbatim.
//
// CommonMark closes a fenced block on the first line holding at least as many
// backticks as the opening fence, so quoting output that itself contains ```
// with a three-backtick fence lets that output break out of the block and into
// the comment as markup. Counting the longest run and going one better is the
// spec's own answer to this.
func fenceFor(content string) string {
	longest, run := 0, 0
	for i := range len(content) {
		if content[i] != '`' {
			run = 0
			continue
		}
		run++
		if run > longest {
			longest = run
		}
	}
	return strings.Repeat("`", max(3, longest+1))
}

// lastLines returns the last n lines of s, with trailing blank lines removed.
func lastLines(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	if s == "" || n <= 0 {
		return ""
	}
	end := len(s)
	for range n {
		nl := strings.LastIndexByte(s[:end], '\n')
		if nl < 0 {
			return s
		}
		end = nl
	}
	return s[end+1:]
}

// countLines returns the number of lines in s. Empty text is zero lines.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// cutAt returns an index at or below max to cut s at. The caller guarantees
// max < len(s).
//
// "Safe" means three things, in the order they matter.
//
// It never splits a UTF-8 rune, because an invalid byte at the end of a
// truncated body is a 422.
//
// It never splits a whitespace-delimited token, and that one is load-bearing.
// This package cuts *before* redaction runs, so whatever survives a cut is what
// the redaction pass then has to recognise. Half of "AKIAIOSFODNN7EXAMPLE"
// matches no pattern and equals no known literal, so it would sail through the
// pass and the residual check into a public comment — a fragment of a live
// credential leaked by the very step meant to make the body safe. Cutting back
// to the preceding whitespace means the pass only ever sees whole tokens. This
// mirrors the same rule inside internal/redact, deliberately: both cuts happen
// before the same pass and both have to hold.
//
// And it prefers a line break, so the text ends on a whole line where it can.
func cutAt(s string, max int) int {
	if max >= len(s) {
		return len(s)
	}
	i := max
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	if nl := strings.LastIndexByte(s[:i], '\n'); nl >= 0 && i-nl <= cutbackWindow {
		return nl
	}
	for j := i - 1; j >= 0 && i-j <= cutbackWindow; j-- {
		switch s[j] {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			return j
		}
	}
	return i
}

// truncateEnd cuts s from the end so that s plus notice fits in max bytes,
// reporting whether it cut anything.
//
// This is the fallback shape, used where there is no middle worth keeping: the
// run's final result text, whose beginning is the part that says what happened.
func truncateEnd(s, notice string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	room := max - len(notice)
	if room <= 0 {
		return "", true
	}
	return s[:cutAt(s, room)] + notice, true
}

// elideMiddle cuts s down to at most max bytes by removing whole lines from the
// middle and putting notice in the gap, reporting whether it cut anything.
//
// The middle is what goes because the two ends are what carry meaning: the
// first lines of the kept tail show what the run was doing, and the last lines
// show how it stopped. Cutting only from the end would throw away the failure
// itself.
//
// notice is a function rather than a string because its own length depends on
// the numbers it reports, and those numbers depend on where the cut lands.
// Reserving the length of the notice for the largest values it could hold —
// every line, every byte — breaks the circularity from above, and the real
// notice is never longer than what was reserved.
func elideMiddle(s string, max int, notice func(lines, bytes int) string) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	reserved := len(notice(countLines(s), len(s)))
	room := max - reserved
	if room <= 0 {
		return "", true
	}

	head, tail := room/2, room-room/2

	// Whole lines from the front, up to head bytes.
	frontEnd := 0
	for {
		nl := strings.IndexByte(s[frontEnd:], '\n')
		if nl < 0 || frontEnd+nl+1 > head {
			break
		}
		frontEnd += nl + 1
	}

	// Whole lines from the back, up to tail bytes.
	backStart := len(s)
	for backStart > 0 {
		nl := strings.LastIndexByte(s[:backStart-1], '\n')
		if nl < 0 || len(s)-(nl+1) > tail {
			break
		}
		backStart = nl + 1
	}
	if backStart < frontEnd {
		// Cannot happen for room < len(s), which the early return guarantees.
		// Preferring the tail keeps the most recent output if it ever does.
		frontEnd = 0
	}

	removed := s[frontEnd:backStart]
	if removed == "" {
		// A single line longer than the whole budget: there is no line
		// boundary to elide at, so fall back to cutting the end off.
		return truncateEnd(s, "\n"+notice(0, len(s)-room), max)
	}

	removedLines := strings.Count(removed, "\n")
	if !strings.HasSuffix(removed, "\n") {
		removedLines++
	}
	return s[:frontEnd] + notice(removedLines, len(removed)) + s[backStart:], true
}

// formatDuration renders a wall-clock duration at a precision a human reads
// without counting digits: milliseconds under a second, tenths under a minute,
// whole seconds above that.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return d.Round(time.Millisecond).String()
	case d < time.Minute:
		return d.Round(100 * time.Millisecond).String()
	default:
		return d.Round(time.Second).String()
	}
}
