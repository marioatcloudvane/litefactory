package connector

import (
	"context"
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/connectorstate"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
)

// OverviewProvider is the sole read capability that the overview story supplies.
// Implementations must obey cancellation and bound the combined text/structured
// output. The transport separately enforces its serialized response ceiling.
type OverviewProvider interface {
	Overview(context.Context, json.RawMessage) (ToolResult, error)
}

// ToolResult retains the shared factory read contract under the connector API.
// ToolResult is the bounded factory read result.
type ToolResult = contracts.ToolResult

// TextContent retains the shared factory read contract under the connector API.
// TextContent is one rendered factory read item.
type TextContent = contracts.TextContent

// TaskEvidenceProvider supplies only sanitized local evidence for a trusted target.
type TaskEvidenceProvider interface {
	Evidence(context.Context, connectorstate.ConnectorGrantTarget, EvidenceRequest) (EvidencePage, error)
}

// EvidenceRequest retains the shared factory read contract under the connector API.
// EvidenceRequest selects a bounded page of task evidence.
type EvidenceRequest = contracts.EvidenceRequest

// EvidencePage retains the shared factory read contract under the connector API.
// EvidencePage retains sanitized task facts and coverage gaps.
type EvidencePage = contracts.EvidencePage
