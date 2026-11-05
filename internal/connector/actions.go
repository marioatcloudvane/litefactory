package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/cloudvane/litefactory/internal/connectorstate"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/owneraction"
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
				return duplicateArgument()
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
	if !isActionTool(name) || len(raw) == 0 || len(raw) > 64<<10 || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return a, argumentsShapeError()
	}
	allowed, required := []string{"proposal_id"}, []string{"proposal_id"}
	switch name {
	case "action_propose":
		allowed, required = []string{"action", "arguments", "client_request_key", "discord_channel_id"}, []string{"action", "arguments", "client_request_key"}
	case "action_history":
		allowed, required = []string{"filters", "cursor", "limit"}, nil
	}
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
	if err := json.Unmarshal(raw, &fields); err != nil {
		return a, argumentsShapeError()
	}
	if err := checkArgumentFields(fields, allowed, nil); err != nil {
		return a, err
	}
	switch name {
	case "action_propose":
		for _, f := range []struct{ field, value string }{{"action", a.Action}, {"client_request_key", a.ClientRequestKey}} {
			if _, ok := fields[f.field]; !ok {
				return a, missingArgument(f.field)
			}
			if strings.TrimSpace(f.value) == "" {
				return a, invalidArgument(f.field, "must not be empty")
			}
		}
		if a.Arguments == nil {
			return a, missingArgument("arguments")
		}
		if len(a.Action) > 128 || len(a.ClientRequestKey) > 128 || len(a.DiscordChannelID) > 128 {
			field := "action"
			if len(a.ClientRequestKey) > 128 {
				field = "client_request_key"
			}
			if len(a.DiscordChannelID) > 128 {
				field = "discord_channel_id"
			}
			return a, invalidArgument(field, "is longer than 128 bytes")
		}
		if len(a.Arguments) > 64 {
			return a, invalidArgument("arguments", "has more than 64 entries")
		}
		var argumentFields map[string]json.RawMessage
		if err := json.Unmarshal(fields["arguments"], &argumentFields); err != nil {
			return a, invalidArgument("arguments", "must be an object of string values")
		}
		for _, value := range argumentFields {
			if len(value) == 0 || bytes.TrimSpace(value)[0] != '"' {
				return a, invalidArgument("arguments", "must be an object of string values")
			}
		}
		for key, value := range a.Arguments {
			if key == "" || len(key) > 128 || len(value) > 8192 {
				return a, invalidArgument("arguments", "needs non-empty keys of at most 128 bytes and values of at most 8192 bytes")
			}
		}
	case "action_history":
		if len(a.Filters.Action) > 128 || len(a.Filters.Repository) > 1024 || len(a.Filters.State) > 64 {
			return a, invalidArgument("filters", "has a filter longer than its bound")
		}
		if len(a.Cursor) > 4096 {
			return a, invalidArgument("cursor", "is longer than 4096 bytes")
		}
		if a.Limit < 0 || a.Limit > 50 {
			return a, invalidArgument("limit", "must be between 1 and 50")
		}
		if rawFilters, ok := fields["filters"]; ok {
			var filterFields map[string]json.RawMessage
			if err := json.Unmarshal(rawFilters, &filterFields); err != nil {
				return a, invalidArgument("filters", "must be an object")
			}
			for key, value := range filterFields {
				if !slices.Contains([]string{"action", "repository", "state"}, key) || len(value) == 0 || bytes.TrimSpace(value)[0] != '"' {
					return a, invalidArgument("filters", "may hold only string action, repository and state filters")
				}
			}
		}
		if _, ok := fields["limit"]; ok && a.Limit == 0 {
			return a, invalidArgument("limit", "must be between 1 and 50")
		}
		if a.Limit == 0 {
			a.Limit = 50
		}
	default:
		if _, ok := fields["proposal_id"]; !ok {
			return a, missingArgument("proposal_id")
		}
		if a.ProposalID == "" {
			return a, invalidArgument("proposal_id", "must not be empty")
		}
		if len(a.ProposalID) > 128 {
			return a, invalidArgument("proposal_id", "is longer than 128 bytes")
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
	grant, err := s.storage.CheckConnectorAccess(ctx, digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, "", s.now())
	if err != nil || s.localControl.check(ctx, s.storage, s.cfg.OwnerID) != nil {
		return ToolResult{}, connectorstate.ErrConnectorInvalid
	}
	actions := s.actions.Load()
	if actions == nil || !actionGrant(grant) {
		return ToolResult{}, connectorstate.ErrConnectorScope
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

func actionGrant(g connectorstate.ConnectorGrant) bool {
	return connectorstate.ConnectorScopeHas(g.Scope, RepositoryActionsScope) || connectorstate.ConnectorScopeHas(g.Scope, StandardsManagementScope)
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
	case errors.Is(err, connectorstate.ErrConnectorScope):
		summary = "Action scope or explicit selected-repository consent is missing"
	case errors.Is(err, connectorstate.ErrConnectorInvalid), errors.Is(err, connectorstate.ErrConnectorOwner):
		summary = "Current owner, client or original grant is unauthorized"
	}
	return ToolResult{Content: []TextContent{{Type: "text", Text: summary}}, IsError: true}
}

// actionSchema narrows discovery to the enabled domain catalogue. Admission
// still checks the same fixed registry and domain validation independently.
// Brainstorm actions are listed only for a grant that holds the brainstorm scope.
func (s *Server) actionSchema(name string, brainstorm bool) map[string]any {
	schema := actionInputSchema(name)
	actions := s.actions.Load()
	if name != "action_propose" || actions == nil {
		return schema
	}
	catalog := actions.Catalog()
	variants := []any{}
	for _, entry := range catalog {
		if !brainstorm && strings.HasPrefix(entry.Command, "brainstorm_") {
			continue
		}
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
