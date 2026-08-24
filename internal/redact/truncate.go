package redact

import (
	"strings"
	"unicode/utf8"
)

// Margin is the headroom [Redactor.Apply] reserves below the caller's limit
// before it redacts.
//
// Redaction usually shrinks a body — an AKIA key is 20 bytes and becomes 10 —
// but it can grow one. "[redacted]" is ten bytes, so every match shorter than
// that adds length, and a body carrying a hundred eight-character environment
// values grows by two hundred bytes. Margin is the slack that absorbs the
// normal case so the hard-truncate at the end is a genuine last resort rather
// than the usual path.
//
// 2 KiB against GitHub's 65536-byte comment limit is roughly 3%.
const Margin = 2048

// truncationNotice is appended whenever text is removed. It is deliberately
// plain: internal/report writes the human-facing truncation notice with the
// local log path (AC-4.8), and this one only fires when Apply had to cut
// further than report expected.
const truncationNotice = "\n\n_[litefactory: body truncated to fit the comment size limit]_"

// cutbackWindow is how far back [safeCut] will look for a line break, and
// failing that for any whitespace, when choosing a cut point.
//
// The line break is for readability: the tail of a log reads better ending on a
// whole line. The whitespace fallback is not cosmetic — see safeCut. Scanning
// further back than this would throw away more content than either is worth;
// 512 bytes of a 65536-byte comment is under 1%.
const cutbackWindow = 512

// Budget returns the size internal/report should aim its own truncation at:
// the caller's limit less [Margin].
//
// It is exported so the two truncation steps agree. report does the smart cut —
// from the middle of the log tail, never the summary — and it needs to know
// which number to cut to. Without this, report cuts to the limit, redaction
// grows the body past it, and Apply's hard-truncate takes the bottom off the
// carefully preserved summary.
//
// For a small limit the margin shrinks with it, so Budget never returns a
// negative or absurd number.
func Budget(limit int) int {
	if limit <= 0 {
		return 0
	}
	margin := Margin
	if margin > limit/2 {
		margin = limit / 2
	}
	return limit - margin
}

// truncate cuts s to at most max bytes, appending [truncationNotice] when it
// removes anything. It reports whether it cut.
//
// The cut is at a safe boundary in two senses. It never splits a UTF-8 rune —
// GitHub rejects a body that is not valid UTF-8, and an invalid byte at the end
// of a truncated log tail is a 422 that looks like a mystery. And it prefers
// the last line break within [newlineWindow], so the body ends on a whole line.
func truncate(s string, max int) (string, bool) {
	if max <= 0 {
		return "", s != ""
	}
	if len(s) <= max {
		return s, false
	}

	// Reserve room for the notice. If the limit is too small to hold even
	// that, drop the notice rather than overshoot: honouring the limit is the
	// requirement, explaining it is the courtesy.
	room := max - len(truncationNotice)
	if room <= 0 {
		return s[:safeCut(s, max)], true
	}
	return s[:safeCut(s, room)] + truncationNotice, true
}

// safeCut returns an index at or below max to cut s at. The caller guarantees
// max < len(s).
//
// "Safe" means three things, in order of how much they matter.
//
// It never splits a UTF-8 rune. GitHub rejects a body that is not valid UTF-8,
// and an invalid byte at the end of a truncated log tail is a 422 that looks
// like a mystery.
//
// It never splits a whitespace-delimited token, and this one is load-bearing
// rather than tidy. [Redactor.Apply] truncates *before* it redacts, so anything
// this function leaves behind is what the redaction pass then has to recognise.
// Cut "AKIAIOSFODNN7EXAMPLE" in half and the surviving "AKIAIOSFODN" matches no
// pattern, is not equal to any known literal, and sails through the pass and
// the residual check into a public comment — a fragment of a live credential,
// leaked by the very step that was supposed to make the body safe to post. So
// when the cut would land inside a token, it moves back to the whitespace
// before it, and the pass sees whole tokens only. If no whitespace is within
// [cutbackWindow] the fragment is part of an unbroken run of 512+ characters,
// where cutting back would discard a large chunk of a legitimate log line to
// protect a fragment nobody could reassemble; the rune boundary wins there.
//
// And it prefers a line break, so the body ends on a whole line where it can.
func safeCut(s string, max int) int {
	i := max
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	if nl := strings.LastIndexByte(s[:i], '\n'); nl >= 0 && i-nl <= cutbackWindow {
		return nl
	}
	// No line break close enough. Fall back to any whitespace, so the cut does
	// not sever a token. ASCII whitespace bytes never occur inside a multi-byte
	// rune, so this index is still a rune boundary.
	for j := i - 1; j >= 0 && i-j <= cutbackWindow; j-- {
		switch s[j] {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			return j
		}
	}
	return i
}
