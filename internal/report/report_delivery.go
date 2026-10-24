package report

import (
	"context"
	"fmt"
	"html"
	"path"
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/redact"
)

// RenderReportDelivery applies the configured outbound hygiene to an already
// rendered report and its durable marker. An empty marker restores retained
// content; callers require byte equality before dispatching a saved intent.
// Paths on the factory host are replaced before redaction, so no published
// comment exposes the host's file layout. The replacement is stable: a
// rendered body renders to the same bytes again.
func (r *Reporter) RenderReportDelivery(ctx context.Context, body, marker string) (redact.Body, error) {
	body = StripHostPaths(body, r.hostRoots)
	if marker != "" {
		body += "\n\n" + marker
	}
	value, err := r.redactor.ApplyReviewDisplay(ctx, body, r.limit)
	if err != nil {
		return value, err
	}
	if value.Truncated() || (marker != "" && !strings.HasSuffix(value.String(), marker)) {
		return value, fmt.Errorf("report delivery exceeds the complete-content limit")
	}
	return value, nil
}

// hostDirectoryNames mark a factory-owned directory wherever it sits: an
// absolute path through one of them is a factory-host path.
var hostDirectoryNames = []string{"/.litefactory/", "/.lf-worktrees/"}

// hostLink is a markdown link whose target is an absolute path.
var hostLink = regexp.MustCompile(`\[([^\]\n]*)\]\((/[^)\s]*)\)`)

// hostToken is an absolute path that starts a word: at the start of the text or
// after whitespace or an opening delimiter.
var hostToken = regexp.MustCompile("(?:^|[\\s(\\[<\"'`=:])(/[^\\s()\\[\\]<>\"'`]+)")

func cleanHostRoots(roots []string) []string {
	var out []string
	for _, root := range roots {
		root = path.Clean(strings.TrimSpace(root))
		if !strings.HasPrefix(root, "/") || root == "/" {
			continue // never treat the filesystem root as a host root
		}
		out = append(out, root)
	}
	return out
}

func isHostPath(p string, roots []string) bool {
	if strings.HasPrefix(p, "//") {
		return false // a scheme-relative URL, not a path
	}
	for _, root := range roots {
		if p == root || strings.HasPrefix(p, root+"/") {
			return true
		}
	}
	for _, name := range hostDirectoryNames {
		if strings.Contains(p+"/", name) {
			return true
		}
	}
	return false
}

// hostPathTail keeps the last two names of a host path, which is enough for
// the owner to recognise the file or directory without the host layout.
func hostPathTail(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return "…/" + strings.Join(parts, "/")
}

// StripHostPaths replaces absolute paths on the factory host in published text.
// A markdown link to a host path keeps only its text, because the target cannot
// resolve on the forge; a bare host path keeps only its last two names.
func StripHostPaths(body string, roots []string) string {
	// Review source is escaped literal text. Strip private paths after decoding
	// that layer too, before a quoted path can hide from the ordinary matcher.
	var out strings.Builder
	for len(body) > 0 {
		start := strings.Index(body, "<pre>")
		code := strings.Index(body, "<code>")
		tag := "pre"
		if start < 0 || code >= 0 && code < start {
			start = code
			tag = "code"
		}
		if start < 0 {
			out.WriteString(stripHostPathsRaw(body, roots))
			break
		}
		opening, closing := "<"+tag+">", "</"+tag+">"
		end := strings.Index(body[start+len(opening):], closing)
		if end < 0 {
			out.WriteString(stripHostPathsRaw(body, roots))
			break
		}
		out.WriteString(stripHostPathsRaw(body[:start+len(opening)], roots))
		inner := body[start+len(opening) : start+len(opening)+end]
		decoded := html.UnescapeString(inner)
		cleaned := stripHostPathsRaw(decoded, roots)
		if cleaned == decoded {
			out.WriteString(inner)
		} else {
			out.WriteString(html.EscapeString(cleaned))
		}
		out.WriteString(closing)
		body = body[start+len(opening)+end+len(closing):]
	}
	return out.String()
}

func stripHostPathsRaw(body string, roots []string) string {
	body = hostLink.ReplaceAllStringFunc(body, func(link string) string {
		m := hostLink.FindStringSubmatch(link)
		if !isHostPath(m[2], roots) {
			return link
		}
		return m[1]
	})
	matches := hostToken.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return body
	}
	var out strings.Builder
	last := 0
	for _, m := range matches {
		start, end := m[2], m[3]
		token := body[start:end]
		trimmed := strings.TrimRight(token, ".,;:!?")
		if !isHostPath(trimmed, roots) {
			continue
		}
		out.WriteString(body[last:start])
		out.WriteString(hostPathTail(trimmed))
		last = start + len(trimmed)
	}
	out.WriteString(body[last:])
	return out.String()
}
