package connector

import (
	"context"
	"github.com/cloudvane/litefactory/internal/connectorstate"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
)

// CodeEvidenceProvider exposes sanitized committed and published evidence only.
type CodeEvidenceProvider interface {
	Evidence(context.Context, connectorstate.ConnectorCodeTarget, CodeEvidenceRequest) (CodeEvidencePage, error)
}

// CodeEvidencePin retains the shared factory read contract under the connector API.
// CodeEvidencePin identifies an immutable retained code view.
type CodeEvidencePin = contracts.CodeEvidencePin

// CodeEvidenceRequest retains the shared factory read contract under the connector API.
// CodeEvidenceRequest selects bounded committed code evidence.
type CodeEvidenceRequest = contracts.CodeEvidenceRequest

// CodeEvidencePage retains the shared factory read contract under the connector API.
// CodeEvidencePage carries sanitized committed code evidence.
type CodeEvidencePage = contracts.CodeEvidencePage
