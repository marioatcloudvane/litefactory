package submission

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
)

// ToolSpec describes only this server's executable communication surface. It
// never supplies a host's native tool permissions.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Effect      string          `json:"-"`
}

// ToolProfile is immutable, deny by default, and independent of agent.Permissions.
type ToolProfile struct{ mode string }

// NewToolProfile assigns only explicitly supported modes a server-local tool profile.
func NewToolProfile(mode string) (ToolProfile, error) {
	if !slices.Contains([]string{"research", "spike", "plan", "implement", "validate"}, mode) {
		return ToolProfile{}, errors.New("submission: unassigned MCP mode")
	}
	return ToolProfile{mode: mode}, nil
}

// Allows checks the same registered catalog used for discovery, denying unknown names.
func (p ToolProfile) Allows(name string) bool {
	if p.mode == "" {
		return false
	}
	for _, tool := range p.List() {
		if tool.Name == name && (tool.Effect == "read" || tool.Effect == "input") {
			return true
		}
	}
	return false
}

// List returns an independent copy of executable communication tool specifications.
func (p ToolProfile) List() []ToolSpec {
	if p.mode == "" {
		return []ToolSpec{}
	}
	return []ToolSpec{
		{Name: "communication_preflight", Description: "Validate this attempt's shared JSON result without recording it.", InputSchema: json.RawMessage(`{"type":"object","description":"Exact shared agent-result create or update contribution."}`), Effect: "read"},
		{Name: "communication_receipt", Description: "Read an existing application receipt; pending is not workflow success.", InputSchema: json.RawMessage(`{"type":"object","properties":{"request_id":{"type":"string"}},"required":["request_id"],"additionalProperties":false}`), Effect: "read"},
		{Name: "communication_submit", Description: "Record this attempt's shared JSON result; grants no execution, acceptance or publication.", InputSchema: json.RawMessage(`{"type":"object","description":"Exact shared agent-result create or update contribution."}`), Effect: "input"},
	}
}

// ToolCall retains the contribution's bytes independently of connection IDs.
type ToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// DecodeToolCall strictly decodes the envelope while retaining exact argument bytes.
func DecodeToolCall(data []byte) (ToolCall, error) {
	var q ToolCall
	err := decodeStrict(data, &q)
	if err == nil && (q.Name == "" || len(q.Name) > 128 || !json.Valid(q.Arguments)) {
		err = errors.New("submission: invalid tool call")
	}
	return q, err
}

// Client forwards through the same private descriptor and durable submit journal.
// It contains no credentials and starts no process.
type Client struct{ DescriptorPath string }

// List authenticates discovery through the existing private attempt broker.
func (c Client) List(ctx context.Context) (Response, error) {
	return Call(ctx, c.DescriptorPath, "tools/list", json.RawMessage(`{}`))
}

// Call forwards exact arguments through the private broker and existing journal.
func (c Client) Call(ctx context.Context, name string, arguments json.RawMessage) (Response, error) {
	// Do not json.Marshal RawMessage here: it compacts authored whitespace.
	n, err := json.Marshal(name)
	if err != nil {
		return Response{}, err
	}
	data := append([]byte(`{"name":`), n...)
	data = append(data, []byte(`,"arguments":`)...)
	data = append(data, arguments...)
	data = append(data, '}')
	return Call(ctx, c.DescriptorPath, "tools/call", data)
}
