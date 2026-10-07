package turn

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// CreationPublicationContext observes the same frozen selection and fallback as
// Research admission, without admitting a path, preparing a run or binding consent.
func CreationPublicationContext(ctx context.Context, db *store.DB, sessionID int64, _ string) report.CreationPublication {
	path, err := db.ResearchExecutionPath(ctx, sessionID)
	if errors.Is(err, store.ErrNotFound) {
		path, err = "graph", nil
	}
	if err != nil {
		return report.CreationPublicationUnknown
	}
	switch path {
	case "graph":
		return report.CreationPublicationGraph
	case "legacy":
		return report.CreationPublicationLegacy
	default:
		return report.CreationPublicationUnknown
	}
}

// CreationEnabled reports the loaded creation gate.
func (r *MCPRuntime) CreationEnabled() bool {
	return r != nil && r.CreationAuthorizer != nil && r.CreationAuthorizer.enabled
}
