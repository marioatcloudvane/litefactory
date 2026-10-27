package connector

import (
	"context"
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/store"
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

// TextContent is the text representation of a read projection.
type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// TaskEvidenceProvider supplies only sanitized local evidence for a trusted target.
type TaskEvidenceProvider interface {
	Evidence(context.Context, store.ConnectorGrantTarget, EvidenceRequest) (EvidencePage, error)
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
