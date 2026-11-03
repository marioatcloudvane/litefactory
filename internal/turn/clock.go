package turn

import (
	"strings"
	"time"
)

// graphNow is the run's clock, or the wall clock where no runtime is attached.
func graphNow(opts Options) time.Time {
	if opts.MCP != nil && opts.MCP.Now != nil {
		return opts.MCP.Now()
	}
	return time.Now().UTC()
}

// clipText bounds text to limit bytes, keeping valid UTF-8 and marking the cut.
func clipText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return strings.ToValidUTF8(text[:limit], "") + "..."
}
