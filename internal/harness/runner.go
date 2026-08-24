package harness

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
)

// waitDelay bounds how long cmd.Wait may block after the claude process itself
// has exited. claude hands its stdout and stderr to children — node, git, and
// whatever the Bash tool was asked for — and a survivor holding a pipe open
// would otherwise keep Wait blocked indefinitely.
const waitDelay = 10 * time.Second

// ioGrace bounds how long the two reader goroutines are given after the process
// has been reaped. Past it their pipes are closed from this side, which turns a
// grandchild that outlived the kill sweep into a logged warning instead of a
// permanently stuck turn.
const ioGrace = 10 * time.Second

// stderrTailBytes is how much of the child's standard error is retained. S-9
// quotes the last 40 lines in the failure comment; 64 KB holds far more than
// that and bounds what a run that panics in a loop can cost in memory.
const stderrTailBytes = 64 << 10

// runLogFileMode is 0600 and runArtefactDirMode is 0700, matching S-1: the run
// log is unredacted and may contain anything claude read.
const (
	runLogFileMode      fs.FileMode = 0o600
	runArtefactDirMode  fs.FileMode = 0o700
	reasonExcerptLength             = 200
)

// Options configures a [Runner].
//
// Everything the runner needs is passed in rather than read from the process:
// the environment in particular, because a test that could not choose the
// parent environment could not assert the boundary that AC-3.13 is about.
type Options struct {
	// Paths is the resolved $LF_HOME layout. Run artefacts are written beneath
	// it and the default claude settings file lives in it.
	Paths config.Paths

	// Claude is the validated claude configuration block. Validation (a
	// non-empty --tools surface, no bare Bash in allowed_tools, no
	// skip-permissions flag) has already happened in internal/config; this
	// package re-checks only what it would otherwise crash on.
	Claude config.Claude

	// Environ is the daemon's own environment in os.Environ form. It is the
	// input to the allowlist (SD-4), and its PATH is what claude.binary is
	// resolved against — never exec.LookPath, whose search path under launchd
	// is /usr/bin:/bin:/usr/sbin:/sbin and does not contain Homebrew (SD-7).
	Environ []string

	// Logger receives the run's structured log lines.
	Logger *slog.Logger
}

// Runner executes claude. One is built at daemon startup and reused for every
// run, so a missing or non-executable binary is a loud failure at boot rather
// than three hours into a turn (AC-3.18).
//
// It is a concrete struct with no interface in front of it, per the M0 layout
// invariants: internal/turn declares whatever narrow interface it needs at its
// own point of consumption.
type Runner struct {
	binary       config.Executable
	home         string
	tools        []string
	allowedTools []string
	settingsPath string
	timeout      time.Duration
	forwardEnv   bool
	environ      []string
	logger       *slog.Logger
}

// New resolves the claude binary and returns a ready [Runner].
func New(opts Options) (*Runner, error) {
	if opts.Logger == nil {
		return nil, invalidOptions("a logger is required")
	}
	home := opts.Paths.Home()
	if home == "" || !filepath.IsAbs(home) {
		return nil, invalidOptions("$LF_HOME %q must be an absolute path", home)
	}
	if len(opts.Claude.Tools) == 0 {
		// --tools is the actual tool boundary (SD-2). Omitting the flag means
		// allow-all, so an empty surface is not "no restriction configured", it
		// is "no restriction at all".
		return nil, invalidOptions("claude.tools must not be empty; it is the tool boundary")
	}
	if opts.Claude.TimeoutSeconds <= 0 {
		return nil, invalidOptions("claude.timeout_seconds must be positive, got %d", opts.Claude.TimeoutSeconds)
	}

	settingsPath := opts.Claude.SettingsPath
	if settingsPath == "" {
		settingsPath = opts.Paths.SettingsPath()
	}
	if !filepath.IsAbs(settingsPath) {
		return nil, invalidOptions("claude.settings_path %q must be an absolute path", settingsPath)
	}

	environ := append([]string(nil), opts.Environ...)
	pathEnv := envMap(environ)["PATH"]
	if pathEnv == "" {
		pathEnv = fallbackPATH
	}
	binary, err := opts.Claude.ResolveBinary(pathEnv)
	if err != nil {
		return nil, fmt.Errorf("resolve claude binary: %w", err)
	}

	return &Runner{
		binary:       binary,
		home:         home,
		tools:        append([]string(nil), opts.Claude.Tools...),
		allowedTools: append([]string(nil), opts.Claude.AllowedTools...),
		settingsPath: settingsPath,
		timeout:      time.Duration(opts.Claude.TimeoutSeconds) * time.Second,
		forwardEnv:   opts.Claude.ForwardAnthropicEnv,
		environ:      environ,
		logger:       opts.Logger,
	}, nil
}

// Binary returns the resolved claude executable, so that startup can log which
// file it is about to run and whether it was found by searching PATH.
func (r *Runner) Binary() config.Executable { return r.binary }

// Timeout returns the hard wall-clock limit one run is given.
func (r *Runner) Timeout() time.Duration { return r.timeout }

// Request is one claude run: which task, which run number, where, and what to
// say. The prompt is assembled by [BuildPrompt].
type Request struct {
	// Ref identifies the task. It fixes the run log path and the deterministic
	// claude session id.
	Ref naming.Ref

	// RunOrdinal is the per-task run number, allocated by inserting the runs
	// row before the subprocess starts (S-1). It becomes the NNNN file stem.
	RunOrdinal int

	// SegmentOrdinal is the conversation segment, 1 at M0. Together with Ref it
	// derives the claude session id.
	SegmentOrdinal int

	// Mode is the run mode, "spike" at M0. Recorded in the metadata document.
	Mode string

	// WorktreePath is the run's working directory: the task's git worktree,
	// never the registered clone.
	WorktreePath string

	// Prompt is delivered on the child's stdin, never in argv (AC-3.12).
	Prompt string
}

// validate rejects a request that would produce an unusable run rather than a
// confusing one.
func (req Request) validate() error {
	if err := req.Ref.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	if req.RunOrdinal < 1 {
		return invalidRequest("run ordinal %d is below 1", req.RunOrdinal)
	}
	if req.SegmentOrdinal < 1 {
		return invalidRequest("segment ordinal %d is below 1", req.SegmentOrdinal)
	}
	if strings.TrimSpace(req.Mode) == "" {
		return invalidRequest("mode is empty")
	}
	if req.WorktreePath == "" || !filepath.IsAbs(req.WorktreePath) {
		return invalidRequest("worktree path %q must be an absolute path", req.WorktreePath)
	}
	info, err := os.Stat(req.WorktreePath)
	if err != nil {
		return fmt.Errorf("%w: worktree %s: %w", ErrInvalidRequest, req.WorktreePath, err)
	}
	if !info.IsDir() {
		return invalidRequest("worktree %s is not a directory", req.WorktreePath)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return invalidRequest("prompt is empty")
	}
	return nil
}

// Run executes claude for one request and returns what it produced.
//
// A run that failed is not an error: it comes back as a [*RunResult] whose
// Outcome says how it failed, because every run that actually happened has to
// be logged, have its metadata written and be reported to the issue (S-9). Only
// a failure to *start* a run — an invalid request, an unwritable run directory,
// a binary that will not execute — returns a non-nil error.
//
// The shape of the body is dictated by three measured failures:
//
//   - stdout is read with bufio.Reader.ReadBytes, never bufio.Scanner, which
//     aborts its loop on a line over its token limit and so loses the terminal
//     result event (SD-6).
//   - both pipes are drained concurrently with cmd.Wait, and the pipes are this
//     package's own os.Pipe rather than cmd.StdoutPipe, so that a grandchild
//     holding one open past ioGrace can be cut loose from this side instead of
//     wedging the turn.
//   - the wall-clock deadline is independent of any event-driven completion
//     detection, because a stale credential makes claude retry internally and
//     then hang emitting nothing at all (SD-3).
func (r *Runner) Run(ctx context.Context, req Request) (*RunResult, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}

	sessionID, err := naming.ClaudeSessionID(req.Ref, req.SegmentOrdinal)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	logPath, err := naming.RunLogPath(r.home, req.Ref, req.RunOrdinal)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), runArtefactDirMode); err != nil {
		return nil, fmt.Errorf("create run log directory: %w", err)
	}
	if written, err := EnsureSettings(r.settingsPath, r.home); err != nil {
		return nil, fmt.Errorf("prepare claude settings: %w", err)
	} else if written {
		r.logger.Info("wrote claude settings file", slog.String("path", r.settingsPath))
	}

	// O_APPEND rather than O_TRUNC: the ordinal is allocated once per run and
	// never reused, so this file should not exist — and if it somehow does, a
	// previous run's partial evidence is not ours to delete (S-9).
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, runLogFileMode)
	if err != nil {
		return nil, fmt.Errorf("open run log %s: %w", logPath, err)
	}
	defer func() {
		if err := logFile.Close(); err != nil {
			r.logger.Warn("closing run log failed",
				slog.String("path", logPath), slog.String("error", err.Error()))
		}
	}()

	args := r.args(sessionID)
	logger := r.logger.With(
		slog.String("task", req.Ref.String()),
		slog.Int("run", req.RunOrdinal),
		slog.String("claude_session_id", sessionID))

	result, err := r.exec(ctx, req, args, sessionID, logPath, logFile, logger)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// args builds the child's argv, prompt excluded.
//
// The flags are SD-7's verified invocation plus two hardening additions. Every
// one of them is load-bearing:
//
//   - -p is headless print mode; --output-format stream-json is the event
//     stream this package parses.
//   - --verbose is MANDATORY. Without it claude hard-errors and writes nothing
//     at all to stdout, which is indistinguishable from a hang.
//   - --session-id is naming's deterministic UUID v5, so the id on the segments
//     row and the id claude used cannot drift.
//   - --tools is the real tool boundary (SD-2).
//   - --settings points at the litefactory-controlled file, so a run does not
//     inherit the operator's personal permissions.
//   - --allowedTools is the narrower permission list. It is additive to the
//     operator's settings and therefore defence in depth, not the boundary, but
//     AC-3.14 requires it and a narrow list is worth having anyway.
//   - --strict-mcp-config makes claude ignore every MCP configuration except
//     the ones passed with --mcp-config. None are, so no MCP server is
//     reachable — including a .mcp.json committed on the branch being worked.
//
// claude's permission-skipping flag — the one config.forbiddenFlag names — is
// never constructed here or anywhere else in the tree. That is a grep-able
// review invariant enforced by a repository-wide test, which is also why the
// flag is not written out as a literal in this comment.
func (r *Runner) args(sessionID string) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--session-id", sessionID,
		"--tools", strings.Join(r.tools, ","),
		"--settings", r.settingsPath,
	}
	if len(r.allowedTools) > 0 {
		// Comma-joined into a single argument rather than passed variadically:
		// --allowedTools takes <tools...>, and a following flag would otherwise
		// be swallowed as another tool rule.
		args = append(args, "--allowedTools", strings.Join(r.allowedTools, ","))
	}
	return append(args, "--strict-mcp-config")
}

// exec starts the process, streams it, and classifies how it ended.
func (r *Runner) exec(
	ctx context.Context,
	req Request,
	args []string,
	sessionID, logPath string,
	sink io.Writer,
	logger *slog.Logger,
) (*RunResult, error) {
	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// killSubtree runs when runCtx is already done, so it cannot use runCtx for
	// its own pgrep calls — it would be cancelled before it enumerated
	// anything. WithoutCancel keeps the values and drops the cancellation; it is
	// not context.Background(), which the M0 invariants forbid.
	killCtx := context.WithoutCancel(ctx)

	cmd := exec.CommandContext(runCtx, r.binary.Path, args...)
	cmd.Dir = req.WorktreePath
	cmd.Env = buildEnv(r.environ, r.forwardEnv)
	// The prompt goes on stdin (AC-3.12): out of ps, and past the ~1 MB macOS
	// ARG_MAX ceiling that a large issue body would otherwise hit as E2BIG.
	cmd.Stdin = strings.NewReader(req.Prompt)
	// Setpgid, Cancel and WaitDelay are the outer safety net. They are not the
	// kill mechanism: claude's Bash tool setsids out of this process group, so
	// the mechanism is killSubtree's pgid sweep (SD-1).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = waitDelay
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return killSubtree(killCtx, cmd.Process.Pid, logger)
	}

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("%w: stdout pipe: %w", ErrStartFailed, err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		closeAll(stdoutR, stdoutW)
		return nil, fmt.Errorf("%w: stderr pipe: %w", ErrStartFailed, err)
	}
	// Assigning *os.File values means os/exec passes the descriptors straight
	// to the child and starts no copier goroutines of its own for them — and,
	// equally important, never closes the read ends behind this package's back.
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	startedAt := time.Now()
	if err := cmd.Start(); err != nil {
		closeAll(stdoutR, stdoutW, stderrR, stderrW)
		return nil, fmt.Errorf("%w: %s: %w", ErrStartFailed, r.binary.Path, err)
	}
	// The child holds its own copies now. Without this the read ends never see
	// EOF, because this process would still hold a writer open.
	closeAll(stdoutW, stderrW)
	defer closeAll(stdoutR, stderrR)

	logger.Info("claude started",
		slog.String("binary", r.binary.Path),
		slog.Int("pid", cmd.Process.Pid),
		slog.String("worktree", req.WorktreePath),
		slog.String("tools", strings.Join(r.tools, ",")),
		slog.String("settings", r.settingsPath),
		slog.Int("env_vars", len(cmd.Env)),
		slog.String("timeout", r.timeout.String()))

	// The package's entire goroutine budget: one stdout reader, one stderr
	// reader. Both are joined below before anything they wrote is read, and
	// neither can outlive this call — if a survivor holds a pipe open past
	// ioGrace the read ends are closed from here and both return at once.
	stream := &eventStream{sink: sink, logger: logger}
	stderrTail := &tailWriter{limit: stderrTailBytes}
	stdoutDone := make(chan error, 1)
	stderrDone := make(chan error, 1)
	go func() { stdoutDone <- stream.consume(stdoutR) }()
	go func() {
		_, err := io.Copy(stderrTail, stderrR)
		stderrDone <- err
	}()

	waitErr := cmd.Wait()
	finishedAt := time.Now()

	stdoutErr, stderrErr := r.join(stdoutDone, stderrDone, stdoutR, stderrR, logger)

	exitCode := SignalExitCode
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	abandoned := ctx.Err() != nil
	timedOut := !abandoned && errors.Is(runCtx.Err(), context.DeadlineExceeded)

	outcome, reason, recordedExit := classify(classification{
		abandoned: abandoned,
		timedOut:  timedOut,
		timeout:   r.timeout,
		waitErr:   waitErr,
		exitCode:  exitCode,
		result:    stream.result,
	})

	result := &RunResult{
		Outcome:     outcome,
		Reason:      reason,
		ExitCode:    recordedExit,
		SessionID:   sessionID,
		LogPath:     logPath,
		Command:     append([]string{r.binary.Path}, args...),
		Model:       stream.model,
		Tools:       stream.tools,
		Events:      stream.events,
		Malformed:   stream.malformed,
		LongestLine: stream.longest,
		Stderr:      stderrTail.String(),
		StreamError: firstError(stream.err(), stdoutErr, stderrErr),
		StartedAt:   startedAt,
		FinishedAt:  finishedAt,
		Duration:    finishedAt.Sub(startedAt),
	}
	if stream.sessionID != "" && stream.sessionID != sessionID {
		// The segments row records what we asked for. If claude answered with a
		// different id, the row and the conversation on disk have parted
		// company, and a future segment would resume the wrong one.
		logger.Warn("claude reported a different session id than it was given",
			slog.String("requested", sessionID),
			slog.String("reported", stream.sessionID))
	}

	if event := stream.result; event != nil {
		result.HasResult = true
		result.ResultText = event.Result
		result.CostUSD = event.TotalCostUSD
		result.IsError = event.IsError
		result.NumTurns = event.NumTurns
	}

	r.logCompletion(logger, result)
	return result, nil
}

// join waits for both readers, closing their pipes if they outstay ioGrace.
//
// The grace exists for exactly one situation: claude exited but a descendant it
// setsid'd away inherited the descriptor and is still holding it. cmd.Wait has
// already returned by then, so nothing else will ever close it.
func (r *Runner) join(
	stdoutDone, stderrDone <-chan error,
	stdoutR, stderrR *os.File,
	logger *slog.Logger,
) (stdoutErr, stderrErr error) {
	timer := time.NewTimer(ioGrace)
	defer timer.Stop()

	grace := timer.C
	for remaining := 2; remaining > 0; {
		select {
		case err := <-stdoutDone:
			stdoutErr = err
			remaining--
		case err := <-stderrDone:
			stderrErr = err
			remaining--
		case <-grace:
			// Fire once. Closing the read ends makes both goroutines return
			// with a "file already closed" error on their next read.
			grace = nil
			logger.Warn("claude output pipes still open after the process exited; closing them",
				slog.String("grace", ioGrace.String()))
			closeAll(stdoutR, stderrR)
		}
	}
	return stdoutErr, stderrErr
}

// logCompletion emits the one line that says what happened, at the level the
// outcome deserves.
func (r *Runner) logCompletion(logger *slog.Logger, result *RunResult) {
	attrs := []any{
		slog.String("outcome", result.Outcome.String()),
		slog.Int("exit_code", result.ExitCode),
		slog.String("duration", result.Duration.String()),
		slog.Int("events", result.Events),
		slog.Int("malformed_lines", result.Malformed),
		slog.Int("longest_line_bytes", result.LongestLine),
		slog.Bool("has_result", result.HasResult),
		slog.String("log_path", result.LogPath),
	}
	if result.HasResult {
		attrs = append(attrs, slog.Float64("cost_usd", result.CostUSD))
	}
	if result.StreamError != nil {
		attrs = append(attrs, slog.String("stream_error", result.StreamError.Error()))
	}
	if result.Succeeded() {
		logger.Info("claude run finished", attrs...)
		return
	}
	logger.Error("claude run failed", append(attrs, slog.String("reason", result.Reason))...)
}

// classification is the raw material for [classify], gathered in one place so
// that the decision below reads as a single table.
type classification struct {
	abandoned bool
	timedOut  bool
	timeout   time.Duration
	waitErr   error
	exitCode  int
	result    *resultEvent
}

// classify decides how a run ended (S-9, AC-3.20, SD-3).
//
// Two rules are worth stating out loud:
//
//   - The terminal event's .subtype is never consulted. It reads "success" even
//     on is_error: true runs, so branching on it reports failed runs as
//     successful. .is_error is the only trustworthy field.
//   - Success is decided first, on evidence rather than on absence of trouble:
//     the process exited zero *and* claude emitted a terminal result event that
//     did not report an error. A run that finished cleanly a moment before the
//     daemon was signalled is a success, not an abandonment.
func classify(c classification) (Outcome, string, int) {
	if c.exitCode == 0 && c.result != nil && !c.result.IsError {
		return OutcomeSucceeded, "", 0
	}

	switch {
	case c.abandoned:
		return OutcomeAbandoned,
			"abandoned: the caller's run context ended before claude finished (daemon shutdown or cancellation)",
			SignalExitCode
	case c.timedOut:
		return OutcomeTimedOut,
			fmt.Sprintf("timed out after %s; the process subtree was killed", c.timeout),
			SignalExitCode
	}

	if c.waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(c.waitErr, &exitErr) {
			if c.exitCode >= 0 {
				return OutcomeFailed, fmt.Sprintf("claude exited with status %d", c.exitCode), c.exitCode
			}
			return OutcomeFailed,
				fmt.Sprintf("claude was terminated by a signal: %s", exitErr.ProcessState.String()),
				SignalExitCode
		}
		return OutcomeFailed, fmt.Sprintf("claude could not be waited on: %v", c.waitErr), c.exitCode
	}

	if c.result == nil {
		// The SD-3 stall, or a run whose stdout was lost. Either way there is no
		// terminal event, so there is nothing to report as a result (S-9).
		return OutcomeNoResult,
			"claude exited 0 without emitting a terminal result event",
			c.exitCode
	}
	return OutcomeFailed,
		"claude reported is_error on its terminal result event: " + excerpt(c.result.Result),
		c.exitCode
}

// excerpt renders untrusted result text as a single short line, fit for a log
// attribute and a runs row. It is not redaction — that happens on the way to
// the forge (S-7) — it is length and newline control.
func excerpt(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " "))
	if s == "" {
		return "(no message)"
	}
	if len(s) <= reasonExcerptLength {
		return s
	}
	return truncateUTF8(s, reasonExcerptLength) + "…"
}

// firstError returns the first non-nil error, or nil. The stream write failure
// is checked before the read failures because it is the one that means the run
// log on disk is incomplete.
func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// closeAll closes every file, ignoring the errors. Every call site is either
// releasing a descriptor the child now owns or forcing a stuck reader to
// return; in neither case is there anything a caller could do with a failure.
func closeAll(files ...*os.File) {
	for _, f := range files {
		if f != nil {
			_ = f.Close()
		}
	}
}
