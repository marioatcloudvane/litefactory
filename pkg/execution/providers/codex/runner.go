// Package codex executes prepared Codex CLI invocations without factory state.
package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/pkg/execution/agent"
)

// Sandbox is an explicit native sandbox permission selection.
type Sandbox struct{ Mode string }

// Options holds native Codex configuration. A nil Permission is omitted and
// leaves native sandbox selection to Codex; an explicit empty mode is refused.
type Options struct {
	Binary                          string
	Environment                     []string
	Model, Effort, ReasoningSummary string
	Permission                      *Sandbox
	WritableRoots                   []string
	NetworkAccess                   bool
	AllowUserConfig                 bool
	ExtraArgs                       []string
	Logger                          *slog.Logger
}

// Runner executes prepared inputs using the native JSONL protocol.
type Runner struct{ options Options }

// New validates configuration without launching Codex or reading credentials.
func New(options Options) (*Runner, error) {
	if !filepath.IsAbs(options.Binary) {
		return nil, errors.New("codex binary must be an absolute path")
	}
	info, err := os.Stat(options.Binary)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return nil, errors.New("codex binary must be executable")
	}
	if options.Permission != nil {
		if options.Permission.Mode != "read-only" && options.Permission.Mode != "workspace-write" {
			return nil, fmt.Errorf("unsupported Codex sandbox %q", options.Permission.Mode)
		}
		permission := *options.Permission
		options.Permission = &permission
	}
	for _, root := range options.WritableRoots {
		if !filepath.IsAbs(root) {
			return nil, fmt.Errorf("codex writable root %q must be absolute", root)
		}
	}
	options.Environment = append([]string{}, options.Environment...)
	options.WritableRoots = append([]string(nil), options.WritableRoots...)
	options.ExtraArgs = append([]string(nil), options.ExtraArgs...)
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}
	return &Runner{options: options}, nil
}

// Arguments builds native exec arguments, keeping explicit resume identity.
func Arguments(options Options, session string) []string {
	args := []string{"exec", "--json"}
	if !options.AllowUserConfig {
		args = append(args, "--ignore-user-config")
	}
	args = append(args, "--ignore-rules")
	if options.Permission != nil {
		args = append(args, "--sandbox", options.Permission.Mode)
	}
	args = append(args, options.ExtraArgs...)
	for _, root := range options.WritableRoots {
		args = append(args, "--add-dir", root)
	}
	if options.NetworkAccess {
		args = append(args, "-c", "sandbox_workspace_write.network_access=true")
	}
	if options.Model != "" {
		args = append(args, "--model", options.Model)
	}
	if options.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(options.Effort))
	}
	if options.ReasoningSummary != "" {
		args = append(args, "-c", "model_reasoning_summary="+strconv.Quote(options.ReasoningSummary))
	}
	return SessionArguments(args, session)
}

// SessionArguments appends the native resume subcommand and stdin sentinel.
func SessionArguments(args []string, session string) []string {
	if session != "" {
		return append(args, "resume", session, "-")
	}
	return append(args, "-")
}

// Environment preserves the existing ordinary Codex credential boundary.
func Environment(parent []string) []string {
	allowed := []string{"PATH", "HOME", "USER", "SHELL", "LANG", "LC_ALL", "TERM", "TMPDIR"}
	out := []string{}
	for _, name := range allowed {
		for _, entry := range parent {
			if k, v, ok := strings.Cut(entry, "="); ok && k == name {
				if v != "" {
					out = append(out, name+"="+v)
				}
				break
			}
		}
	}
	return out
}

// Command creates a native process for auxiliary protocols with caller-owned I/O.
func Command(ctx context.Context, binary string, args ...string) *exec.Cmd {
	return agent.Command(ctx, binary, args...)
}

// Prepared carries an already selected native launch and its output observer.
type Prepared struct {
	Binary                 string
	Arguments, Environment []string
	Directory, Prompt      string
	BeforeLaunch           func(context.Context) error
	Consume                func(io.Reader)
	OnStarted              func()
	Logger                 *slog.Logger
}

// Capture reports native lifecycle facts without factory outcome policy.
type Capture struct {
	Started               bool
	ExitCode              int
	WaitError             error
	Stderr                string
	StartedAt, FinishedAt time.Time
}

// Execute preserves ordinary Codex process start retries and descendant cleanup.
func Execute(ctx context.Context, input Prepared) (capture Capture, failure error) {
	if input.Logger == nil {
		input.Logger = slog.New(slog.DiscardHandler)
	}
	killCtx := context.WithoutCancel(ctx)
	var cmd *exec.Cmd
	var stdout io.ReadCloser
	stderr := newTailWriter(64 << 10)
	capture.StartedAt = time.Now()
	err := agent.Start(ctx, input.Binary, input.Directory, func() error {
		cmd = Command(ctx, input.Binary, input.Arguments...)
		cmd.Dir, cmd.Env, cmd.Stdin = input.Directory, append([]string{}, input.Environment...), strings.NewReader(input.Prompt)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			if cmd.Process == nil {
				return os.ErrProcessDone
			}
			return agent.KillSubtree(killCtx, cmd.Process.Pid, input.Logger)
		}
		var err error
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			return err
		}
		cmd.Stderr = stderr
		if input.BeforeLaunch != nil {
			if err := input.BeforeLaunch(ctx); err != nil {
				_ = stdout.Close()
				if pipe, ok := cmd.Stdout.(io.Closer); ok {
					_ = pipe.Close()
				}
				return err
			}
		}
		if err := cmd.Start(); err != nil {
			_ = stdout.Close()
			return fmt.Errorf("codex did not start: %w", err)
		}
		return nil
	})
	if err != nil {
		return capture, err
	}
	capture.Started = true
	if input.OnStarted != nil {
		input.OnStarted()
	}
	if input.Consume != nil {
		input.Consume(stdout)
	} else {
		_, _ = io.Copy(io.Discard, stdout)
	}
	capture.WaitError = cmd.Wait()
	capture.FinishedAt = time.Now()
	capture.ExitCode = -1
	if cmd.ProcessState != nil {
		capture.ExitCode = cmd.ProcessState.ExitCode()
	}
	capture.Stderr = stderr.String()
	return capture, nil
}

// Run invokes Codex using prepared text and returns native evidence.
func (r *Runner) Run(ctx context.Context, request agent.Request) (agent.Result, error) {
	var result agent.Result
	if !filepath.IsAbs(request.WorkingDirectory) || strings.TrimSpace(request.Prompt) == "" || request.Timeout <= 0 {
		return result, &agent.Failure{Phase: "validation", Cause: errors.New("absolute working directory, prepared prompt and positive timeout are required")}
	}
	runCtx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	collector := NewUsageCollector(runCtx, request.Usage)
	collector.PrepareRollouts(r.options.Environment)
	var stream Stream
	var sessionErr, outputErr error
	capture, err := Execute(runCtx, Prepared{Binary: r.options.Binary, Arguments: Arguments(r.options, request.Session), Environment: r.options.Environment, Directory: request.WorkingDirectory, Prompt: request.Prompt, Logger: r.options.Logger, BeforeLaunch: request.BeforeLaunch, Consume: func(src io.Reader) {
		output := agent.NewLinesWithPolicy(func(line []byte) {
			collector.Observe(line)
			stream.ConsumeLine(line)
			if request.Events != nil && outputErr == nil {
				if _, err := request.Events.Write(append(append([]byte(nil), line...), '\n')); err != nil {
					outputErr = err
					cancel()
				}
			}

			if json.Valid(line) {
				result.Events = append(result.Events, append(json.RawMessage(nil), line...))
			}
			var event struct {
				Type   string          `json:"type"`
				Thread string          `json:"thread_id"`
				Usage  json.RawMessage `json:"usage"`
			}
			if json.Unmarshal(line, &event) != nil {
				return
			}
			if len(event.Usage) > 0 && string(event.Usage) != "null" {
				result.Usage = append(result.Usage, append(json.RawMessage(nil), event.Usage...))
			}
			if event.Type == "thread.started" && event.Thread != "" && event.Thread != result.Session && sessionErr == nil {
				if request.OnSession != nil {
					sessionErr = request.OnSession(runCtx, event.Thread)
					if sessionErr != nil {
						cancel()
						return
					}
				}
				result.Session = event.Thread
			}
		}, cancel, request.Output)
		_, copyErr := io.Copy(output, src)
		if err := output.Finish(); err != nil {
			outputErr = err
		} else if copyErr != nil {
			outputErr = copyErr
		}
	}})
	result.Coverage = collector.Finish()
	result.Stderr = capture.Stderr
	result.Started, result.ExitCode, result.StartedAt, result.FinishedAt = capture.Started, capture.ExitCode, capture.StartedAt, capture.FinishedAt
	if sessionErr == nil {
		result.Text, result.Terminal, result.IsError = stream.Text, stream.Completed || stream.Failed, stream.Failed
	}
	if sessionErr != nil {
		return result, &agent.Failure{Phase: "session", Started: capture.Started, Cause: sessionErr}
	}
	if err != nil {
		if runCtx.Err() != nil && !errors.Is(err, context.Cause(runCtx)) {
			err = errors.Join(err, cancellationCause(runCtx))
		}
		return result, &agent.Failure{Phase: "launch", Started: capture.Started, Cause: err}
	}
	if outputErr != nil {
		return result, &agent.Failure{Phase: "output", Started: capture.Started, Cause: outputErr}
	}
	if runCtx.Err() != nil {
		return result, &agent.Failure{Phase: "process_wait", Started: capture.Started, Cause: cancellationCause(runCtx)}
	}
	if capture.WaitError != nil {
		return result, &agent.Failure{Phase: "process_wait", Started: true, Cause: capture.WaitError}
	}
	if stream.Err != nil {
		return result, &agent.Failure{Phase: "output", Started: true, Cause: stream.Err}
	}
	if stream.Failed && !stream.Completed {
		return result, &agent.Failure{Phase: "native", Started: true, Cause: fmt.Errorf("codex turn failed: %s", stream.FailureDetail)}
	}
	if !result.Terminal {
		return result, &agent.Failure{Phase: "output", Started: true, Cause: errors.New("codex returned no terminal result")}
	}
	return result, nil
}

type tailWriter struct {
	mu        sync.Mutex
	limit     int
	buf       []byte
	truncated bool
}

// newTailWriter constructs a tail with a byte limit. A nonpositive limit retains
// no bytes and still drains the producer.
func newTailWriter(limit int) *tailWriter {
	return &tailWriter{limit: max(0, limit)}
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

// Buffered executes stateless advisory calls with their existing process-group
// cancellation and caller-selected output buffers.
func Buffered(ctx context.Context, binary string, args, env []string, directory, prompt string, stdout, stderr io.Writer) error {
	cmd := Command(ctx, binary, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = directory, append([]string{}, env...), strings.NewReader(prompt)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return cmd.Run()
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
