package harness

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/providerprocess"
)

// thinkingDisplayFlag is Claude Code's hidden option that fills the
// stream-json thinking blocks with a summary. It is absent from --help, so
// support is probed rather than assumed.
const thinkingDisplayFlag = "--thinking-display"

// thinkingProbeSentinel is an invalid choice: a binary that knows the option
// rejects it and lists the allowed choices, while --version ignores options
// it does not know. Neither path reaches a model.
const thinkingProbeSentinel = "litefactory-probe"

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
	supported, detail := probeThinkingDisplay(ctx, r.binary.Path, providerprocess.DoctorEnvironment(r.environ, os.TempDir()))
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
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	//nolint:gosec // Resolved provider executable and constant probe arguments.
	cmd := exec.CommandContext(probeCtx, binary, thinkingDisplayFlag, thinkingProbeSentinel, "--version")
	cmd.Env, cmd.Dir = env, os.TempDir()
	cmd.WaitDelay = time.Second
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	text := output.String()
	if err != nil && strings.Contains(text, thinkingDisplayFlag) && strings.Contains(text, "summarized") {
		return true, ""
	}
	if len(text) > 300 {
		text = text[:300]
	}
	text = strings.TrimSpace(text)
	if err != nil {
		return false, strings.TrimSpace(err.Error() + ": " + text)
	}
	return false, text
}
