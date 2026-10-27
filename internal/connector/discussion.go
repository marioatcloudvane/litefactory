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
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/store"
)

// DiscussionMaxResponseBytes includes the complete UTF-8 JSON-RPC envelope.
const DiscussionMaxResponseBytes = 128 << 10

// EvidencePageBytes caps one complete task evidence page before its two output forms.
const EvidencePageBytes = 64 << 10

var errEvidenceChanged = errors.New("evidence changed; restart without cursor")

var evidenceSections = []string{"status", "results", "questions", "research", "plan", "approvals"}

type evidenceArguments struct {
	TaskRef string `json:"task_ref"`
	Section string `json:"section"`
	Cursor  string `json:"cursor,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

type evidenceCursor struct {
	Grant    string `json:"grant"`
	Task     string `json:"task"`
	Session  int64  `json:"session"`
	Section  string `json:"section"`
	Revision string `json:"revision"`
	Offset   int    `json:"offset"`
	Expires  int64  `json:"expires"`
}

func parseEvidenceArguments(raw json.RawMessage) (evidenceArguments, error) {
	var a evidenceArguments
	// Decoder.DisallowUnknownFields does not reject duplicate keys.
	d := json.NewDecoder(bytes.NewReader(raw))
	t, err := d.Token()
	if err != nil || t != json.Delim('{') {
		return a, errors.New("invalid arguments")
	}
	seen := map[string]bool{}
	for d.More() {
		t, err = d.Token()
		if err != nil {
			return a, errors.New("invalid arguments")
		}
		key, ok := t.(string)
		if !ok || seen[key] {
			return a, errors.New("duplicate argument")
		}
		if !slices.Contains([]string{"task_ref", "section", "cursor", "limit"}, key) {
			return a, errors.New("unknown argument")
		}
		seen[key] = true
		var discard json.RawMessage
		if err = d.Decode(&discard); err != nil || bytes.Equal(bytes.TrimSpace(discard), []byte("null")) {
			return a, errors.New("invalid arguments")
		}
	}
	if _, err = d.Token(); err != nil {
		return a, errors.New("invalid arguments")
	}
	if _, err = d.Token(); err != io.EOF {
		return a, errors.New("trailing arguments")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&a); err != nil || a.TaskRef == "" || len(a.TaskRef) > 512 || !slices.Contains(evidenceSections, a.Section) || a.Limit < 0 || a.Limit > 50 || len(a.Cursor) > 4096 {
		return a, errors.New("invalid arguments")
	}
	if seen["limit"] && a.Limit == 0 {
		return a, errors.New("invalid limit")
	}
	if a.Limit == 0 {
		a.Limit = 50
	}
	return a, nil
}

func (s *Server) sealCursor(c evidenceCursor) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, s.cookie.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(s.cookie.Seal(nonce, nonce, b, []byte("task-evidence-cursor-v1"))), nil
}
func (s *Server) openCursor(raw string) (evidenceCursor, error) {
	var c evidenceCursor
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(b) < s.cookie.NonceSize() {
		return c, errors.New("invalid cursor")
	}
	b, err = s.cookie.Open(nil, b[:s.cookie.NonceSize()], b[s.cookie.NonceSize():], []byte("task-evidence-cursor-v1"))
	if err != nil || json.Unmarshal(b, &c) != nil || c.Offset <= 0 || c.Revision == "" || c.Expires <= s.now().Unix() {
		return c, errors.New("invalid cursor")
	}
	return c, nil
}

// accessRefusal carries the plain reason a per-call authorization check
// refused, so the tool result can name it.
type accessRefusal struct{ reason string }

func (e accessRefusal) Error() string { return e.reason }

// accessError keeps a store refusal's plain reason and hides anything else
// behind the fallback.
func accessError(err error, fallback string) error {
	if reason := store.ConnectorRefusalReason(err); reason != "" {
		return accessRefusal{reason: reason}
	}
	return errors.New(fallback)
}

// refusalSummary is the tool-result summary for a refused call, or "" when
// err carries no plain reason.
func refusalSummary(err error) string {
	var refusal accessRefusal
	if errors.As(err, &refusal) {
		return "Access refused: " + refusal.reason + "."
	}
	return ""
}

func (s *Server) readEvidence(ctx context.Context, credential string, a evidenceArguments) (ToolResult, error) {
	grant, target, err := s.db.CheckConnectorDiscussionAccess(ctx, digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, a.TaskRef, s.now())
	if err != nil {
		return ToolResult{}, accessError(err, "discussion access denied")
	}
	position := 0
	var cursor evidenceCursor
	if a.Cursor != "" {
		cursor, err = s.openCursor(a.Cursor)
		if err != nil || cursor.Grant != grant.ID || cursor.Task != target.TaskRef || cursor.Session != target.SessionID || cursor.Section != a.Section {
			return ToolResult{}, errors.New("invalid continuation")
		}
		position = cursor.Offset
	}
	page, err := s.taskEvidence.Evidence(ctx, target, EvidenceRequest{Section: a.Section, Offset: position, Limit: a.Limit})
	if err != nil {
		return ToolResult{}, err
	}
	if page.Revision == "" || a.Cursor != "" && cursor.Revision != page.Revision {
		return ToolResult{}, errEvidenceChanged
	}
	if len(page.Items) > a.Limit || len(page.Items) > 50 {
		return ToolResult{}, errors.New("invalid provider page")
	}
	next := ""
	if page.HasMore {
		if page.NextOffset <= position {
			return ToolResult{}, errors.New("nonadvancing evidence page")
		}
		next, err = s.sealCursor(evidenceCursor{Grant: grant.ID, Task: target.TaskRef, Session: target.SessionID, Section: a.Section, Revision: page.Revision, Offset: page.NextOffset, Expires: s.now().Add(15 * time.Minute).Unix()})
		if err != nil {
			return ToolResult{}, err
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
	evidence := map[string]any{"version": 1, "task_ref": a.TaskRef, "section": a.Section, "generated_at": s.now().UTC(), "revision": page.Revision, "items": page.Items, "sources": page.Sources, "gaps": page.Gaps, "has_more": page.HasMore, "next_cursor": next}
	raw, err := json.Marshal(evidence)
	if err != nil || len(raw) > EvidencePageBytes {
		return ToolResult{}, errors.New("evidence page unavailable")
	}
	summary := evidenceSummary(a.Section, page)
	return DiscussionResult(summary, evidence)
}

// evidenceSummary selects only already sanitized facts in stable page order.
func evidenceSummary(section string, page EvidencePage) string {
	details := []string{}
	for _, item := range page.Items {
		if len(details) == 3 {
			break
		}
		if unavailable, _ := item["unavailable"].(bool); unavailable {
			details = append(details, "One recorded item is unavailable")
			continue
		}
		detail := ""
		switch section {
		case "status":
			detail = stringFact(item, "state")
			if run, ok := item["latest_run"].(map[string]any); ok {
				detail += "; latest run: " + stringFact(run, "mode") + " " + stringFact(run, "status")
			}
		case "approvals":
			detail = stringFact(item, "story_key") + " " + stringFact(item, "stage") + ": " + stringFact(item, "applicability")
		case "plan":
			if draft, ok := item["draft"].(map[string]any); ok {
				detail = stringFact(draft, "name")
			}
		case "questions":
			if questions, ok := item["questions"].([]any); ok && len(questions) > 0 {
				if q, ok := questions[0].(map[string]any); ok {
					detail = stringFact(q, "text")
				}
			}
			if detail != "" {
				detail += " (" + stringFact(item, "applicability") + ")"
			}
		case "results", "research":
			if recorded, ok := item["recorded"].(map[string]any); ok {
				if result, ok := recorded["result"].(map[string]any); ok {
					recorded = result
				}
				detail = stringFact(recorded, "comment")
				if detail == "" {
					detail = stringFact(recorded, "summary")
				}
				if detail == "" {
					if research, ok := recorded["research"].(map[string]any); ok {
						if findings, ok := research["findings"].([]any); ok && len(findings) > 0 {
							if finding, ok := findings[0].(map[string]any); ok {
								detail = stringFact(finding, "text")
							}
						}
					}
				}
			}
		}
		if detail != "" {
			details = append(details, detail)
		}
	}
	summary := "Recorded " + section + " evidence is available."
	if len(details) > 0 {
		summary = "Recorded " + section + ": " + strings.Join(details, "; ") + "."
	}
	if len(page.Items) == 0 {
		summary = "No readable " + section + " evidence on this page."
	}
	if len(page.Gaps) > 0 {
		summary += " Coverage has recorded gaps."
	}
	if page.HasMore {
		summary += " More evidence is available on the next page."
	}
	return summary
}
func stringFact(item map[string]any, key string) string { value, _ := item[key].(string); return value }

// DiscussionResult serializes already sanitized facts for common discussion
// providers. It counts Unicode code points including its truncation marker.
// The transport checks the final envelope separately before releasing bytes.
func DiscussionResult(summary string, evidence any) (ToolResult, error) {
	if !utf8.ValidString(summary) {
		return ToolResult{}, errors.New("invalid summary")
	}
	runes := []rune(summary)
	if len(runes) > 1000 {
		summary = string(runes[:999]) + "…"
	}
	value := map[string]any{"summary": summary, "evidence": evidence}
	raw, err := json.Marshal(value)
	if err != nil {
		return ToolResult{}, err
	}
	result := ToolResult{Content: []TextContent{{Type: "text", Text: string(raw)}}, StructuredContent: raw}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > DiscussionMaxResponseBytes-1024 {
		return ToolResult{}, errors.New("discussion response unavailable")
	}
	return result, nil
}

func (s *Server) availableTools(grant store.ConnectorGrant) []string {
	names := []string{}
	if s.overview != nil && store.ConnectorScopeHas(grant.Scope, Scope) {
		names = append(names, "factory_overview")
	}
	if s.taskEvidence != nil && store.ConnectorScopeHas(grant.Scope, DiscussionScope) {
		names = append(names, "task_evidence_read")
	}
	if s.usage != nil && (store.ConnectorScopeHas(grant.Scope, FactoryUsageScope) || store.ConnectorScopeHas(grant.Scope, RepositoryUsageScope) || store.ConnectorScopeHas(grant.Scope, TaskUsageScope)) {
		names = append(names, "usage_read")
	}
	if s.codeEvidence != nil && store.ConnectorScopeHas(grant.Scope, DiscussionScope) && store.ConnectorScopeHas(grant.Scope, CodeReviewScope) {
		names = append(names, codeToolNames...)
	}
	return names
}

func evidenceInputSchema() map[string]any {
	return map[string]any{"type": "object", "required": []string{"task_ref", "section"}, "additionalProperties": false, "properties": map[string]any{"task_ref": map[string]any{"type": "string", "maxLength": 512}, "section": map[string]any{"type": "string", "enum": evidenceSections}, "cursor": map[string]any{"type": "string", "maxLength": 4096}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50}}}
}
func evidenceOutputSchema() map[string]any {
	return map[string]any{"type": "object", "required": []string{"summary", "evidence"}, "additionalProperties": false, "properties": map[string]any{"summary": map[string]any{"type": "string", "maxLength": 1000}, "evidence": map[string]any{"type": "object", "required": []string{"version", "task_ref", "section", "generated_at", "revision", "items", "sources", "gaps", "has_more", "next_cursor"}, "additionalProperties": false, "properties": map[string]any{"version": map[string]any{"type": "integer", "const": 1}, "task_ref": map[string]any{"type": "string"}, "section": map[string]any{"type": "string", "enum": evidenceSections}, "generated_at": map[string]any{"type": "string"}, "revision": map[string]any{"type": "string"}, "items": map[string]any{"type": "array", "maxItems": 50, "items": map[string]any{"type": "object"}}, "sources": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, "gaps": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "has_more": map[string]any{"type": "boolean"}, "next_cursor": map[string]any{"type": "string"}}}}}
}
