// Package codex implements the vendor-neutral agent.Runner contract for the
// OpenAI Codex CLI.
package codex

import (
	"bufio"
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
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
)

const (
	logMode = os.FileMode(0o600)
	dirMode = os.FileMode(0o700)
)

// Options configures a Runner.
type Options struct {
	Paths   config.Paths
	Codex   config.Codex
	Tiers   config.Tiers
	Environ []string
	Logger  *slog.Logger
}

// Runner executes the Codex CLI for work and classification requests.
type Runner struct {
	binary  config.Executable
	home    string
	sandbox string
	model   string
	// writable are the operator-configured directories added to the
	// sandbox alongside the worktree, one --add-dir each. Copied at
	// construction so a later mutation of the config cannot change what an
	// in-flight run was launched with.
	writable []string
	timeout  time.Duration
	environ  []string
	logger   *slog.Logger
}

// Sandbox is the Codex-specific permission payload carried by an agent request.
type Sandbox struct{ Mode string }

// New resolves and validates a Codex runner.
func New(opts Options) (*Runner, error) {
	if opts.Logger == nil {
		return nil, errors.New("invalid codex options: a logger is required")
	}
	if !filepath.IsAbs(opts.Paths.Home()) {
		return nil, fmt.Errorf("invalid codex options: LF_HOME %q must be absolute", opts.Paths.Home())
	}
	if opts.Codex.TimeoutSeconds <= 0 {
		return nil, fmt.Errorf("invalid codex options: timeout must be positive")
	}
	pathValue := envValue(opts.Environ, "PATH")
	if pathValue == "" {
		pathValue = "/usr/bin:/bin:/usr/sbin:/sbin"
	}
	binary, err := opts.Codex.ResolveBinary(pathValue)
	if err != nil {
		return nil, fmt.Errorf("resolve codex binary: %w", err)
	}
	model := opts.Codex.Model
	if opts.Codex.ModelTier != "" {
		var ok bool
		model, ok = opts.Tiers.ResolveModel(config.KindCodex, opts.Codex.ModelTier)
		if !ok {
			return nil, fmt.Errorf("agents.codex.model_tier %q has no configured model", opts.Codex.ModelTier)
		}
	}
	for _, root := range opts.Codex.WritableRoots {
		if !filepath.IsAbs(root) {
			return nil, fmt.Errorf("invalid codex options: writable root %q must be absolute", root)
		}
	}
	return &Runner{binary: binary, home: opts.Paths.Home(), sandbox: opts.Codex.DefaultSandbox,
		model: model, writable: append([]string(nil), opts.Codex.WritableRoots...),
		timeout: time.Duration(opts.Codex.TimeoutSeconds) * time.Second,
		environ: append([]string(nil), opts.Environ...), logger: opts.Logger}, nil
}

// Binary returns the resolved Codex executable.
func (r *Runner) Binary() config.Executable { return r.binary }

// Timeout returns the wall-clock limit for an invocation.
func (r *Runner) Timeout() time.Duration { return r.timeout }

// Classify performs a stateless read-only Codex invocation with no writable
// roots and accepts only an exact terminal mode value.
func (r *Runner) Classify(ctx context.Context, req agent.Classification) (agent.Mode, error) {
	if !filepath.IsAbs(req.WorkingDirectory) || strings.TrimSpace(req.Prompt) == "" {
		return "", fmt.Errorf("%w: classifier needs an absolute directory and prompt", agent.ErrInvalidRequest)
	}
	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	args := []string{"exec", "--json", "--ignore-user-config", "--ignore-rules", "--sandbox", "read-only"}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}
	args = append(args, "-")
	//nolint:gosec // binary and args are configuration/constants; untrusted text is stdin.
	cmd := exec.CommandContext(runCtx, r.binary.Path, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = req.WorkingDirectory, codexEnv(r.environ), strings.NewReader(req.Prompt)
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
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("mode classifier failed: %w", err)
	}
	stream := consume(bytes.NewReader(stdout.Bytes()), io.Discard)
	mode, ok := agent.ParseMode(stream.text)
	if !ok || strings.TrimSpace(stream.text) != string(mode) || !stream.completed || stream.failed {
		return "", fmt.Errorf("mode classifier returned invalid value")
	}
	return mode, nil
}

// Run executes one ordinary coding-agent request.
func (r *Runner) Run(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(req.WorktreePath) {
		return nil, fmt.Errorf("%w: worktree path must be absolute", agent.ErrInvalidRequest)
	}
	if st, err := os.Stat(req.WorktreePath); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%w: worktree %s is not a directory", agent.ErrInvalidRequest, req.WorktreePath)
	}

	sandbox := r.sandbox
	if p, ok := req.Permissions.Payload.(Sandbox); ok && p.Mode != "" {
		sandbox = p.Mode
	}
	if sandbox != "read-only" && sandbox != "workspace-write" {
		return nil, fmt.Errorf("%w: unsupported codex sandbox %q", agent.ErrInvalidRequest, sandbox)
	}
	logPath, err := naming.RunLogPath(r.home, req.Ref, req.RunOrdinal)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), dirMode); err != nil {
		return nil, err
	}
	//nolint:gosec // logPath is derived from validated LF_HOME, task ref and run ordinal.
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, logMode)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()

	args := r.args(req, sandbox)
	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	//nolint:gosec // binary/args are configuration; issue text is delivered on stdin.
	cmd := exec.CommandContext(runCtx, r.binary.Path, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = req.WorktreePath, codexEnv(r.environ), strings.NewReader(req.Prompt)
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
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codex did not start: %w", err)
	}
	r.logger.Info("coding agent started", "agent", "codex", "task", req.Ref.String(), "run", req.RunOrdinal,
		"binary", r.binary.Path, "sandbox", sandbox, "tool_profile", req.Permissions.Profile)
	stream := consume(stdout, logFile)
	waitErr := cmd.Wait()
	finished := time.Now()
	exitCode := agent.SignalExitCode
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	result := &agent.RunResult{ExitCode: exitCode, SessionID: req.Session.Ref, ResumeToken: stream.threadID,
		LogPath: logPath, Command: append([]string{r.binary.Path}, args...), Model: r.model,
		ResultText: stream.text, IsError: stream.failed, HasResult: stream.completed,
		Events: stream.events, Malformed: stream.malformed, LongestLine: stream.longest,
		Stderr: stderr.String(), StreamError: stream.err, StartedAt: started, FinishedAt: finished,
		Duration: finished.Sub(started)}
	switch {
	case exitCode == 0 && stream.completed && !stream.failed:
		result.Outcome = agent.OutcomeSucceeded
	case ctx.Err() != nil:
		result.Outcome, result.Reason, result.ExitCode = agent.OutcomeAbandoned, "abandoned: caller cancelled the run", agent.SignalExitCode
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		result.Outcome, result.Reason, result.ExitCode = agent.OutcomeTimedOut, fmt.Sprintf("timed out after %s", r.timeout), agent.SignalExitCode
	case exitCode == 0 && !stream.completed:
		result.Outcome, result.Reason = agent.OutcomeNoResult, "coding agent exited 0 without a terminal result event"
	default:
		result.Outcome, result.Reason = agent.OutcomeFailed, fmt.Sprintf("coding agent exited with status %d", exitCode)
	}
	r.logger.Info("coding agent finished", "agent", "codex", "outcome", result.Outcome.String(), "events", result.Events, "log_path", logPath)
	if req.Path.Resuming() && result.Outcome == agent.OutcomeFailed && !stream.turnStarted && exitCode > 0 {
		return result, fmt.Errorf("%w: codex thread %s", agent.ErrSessionNotResumable, req.Session.Resume)
	}
	_ = waitErr // ProcessState and stream provide the stable classification.
	return result, nil
}

func (r *Runner) args(req agent.Request, sandbox string) []string {
	args := []string{"exec", "--json", "--ignore-user-config", "--ignore-rules", "--sandbox", sandbox}
	// Before --model and before the subcommand, so the resume form below
	// still ends with the prompt argument. A read-only run gets them too:
	// --add-dir names a directory, and it is the sandbox mode that decides
	// whether anything may be written to it.
	for _, root := range r.writable {
		args = append(args, "--add-dir", root)
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}
	if req.Path.Resuming() {
		return append(args, "resume", req.Session.Resume, "-")
	}
	return append(args, "-")
}

type streamState struct {
	threadID, text                 string
	events, malformed, longest     int
	turnStarted, completed, failed bool
	err                            error
}

func consume(src io.Reader, sink io.Writer) streamState {
	var s streamState
	reader := bufio.NewReader(src)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			s.events++
			if len(line) > s.longest {
				s.longest = len(line)
			}
			if _, werr := sink.Write(line); werr != nil && s.err == nil {
				s.err = werr
			}
			var e struct {
				Type     string `json:"type"`
				ThreadID string `json:"thread_id"`
				Message  string `json:"message"`
				Item     struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"item"`
			}
			if json.Unmarshal(bytes.TrimSpace(line), &e) != nil {
				s.malformed++
			} else {
				switch e.Type {
				case "thread.started":
					s.threadID = e.ThreadID
				case "turn.started":
					s.turnStarted = true
				case "item.completed":
					if e.Item.Type == "agent_message" {
						s.text = e.Item.Text
					}
				case "turn.completed":
					s.completed = true
				case "turn.failed", "error":
					s.failed = true
					if e.Message != "" {
						s.text = e.Message
					}
				}
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.err = err
			}
			break
		}
	}
	return s
}

func codexEnv(parent []string) []string {
	allowed := []string{"PATH", "HOME", "USER", "SHELL", "LANG", "LC_ALL", "TERM", "TMPDIR"}
	var out []string
	for _, name := range allowed {
		if value := envValue(parent, name); value != "" {
			out = append(out, name+"="+value)
		}
	}
	return out
}
func envValue(env []string, name string) string {
	for _, entry := range env {
		if k, v, ok := strings.Cut(entry, "="); ok && k == name {
			return v
		}
	}
	return ""
}
