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
)

var errActionUnclassified = errors.New("action is not classified")

var actionToolNames = []string{"action_propose", "action_confirm", "action_cancel", "action_inspect", "action_history"}

func isActionTool(name string) bool { return slices.Contains(actionToolNames, name) }

type actionArguments struct {
	Filters          owneraction.Filters `json:"filters,omitempty"`
	Action           string              `json:"action,omitempty"`
	Arguments        map[string]string   `json:"arguments,omitempty"`
	ClientRequestKey string              `json:"client_request_key,omitempty"`
	DiscordChannelID string              `json:"discord_channel_id,omitempty"`
	ProposalID       string              `json:"proposal_id,omitempty"`
	Cursor           string              `json:"cursor,omitempty"`
	Limit            int                 `json:"limit,omitempty"`
}

// uniqueJSONObject rejects duplicated keys recursively, including fixed action
// arguments; a decoder alone otherwise silently keeps the last supplied value.
func uniqueJSONObject(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("duplicate argument")
			}
			seen[name] = true
			if err = uniqueJSONObject(d); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err = uniqueJSONObject(d); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid arguments")
	}
	_, err = d.Token()
	return err
}

func parseActionArguments(name string, raw json.RawMessage) (actionArguments, error) {
	var a actionArguments
	invalid := errors.New("invalid action arguments")
	if !isActionTool(name) || len(raw) == 0 || len(raw) > 64<<10 || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return a, invalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueJSONObject(d); err != nil {
		return a, invalid
	}
	if _, err := d.Token(); err != io.EOF {
		return a, invalid
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&a); err != nil {
		return a, invalid
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return a, invalid
	}
	allowed := []string{"proposal_id"}
	switch name {
	case "action_propose":
		allowed = []string{"action", "arguments", "client_request_key", "discord_channel_id"}
	case "action_history":
		allowed = []string{"filters", "cursor", "limit"}
	}
	for key, value := range fields {
		if !slices.Contains(allowed, key) || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return a, invalid
		}
	}
	switch name {
	case "action_propose":
		if strings.TrimSpace(a.Action) == "" || len(a.Action) > 128 || strings.TrimSpace(a.ClientRequestKey) == "" || len(a.ClientRequestKey) > 128 || len(a.DiscordChannelID) > 128 || a.Arguments == nil || len(a.Arguments) > 64 {
			return a, invalid
		}
		var argumentFields map[string]json.RawMessage
		if err := json.Unmarshal(fields["arguments"], &argumentFields); err != nil {
			return a, invalid
		}
		for _, value := range argumentFields {
			if len(value) == 0 || bytes.TrimSpace(value)[0] != '"' {
				return a, invalid
			}
		}
		for key, value := range a.Arguments {
			if key == "" || len(key) > 128 || len(value) > 8192 {
				return a, invalid
			}
		}
	case "action_history":
		if len(a.Filters.Action) > 128 || len(a.Filters.Repository) > 1024 || len(a.Filters.State) > 64 || len(a.Cursor) > 4096 || a.Limit < 0 || a.Limit > 50 {
			return a, invalid
		}
		if rawFilters, ok := fields["filters"]; ok {
			var filterFields map[string]json.RawMessage
			if err := json.Unmarshal(rawFilters, &filterFields); err != nil {
				return a, invalid
			}
			for key, value := range filterFields {
				if !slices.Contains([]string{"action", "repository", "state"}, key) || len(value) == 0 || bytes.TrimSpace(value)[0] != '"' {
					return a, invalid
				}
			}
		}
		if _, ok := fields["limit"]; ok && a.Limit == 0 {
			return a, invalid
		}
		if a.Limit == 0 {
			a.Limit = 50
		}
	default:
		if a.ProposalID == "" || len(a.ProposalID) > 128 {
			return a, invalid
		}
	}
	return a, nil
}

func actionInputSchema(name string) map[string]any {
	str := func(maximum int) map[string]any {
		return map[string]any{"type": "string", "minLength": 1, "maxLength": maximum}
	}
	properties := map[string]any{"proposal_id": str(128)}
	required := []string{"proposal_id"}
	switch name {
	case "action_propose":
		properties = map[string]any{"action": str(128), "arguments": map[string]any{"type": "object", "maxProperties": 64, "additionalProperties": map[string]any{"type": "string", "maxLength": 8192}}, "client_request_key": str(128), "discord_channel_id": str(128)}
		required = []string{"action", "arguments", "client_request_key"}
	case "action_history":
		properties = map[string]any{"filters": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"action": str(128), "repository": str(1024), "state": str(64)}}, "cursor": str(4096), "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50}}
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func actionDescription(name string) string {
	switch name {
	case "action_propose":
		return "Resolve an enabled existing action into immutable review facts, without executing it. Returns its identifier, target, impact tier, earliest confirmation and expiry."
	case "action_confirm":
		return "Record a separate conversational confirmation for the original client's proposal. Binding actions await the configured owner's Discord reaction."
	case "action_cancel":
		return "Cancel a pending proposal directly; already admitted work cannot be undone."
	case "action_inspect":
		return "Read current authorized proposal facts, confirmation decisions and recorded outcome."
	default:
		return "Read a bounded page of the current owner's authorized proposal history across clients."
	}
}

func (s *Server) callAction(ctx context.Context, credential, name string, a actionArguments) (ToolResult, error) {
	grant, err := s.db.CheckConnectorAccess(ctx, digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, "", s.now())
	if err != nil || s.localControl.check(ctx, s.db, s.cfg.OwnerID) != nil {
		return ToolResult{}, store.ErrConnectorInvalid
	}
	actions := s.actions.Load()
	if actions == nil || !actionGrant(grant) {
		return ToolResult{}, store.ErrConnectorScope
	}
	p := owneraction.Principal{OwnerID: strconv.FormatInt(grant.OwnerID, 10), OwnerGeneration: grant.OwnerGeneration, Transport: "mcp", ClientID: grant.ClientID, GrantID: grant.ID, ChannelID: a.DiscordChannelID}
	var value any
	switch name {
	case "action_propose":
		if _, _, classifyErr := owneraction.Classify(owneraction.Invocation{Command: a.Action, Arguments: a.Arguments}); classifyErr != nil {
			return ToolResult{}, errActionUnclassified
		}
		value, err = actions.Propose(ctx, p, owneraction.Invocation{Command: a.Action, Arguments: a.Arguments}, a.ClientRequestKey)
	case "action_confirm":
		value, err = actions.Confirm(ctx, p, a.ProposalID)
	case "action_cancel":
		value, err = actions.Cancel(ctx, p, a.ProposalID)
	case "action_inspect":
		value, err = actions.Inspect(ctx, p, a.ProposalID)
	case "action_history":
		for {
			page, readErr := actions.HistoryFiltered(ctx, p, a.Filters, a.Cursor, a.Limit)
			if readErr != nil {
				return ToolResult{}, readErr
			}
			result, encodeErr := actionResult(page)
			if encodeErr == nil {
				return result, nil
			}
			if a.Limit <= 1 {
				return ToolResult{}, encodeErr
			}
			a.Limit /= 2
		}
	default:
		return ToolResult{}, errors.New("unknown action tool")
	}
	if err != nil {
		return ToolResult{}, err
	}
	return actionResult(value)
}

func actionResult(value any) (ToolResult, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return ToolResult{}, errors.New("action response unavailable")
	}
	result := ToolResult{Content: []TextContent{{Type: "text", Text: string(b)}}, StructuredContent: b}
	if p, ok := value.(owneraction.Proposal); ok {
		// A readable factory rendering sits next to the unchanged JSON facts.
		result.Content = append(result.Content, TextContent{Type: "text", Text: owneraction.Render(p, owneraction.Options{Transport: owneraction.TransportMCP}).Text()})
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > DiscussionMaxResponseBytes-4096 {
		return ToolResult{}, errors.New("action response unavailable")
	}
	return result, nil
}

func actionGrant(g store.ConnectorGrant) bool {
	return store.ConnectorScopeHas(g.Scope, RepositoryActionsScope) || store.ConnectorScopeHas(g.Scope, StandardsManagementScope)
}

func actionFailure(err error) ToolResult {
	summary := "Action unavailable"
	switch {
	case errors.Is(err, owneraction.ErrDiscordUnavailable):
		summary = "Discord owner approval is not configured"
	case errors.Is(err, owneraction.ErrDiscordDestination):
		summary = "A configured Discord approval channel is required"
	case errors.Is(err, owneraction.ErrRequestConflict):
		summary = "Request key already belongs to different action facts"
	case errors.Is(err, errActionUnclassified):
		summary = "Action is not classified or enabled"
	case errors.Is(err, store.ErrConnectorScope):
		summary = "Action scope or explicit selected-repository consent is missing"
	case errors.Is(err, store.ErrConnectorInvalid), errors.Is(err, store.ErrConnectorOwner):
		summary = "Current owner, client or original grant is unauthorized"
	}
	return ToolResult{Content: []TextContent{{Type: "text", Text: summary}}, IsError: true}
}

// actionSchema narrows discovery to the enabled domain catalogue. Admission
// still checks the same fixed registry and domain validation independently.
func (s *Server) actionSchema(name string) map[string]any {
	schema := actionInputSchema(name)
	actions := s.actions.Load()
	if name != "action_propose" || actions == nil {
		return schema
	}
	catalog := actions.Catalog()
	variants := []any{}
	for _, entry := range catalog {
		argumentProperties := map[string]any{}
		required := []string{}
		for _, argument := range entry.Arguments {
			maximum := argument.MaxBytes
			if maximum < 1 || maximum > 8192 {
				maximum = 8192
			}
			argumentProperties[argument.Name] = map[string]any{"type": "string", "maxLength": maximum}
			if argument.Required {
				required = append(required, argument.Name)
			}
		}
		if entry.Subaction != "" {
			argumentProperties["action"] = map[string]any{"type": "string", "const": entry.Subaction}
			if !slices.Contains(required, "action") {
				required = append(required, "action")
			}
		}
		variant := actionInputSchema(name)
		properties, ok := variant["properties"].(map[string]any)
		if !ok {
			continue
		}
		properties["action"] = map[string]any{"type": "string", "const": entry.Command}
		properties["arguments"] = map[string]any{"type": "object", "properties": argumentProperties, "required": required, "additionalProperties": false}
		variants = append(variants, variant)
	}
	if len(variants) > 0 {
		return map[string]any{"type": "object", "oneOf": variants}
	}
	return schema
}
