package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/connectorstate"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/durableworkflow"
	"github.com/cloudvane/litefactory/internal/owneraction"
)

var workflowToolNames = []string{"workflows_list", "workflow_describe", "workflow_propose", "workflow_continue_propose", "workflow_job_get"}

func isWorkflowTool(name string) bool { return slices.Contains(workflowToolNames, name) }

type workflowArguments struct {
	WorkflowRef      string            `json:"workflow_ref,omitempty"`
	Version          string            `json:"version,omitempty"`
	Project          string            `json:"project,omitempty"`
	Inputs           map[string]any    `json:"inputs,omitempty"`
	ClientRequestKey string            `json:"client_request_key,omitempty"`
	Job              string            `json:"job,omitempty"`
	SessionRevision  int64             `json:"session_revision,omitempty"`
	Answers          map[string]string `json:"answers,omitempty"`
	Cursor           string            `json:"cursor,omitempty"`
	Limit            int               `json:"limit,omitempty"`
}

func workflowFields(name string) ([]string, []string) {
	switch name {
	case "workflows_list":
		return []string{"project", "cursor", "limit"}, nil
	case "workflow_describe":
		return []string{"workflow_ref", "version", "project"}, []string{"workflow_ref", "version"}
	case "workflow_propose":
		return []string{"workflow_ref", "version", "project", "inputs", "client_request_key"}, []string{"workflow_ref", "version", "inputs", "client_request_key"}
	case "workflow_continue_propose":
		return []string{"job", "session_revision", "answers", "client_request_key"}, []string{"job", "session_revision", "answers", "client_request_key"}
	default:
		return []string{"job", "cursor", "limit"}, []string{"job"}
	}
}
func parseWorkflowArguments(name string, raw json.RawMessage) (workflowArguments, error) {
	var a workflowArguments
	if !isWorkflowTool(name) || len(raw) == 0 || len(raw) > 65536 || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return a, argumentsShapeError()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if e := uniqueJSONObject(d); e != nil {
		return a, uniqueArgumentsError(e)
	}
	if _, e := d.Token(); e != io.EOF {
		return a, argumentsShapeError()
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&a); e != nil {
		return a, decodeArgumentError(e)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return a, argumentsShapeError()
	}
	allowed, required := workflowFields(name)
	if e := checkArgumentFields(fields, allowed, required); e != nil {
		return a, e
	}
	for _, k := range required {
		if bytes.Equal(fields[k], []byte(`""`)) {
			return a, invalidArgument(k, "must not be empty")
		}
	}
	for _, bound := range []struct {
		name string
		size int
		max  int
	}{
		{"workflow_ref", len(a.WorkflowRef), 128}, {"version", len(a.Version), 128}, {"project", len(a.Project), 1024}, {"client_request_key", len(a.ClientRequestKey), 128}, {"job", len(a.Job), 128}, {"cursor", len(a.Cursor), 4096},
	} {
		if bound.size > bound.max {
			return a, invalidArgument(bound.name, fmt.Sprintf("is longer than %d bytes", bound.max))
		}
	}
	if a.Limit < 0 || a.Limit > 50 {
		return a, invalidArgument("limit", "must be between 1 and 50")
	}
	if len(a.Inputs) > 64 {
		return a, invalidArgument("inputs", "has more than 64 entries")
	}
	for _, field := range []struct{ name, value string }{
		{"workflow_ref", a.WorkflowRef}, {"version", a.Version}, {"project", a.Project}, {"client_request_key", a.ClientRequestKey}, {"job", a.Job}, {"cursor", a.Cursor},
	} {
		if _, present := fields[field.name]; present && strings.TrimSpace(field.value) == "" {
			return a, invalidArgument(field.name, "must not be empty")
		}
	}
	for question, answer := range a.Answers {
		if strings.TrimSpace(question) == "" || len(question) > 128 || strings.TrimSpace(answer) == "" || len(answer) > 8192 {
			return a, invalidArgument("answers", "needs non-empty question keys of at most 128 bytes and non-empty answers of at most 8192 bytes")
		}
	}
	if name == "workflow_continue_propose" && a.SessionRevision < 1 {
		return a, invalidArgument("session_revision", "must be at least 1")
	}
	if name == "workflow_continue_propose" && (len(a.Answers) < 1 || len(a.Answers) > 50) {
		return a, invalidArgument("answers", "must hold between 1 and 50 answers")
	}
	if _, ok := fields["limit"]; ok && a.Limit == 0 {
		return a, invalidArgument("limit", "must be between 1 and 50")
	}
	if a.Limit == 0 {
		a.Limit = 25
	}
	return a, nil
}
func workflowInputSchema(name string) map[string]any {
	allowed, required := workflowFields(name)
	props := map[string]any{}
	for _, key := range allowed {
		switch key {
		case "inputs":
			props[key] = map[string]any{"type": "object", "maxProperties": 64}
		case "answers":
			props[key] = map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string", "minLength": 1, "maxLength": 8192}, "propertyNames": map[string]any{"minLength": 1, "maxLength": 128}, "minProperties": 1, "maxProperties": 50}
		case "limit":
			props[key] = map[string]any{"type": "integer", "minimum": 1, "maximum": 50}
		case "session_revision":
			props[key] = map[string]any{"type": "integer", "minimum": 1}
		default:
			maximum := 128
			if key == "cursor" {
				maximum = 4096
			}
			if key == "project" {
				maximum = 1024
			}
			props[key] = map[string]any{"type": "string", "minLength": 1, "maxLength": maximum}
		}
	}
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}
func workflowDescription(name string) string {
	switch name {
	case "workflows_list":
		return "List authorized installed workflow versions and availability, with optional project filter and pagination."
	case "workflow_describe":
		return "Inspect an exact workflow version, target rules, inputs, permissions, limits and result contract without executing work."
	case "workflow_propose":
		return "Create a frozen workflow proposal for review without launching work; the returned proposal requires separate action_confirm."
	case "workflow_continue_propose":
		return "Propose answers to exact saved questions and author revision on the same job; confirm separately using action_confirm."
	default:
		return "Read a durable job, saved results, exact questions, next action and separate notification delivery after reconnect."
	}
}
func (s *Server) callWorkflow(ctx context.Context, credential, name string, a workflowArguments) (ToolResult, error) {
	grant, e := s.storage.CheckConnectorAccess(ctx, digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, "", s.now())
	if e != nil || s.localControl.check(ctx, s.storage, s.cfg.OwnerID) != nil {
		return ToolResult{}, connectorstate.ErrConnectorInvalid
	}
	service := s.workflows.Load()
	if service == nil || !actionGrant(grant) {
		return ToolResult{}, connectorstate.ErrConnectorScope
	}
	p := owneraction.Principal{OwnerID: strconv.FormatInt(grant.OwnerID, 10), OwnerGeneration: grant.OwnerGeneration, Transport: "mcp", ClientID: grant.ClientID, GrantID: grant.ID}
	var value any
	switch name {
	case "workflows_list":
		value, e = service.List(ctx, p, a.Project, a.Cursor, a.Limit)
	case "workflow_describe":
		value, e = service.Describe(ctx, p, a.WorkflowRef, a.Version, a.Project)
	case "workflow_propose":
		value, e = service.Propose(ctx, p, a.WorkflowRef, a.Version, a.Project, a.Inputs, a.ClientRequestKey)
	case "workflow_continue_propose":
		value, e = service.Continue(ctx, p, durableworkflow.Continuation{Job: a.Job, SessionRevision: a.SessionRevision, Answers: a.Answers}, a.ClientRequestKey)
	case "workflow_job_get":
		value, e = service.Get(ctx, p, a.Job, a.Cursor, a.Limit)
	default:
		e = errors.New("unknown workflow tool")
	}
	if e != nil {
		return ToolResult{}, e
	}
	if _, err := s.storage.CheckConnectorAccess(ctx, digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, "", s.now()); err != nil || s.localControl.check(ctx, s.storage, s.cfg.OwnerID) != nil {
		return ToolResult{}, connectorstate.ErrConnectorInvalid
	}
	result, encodeErr := actionResult(value)
	if encodeErr != nil && a.Limit > 1 && (name == "workflows_list" || name == "workflow_job_get") {
		a.Limit = max(1, a.Limit/2)
		return s.callWorkflow(ctx, credential, name, a)
	}
	return result, encodeErr
}
