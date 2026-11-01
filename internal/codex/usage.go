package codex

import (
	"context"
	"github.com/cloudvane/litefactory/internal/agent"
	native "github.com/cloudvane/litefactory/pkg/execution/providers/codex"
)

// usageCollector retains factory attribution while native collection lives in the provider.
type usageCollector struct{ native *native.UsageCollector }

func newUsageCollector(ctx context.Context, req agent.Request) *usageCollector {
	return newUsageCaptureCollector(ctx, req.Usage)
}
func newUsageCaptureCollector(ctx context.Context, capture *agent.UsageCapture) *usageCollector {
	if capture == nil {
		return nil
	}
	return &usageCollector{native: native.NewUsageCollector(ctx, capture.NativeCapture())}
}
func (c *usageCollector) observe(line []byte) {
	if c != nil {
		c.native.Observe(line)
	}
}
func (c *usageCollector) finish() *agent.UsageCoverage {
	if c == nil {
		return nil
	}
	return c.native.Finish()
}
func (c *usageCollector) prepareRollouts(environ []string) {
	if c != nil {
		c.native.PrepareRollouts(environ)
	}
}
