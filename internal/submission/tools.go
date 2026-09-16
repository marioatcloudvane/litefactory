package submission

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
)

// ToolSpec describes only this server's executable tool surface. It
// never supplies a host's native tool permissions.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Effect      string          `json:"-"`
}

// ToolProfile is immutable, deny by default, and independent of agent.Permissions.
type ToolProfile struct {
	mode     string
	families []string
	creation bool
}

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
		if tool.Name == name {
			return true
		}
	}
	return false
}

// A contribution is either a create or an update, never both, and implement
// may instead send a factory-bound aggregate capture. Expressing that choice as
// oneOf is not portable across the installed hosts, so every branch's fields are
// declared optional here and the exclusive rule stays with the server's
// validators, which already answer a wrong combination with typed diagnostics.
// Declaring the fields at all is the point: a property-less object leaves the
// model guessing names from prose, and the host rejects each guess before the
// call is ever sent. Open-ended members -- payload, operation values, capture
// bytes -- stay untyped so no valid contribution is refused by the schema.
const sharedResultCreate = `"result":{"type":"object","properties":{` +
	`"outcome":{"type":"string","enum":["completed","incomplete","blocked"]},` +
	`"comment":{"type":"string"},"reason":{"type":"string"},` +
	`"questions":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"text":{"type":"string"},` +
	`"options":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"text":{"type":"string"}},"required":["id","text"]}},` +
	`"recommended_option":{"type":"string"}},"required":["id","text"]}},` +
	`"recommendation":{"type":"object"},"artifacts":{"type":"array","items":{"type":"object"}},` +
	`"payload":{"type":"object","description":"Exactly one mode key: research, plan, implement, validate or spike."}},` +
	`"required":["outcome","comment","payload"]}`

const sharedResultUpdate = `"expected_revision":{"type":"integer","description":"The supplied authorized baseline revision."},` +
	`"operations":{"type":"array","items":{"type":"object","properties":{` +
	`"op":{"type":"string","enum":["set","add_item","update_item","remove","remove_item","reorder"]},` +
	`"target":{"type":"object","properties":{"field":{"type":"string"},"story":{"type":"string"},"id":{"type":"string"}},"required":["field"]},` +
	`"value":{"description":"Any JSON value; omitted for remove."},` +
	`"order":{"type":"array","items":{"type":"string"}}},"required":["op","target"]}}`

const sharedResultSchema = `{"type":"object","description":"Exact shared agent-result contribution: either a create (result) or an update (expected_revision with operations), never both.","properties":{` +
	sharedResultCreate + `,` + sharedResultUpdate + `},"additionalProperties":false}`

const implementResultSchema = `{"type":"object","description":"Shared result create (result) or update (expected_revision with operations), or a factory-bound aggregate capture (engineering with capture). Send exactly one of the three.","properties":{` +
	sharedResultCreate + `,` + sharedResultUpdate + `,` +
	`"engineering":{"type":"object","description":"Engineering reference the aggregate capture is bound to."},` +
	`"capture":{"type":"object","description":"Typed aggregate capture: implementation_digest, files and executed checks."}},"additionalProperties":false}`

// List returns independent specifications for the installed, permitted catalog.
func (p ToolProfile) List() []ToolSpec {
	if p.mode == "" {
		return []ToolSpec{}
	}
	tools := []ToolSpec{
		{Name: "communication_preflight", Description: "Validate this attempt's shared JSON result without recording it.", InputSchema: json.RawMessage(sharedResultSchema), Effect: "read"},
		{Name: "communication_receipt", Description: "Read an existing application receipt; pending is not workflow success.", InputSchema: json.RawMessage(`{"type":"object","properties":{"request_id":{"type":"string"}},"required":["request_id"],"additionalProperties":false}`), Effect: "read"},
		{Name: "communication_submit", Description: "Record this attempt's shared JSON result; grants no execution, acceptance or publication.", InputSchema: json.RawMessage(sharedResultSchema), Effect: "input"},
	}
	if p.mode == "implement" {
		for i := range tools {
			if tools[i].Name == "communication_preflight" || tools[i].Name == "communication_submit" {
				tools[i].InputSchema = json.RawMessage(implementResultSchema)
			}
		}
	}
	for _, name := range p.families {
		spec, ok := familyTool(name)
		if ok && familyAllowed(p.mode, p.creation, name) {
			tools = append(tools, spec)
		}
	}
	return tools
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
