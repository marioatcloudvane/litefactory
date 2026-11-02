// Package claude executes prepared Claude CLI invocations without factory state.
package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/pkg/execution/agent"
)

// Options contains native Claude configuration. A nil Tools slice omits the
// flag; a non-nil empty slice explicitly disables native tools.
type Options struct {
	Binary string
	// ConfigDirectory selects transcript evidence; Environment supplies the matching native config directory.
	ConfigDirectory string
	Environment     []string
	Model           string
	Tools           []string
	AllowedTools    []string
	PermissionMode  string
	SettingsPath    string
	AutoCompact     string
	Effort          string
	Resume          bool
	StrictMCPConfig bool
	ExtraArgs       []string
	Logger          *slog.Logger
}

// Runner executes prepared Claude calls.
type Runner struct{ options Options }

// New validates explicit provider construction without launching it.
func New(options Options) (*Runner, error) {
	if !filepath.IsAbs(options.Binary) {
		return nil, errors.New("claude binary must be an absolute path")
	}
	info, err := os.Stat(options.Binary)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return nil, errors.New("claude binary must be executable")
	}
	options.Environment = append([]string{}, options.Environment...)
	if options.Tools != nil {
		options.Tools = append([]string{}, options.Tools...)
	}
	options.AllowedTools = append([]string(nil), options.AllowedTools...)
	options.ExtraArgs = append([]string(nil), options.ExtraArgs...)
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}
	return &Runner{options: options}, nil
}

// Arguments preserves Claude's native session and tool-presence semantics.
func Arguments(options Options, session string) []string {
	args := []string{"-p", "--output-format", "stream-json", "--verbose"}
	if session != "" {
		flag := "--session-id"
		if options.Resume {
			flag = "--resume"
		}
		args = append(args, flag, session)
	}
	if options.Tools != nil {
		args = append(args, "--tools", strings.Join(options.Tools, ","))
	}
	if options.SettingsPath != "" {
		args = append(args, "--settings", options.SettingsPath)
	}
	if options.PermissionMode != "" {
		args = append(args, "--permission-mode", options.PermissionMode)
	}
	if options.Model != "" {
		args = append(args, "--model", options.Model)
	}
	if options.AutoCompact != "" {
		args = append(args, "--autocompact", options.AutoCompact)
	}
	if options.Effort != "" {
		args = append(args, "--effort", options.Effort)
	}
	if len(options.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(options.AllowedTools, ","))
	}
	if options.StrictMCPConfig {
		args = append(args, "--strict-mcp-config")
	}
	return append(args, options.ExtraArgs...)
}

// Run executes one prepared native invocation.
func (r *Runner) Run(ctx context.Context, request agent.Request) (agent.Result, error) {
	var result agent.Result
	if !filepath.IsAbs(request.WorkingDirectory) || strings.TrimSpace(request.Prompt) == "" {
		return result, &agent.Failure{Phase: "validation", Cause: errors.New("absolute working directory and prepared prompt are required")}
	}
	if request.Timeout <= 0 {
		return result, &agent.Failure{Phase: "validation", Cause: errors.New("positive timeout is required")}
	}
	runCtx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	collector := NewUsageCollector(runCtx, request.Usage, request.Session, r.options.Resume)
	collector.PrepareTranscripts(r.options.ConfigDirectory, request.WorkingDirectory)
	stream := &Stream{Sink: request.Events, Logger: r.options.Logger}
	var sessionErr error
	stream.Observe = func(line []byte) {
		raw := append(json.RawMessage(nil), line...)
		if json.Valid(raw) {
			result.Events = append(result.Events, raw)
		}
		var event struct {
			Session    string          `json:"session_id"`
			Usage      json.RawMessage `json:"usage"`
			ModelUsage json.RawMessage `json:"modelUsage"`
			Message    struct {
				Usage json.RawMessage `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &event) == nil {
			for _, usage := range []json.RawMessage{event.Usage, event.ModelUsage, event.Message.Usage} {
				if len(usage) > 0 && string(usage) != "null" {
					result.Usage = append(result.Usage, append(json.RawMessage(nil), line...))
					break
				}
			}
			if event.Session != "" && event.Session != result.Session && sessionErr == nil {
				if request.OnSession != nil {
					sessionErr = request.OnSession(runCtx, event.Session)
					if sessionErr != nil {
						cancel()
						return
					}
				}
				result.Session = event.Session
				collector.SetSession(event.Session)
			}
		}
		collector.Observe(line)
	}
	capture, err := Execute(runCtx, Prepared{Binary: r.options.Binary, Arguments: Arguments(r.options, request.Session), Environment: r.options.Environment, Directory: request.WorkingDirectory, Prompt: request.Prompt, BeforeLaunch: request.BeforeLaunch, Consume: func(reader io.Reader) error {
		lines := agent.NewLinesWithPolicy(func(line []byte) { stream.Record(append(line, '\n')) }, cancel, request.Output)
		_, readErr := io.Copy(lines, reader)
		return errors.Join(readErr, lines.Finish())
	}, Logger: r.options.Logger})
	result.Coverage = collector.Finish()
	result.Stderr = capture.Stderr
	result.Started, result.ExitCode, result.StartedAt, result.FinishedAt = capture.Started, capture.ExitCode, capture.StartedAt, capture.FinishedAt
	if stream.Result != nil && sessionErr == nil {
		result.Text = stream.Result.Result
		result.Terminal = true
		result.IsError = stream.Result.IsError
	}
	if sessionErr != nil {
		return result, &agent.Failure{Phase: "session", Started: capture.Started, Cause: sessionErr}
	}
	if err != nil {
		if runCtx.Err() != nil && !errors.Is(err, context.Cause(runCtx)) {
			err = errors.Join(err, cancellationCause(runCtx))
		}
		return result, err
	}
	if runCtx.Err() != nil {
		return result, &agent.Failure{Phase: "process_wait", Started: capture.Started, Cause: cancellationCause(runCtx)}
	}
	if capture.WaitError != nil {
		return result, &agent.Failure{Phase: "process_wait", Started: true, Cause: capture.WaitError}
	}
	if stream.Err() != nil {
		return result, &agent.Failure{Phase: "output", Started: true, Cause: stream.Err()}
	}
	if capture.StreamError != nil {
		return result, &agent.Failure{Phase: "output", Started: true, Cause: capture.StreamError}
	}
	if !result.Terminal {
		return result, &agent.Failure{Phase: "output", Started: true, Cause: errors.New("claude returned no terminal result")}
	}
	return result, nil
}

// Prepared is native process input after the caller has selected its policy.
type Prepared struct {
	Binary                 string
	Arguments, Environment []string
	Directory, Prompt      string
	BeforeLaunch           func(context.Context) error
	Consume                func(io.Reader) error
	Logger                 *slog.Logger
	OnStarted              func(*exec.Cmd)
}

// Capture contains process facts before factory interpretation.
type Capture struct {
	Started                bool
	ExitCode               int
	WaitError, StreamError error
	Stderr                 string
	StartedAt, FinishedAt  time.Time
}

// Command delegates process creation for provider capability probes and caller
// protocols whose output handling differs from ordinary stream-json execution.
func Command(ctx context.Context, binary string, args ...string) *exec.Cmd {
	return agent.Command(ctx, binary, args...)
}

// Execute preserves the existing pipe draining and bounded descendant cleanup.
func Execute(ctx context.Context, input Prepared) (capture Capture, failure error) {
	if input.Logger == nil {
		input.Logger = slog.New(slog.DiscardHandler)
	}
	capture.StartedAt = time.Now()
	var cmd *exec.Cmd
	var hookError error
	var stdoutR, stdoutW, stderrR, stderrW *os.File
	closeAll := func(files ...*os.File) {
		for _, file := range files {
			if file != nil {
				_ = file.Close()
			}
		}
	}
	err := agent.Start(ctx, input.Binary, input.Directory, func() error {
		cmd = Command(ctx, input.Binary, input.Arguments...)
		cmd.Dir, cmd.Env, cmd.Stdin = input.Directory, append([]string{}, input.Environment...), strings.NewReader(input.Prompt)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.WaitDelay = 10 * time.Second
		cmd.Cancel = func() error {
			if cmd.Process == nil {
				return os.ErrProcessDone
			}
			return agent.KillSubtree(context.WithoutCancel(ctx), cmd.Process.Pid, input.Logger)
		}
		var err error
		stdoutR, stdoutW, err = os.Pipe()
		if err != nil {
			return fmt.Errorf("stdout pipe: %w", err)
		}
		stderrR, stderrW, err = os.Pipe()
		if err != nil {
			closeAll(stdoutR, stdoutW)
			return fmt.Errorf("stderr pipe: %w", err)
		}
		cmd.Stdout, cmd.Stderr = stdoutW, stderrW
		if input.BeforeLaunch != nil {
			if err := input.BeforeLaunch(ctx); err != nil {
				closeAll(stdoutR, stdoutW, stderrR, stderrW)
				hookError = err
				return errors.New("caller launch authorization refused")
			}
		}
		if err := cmd.Start(); err != nil {
			closeAll(stdoutR, stdoutW, stderrR, stderrW)
			return err
		}
		return nil
	})
	if err != nil {
		if hookError != nil {
			return capture, &agent.Failure{Phase: "before_launch", Cause: hookError}
		}
		return capture, &agent.Failure{Phase: "launch", Cause: err}
	}
	capture.Started = true
	closeAll(stdoutW, stderrW)
	defer closeAll(stdoutR, stderrR)
	if input.OnStarted != nil {
		input.OnStarted(cmd)
	}
	// Both drain goroutines are joined below; closing our pipe ends after the
	// grace period prevents surviving descendants from retaining them forever.
	stdoutDone, stderrDone := make(chan error, 1), make(chan error, 1)
	var stdoutError, stderrError error
	tail := &tailWriter{limit: 64 << 10}
	go func() {
		if input.Consume == nil {
			_, err := io.Copy(io.Discard, stdoutR)
			stdoutDone <- err
		} else {
			stdoutDone <- input.Consume(stdoutR)
		}
	}()
	go func() { _, err := io.Copy(tail, stderrR); stderrDone <- err }()
	capture.WaitError = cmd.Wait()
	capture.FinishedAt = time.Now()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	grace := timer.C
	for remaining := 2; remaining > 0; {
		select {
		case err := <-stdoutDone:
			stdoutError = err
			remaining--
		case err := <-stderrDone:
			stderrError = err
			remaining--
		case <-grace:
			grace = nil
			input.Logger.Warn("claude output pipes still open after the process exited; closing them", slog.String("grace", (10*time.Second).String()))
			closeAll(stdoutR, stderrR)
		}
	}
	capture.StreamError = stdoutError
	if capture.StreamError == nil {
		capture.StreamError = stderrError
	}
	capture.ExitCode = -1
	if cmd.ProcessState != nil {
		capture.ExitCode = cmd.ProcessState.ExitCode()
	}
	capture.Stderr = tail.String()
	return capture, nil
}

type tailWriter struct {
	mu        sync.Mutex
	limit     int
	buf       []byte
	truncated bool
}

// Write implements io.Writer, accepting all bytes even when earlier bytes drop.
func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	if n == 0 {
		return 0, nil
	}
	if n >= w.limit {
		w.truncated = w.truncated || len(w.buf)+n > w.limit
		w.buf = append(w.buf[:0], p[n-w.limit:]...)
		return n, nil
	}
	if excess := len(w.buf) + n - w.limit; excess > 0 {
		copy(w.buf, w.buf[excess:])
		w.buf = w.buf[:len(w.buf)-excess]
		w.truncated = true
	}
	w.buf = append(w.buf, p...)
	return n, nil
}

// String marks a truncated stream so its tail is never mistaken for full output.
func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.truncated {
		return "[earlier stderr dropped]\n" + string(w.buf)
	}
	return string(w.buf)
}

// Probe performs a bounded native capability query without a model invocation.
func Probe(ctx context.Context, binary string, args, environment []string, directory string) (string, error) {
	return agent.Probe(ctx, binary, args, environment, directory)
}

// RunBounded uses the existing scoped JSON-line policy. Judge output may discard
// oversized records; other calls cancel when the line or total limit is reached.
func RunBounded(ctx context.Context, binary string, args, environment []string, prompt, directory string, timeout time.Duration, observe func([]byte), failure agent.FailureObserver, cancelProcess func(int) error, judge bool, oversized agent.OversizedObserver) error {
	return agent.RunProcess(ctx, binary, args, environment, prompt, directory, timeout, observe, failure, cancelProcess, judge, oversized)
}

// ConversationOptions contains only provider-native session input.
type ConversationOptions struct{ Model, Effort, Instructions, Session, Resume, MCPConfig string }

// ConversationArguments builds the isolated native session argv.
func ConversationArguments(options ConversationOptions) []string {
	args := []string{"-p", "--verbose", "--output-format", "stream-json", "--restricted", "--disable-slash-commands", "--no-chrome", "--setting-sources", "", "--tools", "", "--permission-mode", "dontAsk", "--strict-mcp-config", "--mcp-config", options.MCPConfig, "--agents", "{}", "--settings", `{"disableAllHooks":true}`, "--system-prompt", options.Instructions, "--model", options.Model}
	if options.Effort != "" {
		args = append(args, "--effort", options.Effort)
	}
	if options.Resume != "" {
		return append(args, "--resume", options.Resume)
	}
	return append(args, "--session-id", options.Session)
}

// ChatArguments builds response-only native argv.
func ChatArguments(model, mcpPath string, permitted []string) []string {
	args := []string{"-p", "--verbose", "--output-format", "stream-json", "--restricted", "--disable-slash-commands", "--no-session-persistence", "--no-chrome", "--setting-sources", "", "--tools", "", "--permission-mode", "dontAsk", "--strict-mcp-config", "--mcp-config", mcpPath, "--model", model}
	if len(permitted) > 0 {
		args = append(args, "--allowedTools", strings.Join(permitted, ","))
	}
	return args
}

// Buffered executes a prepared stateless call with caller-selected output
// buffers. This preserves distinct auxiliary output policies.
func Buffered(ctx context.Context, input Prepared, stdout, stderr io.Writer) error {
	if input.Logger == nil {
		input.Logger = slog.New(slog.DiscardHandler)
	}
	cmd := Command(ctx, input.Binary, input.Arguments...)
	cmd.Dir, cmd.Env, cmd.Stdin = input.Directory, append([]string{}, input.Environment...), strings.NewReader(input.Prompt)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 10 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return agent.KillSubtree(context.WithoutCancel(ctx), cmd.Process.Pid, input.Logger)
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if input.BeforeLaunch != nil {
		if err := input.BeforeLaunch(ctx); err != nil {
			return err
		}
	}
	return cmd.Run()
}

// StatelessArguments builds tool-free advisory argv.
func StatelessArguments(settings, model string) []string {
	return []string{"-p", "--output-format", "stream-json", "--verbose", "--tools", "", "--settings", settings, "--model", model, "--strict-mcp-config"}
}

// ScopedArguments appends the native bounded read-only invocation flags after
// caller-selected sandbox and resource controls.
func ScopedArguments(prefix []string, tools, mcpPath, model string, allowed []string) []string {
	return append(prefix, "-p", "--verbose", "--output-format", "stream-json", "--restricted", "--disable-slash-commands", "--no-chrome", "--setting-sources", "", "--tools", tools, "--permission-mode", "dontAsk", "--strict-mcp-config", "--mcp-config", mcpPath, "--allowedTools", strings.Join(allowed, ","), "--model", model)
}

// RestrictedOptions supplies native isolation settings for a prepared adviser.
type RestrictedOptions struct {
	Model, Effort, MCPConfig, Instructions string
	AllowedTools                           []string
	NoSessionPersistence                   bool
}

// RestrictedArguments builds the native isolated adviser argv. Instructions are
// prepared by the caller; the provider assigns no application role or policy.
func RestrictedArguments(options RestrictedOptions) []string {
	args := []string{"-p", "--verbose", "--output-format", "stream-json", "--restricted", "--disable-slash-commands", "--no-chrome"}
	if options.NoSessionPersistence {
		args = append(args, "--no-session-persistence")
	}
	args = append(args, "--setting-sources", "", "--tools", "", "--permission-mode", "dontAsk", "--strict-mcp-config", "--mcp-config", options.MCPConfig)
	if options.AllowedTools != nil {
		args = append(args, "--allowedTools", strings.Join(options.AllowedTools, ","))
	}
	args = append(args, "--agents", "{}", "--settings", `{"disableAllHooks":true}`, "--system-prompt", options.Instructions, "--model", options.Model)
	if options.Effort != "" {
		args = append(args, "--effort", options.Effort)
	}
	return args
}

// ProbeThinkingDisplay inspects native flag support without invoking a model.
func ProbeThinkingDisplay(ctx context.Context, binary string, env []string) (bool, string) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	//nolint:gosec // Resolved provider executable and constant probe arguments.
	cmd := Command(probeCtx, binary, "--thinking-display", "litefactory-probe", "--version")
	cmd.Env, cmd.Dir = env, os.TempDir()
	cmd.WaitDelay = time.Second
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	text := output.String()
	if err != nil && strings.Contains(text, "--thinking-display") && strings.Contains(text, "summarized") {
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

// cancellationCause keeps a caller's cancellation reason alongside the standard
// context category, so errors.Is works for both without duplicating deadlines.
func cancellationCause(ctx context.Context) error {
	err := ctx.Err()
	cause := context.Cause(ctx)
	if cause != nil && !errors.Is(err, cause) {
		return errors.Join(err, cause)
	}
	return err
}
