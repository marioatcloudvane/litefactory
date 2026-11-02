package contracts

import "encoding/json"

// ToolResult is a bounded MCP read result, with optional structured content.
type ToolResult struct {
	Content           []TextContent   `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

// TextContent is the text representation of a read projection.
type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// EvidenceRequest is a validated section and bounded advancing position.
type EvidenceRequest struct {
	Section string
	Offset  int
	Limit   int
}

// EvidencePage carries sanitized source facts and explicit coverage gaps.
type EvidencePage struct {
	Revision   string           `json:"revision"`
	Items      []map[string]any `json:"items"`
	Sources    []map[string]any `json:"sources"`
	Gaps       []string         `json:"gaps"`
	HasMore    bool             `json:"has_more"`
	NextOffset int              `json:"-"`
}

// OverviewProjectionMaxBytes reserves the existing envelope and request budget.
const OverviewProjectionMaxBytes = (256 << 10) - (64 << 10) - 1024
