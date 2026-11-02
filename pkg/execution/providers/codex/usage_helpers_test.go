package codex

import (
	"context"
	"io"

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
	return newUsageCaptureCollector(ctx, req.Usage)
}

type usageTestState struct{ completed, failed bool }

func consumeUsage(src io.Reader, sink io.Writer, c *UsageCollector) usageTestState {
	s := Consume(src, sink, c.observe)
	return usageTestState{s.Completed, s.Failed}
}
