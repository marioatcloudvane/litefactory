package connector

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/cloudvane/litefactory/internal/store"
)

var codeToolNames = []string{"repo_read", "repo_grep", "repo_log", "repo_diff", "standards_read", "standards_role_counts"}

func isCodeTool(name string) bool { return slices.Contains(codeToolNames, name) }

type codeArguments struct {
	TaskRef    string   `json:"task_ref,omitempty"`
	Branch     string   `json:"branch,omitempty"`
	Path       string   `json:"path,omitempty"`
	Pattern    string   `json:"pattern,omitempty"`
	Query      string   `json:"query,omitempty"`
	Mode       string   `json:"mode,omitempty"`
	Commit     string   `json:"commit,omitempty"`
	Role       string   `json:"role,omitempty"`
	Categories []string `json:"categories,omitempty"`
	IgnoreCase bool     `json:"ignore_case,omitempty"`
	StartLine  int      `json:"start_line,omitempty"`
	Lines      int      `json:"lines,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	ViewToken  string   `json:"view_token,omitempty"`
	Cursor     string   `json:"cursor,omitempty"`
}

type codeToken struct {
	Grant     string                    `json:"g"`
	Target    store.ConnectorCodeTarget `json:"t"`
	Pin       CodeEvidencePin           `json:"p"`
	Expires   int64                     `json:"e"`
	Operation string                    `json:"o,omitempty"`
	Arguments codeArguments             `json:"a,omitempty"`
	Offset    int                       `json:"n,omitempty"`
}

func codeProperties(name string) map[string]any {
	p := map[string]any{"task_ref": map[string]any{"type": "string", "minLength": 1, "maxLength": 512}, "view_token": map[string]any{"type": "string", "maxLength": 8192}, "cursor": map[string]any{"type": "string", "maxLength": 16384}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50}}
	if strings.HasPrefix(name, "repo_") {
		p["branch"] = map[string]any{"type": "string", "enum": []string{"task", "base"}}
		p["path"] = map[string]any{"type": "string", "maxLength": 500}
	}
	switch name {
	case "repo_read":
		p["start_line"] = map[string]any{"type": "integer", "minimum": 1}
		p["lines"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 400}
	case "repo_grep":
		p["pattern"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 500}
		p["ignore_case"] = map[string]any{"type": "boolean"}
	case "repo_log":
		p["query"] = map[string]any{"type": "string", "maxLength": 200}
	case "repo_diff":
		p["mode"] = map[string]any{"type": "string", "enum": []string{"base_to_task", "commit"}}
		p["commit"] = map[string]any{"type": "string", "pattern": "^[0-9a-f]{40}([0-9a-f]{24})?$"}
	case "standards_read":
		p["role"] = map[string]any{"type": "string", "maxLength": 200}
		fallthrough
	case "standards_role_counts":
		p["categories"] = map[string]any{"type": "array", "maxItems": 50, "uniqueItems": true, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 200}}
	}
	return p
}
func codeInputSchema(name string) map[string]any {
	required := []string{"task_ref"}
	if name == "repo_grep" {
		required = append(required, "pattern")
	}
	if name == "repo_diff" {
		required = append(required, "mode")
	}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": codeProperties(name), "oneOf": []any{map[string]any{"required": required, "not": map[string]any{"required": []string{"cursor"}}}, map[string]any{"required": []string{"cursor"}, "maxProperties": 1}}}
}
func parseCodeArguments(name string, raw json.RawMessage) (codeArguments, error) {
	var a codeArguments
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return a, errors.New("invalid arguments")
	}
	seen := map[string]bool{}
	props := codeProperties(name)
	for d.More() {
		t, e = d.Token()
		if e != nil {
			return a, e
		}
		k, ok := t.(string)
		if !ok || seen[k] || props[k] == nil {
			return a, errors.New("unknown or duplicate selector")
		}
		seen[k] = true
		var value json.RawMessage
		if e = d.Decode(&value); e != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return a, errors.New("invalid selector")
		}
	}
	if _, e = d.Token(); e != nil {
		return a, e
	}
	if _, e = d.Token(); !errors.Is(e, io.EOF) {
		return a, errors.New("trailing arguments")
	}
	if e = json.Unmarshal(raw, &a); e != nil {
		return a, e
	}
	if a.Cursor != "" {
		if len(seen) != 1 || len(a.Cursor) > 16384 {
			return a, errors.New("continuation conflicts with selectors")
		}
		return a, nil
	}
	if a.TaskRef == "" || len(a.TaskRef) > 512 || seen["cursor"] || len(a.ViewToken) > 8192 || a.Limit < 0 || a.Limit > 50 || seen["limit"] && a.Limit == 0 {
		return a, errors.New("invalid selector")
	}
	if a.Branch == "" {
		a.Branch = "task"
	}
	if a.Branch != "task" && a.Branch != "base" {
		return a, errors.New("invalid branch")
	}
	if len(a.Path) > 500 || len(a.Pattern) > 500 || len(a.Query) > 200 || len(a.Role) > 200 || a.StartLine < 0 || a.StartLine > 100000000 || a.Lines < 0 || a.Lines > 400 || seen["start_line"] && a.StartLine == 0 || seen["lines"] && a.Lines == 0 || len(a.Categories) > 50 {
		return a, errors.New("invalid selector")
	}
	if a.Path != "" {
		if strings.HasPrefix(a.Path, "/") || strings.HasPrefix(a.Path, "-") || strings.HasPrefix(a.Path, ":") || strings.Contains(a.Path, "\\") {
			return a, errors.New("unsafe path")
		}
		for _, p := range strings.Split(a.Path, "/") {
			if p == "" || p == "." || p == ".." {
				return a, errors.New("unsafe path")
			}
		}
	}
	for _, v := range []string{a.TaskRef, a.Path, a.Pattern, a.Query, a.Role} {
		if strings.ContainsFunc(v, unicode.IsControl) {
			return a, errors.New("control character")
		}
	}
	cats := map[string]bool{}
	for _, c := range a.Categories {
		if c == "" || len(c) > 200 || cats[c] || strings.ContainsFunc(c, unicode.IsControl) {
			return a, errors.New("invalid category")
		}
		cats[c] = true
	}
	if name == "repo_grep" && a.Pattern == "" {
		return a, errors.New("pattern required")
	}
	if name == "repo_diff" {
		if a.Mode != "base_to_task" && a.Mode != "commit" {
			return a, errors.New("diff mode required")
		}
		if a.Mode == "commit" && !fullCodeCommit(a.Commit) || a.Mode == "base_to_task" && a.Commit != "" {
			return a, errors.New("invalid diff commit")
		}
	}
	if a.Limit == 0 {
		a.Limit = 50
	}
	return a, nil
}
func fullCodeCommit(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (s *Server) sealCodeToken(c codeToken) (string, error) {
	b, e := json.Marshal(c)
	if e != nil {
		return "", e
	}
	nonce := make([]byte, s.cookie.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(s.cookie.Seal(nonce, nonce, b, []byte("code-evidence-v1"))), nil
}
func (s *Server) openCodeToken(raw string) (codeToken, error) {
	var c codeToken
	b, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil || len(b) < s.cookie.NonceSize() {
		return c, errors.New("invalid evidence token")
	}
	b, e = s.cookie.Open(nil, b[:s.cookie.NonceSize()], b[s.cookie.NonceSize():], []byte("code-evidence-v1"))
	if e != nil || json.Unmarshal(b, &c) != nil || c.Expires <= s.now().Unix() || c.Pin.ObservedAt.IsZero() || c.Expires > c.Pin.ObservedAt.Add(15*time.Minute).Unix() {
		return c, errors.New("expired or invalid evidence token; restart required")
	}
	return c, nil
}
func (s *Server) codeAccess(ctx context.Context, credential, task string) (store.ConnectorGrant, store.ConnectorCodeTarget, error) {
	return s.db.CheckConnectorCodeAccess(ctx, digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, task, s.now())
}
func (s *Server) readCode(ctx context.Context, credential, name string, a codeArguments) (ToolResult, error) {
	var token codeToken
	var e error
	offset := 0
	continuing := a.Cursor != "" || a.ViewToken != ""
	if a.Cursor != "" {
		token, e = s.openCodeToken(a.Cursor)
		if e != nil || token.Operation != name || token.Offset <= 0 {
			return ToolResult{}, errors.New("invalid continuation; restart required")
		}
		a = token.Arguments
		offset = token.Offset
	} else if a.ViewToken != "" {
		token, e = s.openCodeToken(a.ViewToken)
		if e != nil || token.Operation != "" {
			return ToolResult{}, errors.New("invalid view; restart required")
		}
	}
	g, target, e := s.codeAccess(ctx, credential, a.TaskRef)
	if e != nil {
		return ToolResult{}, accessError(e, "code access denied")
	}
	if continuing && (token.Grant != g.ID || token.Target != target) {
		return ToolResult{}, errors.New("evidence binding changed; restart required")
	}
	if !continuing {
		token = codeToken{Grant: g.ID, Target: target, Expires: min(s.now().Add(15*time.Minute).Unix(), g.ExpiresAt.Unix())}
	}
	request := CodeEvidenceRequest{Operation: name, Branch: a.Branch, Path: a.Path, Pattern: a.Pattern, Query: a.Query, Mode: a.Mode, Commit: a.Commit, Role: a.Role, Categories: a.Categories, IgnoreCase: a.IgnoreCase, StartLine: a.StartLine, Lines: a.Lines, Limit: a.Limit, Offset: offset, Pin: token.Pin}
	page, e := s.codeEvidence.Evidence(ctx, target, request)
	if e != nil {
		return ToolResult{}, e
	}
	if page.Pin.ObservedAt.IsZero() || len(page.Items) > 50 || len(page.Items) > a.Limit || continuing && page.Pin != token.Pin || page.HasMore && page.NextOffset <= offset {
		return ToolResult{}, errors.New("invalid evidence page")
	}
	token.Pin = page.Pin
	token.Operation = ""
	token.Offset = 0
	token.Arguments = codeArguments{}
	view, e := s.sealCodeToken(token)
	if e != nil || len(view) > 8192 {
		return ToolResult{}, errors.New("view metadata exceeds bound")
	}
	next := ""
	if page.HasMore {
		token.Operation = name
		token.Offset = page.NextOffset
		a.Cursor = ""
		a.ViewToken = ""
		token.Arguments = a
		next, e = s.sealCodeToken(token)
		if e != nil || len(next) > 16384 {
			return ToolResult{}, errors.New("continuation metadata exceeds bound")
		}
	}
	if page.Items == nil {
		page.Items = []map[string]any{}
	}
	if page.Sources == nil {
		page.Sources = []map[string]any{}
	}
	if page.Gaps == nil {
		page.Gaps = []string{}
	}
	evidence := map[string]any{"version": 1, "task_ref": a.TaskRef, "operation": name, "pin": page.Pin, "items": page.Items, "sources": page.Sources, "gaps": page.Gaps, "has_more": page.HasMore, "next_cursor": next, "view_token": view}
	b, e := json.Marshal(evidence)
	if e != nil || len(b) > 32<<10 {
		return ToolResult{}, errors.New("evidence page exceeds bound")
	}
	if _, current, e := s.codeAccess(ctx, credential, a.TaskRef); e != nil || current != target || s.localControl.check(ctx, s.db, s.cfg.OwnerID) != nil {
		return ToolResult{}, errors.New("code access denied")
	}
	summary := "Locally observed committed evidence is available."
	if strings.HasPrefix(name, "standards_") {
		summary = "Published standards evidence is available from the identified book revision."
	}
	if len(page.Items) == 0 {
		summary = "No readable evidence on this page."
	}
	if len(page.Gaps) > 0 {
		summary += " Coverage has named gaps."
	}
	if page.HasMore {
		summary += " More evidence is available on the next page."
	}
	return DiscussionResult(summary, evidence)
}
func codeFailureResult(summary string) ToolResult {
	if summary == "" {
		summary = "Code and standards evidence is unavailable. Restart the view if its evidence or permission changed."
	}
	r, _ := DiscussionResult(summary, map[string]any{"version": 1, "task_ref": "", "operation": "", "pin": CodeEvidencePin{}, "gaps": []string{"source_or_authorization_unavailable_restart_required"}, "items": []any{}, "sources": []any{}, "has_more": false, "next_cursor": "", "view_token": ""})
	r.IsError = true
	return r
}

func codeToolDescription(name string) string {
	switch name {
	case "repo_read":
		return "Read committed task/base files or directory entries with full local commit and line provenance and stable bounded pages."
	case "repo_grep":
		return "Search committed task/base content with bounded regular-expression matches and explicit search coverage."
	case "repo_log":
		return "Read relevant history reachable from a pinned task/base commit, including full commit and parent identities."
	case "repo_diff":
		return "Read direct base-to-task committed tree changes or one reachable commit against its first parent (empty tree for roots)."
	case "standards_read":
		return "Read verified published standards, optionally filtered by published role and categories, from one immutable book revision."
	case "standards_role_counts":
		return "Compare separately labeled selected-category and whole-book applicability counts for every role from one published book revision."
	default:
		return ""
	}
}
func codeOutputSchema() map[string]any {
	pin := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"observed_at", "redaction_policy"}, "properties": map[string]any{
		"task_commit": map[string]any{"type": "string"}, "base_commit": map[string]any{"type": "string"}, "task_ref": map[string]any{"type": "string"}, "base_ref": map[string]any{"type": "string"}, "book_revision": map[string]any{"type": "integer"}, "book_digest": map[string]any{"type": "string"}, "observed_at": map[string]any{"type": "string"}, "redaction_policy": map[string]any{"type": "string"}}}
	evidence := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"version", "task_ref", "operation", "pin", "items", "sources", "gaps", "has_more", "next_cursor", "view_token"}, "properties": map[string]any{
		"version": map[string]any{"type": "integer", "const": 1}, "task_ref": map[string]any{"type": "string"}, "operation": map[string]any{"type": "string"}, "pin": pin, "items": map[string]any{"type": "array", "maxItems": 50, "items": map[string]any{"type": "object"}}, "sources": map[string]any{"type": "array", "maxItems": 50, "items": map[string]any{"type": "object"}}, "gaps": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "has_more": map[string]any{"type": "boolean"}, "next_cursor": map[string]any{"type": "string", "maxLength": 16384}, "view_token": map[string]any{"type": "string", "maxLength": 8192}}}
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"summary", "evidence"}, "properties": map[string]any{"summary": map[string]any{"type": "string", "maxLength": 1000}, "evidence": evidence}}
}
