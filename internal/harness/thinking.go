package harness

import (
	"context"
	"fmt"
	execution "github.com/cloudvane/litefactory/pkg/execution/agent"
	native "github.com/cloudvane/litefactory/pkg/execution/providers/claude"
	"log/slog"
	"os"
	"sync"
)

// thinkingDisplayFlag is Claude Code's hidden option that fills the
// stream-json thinking blocks with a summary. It is absent from --help, so
// support is probed rather than assumed.
const thinkingDisplayFlag = "--thinking-display"

// thinkingSupport caches the probe per binary file, so an in-place upgrade or
// downgrade of claude is probed again.
type thinkingSupport struct {
	mu        sync.Mutex
	probed    bool
	key       string
	supported bool
}

// thinkingArgs returns the launch flags settings.thinking_logs asks for. Both
// summarized and full map to "summarized", the most detailed form Claude
// offers. An unsupported or unprobeable binary gets no flag and one WARN, so
// the setting never stops a run.
func (r *Runner) thinkingArgs(ctx context.Context) []string {
	if !r.thinkingLogs.Enabled() || !r.thinkingDisplaySupported(ctx) {
		return nil
	}
	return []string{thinkingDisplayFlag, "summarized"}
}

func (r *Runner) thinkingDisplaySupported(ctx context.Context) bool {
	key := r.binary.Path
	if info, err := os.Stat(r.binary.Path); err == nil {
		key = fmt.Sprintf("%s|%d|%d", r.binary.Path, info.Size(), info.ModTime().UnixNano())
	}
	r.thinking.mu.Lock()
	defer r.thinking.mu.Unlock()
	if r.thinking.probed && r.thinking.key == key {
		return r.thinking.supported
	}
	supported, detail := probeThinkingDisplay(ctx, r.binary.Path, execution.IsolatedEnvironment(r.environ, os.TempDir()))
	if ctx.Err() != nil {
		return false
	}
	r.thinking.probed, r.thinking.key, r.thinking.supported = true, key, supported
	if !supported && r.logger != nil {
		r.logger.Warn("claude does not accept "+thinkingDisplayFlag+"; runs launch without thinking logs",
			slog.String("binary", r.binary.Path),
			slog.String("thinking_logs", string(r.thinkingLogs)),
			slog.String("detail", detail))
	}
	return supported
}

// probeThinkingDisplay runs `claude --thinking-display <sentinel> --version`.
// A supporting binary exits non-zero naming the option and its "summarized"
// choice; any other answer, including a clean version print, is unsupported.
func probeThinkingDisplay(ctx context.Context, binary string, env []string) (bool, string) {
	return native.ProbeThinkingDisplay(ctx, binary, env)
}
