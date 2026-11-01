package linearmd

import (
	"regexp"
	"strings"
	"testing"
)

// adversarial is the shared input matrix: ordinary punctuation, pipes, links,
// emails, Markdown, HTML, mentions and multiline block syntax.
var adversarial = []string{
	"MAR-60-001: Show what each uploaded invoice became",
	"Fix story-001: done.",
	"being built now.",
	`a | b || c\|d`,
	"https://evil.example/x",
	"www.evil.example",
	"evil.example",
	"a@b.co",
	"mailto:a@b.co",
	"<https://x.y>",
	"[click](https://evil.example)",
	"![i](https://evil.example/i.png)",
	"**b** _i_ ~~s~~ `c`",
	"<b>x</b> <!-- lf:implement-handoff -->",
	"@user #12",
	"see https://evil.example|x and `a|b` ``` `` `x`",
	"one\n# h\n> q\n- a\n+ b\n* c\n1. d\n2) e\n```\n~~~\n---\n***\n===\n    code\n| a | b |\n|---|---|",
	"  \t ",
	"",
}

// scan reads inert Markdown: it resolves backslash escapes, reads code
// spans, and reports anything that is still active outside a code span.
type scan struct {
	plain    string   // literal text a reader sees
	outside  string   // text outside code spans, escapes resolved
	active   []string // unescaped Markdown punctuation outside code spans
	unclosed bool
}

func scanInline(s string) scan {
	var out scan
	var plain, outside strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && strings.IndexByte("\\`*_[]<>~|#+-=.)!", s[i+1]) >= 0:
			plain.WriteByte(s[i+1])
			outside.WriteByte(s[i+1])
			i += 2
		case c == '`':
			n := 0
			for i+n < len(s) && s[i+n] == '`' {
				n++
			}
			fence := strings.Repeat("`", n)
			end := -1
			for j := i + n; j < len(s); {
				k := strings.Index(s[j:], fence)
				if k < 0 {
					break
				}
				k += j
				if (k+n == len(s) || s[k+n] != '`') && (k == 0 || s[k-1] != '`') {
					end = k
					break
				}
				j = k + 1
			}
			if end < 0 {
				out.unclosed = true
				out.active = append(out.active, "`")
				i += n
				continue
			}
			content := s[i+n : end]
			if len(content) >= 2 && content[0] == ' ' && content[len(content)-1] == ' ' && strings.Trim(content, " ") != "" {
				content = content[1 : len(content)-1]
			}
			plain.WriteString(content)
			outside.WriteString(" ")
			i = end + n
		default:
			if strings.IndexByte("[]*_~<|", c) >= 0 {
				out.active = append(out.active, string(c))
			}
			plain.WriteByte(c)
			outside.WriteByte(c)
			i++
		}
	}
	out.plain, out.outside = plain.String(), outside.String()
	return out
}

var linkShape = regexp.MustCompile(`(?i)://|^www\.|^mailto:|@[^ ]*\.|[a-z0-9-]+\.[a-z]{2,}`)

func linkTokens(outside string) []string {
	var found []string
	for _, token := range strings.Fields(outside) {
		if linkShape.MatchString(token) {
			found = append(found, token)
		}
	}
	return found
}

func normalised(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func TestInlineTextIsInertAndLossless(t *testing.T) {
	for _, in := range adversarial {
		got := InlineText(in)
		if strings.Contains(got, "&#") || strings.ContainsAny(got, "\n\r") {
			t.Errorf("InlineText(%q) = %q: entity or line break", in, got)
		}
		s := scanInline(got)
		if len(s.active) > 0 {
			t.Errorf("InlineText(%q) = %q leaves active Markdown %q", in, got, s.active)
		}
		if links := linkTokens(s.outside); len(links) > 0 {
			t.Errorf("InlineText(%q) = %q leaves link-like tokens %q outside code spans", in, got, links)
		}
		if s.plain != normalised(in) {
			t.Errorf("InlineText(%q) reads %q, want %q", in, s.plain, normalised(in))
		}
		if strings.Join(InlineUnits(in), "") != got {
			t.Errorf("InlineUnits(%q) differs from InlineText", in)
		}
	}
}

func TestInlineTextKeepsOrdinaryPunctuation(t *testing.T) {
	for in, want := range map[string]string{
		"MAR-60-001: Show what each uploaded invoice became": "MAR-60-001: Show what each uploaded invoice became",
		"being built now.":     "being built now.",
		"Fix story-001: done.": "Fix story-001: done.",
		"a|b":                  `a\|b`,
		"evil.example":         "`evil.example`",
		"x`y.com":              "``x`y.com``",
		"`a.com":               "`` `a.com ``",
	} {
		if got := InlineText(in); got != want {
			t.Errorf("InlineText(%q) = %q, want %q", in, got, want)
		}
	}
}

// blockStart matches a line that could open a block. A leading code span is
// safe: its closing fence sits on the same line, and a backtick fence's info
// string cannot contain backticks, so it never opens a fenced code block.
var blockStart = regexp.MustCompile("^(#|>|-|\\+|=|\\*|_|~|\\||\\d+[.)]| {4}|\t)")

func TestBlockTextFormsNoBlocks(t *testing.T) {
	for _, in := range adversarial {
		got := BlockText(in)
		if strings.Contains(got, "&#") || strings.Contains(got, "<br>") {
			t.Errorf("BlockText(%q) = %q writes entities or <br>", in, got)
		}
		var lines []string
		for _, raw := range strings.Split(in, "\n") {
			if line := normalised(raw); line != "" {
				lines = append(lines, line)
			}
		}
		paragraphs := strings.Split(got, "\n\n")
		if got == "" {
			paragraphs = nil
		}
		if len(paragraphs) != len(lines) {
			t.Fatalf("BlockText(%q) has %d paragraphs, want %d: %q", in, len(paragraphs), len(lines), got)
		}
		for i, p := range paragraphs {
			if strings.Contains(p, "\n") || blockStart.MatchString(p) {
				t.Errorf("BlockText(%q) paragraph %q can start a block", in, p)
			}
			s := scanInline(p)
			if len(s.active) > 0 || len(linkTokens(s.outside)) > 0 {
				t.Errorf("BlockText(%q) paragraph %q is not inert: %q", in, p, s.active)
			}
			if s.plain != lines[i] {
				t.Errorf("BlockText(%q) paragraph reads %q, want %q", in, s.plain, lines[i])
			}
		}
	}
}

func TestInlineUnitsAreWholeEscapes(t *testing.T) {
	for _, in := range adversarial {
		for _, unit := range InlineUnits(in) {
			switch {
			case unit == "":
				t.Errorf("InlineUnits(%q) has an empty unit", in)
			case strings.HasPrefix(unit, "`"):
				if s := scanInline(unit); s.unclosed || s.outside != " " {
					t.Errorf("InlineUnits(%q) code span unit %q is not whole", in, unit)
				}
			case strings.HasPrefix(unit, `\`) && len(unit) != 2:
				t.Errorf("InlineUnits(%q) escape unit %q is not one escaped byte", in, unit)
			}
		}
	}
}
