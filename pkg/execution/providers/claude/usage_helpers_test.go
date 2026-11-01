package claude

import (
	"context"

	"github.com/cloudvane/litefactory/pkg/execution/agent"
)

type testSession struct{ Ref string }
type testUsageRequest struct {
	Usage   *agent.UsageCapture
	Session testSession
	Path    bool
	Mode    string
}

func newUsageCollector(ctx context.Context, req testUsageRequest) *UsageCollector {
	return newUsageCaptureCollector(ctx, req.Usage, req.Session.Ref, req.Path)
}
