package turn

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// CreationPublicationContext observes the same frozen selection and fallback as
// Research admission, without admitting a path, preparing a run or binding consent.
func CreationPublicationContext(ctx context.Context, db *store.DB, sessionID int64, repoPath string) report.CreationPublication {
	path, err := db.ResearchExecutionPath(ctx, sessionID)
	if errors.Is(err, store.ErrNotFound) {
		path, err = config.ResearchExecution(repoPath)
		if err == nil && path == "" {
			path = "legacy"
			if researchDefaultActive {
				path = "graph"
			}
		}
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

// CreationPublishingAvailable observes configured capability only. It neither
// discovers a grant nor prepares the native tool catalog.
func (r *MCPRuntime) CreationPublishingAvailable(runner Runner) bool {
	native, ok := runner.(interface{ SupportsMCP() bool })
	return r != nil && !r.DisableMCP && r.CreationAuthorizer != nil && r.CreationAuthorizer.enabled && r.Creation != nil && ok && native.SupportsMCP()
}
