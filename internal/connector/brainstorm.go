package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/brainstorm"
	"github.com/cloudvane/litefactory/internal/connectorstate"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// Brainstorms exposes one owner's repository brainstorms, their research scans
// and issue search. Scan execution stays with the factory's durable jobs.
type Brainstorms interface {
	Open(context.Context, owneraction.Principal, string) (brainstorm.View, error)
	Get(context.Context, owneraction.Principal, string) (brainstorm.View, error)
	Entries(context.Context, owneraction.Principal, string, string, string, int) (brainstorm.EntryPage, error)
	Scans(context.Context, owneraction.Principal, string, string, int) (brainstorm.ScanPage, error)
	List(context.Context, owneraction.Principal, string, string, int) (brainstorm.ListPage, error)
	Add(context.Context, owneraction.Principal, brainstorm.AddRequest) (brainstorm.View, error)
	Close(context.Context, owneraction.Principal, string, string, int64) (brainstorm.View, error)
	Ask(context.Context, owneraction.Principal, brainstorm.AskRequest) (brainstorm.ScanView, error)
	ScanGet(context.Context, owneraction.Principal, string, string, int) (brainstorm.ScanView, error)
	ScanCancel(context.Context, owneraction.Principal, string) (brainstorm.ScanView, error)
	SearchIssues(context.Context, owneraction.Principal, string, string, int) (brainstorm.IssueSearchView, error)
	Draft(context.Context, owneraction.Principal, brainstorm.DraftRequest) (brainstorm.ScanView, error)
	DraftsGet(context.Context, owneraction.Principal, string) (brainstorm.DraftSetView, error)
	TaskBatchRepository(context.Context, owneraction.Principal, string) (string, error)
	TasksGet(context.Context, owneraction.Principal, string) (brainstorm.TasksView, error)
}

var brainstormToolNames = []string{"brainstorm_open", "brainstorm_get", "brainstorm_entries", "brainstorm_scans", "brainstorm_list", "brainstorm_add", "brainstorm_close", "brainstorm_ask", "brainstorm_scan_get", "brainstorm_scan_cancel", "issue_search", "brainstorm_draft", "brainstorm_drafts_get", "brainstorm_tasks_get"}

func isBrainstormTool(name string) bool { return slices.Contains(brainstormToolNames, name) }

// brainstormOwnerKinds are the entry kinds an owner may add; exchanges are
// written only by finished scans.
var brainstormOwnerKinds = []string{brainstorm.KindIdea, brainstorm.KindDecision, brainstorm.KindOpenQuestion, brainstorm.KindRelatedIssue, brainstorm.KindDocumentNote}

var brainstormEntryKinds = append(slices.Clone(brainstormOwnerKinds), brainstorm.KindExchange)

type brainstormArguments struct {
	Repository       string `json:"repository,omitempty"`
	Brainstorm       string `json:"brainstorm,omitempty"`
	Scan             string `json:"scan,omitempty"`
	Kind             string `json:"kind,omitempty"`
	Text             string `json:"text,omitempty"`
	DocumentName     string `json:"document_name,omitempty"`
	IssueReference   string `json:"issue_reference,omitempty"`
	Question         string `json:"question,omitempty"`
	Note             string `json:"note,omitempty"`
	Batch            string `json:"batch,omitempty"`
	RequestKey       string `json:"request_key,omitempty"`
	ExpectedRevision int64  `json:"expected_revision,omitempty"`
	Cursor           string `json:"cursor,omitempty"`
	Limit            int    `json:"limit,omitempty"`
}

func brainstormFields(name string) (allowed, required []string) {
	switch name {
	case "brainstorm_open":
		return []string{"repository"}, []string{"repository"}
	case "brainstorm_get":
		return []string{"brainstorm"}, []string{"brainstorm"}
	case "brainstorm_entries":
		return []string{"brainstorm", "kind", "cursor", "limit"}, []string{"brainstorm"}
	case "brainstorm_scans":
		return []string{"brainstorm", "cursor", "limit"}, []string{"brainstorm"}
	case "brainstorm_list":
		return []string{"repository", "cursor", "limit"}, []string{"repository"}
	case "brainstorm_add":
		return []string{"brainstorm", "kind", "text", "document_name", "issue_reference", "request_key", "expected_revision"}, []string{"brainstorm", "kind", "text", "request_key", "expected_revision"}
	case "brainstorm_close":
		return []string{"brainstorm", "request_key", "expected_revision"}, []string{"brainstorm", "request_key", "expected_revision"}
	case "brainstorm_ask":
		return []string{"brainstorm", "question", "request_key"}, []string{"brainstorm", "question", "request_key"}
	case "brainstorm_scan_get":
		return []string{"scan", "cursor", "limit"}, []string{"scan"}
	case "brainstorm_scan_cancel":
		return []string{"scan"}, []string{"scan"}
	case "issue_search":
		return []string{"repository", "text", "limit"}, []string{"repository", "text"}
	case "brainstorm_draft":
		return []string{"brainstorm", "request_key", "note"}, []string{"brainstorm", "request_key"}
	case "brainstorm_drafts_get":
		return []string{"brainstorm"}, []string{"brainstorm"}
	case "brainstorm_tasks_get":
		return []string{"batch"}, []string{"batch"}
	}
	return nil, nil
}

// brainstormStringMax bounds each string argument in bytes; issue search text
// is bounded in code points separately.
func brainstormStringMax(field string) int {
	switch field {
	case "repository":
		return 1024
	case "text":
		return brainstorm.MaxDocumentBytes
	case "document_name", "issue_reference":
		return 512
	case "question", "note":
		return brainstorm.MaxQuestionBytes
	case "batch":
		return 256
	case "cursor":
		return brainstorm.MaxCursorBytes
	default:
		return 128
	}
}

func parseBrainstormArguments(name string, raw json.RawMessage) (brainstormArguments, error) {
	var a brainstormArguments
	if !isBrainstormTool(name) || len(raw) == 0 || len(raw) > 65536 || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return a, argumentsShapeError()
	}
	allowed, required := brainstormFields(name)
	raw = withoutUnsupplied(raw, allowed, required)
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueJSONObject(d); err != nil {
		return a, uniqueArgumentsError(err)
	}
	if _, err := d.Token(); err != io.EOF {
		return a, argumentsShapeError()
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&a); err != nil {
		return a, decodeArgumentError(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return a, argumentsShapeError()
	}
	if err := checkArgumentFields(fields, allowed, required); err != nil {
		return a, err
	}
	for _, f := range []struct{ name, value string }{
		{"repository", a.Repository}, {"brainstorm", a.Brainstorm}, {"scan", a.Scan}, {"kind", a.Kind}, {"text", a.Text}, {"document_name", a.DocumentName}, {"issue_reference", a.IssueReference}, {"question", a.Question}, {"request_key", a.RequestKey}, {"cursor", a.Cursor}, {"note", a.Note}, {"batch", a.Batch},
	} {
		if _, present := fields[f.name]; present && strings.TrimSpace(f.value) == "" {
			return a, invalidArgument(f.name, "must not be empty")
		}
		if _, present := fields[f.name]; present && len(f.value) > brainstormStringMax(f.name) {
			return a, invalidArgument(f.name, fmt.Sprintf("is longer than %d bytes", brainstormStringMax(f.name)))
		}
	}
	if _, present := fields["limit"]; present {
		maximum := brainstorm.MaxPageLimit
		if name == "issue_search" {
			maximum = brainstorm.MaxIssueSearchLimit
		}
		if a.Limit < 1 || a.Limit > maximum {
			return a, invalidArgument("limit", fmt.Sprintf("must be between 1 and %d", maximum))
		}
	}
	if _, present := fields["expected_revision"]; present && a.ExpectedRevision < 0 {
		return a, invalidArgument("expected_revision", "must not be negative")
	}
	switch name {
	case "brainstorm_entries":
		if _, present := fields["kind"]; present && !slices.Contains(brainstormEntryKinds, a.Kind) {
			return a, invalidArgument("kind", "must be one of "+strings.Join(brainstormEntryKinds, ", "))
		}
	case "brainstorm_add":
		if !slices.Contains(brainstormOwnerKinds, a.Kind) {
			return a, invalidArgument("kind", "must be one of "+strings.Join(brainstormOwnerKinds, ", "))
		}
		// A document note names its document; no other kind carries one.
		if a.Kind == brainstorm.KindDocumentNote && a.DocumentName == "" {
			return a, missingArgument("document_name")
		}
		if a.Kind != brainstorm.KindDocumentNote && a.DocumentName != "" {
			return a, invalidArgument("document_name", "is allowed only with kind "+brainstorm.KindDocumentNote)
		}
		if a.Kind != brainstorm.KindDocumentNote && len(a.Text) > brainstorm.MaxEntryBytes {
			return a, invalidArgument("text", fmt.Sprintf("is longer than %d bytes", brainstorm.MaxEntryBytes))
		}
	case "issue_search":
		if utf8.RuneCountInString(a.Text) > brainstorm.MaxIssueSearchText {
			return a, invalidArgument("text", fmt.Sprintf("is longer than %d characters", brainstorm.MaxIssueSearchText))
		}
	}
	return a, nil
}

func brainstormInputSchema(name string) map[string]any {
	allowed, required := brainstormFields(name)
	props := map[string]any{}
	for _, key := range allowed {
		switch key {
		case "expected_revision":
			props[key] = map[string]any{"type": "integer", "minimum": 0}
		case "limit":
			maximum := brainstorm.MaxPageLimit
			if name == "issue_search" {
				maximum = brainstorm.MaxIssueSearchLimit
			}
			props[key] = map[string]any{"type": "integer", "minimum": 1, "maximum": maximum}
		case "kind":
			kinds := brainstormEntryKinds
			if name == "brainstorm_add" {
				kinds = brainstormOwnerKinds
			}
			props[key] = map[string]any{"type": "string", "enum": kinds}
		case "text":
			maximum := brainstorm.MaxDocumentBytes
			if name == "issue_search" {
				maximum = brainstorm.MaxIssueSearchText
			}
			props[key] = map[string]any{"type": "string", "minLength": 1, "maxLength": maximum}
		default:
			props[key] = map[string]any{"type": "string", "minLength": 1, "maxLength": brainstormStringMax(key)}
		}
	}
	return map[string]any{"type": "object", "required": required, "additionalProperties": false, "properties": props}
}

func brainstormDescription(name string) string {
	switch name {
	case "brainstorm_open":
		return "Open this owner's brainstorm for a repository, or resume the open one. Returns its revision, entry and question counts and the newest entries and questions. Requires repository:brainstorm consent for the repository."
	case "brainstorm_get":
		return "Read one brainstorm's header, entry counts per kind, question counts per state and the newest entries and questions. Requires repository:brainstorm consent."
	case "brainstorm_entries":
		return "Page every kept entry of one brainstorm oldest first, optionally of one kind. Follow next_cursor until it is absent. Requires repository:brainstorm consent."
	case "brainstorm_scans":
		return "Page every question ever asked in one brainstorm oldest first with its current state. Follow next_cursor until it is absent. Requires repository:brainstorm consent."
	case "brainstorm_list":
		return "List this owner's brainstorms for a repository, newest first. Requires repository:brainstorm consent for the repository."
	case "brainstorm_add":
		return "Keep one idea, decision, open question, related issue or document note in an open brainstorm. A document note names its document. Needs the current revision and a request key; a retry with the same key is replayed. Requires repository:brainstorm consent."
	case "brainstorm_close":
		return "Close an open brainstorm; it stays readable. Needs the current revision and a request key. Requires repository:brainstorm consent."
	case "brainstorm_ask":
		return "Ask one factual question about the repository. Starts one paid read-only research agent run without another confirmation and returns the scan and its state. A retry with the same request key returns the same scan. Requires repository:brainstorm consent."
	case "brainstorm_scan_get":
		return "Read one question's state, spoken answer, cost and paged evidence. Reading never starts another run. Requires repository:brainstorm consent."
	case "brainstorm_scan_cancel":
		return "Cancel a waiting question or ask a running one to stop. A finished answer may still win. Requires repository:brainstorm consent."
	case "brainstorm_draft":
		return "Ask the Shaping agent to turn this brainstorm into one to five issue drafts. Starts one paid read-only agent run and returns the scan and its state; read the drafts with brainstorm_drafts_get when it is answered. A retry with the same request key returns the same scan. Requires repository:brainstorm consent."
	case "brainstorm_drafts_get":
		return "Read the latest draft set of a brainstorm: its spoken summary and each draft's title, size, impact, related issues, possible duplicates and ledger. Requires repository:brainstorm consent."
	case "brainstorm_tasks_get":
		return "Read one batch of issues created from brainstorm drafts: per draft its state, issue number and plain next step. Requires repository:actions and repository:brainstorm consent for the repository."
	default:
		return "Search open and closed issues of the repository's project through its GitHub or Linear ticket connection. A match is a warning to check, never proof of a duplicate. Requires repository:brainstorm consent for the repository."
	}
}

func brainstormReadOnly(name string) bool {
	switch name {
	case "brainstorm_get", "brainstorm_entries", "brainstorm_scans", "brainstorm_list", "brainstorm_scan_get", "issue_search", "brainstorm_drafts_get", "brainstorm_tasks_get":
		return true
	}
	return false
}

func brainstormGrant(g connectorstate.ConnectorGrant) bool {
	return connectorstate.ConnectorScopeHas(g.Scope, BrainstormScope)
}

// brainstormToolAllowed also requires repository:actions for brainstorm_tasks_get.
func brainstormToolAllowed(g connectorstate.ConnectorGrant, name string) bool {
	if !brainstormGrant(g) {
		return false
	}
	return name != "brainstorm_tasks_get" || connectorstate.ConnectorScopeHas(g.Scope, RepositoryActionsScope)
}

func (s *Server) brainstormRepositoryCovered(ctx context.Context, grant connectorstate.ConnectorGrant, ref string) error {
	if grant.Coverage == nil || grant.Coverage.All {
		return connectorstate.ErrConnectorScope
	}
	repositories, err := s.access.ListConnectorRepositories(ctx)
	if err != nil {
		return err
	}
	for _, r := range repositories {
		if r.RepositoryRef == ref && grant.Coverage.Covers(r.RepoID, r.RepositoryRef) {
			return nil
		}
	}
	return connectorstate.ErrConnectorScope
}

// brainstormRepository names the repository a call reaches: the named one, or
// the repository of the named brainstorm or scan, read without side effects
// so coverage is checked before anything changes.
func brainstormRepository(ctx context.Context, service Brainstorms, p owneraction.Principal, a brainstormArguments) (string, error) {
	if a.Repository != "" {
		return a.Repository, nil
	}
	if a.Batch != "" {
		return service.TaskBatchRepository(ctx, p, a.Batch)
	}
	id := a.Brainstorm
	if id == "" {
		scan, err := service.ScanGet(ctx, p, a.Scan, "", 1)
		if err != nil {
			return "", err
		}
		id = scan.BrainstormID
	}
	v, err := service.Get(ctx, p, id)
	if err != nil {
		return "", err
	}
	return v.Brainstorm.RepositoryRef, nil
}

func (s *Server) callBrainstorm(ctx context.Context, credential, name string, a brainstormArguments) (ToolResult, error) {
	grant, err := s.storage.CheckConnectorAccess(ctx, digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, "", s.now())
	if err != nil || s.localControl.check(ctx, s.storage, s.cfg.OwnerID) != nil {
		return ToolResult{}, connectorstate.ErrConnectorInvalid
	}
	service := s.brainstorms.Load()
	if service == nil || !brainstormToolAllowed(grant, name) {
		return ToolResult{}, connectorstate.ErrConnectorScope
	}
	p := owneraction.Principal{OwnerID: strconv.FormatInt(grant.OwnerID, 10), OwnerGeneration: grant.OwnerGeneration, Transport: "mcp", ClientID: grant.ClientID, GrantID: grant.ID}
	repository, err := brainstormRepository(ctx, service, p, a)
	if err != nil {
		return ToolResult{}, err
	}
	if err = s.brainstormRepositoryCovered(ctx, grant, repository); err != nil {
		return ToolResult{}, err
	}
	var value any
	var summary string
	switch name {
	case "brainstorm_open", "brainstorm_get", "brainstorm_add", "brainstorm_close":
		var v brainstorm.View
		switch name {
		case "brainstorm_open":
			v, err = service.Open(ctx, p, a.Repository)
		case "brainstorm_get":
			v, err = service.Get(ctx, p, a.Brainstorm)
		case "brainstorm_add":
			v, err = service.Add(ctx, p, brainstorm.AddRequest{BrainstormID: a.Brainstorm, Kind: a.Kind, Text: a.Text, DocumentName: a.DocumentName, IssueReference: a.IssueReference, RequestKey: a.RequestKey, ExpectedRevision: a.ExpectedRevision})
		default:
			v, err = service.Close(ctx, p, a.Brainstorm, a.RequestKey, a.ExpectedRevision)
		}
		value, summary = v, brainstormViewSummary(v)
	case "brainstorm_entries":
		var page brainstorm.EntryPage
		page, err = service.Entries(ctx, p, a.Brainstorm, a.Kind, a.Cursor, a.Limit)
		if page.Entries == nil {
			page.Entries = []brainstorm.Entry{}
		}
		value, summary = page, pagedSummary(len(page.Entries), "entries", page.NextCursor != "")
	case "brainstorm_scans":
		var page brainstorm.ScanPage
		page, err = service.Scans(ctx, p, a.Brainstorm, a.Cursor, a.Limit)
		if page.Scans == nil {
			page.Scans = []brainstorm.ScanItem{}
		}
		value, summary = page, pagedSummary(len(page.Scans), "questions", page.NextCursor != "")
	case "brainstorm_list":
		var page brainstorm.ListPage
		page, err = service.List(ctx, p, a.Repository, a.Cursor, a.Limit)
		if page.Brainstorms == nil {
			page.Brainstorms = []brainstorm.Brainstorm{}
		}
		value, summary = page, pagedSummary(len(page.Brainstorms), "brainstorms", page.NextCursor != "")
	case "brainstorm_ask", "brainstorm_scan_get", "brainstorm_scan_cancel":
		var v brainstorm.ScanView
		switch name {
		case "brainstorm_ask":
			v, err = service.Ask(ctx, p, brainstorm.AskRequest{BrainstormID: a.Brainstorm, Question: a.Question, RequestKey: a.RequestKey})
		case "brainstorm_scan_get":
			v, err = service.ScanGet(ctx, p, a.Scan, a.Cursor, a.Limit)
		default:
			v, err = service.ScanCancel(ctx, p, a.Scan)
		}
		value, summary = v, brainstormScanSummary(v)
	case "issue_search":
		var v brainstorm.IssueSearchView
		v, err = service.SearchIssues(ctx, p, a.Repository, a.Text, a.Limit)
		if v.Matches == nil {
			v.Matches = []brainstorm.IssueMatch{}
		}
		value, summary = v, v.Summary
		if summary == "" {
			summary = fmt.Sprintf("%d matching issues found.", len(v.Matches))
		}
	case "brainstorm_draft":
		var v brainstorm.ScanView
		v, err = service.Draft(ctx, p, brainstorm.DraftRequest{BrainstormID: a.Brainstorm, RequestKey: a.RequestKey, Note: a.Note})
		value, summary = v, brainstormScanSummary(v)
	case "brainstorm_drafts_get":
		var v brainstorm.DraftSetView
		v, err = service.DraftsGet(ctx, p, a.Brainstorm)
		value, summary = v, v.Summary
		if summary == "" {
			summary = fmt.Sprintf("%d drafts.", len(v.Drafts))
		}
	case "brainstorm_tasks_get":
		var v brainstorm.TasksView
		v, err = service.TasksGet(ctx, p, a.Batch)
		value, summary = v, brainstormTasksSummary(v)
	default:
		return ToolResult{}, errors.New("unknown brainstorm tool")
	}
	if err != nil {
		return ToolResult{}, err
	}
	if _, err = s.storage.CheckConnectorAccess(ctx, digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, "", s.now()); err != nil || s.localControl.check(ctx, s.storage, s.cfg.OwnerID) != nil {
		return ToolResult{}, connectorstate.ErrConnectorInvalid
	}
	return DiscussionResult(summary, value)
}

func pagedSummary(n int, noun string, more bool) string {
	summary := fmt.Sprintf("%d %s on this page.", n, noun)
	if more {
		summary += " More remain; read the next page with next_cursor."
	}
	return summary
}

func brainstormViewSummary(v brainstorm.View) string {
	var entries, scans int64
	for _, n := range v.EntryCounts {
		entries += n
	}
	for _, n := range v.ScanCounts {
		scans += n
	}
	summary := fmt.Sprintf("Brainstorm for %s is %s at revision %d with %d entries and %d questions.", v.Brainstorm.RepositoryRef, v.Brainstorm.State, v.Brainstorm.Revision, entries, scans)
	if v.Replayed {
		summary += " This request was already applied."
	}
	return summary
}

func brainstormScanSummary(v brainstorm.ScanView) string {
	if v.Scan.State == brainstorm.ScanAnswered && v.Answer != "" {
		return v.Answer
	}
	summary := "The question is " + v.Scan.State + "."
	if v.Scan.Reason != "" {
		summary += " " + v.Scan.Reason
	}
	if v.Cost != "" {
		summary += " Cost: " + v.Cost + "."
	}
	return summary
}

func brainstormTasksSummary(v brainstorm.TasksView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Batch is %s.", v.State)
	for _, it := range v.Items {
		fmt.Fprintf(&b, " %s: %s", it.Title, strings.ReplaceAll(it.State, "_", " "))
		if it.IssueNumber > 0 {
			fmt.Fprintf(&b, ", issue %d", it.IssueNumber)
		}
		fmt.Fprintf(&b, "; next: %s.", it.NextStep)
	}
	return b.String()
}

func brainstormFailure(err error) ToolResult {
	summary := "Brainstorm is unavailable."
	for _, known := range []error{brainstorm.ErrNotFound, brainstorm.ErrClosed, brainstorm.ErrStaleRevision, brainstorm.ErrRequestReuse, brainstorm.ErrSkillMissing, brainstorm.ErrNoTracker, brainstorm.ErrQueueFull, brainstorm.ErrCapReached, brainstorm.ErrInvalid, brainstorm.ErrCursor, brainstorm.ErrForbidden, brainstorm.ErrDraftsSkillMissing, brainstorm.ErrNoDrafts, brainstorm.ErrDraftsChanged} {
		if errors.Is(err, known) {
			summary = known.Error() + "."
			return ToolResult{Content: []TextContent{{Type: "text", Text: summary}}, IsError: true}
		}
	}
	switch {
	case errors.Is(err, connectorstate.ErrConnectorScope):
		summary = "repository:brainstorm consent for this repository is required."
	case errors.Is(err, connectorstate.ErrConnectorInvalid), errors.Is(err, connectorstate.ErrConnectorOwner):
		summary = "Current owner, client or repository authorization is unavailable."
	case errors.Is(err, store.ErrNotFound):
		summary = brainstorm.ErrNotFound.Error() + "."
	}
	return ToolResult{Content: []TextContent{{Type: "text", Text: summary}}, IsError: true}
}
