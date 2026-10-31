package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
)

var advisoryToolNames = []string{"ask_task_agent", "judge_task", "advisory_job_get", "advisory_job_cancel", "discussion_open", "discussion_summary_publish", "discussion_summary_get"}

func isAdvisoryTool(name string) bool { return slices.Contains(advisoryToolNames, name) }

type advisoryArguments struct {
	TaskRef              string                 `json:"task_ref,omitempty"`
	Question             string                 `json:"question,omitempty"`
	Focus                string                 `json:"focus,omitempty"`
	ClientRequestKey     string                 `json:"client_request_key,omitempty"`
	ConversationRef      string                 `json:"conversation_ref,omitempty"`
	ConversationRevision int64                  `json:"conversation_revision,omitempty"`
	JobRef               string                 `json:"job_ref,omitempty"`
	Cursor               string                 `json:"cursor,omitempty"`
	ConclusionID         string                 `json:"conclusion_id,omitempty"`
	Summary              taskdiscussion.Summary `json:"summary,omitempty"`
	SourceJobRef         string                 `json:"source_job_ref,omitempty"`
	PublicationRef       string                 `json:"publication_ref,omitempty"`
	CancelPending        bool                   `json:"cancel_pending,omitempty"`
}

func advisoryFields(name string) (allowed, required []string) {
	switch name {
	case "ask_task_agent":
		return []string{"task_ref", "question", "client_request_key", "conversation_ref", "conversation_revision"}, []string{"task_ref", "question", "client_request_key"}
	case "judge_task":
		return []string{"task_ref", "focus", "client_request_key", "conversation_ref", "conversation_revision"}, []string{"task_ref", "client_request_key"}
	case "advisory_job_get":
		return []string{"job_ref", "cursor"}, []string{"job_ref"}
	case "advisory_job_cancel":
		return []string{"job_ref"}, []string{"job_ref"}
	case "discussion_open":
		return []string{"task_ref", "client_request_key"}, []string{"task_ref", "client_request_key"}
	case "discussion_summary_publish":
		return []string{"task_ref", "conversation_ref", "conclusion_id", "summary", "source_job_ref"}, []string{"task_ref", "conversation_ref", "conclusion_id", "summary"}
	case "discussion_summary_get":
		return []string{"publication_ref", "cancel_pending"}, []string{"publication_ref"}
	}
	return nil, nil
}

func parseAdvisoryArguments(name string, raw json.RawMessage) (advisoryArguments, error) {
	var a advisoryArguments
	invalid := errors.New("invalid task discussion arguments")
	if !isAdvisoryTool(name) || len(raw) == 0 || len(raw) > 65536 || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return a, invalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if uniqueJSONObject(d) != nil {
		return a, invalid
	}
	if _, err := d.Token(); err != io.EOF {
		return a, invalid
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&a) != nil {
		return a, invalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return a, invalid
	}
	allowed, required := advisoryFields(name)
	for key, value := range fields {
		if !slices.Contains(allowed, key) || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return a, invalid
		}
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return a, invalid
		}
	}
	for _, f := range []struct {
		name, value string
		max         int
	}{
		{"task_ref", a.TaskRef, 512}, {"question", a.Question, 32768}, {"focus", a.Focus, 32768}, {"client_request_key", a.ClientRequestKey, 128}, {"conversation_ref", a.ConversationRef, 128}, {"job_ref", a.JobRef, 128}, {"cursor", a.Cursor, 4096}, {"conclusion_id", a.ConclusionID, 128}, {"source_job_ref", a.SourceJobRef, 128}, {"publication_ref", a.PublicationRef, 128},
	} {
		if _, present := fields[f.name]; present && (strings.TrimSpace(f.value) == "" || len(f.value) > f.max) {
			return a, invalid
		}
	}
	if _, present := fields["conversation_revision"]; present && a.ConversationRevision < 0 {
		return a, invalid
	}
	if a.ConversationRef == "" {
		if _, present := fields["conversation_revision"]; present {
			return a, invalid
		}
	}
	if name == "discussion_summary_publish" {
		var summaryFields map[string]json.RawMessage
		if json.Unmarshal(fields["summary"], &summaryFields) != nil || len(summaryFields) != 5 {
			return a, invalid
		}
		for _, key := range []string{"question", "conclusion", "alternatives_or_uncertainty", "evidence_basis", "attribution"} {
			var value string
			if json.Unmarshal(summaryFields[key], &value) != nil || strings.TrimSpace(value) == "" || len(value) > 8192 {
				return a, invalid
			}
		}
	}
	return a, nil
}

func advisoryInputSchema(name string) map[string]any {
	allowed, required := advisoryFields(name)
	str := func(maximum int) map[string]any {
		return map[string]any{"type": "string", "minLength": 1, "maxLength": maximum}
	}
	props := map[string]any{}
	for _, key := range allowed {
		switch key {
		case "conversation_revision":
			props[key] = map[string]any{"type": "integer", "minimum": 0}
		case "cancel_pending":
			props[key] = map[string]any{"type": "boolean"}
		case "summary":
			fields := []string{"question", "conclusion", "alternatives_or_uncertainty", "evidence_basis", "attribution"}
			summary := map[string]any{}
			for _, field := range fields {
				summary[field] = str(8192)
			}
			props[key] = map[string]any{"type": "object", "required": fields, "additionalProperties": false, "properties": summary}
		case "task_ref":
			props[key] = str(512)
		case "question", "focus":
			props[key] = str(32768)
		case "cursor":
			props[key] = str(4096)
		default:
			props[key] = str(128)
		}
	}
	return map[string]any{"type": "object", "required": required, "additionalProperties": false, "properties": props}
}

func advisoryDescription(name string) string {
	switch name {
	case "ask_task_agent":
		return "Admit one private consultation of the task's current saved analysis under repository task:advisory consent. May incur provider cost; no separate per-job confirmation. Returns a durable job and conversation reference."
	case "judge_task":
		return "Admit one private independent assessment of the task's eligible current saved work under repository task:advisory consent. May incur provider cost; no separate per-job confirmation."
	case "advisory_job_get":
		return "Read the private advisory job's lifecycle, immutable task basis, freshness, sourced answer, context gaps and next actions after reconnect. Requires current task:advisory consent."
	case "advisory_job_cancel":
		return "Cancel queued advisory work or request a best-effort running stop. Completion may win the race. Requires current task:advisory consent."
	case "discussion_open":
		return "Open a durable scoped conversation for ordinary chat without launching paid work or retaining its transcript. Requires task:advisory or task:discussion-publish repository consent."
	case "discussion_summary_publish":
		return "Queue a structured conclusion summary under separate task:discussion-publish repository consent, without a transcript or source-job requirement. Returns durable publication status; workflow state is preserved."
	default:
		return "Recover a conclusion publication's pending, delivered, failed or uncertain status and comment link; optionally cancel before dispatch. A possibly sent summary is never blindly reposted."
	}
}

func advisoryToolAllowed(grant store.ConnectorGrant, name string) bool {
	advisory := store.ConnectorScopeHas(grant.Scope, AdvisoryScope)
	publish := store.ConnectorScopeHas(grant.Scope, DiscussionPublishScope)
	switch name {
	case "discussion_open":
		return advisory || publish
	case "discussion_summary_publish", "discussion_summary_get":
		return publish
	default:
		return advisory
	}
}

func (s *Server) callAdvisory(ctx context.Context, credential, name string, a advisoryArguments) (ToolResult, error) {
	grant, err := s.db.CheckConnectorAccess(ctx, digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, "", s.now())
	if err != nil || s.localControl.check(ctx, s.db, s.cfg.OwnerID) != nil {
		return ToolResult{}, store.ErrConnectorInvalid
	}
	service := s.taskDiscussion.Load()
	if service == nil || !advisoryToolAllowed(grant, name) {
		return ToolResult{}, store.ErrConnectorScope
	}
	p := owneraction.Principal{OwnerID: strconv.FormatInt(grant.OwnerID, 10), OwnerGeneration: grant.OwnerGeneration, Transport: "mcp", ClientID: grant.ClientID, GrantID: grant.ID}
	var value any
	switch name {
	case "ask_task_agent", "judge_task":
		operation := "ask"
		if name == "judge_task" {
			operation = "judge"
		}
		value, err = service.Ask(ctx, p, taskdiscussion.Request{TaskRef: a.TaskRef, Question: a.Question, Focus: a.Focus, ClientRequestKey: a.ClientRequestKey, ConversationRef: a.ConversationRef, ConversationRevision: a.ConversationRevision, Operation: operation})
	case "advisory_job_get":
		value, err = service.Get(ctx, p, a.JobRef, a.Cursor)
	case "advisory_job_cancel":
		value, err = service.Cancel(ctx, p, a.JobRef)
	case "discussion_open":
		value, err = service.Open(ctx, p, a.TaskRef, a.ClientRequestKey)
	case "discussion_summary_publish":
		value, err = service.Publish(ctx, p, taskdiscussion.PublicationRequest{TaskRef: a.TaskRef, ConversationRef: a.ConversationRef, ConclusionID: a.ConclusionID, SourceJobRef: a.SourceJobRef, Summary: a.Summary})
	case "discussion_summary_get":
		value, err = service.Publication(ctx, p, a.PublicationRef, a.CancelPending)
	default:
		return ToolResult{}, errors.New("unknown task discussion tool")
	}
	if err != nil {
		return ToolResult{}, err
	}
	if _, err = s.db.CheckConnectorAccess(ctx, digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, "", s.now()); err != nil || s.localControl.check(ctx, s.db, s.cfg.OwnerID) != nil {
		return ToolResult{}, store.ErrConnectorInvalid
	}
	return actionResult(value)
}

func advisoryOutputSchema(name string) map[string]any {
	str := map[string]any{"type": "string"}
	stringsSchema := map[string]any{"type": "array", "items": str}
	object := func(required []string, properties map[string]any) map[string]any {
		return map[string]any{"type": "object", "required": required, "additionalProperties": false, "properties": properties}
	}
	if name == "discussion_open" {
		return object([]string{"conversation_ref", "conversation_revision", "task_ref"}, map[string]any{"conversation_ref": str, "conversation_revision": map[string]any{"type": "integer", "minimum": 0}, "task_ref": str})
	}
	if name == "discussion_summary_publish" || name == "discussion_summary_get" {
		return object([]string{"publication_ref", "state", "next_actions"}, map[string]any{"publication_ref": str, "state": map[string]any{"type": "string", "enum": []string{"pending", "sending", "delivered", "failed", "uncertain"}}, "reason": str, "comment_url": str, "next_actions": stringsSchema})
	}
	basis := object([]string{"task_ref", "repository_ref", "stage", "role", "revision", "commit", "evidence", "guidance", "sources", "gaps"}, map[string]any{"task_ref": str, "repository_ref": str, "stage": str, "role": str, "revision": str, "commit": str, "evidence": map[string]any{}, "guidance": map[string]any{}, "sources": stringsSchema, "gaps": stringsSchema})
	summary := advisorySummarySchema()
	answer := object([]string{"answer", "sources", "gaps"}, map[string]any{"answer": str, "sources": stringsSchema, "gaps": stringsSchema, "summary_suggestion": summary})
	return object([]string{"job_ref", "conversation_ref", "conversation_revision", "state", "captured_basis", "freshness", "omitted_history", "completion_won", "next_actions", "cost_guidance", "result_page_offset", "result_complete"}, map[string]any{"job_ref": str, "conversation_ref": str, "conversation_revision": map[string]any{"type": "integer", "minimum": 0}, "state": map[string]any{"type": "string", "enum": []string{"queued", "running", "stop_requested", "succeeded", "failed", "cancelled", "uncertain"}}, "captured_basis": basis, "freshness": str, "result": answer, "omitted_history": map[string]any{"type": "integer", "minimum": 0}, "completion_won": map[string]any{"type": "boolean"}, "next_cursor": str, "next_actions": stringsSchema, "cost_guidance": str, "result_page_offset": map[string]any{"type": "integer", "minimum": 0}, "result_complete": map[string]any{"type": "boolean"}})
}

func advisoryFailure(err error) ToolResult {
	summary := "Task discussion is unavailable."
	switch {
	case errors.Is(err, taskdiscussion.ErrTargetUnavailable):
		summary = "The task has no single available current analysis to consult or review."
	case errors.Is(err, taskdiscussion.ErrBasisChanged):
		summary = "The task changed while its evidence was captured. Retry against the current analysis."
	case errors.Is(err, store.ErrWorkflowConflict):
		summary = "This request or conclusion identity already belongs to different content."
	case errors.Is(err, store.ErrWorkflowRevision):
		summary = "The conversation changed or already has active work. Retrieve its current status before continuing."
	case errors.Is(err, store.ErrConnectorScope):
		summary = "Separate consent for this operation and its selected repository is required."
	case errors.Is(err, store.ErrConnectorInvalid), errors.Is(err, store.ErrConnectorOwner):
		summary = "Current owner, client or repository authorization is unavailable."
	case errors.Is(err, store.ErrNotFound):
		summary = "The requested discussion record is unavailable in this owner and task scope."
	}
	return ToolResult{Content: []TextContent{{Type: "text", Text: summary}}, IsError: true}
}

func advisorySummarySchema() map[string]any {
	fields := []string{"question", "conclusion", "alternatives_or_uncertainty", "evidence_basis", "attribution"}
	properties := map[string]any{}
	for _, field := range fields {
		properties[field] = map[string]any{"type": "string", "minLength": 1, "maxLength": 8192}
	}
	return map[string]any{"type": "object", "required": fields, "additionalProperties": false, "properties": properties}
}
