// Package linearmd writes untrusted text as inert Linear comment Markdown. It is
// a leaf so planbrief, report and turn can use it; internal/linear's own tests
// import planbrief and report, so the helper cannot live in that package.
package linearmd

import (
	"strings"
	"unicode"
)

// Linear comment Markdown does not decode numeric HTML entities, so untrusted
// text bound for a Linear comment is neutralised with CommonMark backslash
// escapes and inline code spans instead. Two contracts exist: InlineText for
// table cells and single-line fields, BlockText for multiline text.

// inlineEscaped are the characters InlineText writes with a backslash.
const inlineEscaped = "\\`*_[]<>~|"

// InlineText renders untrusted text as one line of inert Markdown: whitespace
// collapses to single spaces, link-like tokens become code spans, and Markdown
// punctuation is backslash-escaped. A '|' never ends a table cell.
func InlineText(text string) string {
	return strings.Join(InlineUnits(text), "")
}

// InlineUnits returns InlineText's output as indivisible units (one escaped
// character, one space or one whole code span), so a budget can cut safely.
func InlineUnits(text string) []string {
	text = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, text)), " ")
	var units []string
	for i, token := range strings.Split(text, " ") {
		if token == "" {
			continue
		}
		if i > 0 {
			units = append(units, " ")
		}
		for j, piece := range strings.Split(token, "|") {
			if j > 0 {
				units = append(units, `\|`)
			}
			if piece == "" {
				continue
			}
			if linkLike(piece) {
				units = append(units, codeSpan(piece))
				continue
			}
			for _, r := range piece {
				if strings.ContainsRune(inlineEscaped, r) {
					units = append(units, `\`+string(r))
				} else {
					units = append(units, string(r))
				}
			}
		}
	}
	return units
}

// BlockText renders untrusted multiline text as inert paragraphs, one per
// non-empty line, so no heading, quote, list, fence, rule, setext underline,
// indented code or table can form.
func BlockText(text string) string {
	var paragraphs []string
	for _, line := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' }) {
		line = InlineText(line)
		if line == "" {
			continue
		}
		paragraphs = append(paragraphs, LineStart(line))
	}
	return strings.Join(paragraphs, "\n\n")
}

// LineStart backslash-escapes a leading block marker of already inert text:
// '#', '>', '-', '+' or '=', or the '.' or ')' after leading digits.
func LineStart(line string) string {
	if line == "" {
		return line
	}
	if strings.ContainsRune("#>-+=", rune(line[0])) {
		return `\` + line
	}
	digits := 0
	for digits < len(line) && line[digits] >= '0' && line[digits] <= '9' {
		digits++
	}
	if digits > 0 && digits < len(line) && (line[digits] == '.' || line[digits] == ')') {
		return line[:digits] + `\` + line[digits:]
	}
	return line
}

// linkLike reports whether a renderer or Linear's autolinker could turn the
// token into a link: a scheme, www. or mailto: prefix, an email shape, or a
// dotted name ending in a label of two or more letters.
func linkLike(token string) bool {
	lower := strings.ToLower(token)
	if strings.Contains(lower, "://") || strings.HasPrefix(lower, "www.") || strings.HasPrefix(lower, "mailto:") {
		return true
	}
	if at := strings.IndexByte(lower, '@'); at >= 0 && strings.Contains(lower[at+1:], ".") {
		return true
	}
	trimmed := strings.TrimRightFunc(lower, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	dot := strings.LastIndexByte(trimmed, '.')
	if dot <= 0 {
		return false
	}
	label := trimmed[dot+1:]
	letters := 0
	for _, r := range label {
		if !unicode.IsLetter(r) {
			return false
		}
		letters++
	}
	return letters >= 2
}

// codeSpan wraps a token in a backtick fence longer than any backtick run it
// contains; a leading or trailing backtick gets padding so the fence holds.
func codeSpan(token string) string {
	longest, run := 0, 0
	for _, r := range token {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(token, "`") || strings.HasSuffix(token, "`") {
		return fence + " " + token + " " + fence
	}
	return fence + token + fence
}
