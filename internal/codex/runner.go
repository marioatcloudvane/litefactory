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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/providerprocess"
)

const (
	logMode = os.FileMode(0o600)
	dirMode = os.FileMode(0o700)
)

// Options configures a Runner.
type Options struct {
	// Instructions supplies per-invocation role captures; nil uses the shared package.
	Instructions instructionpackage.CaptureSource
	Paths        config.Paths
	Codex        config.Codex
	Model        string
	Environ      []string
	Logger       *slog.Logger
}

// Runner executes the Codex CLI for work and classification requests.
type Runner struct {
	instructions instructionpackage.CaptureSource
	paths        config.Paths
	binary       config.Executable
	home         string
	model        string
	// writable are the operator-configured directories added to the
	// sandbox alongside the worktree, one --add-dir each. Copied at
	// construction so a later mutation of the config cannot change what an
	// in-flight run was launched with.
	writable []string
	// network reports that the operator lifted the sandbox's network ban.
	network bool
	// userConfig reports that the operator let Codex read its own config,
	// which is also what lets the repository's .codex/skills be seen.
	userConfig bool
	timeout    time.Duration
	environ    []string
	logger     *slog.Logger
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
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		return nil, fmt.Errorf("invalid codex options: a startup-resolved model is required")
	}
	for _, root := range opts.Codex.WritableRoots {
		if !filepath.IsAbs(root) {
			return nil, fmt.Errorf("invalid codex options: writable root %q must be absolute", root)
		}
	}
	return &Runner{instructions: opts.Instructions, paths: opts.Paths, binary: binary, home: opts.Paths.Home(),
		model: model, writable: append([]string(nil), opts.Codex.WritableRoots...),
		network: opts.Codex.NetworkAccess, userConfig: opts.Codex.AllowUserConfig,
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
	skills, err := r.skillOverrides(ctx, req.WorkingDirectory, nil)
	if err != nil {
		return "", err
	}
	args = append(args, "-c", skills)
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

// ProposeSchedule performs a stateless read-only advisory invocation. It does
// not allocate a normal run, create run artefacts, or attach to a worktree.
func (r *Runner) ProposeSchedule(ctx context.Context, req agent.ScheduleProposalRequest) (agent.ScheduleProposal, error) {
	capture, err := instructionpackage.CaptureRole(ctx, r.instructions, r.paths, r.logger, "schedule-proposal-advisor")
	if err != nil {
		return agent.ScheduleProposal{}, err
	}
	prompt, err := agent.ScheduleProposalPrompt(string(capture.Instructions()), req)
	if err != nil {
		return agent.ScheduleProposal{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	args := []string{"exec", "--json", "--ignore-user-config", "--ignore-rules", "--sandbox", "read-only"}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}
	args = append(args, "-")
	//nolint:gosec // binary/args are configuration/constants; request text is stdin.
	cmd := exec.CommandContext(runCtx, r.binary.Path, args...)
	// A proposal has no worktree. The resolved binary directory is guaranteed
	// to exist at construction and is not a repository the advisor can mutate.
	cmd.Dir, cmd.Env, cmd.Stdin = filepath.Dir(r.binary.Path), codexEnv(r.environ), strings.NewReader(prompt)
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
		return agent.ScheduleProposal{}, fmt.Errorf("schedule advisor failed: %w", err)
	}
	stream := consume(bytes.NewReader(stdout.Bytes()), io.Discard)
	if !stream.completed || stream.failed {
		return agent.ScheduleProposal{}, errors.New("schedule advisor returned no successful terminal result")
	}
	proposal, err := agent.ParseScheduleProposal(stream.text)
	if err != nil {
		return agent.ScheduleProposal{}, fmt.Errorf("schedule advisor returned invalid proposal: %w", err)
	}
	return proposal, nil
}

// ProposeConflictResolution performs a stateless read-only advisory
// invocation, mirroring [Runner.ProposeSchedule]'s shape exactly (US-11): no
// worktree and no run artefacts, so it cannot become an ordinary coding
// workflow. It is bounded by r.timeout — its own deadline, distinct from the
// turn's ambient context and from any git budget already in play for this
// turn (see internal/turn's attemptConflictSuggestion, which passes ctx
// through unbounded on purpose).
func (r *Runner) ProposeConflictResolution(ctx context.Context, req agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
	capture, err := instructionpackage.CaptureRole(ctx, r.instructions, r.paths, r.logger, "merge-conflict-advisor")
	if err != nil {
		return agent.ConflictResolution{}, err
	}
	prompt, err := agent.ConflictResolutionPrompt(string(capture.Instructions()), req)
	if err != nil {
		return agent.ConflictResolution{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	args := []string{"exec", "--json", "--ignore-user-config", "--ignore-rules", "--sandbox", "read-only"}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}
	args = append(args, "-")
	//nolint:gosec // binary/args are configuration/constants; request text is stdin.
	cmd := exec.CommandContext(runCtx, r.binary.Path, args...)
	// A proposal has no worktree. The resolved binary directory is guaranteed
	// to exist at construction and is not a repository the advisor can mutate.
	cmd.Dir, cmd.Env, cmd.Stdin = filepath.Dir(r.binary.Path), codexEnv(r.environ), strings.NewReader(prompt)
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
	stdout, stderr := agent.ConflictOutputBuffers()
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		return agent.ConflictResolution{}, fmt.Errorf("conflict advisor failed: %w", err)
	}
	if stdout.Exceeded || stderr.Exceeded {
		return agent.ConflictResolution{}, errors.New("conflict advisor output exceeds 1 MiB")
	}
	stream := consume(bytes.NewReader(stdout.Bytes()), io.Discard)
	if !stream.completed || stream.failed {
		return agent.ConflictResolution{}, errors.New("conflict advisor returned no successful terminal result")
	}
	resolution, err := agent.ParseRegionConflictResolution(stream.text, req)
	if err != nil {
		return agent.ConflictResolution{}, fmt.Errorf("conflict advisor returned invalid resolution: %w", err)
	}
	return resolution, nil
}

// Run executes one ordinary coding-agent request.
func (r *Runner) Run(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
	if err := agent.VerifyRequestDelivery(ctx, req); err != nil {
		return nil, err
	}
	if req.Invocation == agent.InvocationGraphNode && req.Delivery != nil {
		for _, name := range []string{"main-instructions.md", "roles/" + req.Delivery.MainRole + ".toml"} {
			if _, ok := req.Delivery.RenderedFiles[name]; !ok {
				return nil, agent.DeliveryRefusal(req, fmt.Errorf("missing Codex native resource: %s", name))
			}
		}
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(req.WorktreePath) {
		return nil, fmt.Errorf("%w: worktree path must be absolute", agent.ErrInvalidRequest)
	}
	if st, err := os.Stat(req.WorktreePath); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%w: worktree %s is not a directory", agent.ErrInvalidRequest, req.WorktreePath)
	}

	p, ok := req.Permissions.Payload.(Sandbox)
	if !ok || p.Mode == "" {
		return nil, fmt.Errorf("%w: codex permissions require a sandbox", agent.ErrInvalidRequest)
	}
	sandbox := p.Mode
	if req.Workflow == agent.WorkflowBug && req.Permissions.Profile == "bug-fixer" && sandbox != "workspace-write" {
		return nil, fmt.Errorf("%w: Bug fixer requires contained workspace-write sandbox", agent.ErrInvalidRequest)
	}
	// Only the Validate repair agent writes, through its own validate-repair
	// grant; every other Validate run stays read-only.
	validateRepair := req.Mode == "validate" && req.Workflow != agent.WorkflowBug && req.Permissions.Profile == config.ValidateRepairProfile
	if validateRepair && sandbox != "workspace-write" {
		return nil, fmt.Errorf("%w: Validate repair requires contained workspace-write sandbox", agent.ErrInvalidRequest)
	}
	if ((req.Mode == "validate" && !validateRepair) || (req.Workflow == agent.WorkflowBug && req.Permissions.Profile != "bug-fixer")) && sandbox != "read-only" {
		return nil, fmt.Errorf("%w: Validate requires read-only sandbox", agent.ErrInvalidRequest)
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

	args, err := r.args(req, sandbox)
	if err != nil {
		return nil, err
	}
	if req.Delivery != nil {
		selected := make([]string, 0, len(req.Delivery.Skills))
		for _, skill := range req.Delivery.Skills {
			selected = append(selected, filepath.Join(req.Delivery.Root, skill))
		}
		overrides, err := r.skillOverrides(ctx, req.WorktreePath, selected)
		if err != nil {
			return nil, err
		}
		// Overrides must precede the resume subcommand and its positional id.
		args = append([]string{args[0], "-c", overrides}, args[1:]...)
	}
	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	stackEnv, err := req.StackEnvironment(codexEnv(r.environ))
	if err != nil {
		return nil, err
	}
	var cmd *exec.Cmd
	var stdout io.ReadCloser
	stderr := agent.NewTailBuffer(agent.StderrTailBytes)
	startLocation := machineLocation()
	started := time.Now()
	collector := newUsageCollector(runCtx, req)
	collector.prepareRollouts(stackEnv)
	err = providerprocess.Start(runCtx, r.binary.Path, req.WorktreePath, func() error {
		//nolint:gosec // binary/args are configuration; issue text is delivered on stdin.
		cmd = exec.CommandContext(runCtx, r.binary.Path, args...)
		cmd.Dir, cmd.Env, cmd.Stdin = req.WorktreePath, stackEnv, strings.NewReader(req.Prompt)
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
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			return err
		}
		cmd.Stderr = stderr
		if err := agent.VerifyRequestDelivery(runCtx, req); err != nil {
			_ = stdout.Close()
			if pipe, ok := cmd.Stdout.(io.Closer); ok {
				_ = pipe.Close()
			}
			return err
		}
		if err := cmd.Start(); err != nil {
			_ = stdout.Close()
			return fmt.Errorf("codex did not start: %w", err)
		}
		return nil
	})
	if err != nil {
		collector.finish()
		return nil, err
	}
	r.logger.Info("coding agent started", "agent", "codex", "task", req.Ref.String(), "run", req.RunOrdinal,
		"binary", r.binary.Path, "sandbox", sandbox, "tool_profile", req.Permissions.Profile)
	stream := consumeUsage(stdout, logFile, collector)
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
		Duration: finished.Sub(started), Usage: collector.finish()}
	classifyOutcome(result, stream, ctx.Err(), runCtx.Err(), r.timeout)
	if ctx.Err() == nil && runCtx.Err() == nil && stream.failed && !stream.completed {
		result.Interruption = quotaInterruption(stream.quotaMessage, started, finished, startLocation, machineLocation())
		if result.Interruption != nil {
			result.Transient = nil
			result.Outcome, result.Reason, result.HasResult = agent.OutcomeInterrupted, "session quota limit reached", true
		}
	}
	if result.Transient != nil {
		result.Transient.Model = r.model
		if req.Delivery != nil && req.Delivery.MainRole != "" {
			result.Transient.Model = req.Delivery.MainModel
		}
	}
	r.logger.Info("coding agent finished", "agent", "codex", "outcome", result.Outcome.String(), "events", result.Events, "log_path", logPath)
	if result.Transient == nil && req.Path.Resuming() && result.Outcome == agent.OutcomeFailed && !stream.turnStarted && exitCode > 0 {
		return result, fmt.Errorf("%w: codex thread %s", agent.ErrSessionNotResumable, req.Session.Resume)
	}
	_ = waitErr // ProcessState and stream provide the stable classification.
	return result, nil
}

// classifyOutcome keeps process/context precedence separate from JSONL parsing so
// completion racing with cancellation can be checked without timing a subprocess.
func classifyOutcome(result *agent.RunResult, stream streamState, callerErr, runErr error, timeout time.Duration) {
	switch {
	case result.ExitCode == 0 && stream.completed && !stream.failed:
		result.Outcome = agent.OutcomeSucceeded
	case callerErr != nil:
		result.Outcome, result.Reason, result.ExitCode = agent.OutcomeAbandoned, "abandoned: caller cancelled the run", agent.SignalExitCode
	case errors.Is(runErr, context.DeadlineExceeded):
		result.Outcome, result.Reason, result.ExitCode = agent.OutcomeTimedOut, fmt.Sprintf("timed out after %s", timeout), agent.SignalExitCode
	case stream.failed:
		result.Outcome, result.Reason = agent.OutcomeFailed, "coding agent reported turn.failed"
		if stream.failureDetail != "" {
			result.Reason += ": " + stream.failureDetail
		}
	case result.ExitCode == 0:
		result.Outcome, result.Reason = agent.OutcomeNoResult, "coding agent exited 0 without a turn.completed event"
	default:
		result.Outcome, result.Reason = agent.OutcomeFailed, fmt.Sprintf("coding agent exited with status %d", result.ExitCode)
	}
	if result.Outcome == agent.OutcomeFailed && stream.failed && !stream.completed {
		result.Transient = agent.ProviderCapacity(stream.failureDetail)
		if result.Transient != nil {
			result.Reason = result.Transient.Cause
		}
	}
	// No stdout events is deliberately conservative: once the agent emits a
	// stream, quoted tool output cannot establish a startup failure. Neither
	// generated text nor an isolated permission warning is diagnostic authority.
	if result.Outcome == agent.OutcomeFailed && result.ExitCode > 0 && stream.events == 0 &&
		!stream.turnStarted && !stream.completed && !stream.failed && startupPermissionFailure(result.Stderr) {
		result.Diagnostic = "Codex could not initialize its in-process app-server. A sandbox restriction is possible; " +
			"nested Codex in a managed run has a known initialization limitation, but this error does not establish nesting. " +
			"Use externally captured fixtures with provenance under docs/agent-evidence-capture.md; " +
			"see docs/codex-initialization-troubleshooting.md."
	}
	if result.Outcome == agent.OutcomeFailed && result.ExitCode > 0 && stream.events == 0 &&
		!stream.turnStarted && !stream.completed && !stream.failed && startupMCPFailure(result.Stderr) {
		result.Diagnostic = "Codex could not initialize required MCP servers. Check the retained stderr in the run metadata for the server error; the missing shared final result is a consequence of startup failure."
	}
}

func startupMCPFailure(stderr string) bool {
	const direct = "Error: required MCP servers failed to initialize: "
	const thread = "Error: thread/start: thread/start failed: error creating thread: Fatal error: Failed to initialize session: required MCP servers failed to initialize: "
	for line := range strings.SplitSeq(stderr, "\n") {
		if (strings.HasPrefix(line, direct) && len(strings.TrimSpace(line)) > len(direct)) ||
			(strings.HasPrefix(line, thread) && len(strings.TrimSpace(line)) > len(thread)) {
			return true
		}
	}
	return false
}

func startupPermissionFailure(stderr string) bool {
	const signature = "Error: failed to initialize in-process app-server client: Operation not permitted (os error 1)"
	for line := range strings.SplitSeq(stderr, "\n") {
		if strings.TrimSuffix(line, "\r") == signature {
			return true
		}
	}
	return false
}

func (r *Runner) args(req agent.Request, sandbox string) ([]string, error) {
	args := []string{"exec", "--json"}
	// Dropping this is what lets the repository's .codex/skills reach the
	// model, at the cost of Codex also reading the operator's own
	// config.toml — one flag, both effects. See config.Codex.AllowUserConfig.
	ignoreUserConfig := !r.userConfig || req.Delivery != nil
	if ignoreUserConfig {
		args = append(args, "--ignore-user-config")
	}
	args = append(args, "--ignore-rules", "--sandbox", sandbox)
	if req.Mode == "validate" {
		// Validate keeps the shell, like the Bug analyst: the prompt withholds
		// retained snapshot bodies and lists their paths, and the shell is
		// Codex's only way to read them. The read-only sandbox enforced in Run
		// prevents writes, and the Validate contract forbids running anything
		// but file reads. Delegation stays disabled.
		args = append(args, "-c", "features.multi_agent=false")
	}
	// Additive-only: a launch with no MCP registration produces no overrides
	// here and this invocation is unchanged from before this encoding existed.
	mcpArgs, err := r.mcpOverrides(req.MCPLaunch, ignoreUserConfig)
	if err != nil {
		return nil, err
	}
	args = append(args, mcpArgs...)
	if req.Delivery != nil {
		args = append(args, "--strict-config")
		if req.Delivery.MainRole != "" {
			instructions, err := mainInstructionOverride(req.Delivery.Root, req.Delivery.Skills)
			if err != nil {
				return nil, agent.DeliveryRefusal(req, fmt.Errorf("%w: %w", agent.ErrInvalidRequest, err))
			}
			args = append(args, "-c", instructions)
			// An Implement story worker hands independent tasks and test runs
			// to subagents; every other main role works alone.
			if req.Mode != string(agent.ModeImplement) {
				args = append(args, "-c", "features.multi_agent=false")
			}
		} else {
			for _, role := range req.Delivery.Roles {
				args = append(args, "-c", "agents."+role+".description="+strconv.Quote("Selected mode specialist; consult the mode roster."), "-c", "agents."+role+".config_file="+strconv.Quote(filepath.Join(req.Delivery.Root, "roles", role+".toml")))
			}
		}
	}
	// Before --model and before the subcommand, so the resume form below
	// still ends with the prompt argument. A read-only run gets them too:
	// --add-dir names a directory, and it is the sandbox mode that decides
	// whether anything may be written to it.
	for _, root := range r.writable {
		args = append(args, "--add-dir", root)
	}
	// The broker directory is litefactory's, not the operator's, and a
	// workspace-write run cannot reach it without being told to: the helper
	// this run is instructed to invoke writes its correlation journal and
	// opens its socket there. Without this the run drafts its contribution and
	// then fails at the hand-off with "writing outside of the project", ten
	// minutes in and with nothing recorded.
	if req.SubmissionRoot != "" {
		args = append(args, "--add-dir", req.SubmissionRoot)
	}
	// -c rather than a flag of its own, because Codex exposes this only as a
	// config override. It is emitted regardless of sandbox mode: the key is
	// namespaced to workspace-write and is inert under read-only, so keeping
	// argv independent of the mode is one less interaction to reason about.
	if r.network {
		args = append(args, "-c", "sandbox_workspace_write.network_access=true")
	}
	model := r.model
	if req.Delivery != nil && req.Delivery.MainRole != "" {
		model = req.Delivery.MainModel
	}
	if req.Model != "" {
		model = req.Model
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	thinking := req.Thinking
	if thinking == "" && req.Delivery != nil {
		thinking = req.Delivery.MainThinking
	}
	if err := config.ValidateThinking(config.KindCodex, model, thinking); err != nil {
		return nil, err
	}
	if thinking != "" {
		args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(string(thinking)))
	}
	if req.Path.Resuming() {
		return append(args, "resume", req.Session.Resume, "-"), nil
	}
	return append(args, "-"), nil
}

// InstructionIdentity is part of the retained effective bundle identity.
func (r *Runner) InstructionIdentity() string { return r.model }

type streamState struct {
	threadID, text, failureDetail  string
	quotaMessage                   string
	events, malformed, longest     int
	turnStarted, completed, failed bool
	err                            error
}

func consume(src io.Reader, sink io.Writer) streamState { return consumeUsage(src, sink, nil) }

func consumeUsage(src io.Reader, sink io.Writer, collector *usageCollector) streamState {
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
			collector.observe(line)
			s.consumeLine(line)
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

// consumeLine recognizes explicit failure independently of optional provider
// detail. Other events retain their existing typed-decoding behavior.
func (s *streamState) consumeLine(line []byte) {
	var terminal struct {
		Type    string          `json:"type"`
		Message json.RawMessage `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(line), &terminal) == nil && terminal.Type == "turn.failed" {
		s.failed = true
		var detail struct {
			Message    string `json:"message"`
			Code       string `json:"code"`
			StatusCode int    `json:"status_code"`
		}
		if json.Unmarshal(terminal.Error, &detail) == nil && (strings.TrimSpace(detail.Message) != "" || detail.Code != "" || detail.StatusCode != 0) {
			s.failureDetail = detail.Message
			if detail.Code != "" {
				s.failureDetail += " " + detail.Code
			}
			if detail.StatusCode != 0 {
				s.failureDetail += fmt.Sprintf(" HTTP %d", detail.StatusCode)
			}
		} else {
			var message string
			if json.Unmarshal(terminal.Message, &message) == nil && strings.TrimSpace(message) != "" {
				s.failureDetail = message
			}
		}
		// A quota refusal emits this alongside a generic error notice carrying
		// the same text; taking it here keeps recoverable notices inert.
		s.quotaMessage = s.failureDetail
		return
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
			s.quotaMessage = ""
		case "error":
			// Generic notices can recover; the raw log retains diagnostics.
		}
	}
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
