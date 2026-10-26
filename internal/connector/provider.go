package connector

import (
	"context"
	"encoding/json"
)

// OverviewProvider is the sole read capability that the overview story supplies.
// Implementations must obey cancellation and bound the combined text/structured
// output. The transport separately enforces its serialized response ceiling.
type OverviewProvider interface {
	Overview(context.Context, json.RawMessage) (ToolResult, error)
}

// ToolResult is a bounded MCP read result, with optional structured content.
type ToolResult struct {
	Content           []TextContent   `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

// TextContent is the text representation of an overview.
type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
