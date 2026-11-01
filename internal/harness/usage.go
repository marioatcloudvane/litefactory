package harness

import (
	"context"
	"github.com/cloudvane/litefactory/internal/agent"
	native "github.com/cloudvane/litefactory/pkg/execution/providers/claude"
)

// usageCollector attaches factory attempt attribution to neutral observations.
// The session field is set only after factory identity validation.
type usageCollector struct {
	native  *native.UsageCollector
	session string
}

func newUsageCollector(ctx context.Context, req agent.Request) *usageCollector {
	return newUsageCaptureCollector(ctx, req.Usage, req.Session.Ref, req.Path.Resuming())
}
func newUsageCaptureCollector(ctx context.Context, capture *agent.UsageCapture, session string, resumed bool) *usageCollector {
	if capture == nil {
		return nil
	}
	return &usageCollector{native: native.NewUsageCollector(ctx, capture.NativeCapture(), session, resumed), session: session}
}
func (c *usageCollector) observe(line []byte) {
	if c != nil {
		c.native.SetSession(c.session)
		c.native.Observe(line)
	}
}
func (c *usageCollector) finish() *agent.UsageCoverage {
	if c == nil {
		return nil
	}
	c.native.SetSession(c.session)
	return c.native.Finish()
}
func (c *usageCollector) prepareTranscripts(configDir, cwd string) {
	if c != nil {
		c.native.PrepareTranscripts(configDir, cwd)
	}
}
func transcriptProjectKey(path string) string { return native.TranscriptProjectKey(path) }

func usageIdentity(value string) bool { return native.ValidUsageIdentity(value) }
