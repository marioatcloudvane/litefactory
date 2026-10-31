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
	"github.com/cloudvane/litefactory/internal/stagedecision"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
)

var decisionToolNames = []string{"revise_task", "approve_stage", "decision_get", "decision_cancel"}

func isDecisionTool(name string) bool { return slices.Contains(decisionToolNames, name) }

type decisionArguments struct {
	TaskRef          string                 `json:"task_ref,omitempty"`
	Stage            string                 `json:"stage,omitempty"`
	Change           string                 `json:"change,omitempty"`
	ClientRequestKey string                 `json:"client_request_key,omitempty"`
	ConversationRef  string                 `json:"conversation_ref,omitempty"`
	ConclusionRef    string                 `json:"conclusion_ref,omitempty"`
	DiscordChannelID string                 `json:"discord_channel_id,omitempty"`
	Summary          taskdiscussion.Summary `json:"summary,omitempty"`
	DecisionRef      string                 `json:"decision_ref,omitempty"`
	Target           string                 `json:"target,omitempty"`
}

func decisionFields(name string) (allowed, required []string) {
	switch name {
	case "revise_task", "approve_stage":
		allowed = []string{"task_ref", "stage", "client_request_key", "summary", "conversation_ref", "conclusion_ref", "discord_channel_id"}
		required = []string{"task_ref", "stage", "client_request_key", "summary"}
		if name == "revise_task" {
			allowed = append(allowed, "change")
			required = append(required, "change")
		}
	case "decision_get":
		return []string{"decision_ref"}, []string{"decision_ref"}
	case "decision_cancel":
		return []string{"decision_ref", "target"}, []string{"decision_ref", "target"}
	}
	return allowed, required
}

func parseDecisionArguments(name string, raw json.RawMessage) (decisionArguments, error) {
	var a decisionArguments
	invalid := errors.New("invalid stage decision arguments")
	if !isDecisionTool(name) || len(raw) == 0 || len(raw) > 65536 || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
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
	allowed, required := decisionFields(name)
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
		{"task_ref", a.TaskRef, 512}, {"stage", a.Stage, 64}, {"change", a.Change, 8192}, {"client_request_key", a.ClientRequestKey, 128}, {"conversation_ref", a.ConversationRef, 128}, {"conclusion_ref", a.ConclusionRef, 128}, {"discord_channel_id", a.DiscordChannelID, 128}, {"decision_ref", a.DecisionRef, 128}, {"target", a.Target, 16},
	} {
		if _, ok := fields[f.name]; ok && (strings.TrimSpace(f.value) == "" || len(f.value) > f.max) {
			return a, invalid
		}
	}
	if name == "decision_cancel" && a.Target != "proposal" && a.Target != "summary" {
		return a, invalid
	}
	if name == "revise_task" || name == "approve_stage" {
		var summary map[string]json.RawMessage
		if json.Unmarshal(fields["summary"], &summary) != nil || len(summary) != 5 {
			return a, invalid
		}
		for _, key := range []string{"question", "conclusion", "alternatives_or_uncertainty", "evidence_basis", "attribution"} {
			var value string
			if json.Unmarshal(summary[key], &value) != nil || strings.TrimSpace(value) == "" || len(value) > 8192 {
				return a, invalid
			}
		}
	}
	return a, nil
}

func decisionInputSchema(name string) map[string]any {
	allowed, required := decisionFields(name)
	props := map[string]any{}
	for _, key := range allowed {
		maximum := 128
		switch key {
		case "task_ref":
			maximum = 512
		case "stage":
			maximum = 64
		case "change":
			maximum = 8192
		}
		props[key] = map[string]any{"type": "string", "minLength": 1, "maxLength": maximum}
		if key == "summary" {
			props[key] = advisorySummarySchema()
		}
		if key == "target" {
			props[key] = map[string]any{"type": "string", "enum": []string{"proposal", "summary"}}
		}
	}
	return map[string]any{"type": "object", "required": required, "additionalProperties": false, "properties": props}
}

func decisionDescription(name string) string {
	switch name {
	case "revise_task":
		return "Propose revision of eligible current saved analysis, with exact review facts and public decision summary. Requires separate action_confirm; does not approve replacement work."
	case "approve_stage":
		return "Propose approval of an eligible current analysis stage, with exact dependencies and public summary. Requires separate action_confirm and binding Discord confirmation."
	case "decision_get":
		return "Retrieve a stage decision after reconnect, distinguishing confirmation, recorded transition, revision progress and summary delivery or uncertainty."
	default:
		return "Cancel a pending proposal or an unsent decision summary. A recorded transition cannot be undone and a possibly sent summary requires receipt recovery."
	}
}

func (s *Server) callDecision(ctx context.Context, credential, name string, a decisionArguments) (ToolResult, error) {
	grant, err := s.db.CheckConnectorAccess(ctx, digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, "", s.now())
	if err != nil || s.localControl.check(ctx, s.db, s.cfg.OwnerID) != nil {
		return ToolResult{}, store.ErrConnectorInvalid
	}
	service := s.stageDecisions.Load()
	if service == nil || !service.Ready() || !decisionGrant(grant) {
		return ToolResult{}, store.ErrConnectorScope
	}
	p := owneraction.Principal{OwnerID: strconv.FormatInt(grant.OwnerID, 10), OwnerGeneration: grant.OwnerGeneration, Transport: "mcp", ClientID: grant.ClientID, GrantID: grant.ID, ChannelID: a.DiscordChannelID}
	var value any
	switch name {
	case "revise_task", "approve_stage":
		value, err = service.Propose(ctx, p, name, stagedecision.Request{TaskRef: a.TaskRef, Stage: a.Stage, Change: a.Change, ClientRequestKey: a.ClientRequestKey, ConversationRef: a.ConversationRef, ConclusionRef: a.ConclusionRef, DiscordChannelID: a.DiscordChannelID, Summary: a.Summary})
	case "decision_get":
		value, err = service.Get(ctx, p, a.DecisionRef)
	case "decision_cancel":
		value, err = service.Cancel(ctx, p, a.DecisionRef, a.Target)
	default:
		return ToolResult{}, errors.New("unknown stage decision tool")
	}
	if err != nil {
		return ToolResult{}, err
	}
	if _, err = s.db.CheckConnectorAccess(ctx, digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, "", s.now()); err != nil || s.localControl.check(ctx, s.db, s.cfg.OwnerID) != nil {
		return ToolResult{}, store.ErrConnectorInvalid
	}
	return actionResult(value)
}

func decisionGrant(grant store.ConnectorGrant) bool {
	return actionGrant(grant) && store.ConnectorScopeHas(grant.Scope, RepositoryActionsScope)
}

func decisionFailure(err error) ToolResult {
	result := actionFailure(err)
	switch {
	case errors.Is(err, stagedecision.ErrPreviewTooLarge):
		result.Content = []TextContent{{Type: "text", Text: "The complete reviewed decision cannot fit in one response. Shorten the proposed summary or revision guidance and request a fresh preview; nothing was changed."}}
	case errors.Is(err, stagedecision.ErrProtectedChange):
		result.Content = []TextContent{{Type: "text", Text: "Protection changed the meaning of the revision guidance. Submit corrected guidance and review a fresh proposal before confirming."}}
	case errors.Is(err, store.ErrStageDecisionTarget):
		result.Content = []TextContent{{Type: "text", Text: "The current stage is unavailable, unsupported or changed. Review the task's current eligible stage and request a fresh proposal; no different or future candidate was approved."}}
	case errors.Is(err, owneraction.ErrIntegrity):
		result.Content = []TextContent{{Type: "text", Text: "The protected proposal changed. Request a fresh proposal before confirming."}}
	case errors.Is(err, store.ErrWorkflowRevision), errors.Is(err, store.ErrWorkflowConflict):
		result.Content = []TextContent{{Type: "text", Text: "This decision is changed, conflicting or unresolved. Retrieve its recorded status and recover its receipt before another attempt."}}
	case errors.Is(err, store.ErrNotFound):
		result.Content = []TextContent{{Type: "text", Text: "This decision is unavailable in the current owner and task scope."}}
	}
	return result
}
