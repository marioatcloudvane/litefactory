// Package codeevidence projects committed task/base code and published rules.
package codeevidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/projectread"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// Provider has only immutable book reads, committed Git reads and sanitization.
type Provider struct {
	roots    []string
	books    standards.PublishedStore
	git      *git.Binary
	redactor *redact.Redactor
	now      func() time.Time
}

// New composes the local projection; neither question handling nor paging runs agents.
func New(db *store.DB, binary *git.Binary, r *redact.Redactor, now func() time.Time, hostRoots ...string) *Provider {
	if now == nil {
		now = time.Now
	}
	return &Provider{roots: append([]string{"/Users", "/home", "/private", "/var", "/tmp"}, hostRoots...), books: db, git: binary, redactor: r, now: now}
}

// Evidence acquires complete bounded sources before disclosure. Tokens and live
// grant checks belong to the connector, never to a project invocation cache.
func (p *Provider) Evidence(ctx context.Context, t store.ConnectorCodeTarget, r connector.CodeEvidenceRequest) (connector.CodeEvidencePage, error) {
	local := *p
	local.roots = append(slices.Clone(p.roots), t.ClonePath)
	p = &local
	out := connector.CodeEvidencePage{Pin: r.Pin, Items: []map[string]any{}, Sources: []map[string]any{}, Gaps: []string{}}
	if p.redactor == nil || p.redactor.PolicyID() == "" || r.Offset < 0 || r.Limit < 1 || r.Limit > 50 {
		return out, errors.New("invalid evidence request")
	}
	safeTask, e := p.cleanSource(ctx, t.TaskRef, 0)
	if e != nil || safeTask != t.TaskRef {
		return out, errors.New("sensitive task identity withheld")
	}
	if out.Pin.ObservedAt.IsZero() {
		out.Pin.ObservedAt = p.now().UTC()
		out.Pin.Policy = p.policyID()
		// Observe all three independent versions once, even if one is unavailable.
		clone, e := p.git.Open(ctx, t.ClonePath)
		if e == nil {
			out.Pin.TaskRef = "refs/heads/" + t.TaskBranch
			out.Pin.BaseRef = "refs/remotes/origin/" + t.BaseBranch
			out.Pin.TaskCommit, _ = git.ResolvePinnedRef(ctx, clone, out.Pin.TaskRef)
			out.Pin.BaseCommit, _ = git.ResolvePinnedRef(ctx, clone, out.Pin.BaseRef)
			if out.Pin.BaseCommit == "" {
				out.Pin.BaseRef = "refs/heads/" + t.BaseBranch
				out.Pin.BaseCommit, _ = git.ResolvePinnedRef(ctx, clone, out.Pin.BaseRef)
			}
		}
		if book, e := standards.ReadBook(ctx, p.books, 0); e == nil {
			out.Pin.BookRevision = book.RevisionID
			out.Pin.BookDigest = book.Digest
		}
	}
	for _, ref := range []string{out.Pin.TaskRef, out.Pin.BaseRef, out.Pin.TaskCommit, out.Pin.BaseCommit, out.Pin.BookDigest, out.Pin.Policy} {
		safe, e := p.cleanSource(ctx, ref, 0)
		if e != nil || safe != ref {
			return out, errors.New("sensitive branch identity withheld")
		}
	}
	if out.Pin.Policy != p.policyID() {
		return out, errors.New("redaction policy changed; restart required")
	}
	var items []map[string]any
	if strings.HasPrefix(r.Operation, "standards_") {
		items, e = p.book(ctx, r, &out)
	} else {
		items, e = p.repository(ctx, t, r, &out)
	}
	if e != nil {
		return out, e
	}
	// Sanitize complete units, including every structured string, before paging.
	safeItems := []map[string]any{}
	for _, item := range items {
		safe, e := p.cleanMap(ctx, item)
		if e != nil {
			return out, e
		}
		safeItems = append(safeItems, safe)
	}
	for i, source := range out.Sources {
		out.Sources[i], e = p.cleanMap(ctx, source)
		if e != nil {
			return out, e
		}
	}
	if r.Offset > len(safeItems) {
		return out, errors.New("invalid evidence position")
	}
	total := 0
	for i := r.Offset; i < len(safeItems) && len(out.Items) < r.Limit; i++ {
		item := safeItems[i]
		b, e := json.Marshal(item)
		if e != nil {
			return out, e
		}
		if len(b) > 16<<10 {
			return out, errors.New("evidence unit too large")
		}
		if total+len(b) > 20<<10 && len(out.Items) > 0 {
			break
		}
		out.Items = append(out.Items, item)
		total += len(b)
	}
	out.NextOffset = r.Offset + len(out.Items)
	out.HasMore = out.NextOffset < len(safeItems)
	if out.HasMore && out.NextOffset <= r.Offset {
		return out, errors.New("evidence cannot advance")
	}
	if out.Pin.Policy != p.policyID() || ctx.Err() != nil {
		return out, errors.New("evidence changed or cancelled")
	}
	return out, nil
}
func (p *Provider) book(ctx context.Context, r connector.CodeEvidenceRequest, out *connector.CodeEvidencePage) ([]map[string]any, error) {
	if out.Pin.BookRevision == 0 {
		out.Gaps = append(out.Gaps, "published_book_unavailable_at_observation")
		return nil, nil
	}
	book, e := standards.ReadBook(ctx, p.books, out.Pin.BookRevision)
	if e != nil || book.Digest != out.Pin.BookDigest {
		return nil, errors.New("published book unavailable or changed; restart required")
	}
	out.Sources = append(out.Sources, map[string]any{"kind": "published_standards", "book_revision": book.RevisionID, "book_digest": book.Digest, "observed_at": out.Pin.ObservedAt, "basis": "current published rules at observation; not proof of rules used by earlier runs"})
	if r.Operation == "standards_role_counts" {
		counts, e := book.RoleCounts(r.Categories)
		if e != nil {
			return nil, e
		}
		return []map[string]any{{"selected_category_ids": counts.SelectedCategoryIDs, "selected_categories": counts.SelectedCategories, "whole_book": counts.WholeBook, "count_basis": "one published standard per applicable role; role counts are not additive unique-standard counts"}}, nil
	}
	projection, e := book.Project(r.Role, r.Categories)
	if e != nil {
		return nil, e
	}
	items := []map[string]any{}
	sourceBytes := 0
	for _, standard := range projection.Standards {
		text := standards.Render(projection.Categories, []standards.Standard{standard})
		sourceBytes += len(text)
		if sourceBytes > 1<<20 {
			return nil, errors.New("rendered published book exceeds source bound")
		}
		safe, e := p.cleanSource(ctx, text, 0)
		if e != nil {
			return nil, e
		}
		for _, part := range textParts(safe, 4096) {
			items = append(items, map[string]any{"standard_id": standard.ID, "category_id": standard.CategoryID, "applies_to": standard.AppliesTo, "content": part.Text, "offset": part.Start, "end": part.End, "complete": part.End == len(safe)})
		}
	}
	if len(items) == 0 {
		out.Gaps = append(out.Gaps, "no_published_standards_match_selected_filters")
	}
	return items, nil
}
func (p *Provider) repository(ctx context.Context, t store.ConnectorCodeTarget, r connector.CodeEvidenceRequest, out *connector.CodeEvidencePage) ([]map[string]any, error) {
	commit, ref := out.Pin.TaskCommit, out.Pin.TaskRef
	if r.Branch == "base" {
		commit, ref = out.Pin.BaseCommit, out.Pin.BaseRef
	}
	if commit == "" {
		out.Gaps = append(out.Gaps, "selected_branch_unavailable_at_observation; no branch substitution")
		return nil, nil
	}
	clone, e := p.git.Open(ctx, t.ClonePath)
	if e != nil {
		out.Gaps = append(out.Gaps, "repository_clone_unavailable")
		return nil, nil //nolint:nilerr // a missing source is an explicit evidence gap
	}
	if t.RemoteURL != "" {
		origin, e := clone.RemoteURL(ctx, "origin")
		if e != nil || origin != t.RemoteURL {
			out.Gaps = append(out.Gaps, "retained_repository_origin_mismatch")
			return nil, nil //nolint:nilerr // source identity failure is a named gap
		}
	}
	request := projectread.CommittedRequest{Operation: r.Operation, Commit: commit, Path: r.Path, Pattern: r.Pattern, Query: r.Query, IgnoreCase: r.IgnoreCase, Sanitize: func(ctx context.Context, text string) (string, error) { return p.cleanSource(ctx, text, 0) }}
	if r.Operation == "repo_diff" {
		if r.Mode == "base_to_task" {
			if out.Pin.BaseCommit == "" || out.Pin.TaskCommit == "" {
				out.Gaps = append(out.Gaps, "comparison_endpoint_unavailable")
				return nil, nil
			}
			request.From = out.Pin.BaseCommit
			request.To = out.Pin.TaskCommit
		} else {
			request.To = r.Commit
		}
	}
	source, e := projectread.ReadCommitted(ctx, clone, p.redactor, request)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		gap := "committed_source_unavailable; missing or pruned objects require restart"
		switch {
		case errors.Is(e, git.ErrPinnedReadTooLarge):
			gap = "source_exceeds_acquisition_bound; narrow selectors"
		case errors.Is(e, git.ErrPinnedSourceUnavailable):
			gap = "partial_or_promisor_clone_unavailable; no fetching permitted"
		case errors.Is(e, projectread.ErrCommittedUnsupported):
			gap = "binary_or_unsupported_text_source_withheld"
		case errors.Is(e, git.ErrPinnedUnsupported):
			gap = "committed_submodule_entry_unsupported; no traversal"
		}
		out.Gaps = append(out.Gaps, gap)
		return nil, nil
	}
	out.Gaps = append(out.Gaps, source.Gaps...)
	sourceRef := map[string]any{"kind": "git", "repository": t.RepositoryRef, "branch_ref": ref, "commit": commit, "path": r.Path, "observed_at": out.Pin.ObservedAt, "url": repositoryURL(t, commit, r.Path), "local_only": true, "entry_kind": source.EntryKind, "entry_mode": source.EntryMode, "entry_object": source.EntryObject}
	if r.Operation == "repo_diff" {
		sourceRef["from"] = source.From
		sourceRef["to"] = source.To
		sourceRef["parents"] = source.Parents
		sourceRef["semantics"] = source.Semantics
	}
	if source.Directory {
		sourceRef["url"] = strings.Replace(strings.Replace(repositoryURL(t, commit, r.Path), "/blob/", "/tree/", 1), "/commit/", "/tree/", 1)
	}
	if sourceRef["url"] == "" {
		out.Gaps = append(out.Gaps, "provider_permalink_unavailable; use repository/commit/path identity")
	}
	out.Sources = append(out.Sources, sourceRef)
	if r.Operation == "repo_log" {
		items := []map[string]any{}
		for _, entry := range source.History {
			subject, e := p.cleanSource(ctx, entry.Subject, 0)
			if e != nil {
				return nil, e
			}
			entry.Subject = subject
			items = append(items, map[string]any{"commit": entry.ID, "parents": entry.Parents, "date": entry.Date, "subject": entry.Subject, "url": repositoryURL(t, entry.ID, "")})
		}
		return items, nil
	}
	if r.Operation == "repo_grep" {
		items := []map[string]any{}
		for _, match := range source.Matches {
			for _, part := range textParts(match.Text, 4096) {
				item := map[string]any{"path": match.Path, "line": match.Line, "content": part.Text, "offset": part.Start, "end": part.End, "partial_line": part.Start > 0}
				if link := repositoryURL(t, commit, match.Path); link != "" {
					item["url"] = link + fmt.Sprintf("#L%d", match.Line)
				}
				items = append(items, item)
			}
		}
		if len(items) == 0 && !source.CoverageLimited {
			out.Gaps = append(out.Gaps, "no_matches_in_bounded_search")
		}
		return items, nil
	}

	text, e := p.cleanSource(ctx, source.Text, 0)
	if e != nil {
		return nil, e
	}
	exactLines := source.LinePositions && strings.Count(source.Text, "\n") == strings.Count(text, "\n") && !source.Directory && source.EntryMode != "160000"
	if r.Operation == "repo_read" && !exactLines && !source.Directory {
		out.Gaps = append(out.Gaps, "exact_line_provenance_unavailable_after_sanitization_or_unsupported_entry")
		if r.StartLine > 0 || r.Lines > 0 {
			return nil, nil
		}
	}
	start, end := 0, len(text)
	if r.Operation == "repo_read" && !source.Directory {
		start, end = selectedLines(text, r.StartLine, r.Lines)
	}
	items := []map[string]any{}
	for _, part := range textParts(text[start:end], 4096) {
		line := 1 + strings.Count(text[:start+part.Start], "\n")
		item := map[string]any{"content": part.Text, "path": r.Path, "offset": start + part.Start, "end": start + part.End, "line": line, "partial_line": start+part.Start > 0 && text[start+part.Start-1] != '\n', "directory": source.Directory, "selection_complete": start+part.End == end, "source_complete": start+part.End == len(text)}
		if !exactLines {
			delete(item, "line")
			item["line_basis"] = "sanitized text; exact committed line positions unavailable"
		}
		if r.Operation == "repo_read" && exactLines {
			if link := repositoryURL(t, commit, r.Path); link != "" {
				item["url"] = link + fmt.Sprintf("#L%d", line)
			}
		}
		items = append(items, item)
	}
	return items, nil
}

type textPart struct {
	Text       string
	Start, End int
}

func textParts(text string, size int) []textPart {
	out := []textPart{}
	for start := 0; start < len(text); {
		end := min(len(text), start+size)
		for end > start && !utf8.ValidString(text[start:end]) {
			end--
		}
		out = append(out, textPart{text[start:end], start, end})
		start = end
	}
	return out
}
func selectedLines(text string, start, count int) (int, int) {
	if start < 1 {
		start = 1
	}
	offset := 0
	for line := 1; line < start && offset < len(text); line++ {
		next := strings.IndexByte(text[offset:], '\n')
		if next < 0 {
			return len(text), len(text)
		}
		offset += next + 1
	}
	end := offset
	if count == 0 {
		return offset, len(text)
	}
	for n := 0; n < count && end < len(text); n++ {
		next := strings.IndexByte(text[end:], '\n')
		if next < 0 {
			end = len(text)
			break
		}
		end += next + 1
	}
	return offset, end
}
func codeURL(repository, commit, path string) string {
	repo, ok := strings.CutPrefix(repository, "github:")
	if !ok {
		return ""
	}
	base := "https://github.com/" + repo
	if path == "" {
		return base + "/commit/" + commit
	}
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return base + "/blob/" + commit + "/" + strings.Join(parts, "/")
}

var sourceURL = regexp.MustCompile("(?i)(?:https?://|file://)[^\\s<>()\"`]+")

func (p *Provider) cleanText(ctx context.Context, text string) (string, error) {
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return "", errors.New("unsupported source text")
	}
	var linkErr error
	text = sourceURL.ReplaceAllStringFunc(text, func(raw string) string {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" {
			return "[link unavailable]"
		}
		safe, e := p.redactor.Prepare(ctx, u.Path)
		if e != nil {
			linkErr = e
			return "[link unavailable]"
		}
		if safe.String() != u.Path {
			return "[link unavailable]"
		}
		u.User = nil
		u.RawQuery = ""
		if !lineFragment.MatchString(u.Fragment) {
			u.Fragment = ""
		}
		return u.String()
	})
	if linkErr != nil {
		return "", linkErr
	}
	text = report.StripHostPaths(text, p.roots)
	safe, e := p.redactor.ApplyReviewJSONDisplay(ctx, text, 2<<20)
	if e != nil || !safe.Valid() || safe.Truncated() {
		return "", errors.New("evidence redaction failed")
	}
	return safe.String(), nil
}
func (p *Provider) cleanMap(ctx context.Context, item map[string]any) (map[string]any, error) {
	b, e := json.Marshal(item)
	if e != nil {
		return nil, e
	}
	var value any
	if e = json.Unmarshal(b, &value); e != nil {
		return nil, e
	}
	clean, e := p.cleanValue(ctx, value)
	if e != nil {
		return nil, e
	}
	result, ok := clean.(map[string]any)
	if !ok {
		return nil, errors.New("invalid sanitized evidence map")
	}
	return result, nil
}
func (p *Provider) cleanValue(ctx context.Context, v any) (any, error) {
	switch x := v.(type) {
	case string:
		return p.cleanSource(ctx, x, 0)
	case map[string]any:
		out := map[string]any{}
		for k, v := range x {
			key, e := p.cleanSource(ctx, k, 0)
			if e != nil {
				return nil, e
			}
			if _, exists := out[key]; exists {
				return nil, errors.New("redacted key collision")
			}
			if k == "content" {
				if text, ok := v.(string); ok {
					out[key], e = p.cleanText(ctx, text)
				} else {
					out[key], e = p.cleanValue(ctx, v)
				}
			} else {
				out[key], e = p.cleanValue(ctx, v)
			}
			if e != nil {
				return nil, e
			}
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i := range x {
			v, e := p.cleanValue(ctx, x[i])
			if e != nil {
				return nil, e
			}
			out[i] = v
		}
		return out, nil
	default:
		return v, nil
	}
}

var lineFragment = regexp.MustCompile(`^L[0-9]+(?:-L[0-9]+)?$`)
var sourceJSONString = regexp.MustCompile(`"(?:[^"\\\x00-\x1f]|\\(?:["\\/bfnrt]|u[0-9a-fA-F]{4}))*"`)

// cleanSource decodes complete quoted source units before splitting. Nested
// serialized JSON is bounded independently; fragments are never parsed again.
func (p *Provider) cleanSource(ctx context.Context, text string, depth int) (string, error) {
	if depth > 8 {
		return "", errors.New("nested source exceeds sanitization bound")
	}
	var failure error
	text, e := p.cleanText(ctx, text)
	if e != nil {
		return "", e
	}
	indices := sourceJSONString.FindAllStringIndex(text, -1)
	position := 0
	for _, span := range indices {
		if encodedSourceEscape.MatchString(text[position:span[0]]) {
			return "", errors.New("incomplete encoded source unit withheld")
		}
		position = span[1]
	}
	if encodedSourceEscape.MatchString(text[position:]) {
		return "", errors.New("incomplete encoded source unit withheld")
	}
	text = sourceJSONString.ReplaceAllStringFunc(text, func(token string) string {
		if failure != nil {
			return ""
		}
		var decoded string
		if e := json.Unmarshal([]byte(token), &decoded); e != nil {
			failure = e
			return ""
		}
		safe, e := p.cleanText(ctx, decoded)
		if e != nil {
			failure = e
			return ""
		}
		if json.Valid([]byte(safe)) {
			safe, e = p.cleanSource(ctx, safe, depth+1)
			if e != nil {
				failure = e
				return ""
			}
		}
		if safe == decoded {
			return token
		}
		b, e := json.Marshal(safe)
		if e != nil {
			failure = e
			return ""
		}
		return string(b)
	})
	if failure != nil {
		return "", failure
	}
	return p.cleanText(ctx, text)
}

var encodedSourceEscape = regexp.MustCompile(`\\u[0-9a-fA-F]{4}`)

// repositoryURL derives a web host only from a recognized retained Git remote.
// A local mirror or unsupported transport has identity evidence without a guessed link.
func repositoryURL(target store.ConnectorCodeTarget, commit, path string) string {
	repository, ok := strings.CutPrefix(target.RepositoryRef, "github:")
	if !ok {
		return ""
	}
	remote := target.RemoteURL
	host, remotePath := "", ""
	if strings.Contains(remote, "://") {
		u, e := url.Parse(remote)
		if e != nil {
			return ""
		}
		if u.Scheme != "https" && u.Scheme != "ssh" {
			return ""
		}
		if u.Scheme == "ssh" && u.Port() != "" && u.Port() != "22" {
			return ""
		}
		host = u.Host
		if u.Scheme == "ssh" {
			host = u.Hostname()
		}
		remotePath = u.Path
	} else if _, address, ok := strings.Cut(remote, "@"); ok {
		host, remotePath, ok = strings.Cut(address, ":")
		if !ok {
			return ""
		}
	}
	if host == "" || strings.ContainsAny(host, "/?#@\\ \r\n") || strings.TrimSuffix(strings.Trim(remotePath, "/"), ".git") != repository {
		return ""
	}
	return strings.Replace(codeURL(target.RepositoryRef, commit, path), "https://github.com", "https://"+host, 1)
}

// policyID binds source decoding, link/path protection and registered credentials
// to the view. A host-root or sanitizer policy change requires a fresh view.
func (p *Provider) policyID() string {
	digest := sha256.Sum256([]byte("committed-and-published-evidence-v1\x00" + p.redactor.PolicyID() + "\x00" + strings.Join(p.roots, "\x00")))
	return hex.EncodeToString(digest[:])
}
