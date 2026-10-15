package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/providerprocess"
)

// Classify performs a stateless, tool-free routing invocation. The prompt is
// stdin-only and no session id is supplied, so it cannot become a work
// conversation that a later turn resumes.
func (r *Runner) Classify(ctx context.Context, req agent.Classification) (agent.Mode, error) {
	if !filepath.IsAbs(req.WorkingDirectory) || strings.TrimSpace(req.Prompt) == "" {
		return "", fmt.Errorf("%w: classifier needs an absolute directory and prompt", agent.ErrInvalidRequest)
	}
	if _, err := os.Stat(req.WorkingDirectory); err != nil {
		return "", fmt.Errorf("classifier directory: %w", err)
	}
	if _, err := EnsureSettings(r.settingsPath, r.home); err != nil {
		return "", fmt.Errorf("prepare claude settings: %w", err)
	}
	if _, err := EnsureClaudeConfigDir(r.claudeConfigDir, r.osHome); err != nil {
		return "", fmt.Errorf("prepare claude config dir: %w", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--tools", "", "--settings", r.settingsPath, "--model", r.model, "--strict-mcp-config"}
	args = append(args, "--setting-sources", "", "--disable-slash-commands", "--agents", "{}")
	//nolint:gosec // binary/args are configuration/constants; untrusted text is stdin.
	cmd := exec.CommandContext(runCtx, r.binary.Path, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = req.WorkingDirectory, buildEnv(r.environ, r.forwardEnv, r.claudeConfigDir), strings.NewReader(req.Prompt)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = waitDelay
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return killSubtree(context.WithoutCancel(ctx), cmd.Process.Pid, r.logger)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("mode classifier failed: %w: %s", err, excerpt(stderr.String()))
	}
	stream := eventStream{logger: r.logger}
	if err := stream.consume(bytes.NewReader(stdout.Bytes())); err != nil || stream.result == nil || stream.result.IsError {
		return "", fmt.Errorf("mode classifier returned no successful terminal result")
	}
	if len(stream.tools) != 0 {
		return "", fmt.Errorf("mode classifier exposed tools")
	}
	mode, ok := agent.ParseMode(stream.result.Result)
	if !ok || strings.TrimSpace(stream.result.Result) != string(mode) {
		return "", fmt.Errorf("mode classifier returned invalid value")
	}
	return mode, nil
}

// ProposeSchedule performs a stateless, tool-free advisory invocation. It has
// no session id, run ordinal, or worktree and therefore cannot become an
// ordinary coding workflow or create its run artefacts.
func (r *Runner) ProposeSchedule(ctx context.Context, req agent.ScheduleProposalRequest) (agent.ScheduleProposal, error) {
	prompt, err := agent.ScheduleProposalPrompt(req)
	if err != nil {
		return agent.ScheduleProposal{}, err
	}
	if _, err := os.Stat(r.home); err != nil {
		return agent.ScheduleProposal{}, fmt.Errorf("schedule advisor directory: %w", err)
	}
	if _, err := EnsureSettings(r.settingsPath, r.home); err != nil {
		return agent.ScheduleProposal{}, fmt.Errorf("prepare claude settings: %w", err)
	}
	if _, err := EnsureClaudeConfigDir(r.claudeConfigDir, r.osHome); err != nil {
		return agent.ScheduleProposal{}, fmt.Errorf("prepare claude config dir: %w", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--tools", "", "--settings", r.settingsPath, "--model", r.model, "--strict-mcp-config"}
	//nolint:gosec // binary/args are configuration/constants; request text is stdin.
	cmd := exec.CommandContext(runCtx, r.binary.Path, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = r.home, buildEnv(r.environ, r.forwardEnv, r.claudeConfigDir), strings.NewReader(prompt)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = waitDelay
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return killSubtree(context.WithoutCancel(ctx), cmd.Process.Pid, r.logger)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return agent.ScheduleProposal{}, fmt.Errorf("schedule advisor failed: %w: %s", err, excerpt(stderr.String()))
	}
	stream := eventStream{logger: r.logger}
	if err := stream.consume(bytes.NewReader(stdout.Bytes())); err != nil || stream.result == nil || stream.result.IsError {
		return agent.ScheduleProposal{}, errors.New("schedule advisor returned no successful terminal result")
	}
	if len(stream.tools) != 0 {
		return agent.ScheduleProposal{}, errors.New("schedule advisor exposed tools")
	}
	proposal, err := agent.ParseScheduleProposal(stream.result.Result)
	if err != nil {
		return agent.ScheduleProposal{}, fmt.Errorf("schedule advisor returned invalid proposal: %w", err)
	}
	return proposal, nil
}

// ProposeConflictResolution performs a stateless, tool-free advisory
// invocation, mirroring [Runner.ProposeSchedule]'s shape exactly (US-11): no
// session id, run ordinal, or worktree, so it cannot become an ordinary
// coding workflow or create run artefacts. It is bounded by r.timeout — its
// own deadline, distinct from the turn's ambient context and from any git
// budget already in play for this turn (see internal/turn's
// attemptConflictSuggestion, which passes ctx through unbounded on purpose).
func (r *Runner) ProposeConflictResolution(ctx context.Context, req agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
	prompt, err := agent.ConflictResolutionPrompt(req)
	if err != nil {
		return agent.ConflictResolution{}, err
	}
	if _, err := os.Stat(r.home); err != nil {
		return agent.ConflictResolution{}, fmt.Errorf("conflict advisor directory: %w", err)
	}
	if _, err := EnsureSettings(r.settingsPath, r.home); err != nil {
		return agent.ConflictResolution{}, fmt.Errorf("prepare claude settings: %w", err)
	}
	if _, err := EnsureClaudeConfigDir(r.claudeConfigDir, r.osHome); err != nil {
		return agent.ConflictResolution{}, fmt.Errorf("prepare claude config dir: %w", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--tools", "", "--settings", r.settingsPath, "--model", r.model, "--strict-mcp-config"}
	//nolint:gosec // binary/args are configuration/constants; request text is stdin.
	cmd := exec.CommandContext(runCtx, r.binary.Path, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = r.home, buildEnv(r.environ, r.forwardEnv, r.claudeConfigDir), strings.NewReader(prompt)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = waitDelay
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return killSubtree(context.WithoutCancel(ctx), cmd.Process.Pid, r.logger)
	}
	stdout, stderr := agent.ConflictOutputBuffers()
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		return agent.ConflictResolution{}, fmt.Errorf("conflict advisor failed: %w: %s", err, excerpt(stderr.String()))
	}
	if stdout.Exceeded || stderr.Exceeded {
		return agent.ConflictResolution{}, errors.New("conflict advisor output exceeds 1 MiB")
	}
	stream := eventStream{logger: r.logger}
	if err := stream.consume(bytes.NewReader(stdout.Bytes())); err != nil || stream.result == nil || stream.result.IsError {
		return agent.ConflictResolution{}, errors.New("conflict advisor returned no successful terminal result")
	}
	if len(stream.tools) != 0 {
		return agent.ConflictResolution{}, errors.New("conflict advisor exposed tools")
	}
	resolution, err := agent.ParseRegionConflictResolution(stream.result.Result, req)
	if err != nil {
		return agent.ConflictResolution{}, fmt.Errorf("conflict advisor returned invalid resolution: %w", err)
	}
	return resolution, nil
}

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

// PermissionMode is the value of claude's --permission-mode on every run
// litefactory starts.
//
// It is a constant on purpose and must not become a config.yaml key. A knob
// there is a way to widen the permission boundary from configuration, which is
// the same reasoning that keeps config.forbiddenFlag's flag out of the tree.
// Changing this is a code change and is reviewed like one.
//
// "auto" has a classifier judge each action rather than failing everything the
// allowlist does not already name. Until now no --permission-mode was passed at
// all, so runs reported permissionMode "default": prompt when no rule covers
// it. A headless run has nobody to answer a prompt, so every uncovered action
// resolved to "Permission prompts are not available in this context" and became
// a hard denial — the asyncAgent reason behind 386 denials across 119 runs.
// Measured on this machine with an identical probe: ten denials under dontAsk,
// none under auto, and an rm the agent had been refused eight times went
// through.
//
// The settings document's deny rules remain the boundary. "bypassPermissions"
// is what discards them and is the moral equivalent of config.forbiddenFlag's
// flag; it must never be set here.
const PermissionMode = "auto"

// ReadOnlyShellPermissionMode is the --permission-mode of a run whose surface
// is a Validate reviewer's read-only shell ([ToolSurface.ReadOnlyShell]).
// dontAsk refuses every action --allowedTools does not pre-approve, which is
// what makes the fixed read-only Bash rules a boundary rather than a fast path.
const ReadOnlyShellPermissionMode = "dontAsk"

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

	// Model is the concrete model resolved once by startup configuration.
	Model string

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
	// ceiling is the hard upper bound on any tool surface a request may carry.
	// It bounds the profile-supplied lists only: r.tools is the operator's own
	// configured surface and config validation has already established the
	// ceiling contains it.
	ceiling      []string
	settingsPath string
	// claudeConfigDir is the CLAUDE_CONFIG_DIR the child runs under, so that it
	// reads litefactory's configuration and not the operator's ~/.claude.
	// See [EnsureClaudeConfigDir].
	claudeConfigDir string
	// osHome is the operator's OS home directory, kept only so that the
	// credential symlink under claudeConfigDir can be pointed at their file.
	// It is not $LF_HOME, which is Runner.home.
	osHome string
	// model is the --model the top-level session runs on. Sub-agents that name
	// their own model in their definition are unaffected by it.
	model       string
	autoCompact string
	timeout     time.Duration
	forwardEnv  bool
	environ     []string
	logger      *slog.Logger
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

	// The child's configuration directory is litefactory's, always under
	// $LF_HOME — it is not operator-configurable, because the whole point is
	// that it is not the operator's directory.
	claudeConfigDir := opts.Paths.ClaudeConfigDir()
	osHome := envMap(opts.Environ)["HOME"]

	// The --model value is resolved from tier when a tier is configured
	// (US-6): ModelTier takes precedence over the literal Model, because it
	// is the vendor-neutral source of truth once one exists. config.Validate
	// has already confirmed a configured ModelTier both names a real tier
	// and resolves against opts.Tiers, so a miss here means opts was
	// assembled by hand rather than loaded — the same class of caller the
	// settingsPath and Model defaulting below already accounts for.
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		return nil, invalidOptions("a startup-resolved model is required")
	}
	autoCompact := opts.Claude.AutoCompact
	if autoCompact == "" {
		autoCompact = config.DefaultClaudeAutoCompact
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

	// The ceiling is resolved once, in config.Load, which defaults it to
	// claude.tools before validation runs — so it arrives non-empty on any
	// configuration that came from there and is read exactly as given. Deriving
	// it a second time here is precisely how two definitions of the one boundary
	// that must not drift would drift apart.
	//
	// It can still be empty on a config.Claude assembled by hand rather than
	// loaded, which is what a test or an embedder does. Empty is not "no
	// ceiling" and must not be treated as one: a runner with an empty ceiling
	// rejects every request that carries a profile. It falls back to the tools
	// surface — the same narrowest default Load would have applied, never a
	// wider one — so an unloaded configuration is bounded by exactly what it
	// already grants every unprofiled run.
	ceiling := opts.Claude.ToolCeiling
	if len(ceiling) == 0 {
		ceiling = opts.Claude.Tools
	}

	return &Runner{
		binary:       binary,
		home:         home,
		tools:        append([]string(nil), opts.Claude.Tools...),
		allowedTools: append([]string(nil), opts.Claude.AllowedTools...),
		ceiling:      append([]string(nil), ceiling...),
		settingsPath: settingsPath,

		claudeConfigDir: claudeConfigDir,
		osHome:          osHome,

		model:       model,
		autoCompact: autoCompact,
		timeout:     time.Duration(opts.Claude.TimeoutSeconds) * time.Second,
		forwardEnv:  opts.Claude.ForwardAnthropicEnv,
		environ:     environ,
		logger:      opts.Logger,
	}, nil
}

// Binary returns the resolved claude executable, so that startup can log which
// file it is about to run and whether it was found by searching PATH.
func (r *Runner) Binary() config.Executable { return r.binary }

// Timeout returns the hard wall-clock limit one run is given.
func (r *Runner) Timeout() time.Duration { return r.timeout }

// validateRequest rejects a request that would produce an unusable run rather
// than a confusing one.
//
// It runs req's own generic checks first ([agent.Request.Validate]) and then
// this package's Claude-specific ones: a canonical session id, an absolute
// worktree directory that actually exists, and the tool-ceiling re-check
// (AC-2.6). The ceiling check is deliberately not a duplicate of the one
// config validation runs at boot: that one proves the operator's profile map
// is sound, this one proves that the []string which travelled
// daemon → turn → harness as a value — copied at the resolution boundary,
// carried on a struct, rewritten in place on the cold-rerun path — is still
// within the ceiling at the moment argv is about to be built from it. It is
// what keeps the #nosec justification on the exec below honest.
//
// Only Tools is checked against the ceiling. AllowedTools grants nothing that
// Tools does not already allow, so a rule outside the ceiling there cannot
// widen this run past it.
func (r *Runner) validateRequest(req agent.Request) error {
	if req.Workflow == agent.WorkflowBug {
		role := req.Permissions.Profile
		allowed := config.BugRoleClaudeTools(role)
		if allowed == nil {
			return invalidRequest("unknown Bug role %q", role)
		}
		ts := toolSurface(req)
		if len(ts.Tools) == 0 {
			return invalidRequest("%s requires explicit tools", role)
		}
		for _, tool := range append(append([]string{}, ts.Tools...), ts.AllowedTools...) {
			if !slices.Contains(allowed, config.ClaudeToolName(tool)) {
				return invalidRequest("%s cannot grant tool %q", role, tool)
			}
		}
	}

	if req.MCPLaunch != nil {
		if err := req.MCPLaunch.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidRequest, err)
		}
	}
	if req.Mode == "validate" && req.Workflow != agent.WorkflowBug && req.Permissions.Profile == config.ValidateRepairProfile {
		// The Validate repair agent is bounded by its own role surface; every
		// other Validate run keeps the read-only check below.
		ts := toolSurface(req)
		if len(ts.Tools) == 0 {
			return invalidRequest("%s requires explicit tools", config.ValidateRepairProfile)
		}
		allowed := config.BugRoleClaudeTools(config.ValidateRepairProfile)
		for _, tool := range append(append([]string{}, ts.Tools...), ts.AllowedTools...) {
			if !slices.Contains(allowed, config.ClaudeToolName(tool)) {
				return invalidRequest("%s cannot grant tool %q", config.ValidateRepairProfile, tool)
			}
		}
	} else if req.Mode == "validate" {
		ts := toolSurface(req)
		if len(ts.Tools) == 0 {
			return fmt.Errorf("%w: Validate requires explicit read-only tools", ErrInvalidRequest)
		}
		for _, tool := range ts.Tools {
			if tool == "Bash" && ts.ReadOnlyShell && !req.RetainedValidation {
				continue
			}
			if tool != "Read" && tool != "Glob" && tool != "Grep" && (tool != "Task" || req.RetainedValidation) {
				return fmt.Errorf("%w: Validate cannot grant %s", ErrInvalidRequest, tool)
			}
		}
		shellRules := config.ValidateReviewerShellRules()
		for _, rule := range ts.AllowedTools {
			if config.ClaudeToolName(rule) == "Bash" && (!ts.ReadOnlyShell || !slices.Contains(shellRules, rule)) {
				return fmt.Errorf("%w: Validate cannot grant %s", ErrInvalidRequest, rule)
			}
		}
	}
	if err := req.Validate(); err != nil {
		return err
	}
	if !naming.ValidClaudeSessionID(req.Session.Ref) {
		// claude rejects a malformed --session-id itself, but only after the
		// worktree exists and the process has started. Refusing it here costs
		// nothing, and keeps a corrupted segments row from being diagnosed as
		// a claude failure.
		return invalidRequest("claude session id %q is not a canonical UUID", req.Session.Ref)
	}
	if !filepath.IsAbs(req.WorktreePath) {
		return invalidRequest("worktree path %q must be an absolute path", req.WorktreePath)
	}
	info, err := os.Stat(req.WorktreePath)
	if err != nil {
		return fmt.Errorf("%w: worktree %s: %w", ErrInvalidRequest, req.WorktreePath, err)
	}
	if !info.IsDir() {
		return invalidRequest("worktree %s is not a directory", req.WorktreePath)
	}
	for _, tool := range toolSurface(req).Tools {
		if !slices.Contains(r.ceiling, tool) {
			return invalidRequest("tool %q is outside the configured ceiling [%s]",
				tool, strings.Join(r.ceiling, ", "))
		}
	}
	return nil
}

// Run executes claude for one request and returns what it produced.
//
// A run that failed is not an error: it comes back as a [*RunResult] whose
// Outcome says how it failed, because every run that actually happened has to
// be logged, have its metadata written and be reported to the issue (S-9). A
// failure to *start* a run — an invalid request, an unwritable run directory, a
// binary that will not execute — returns a non-nil error and no result.
//
// [ErrSessionNotResumable] is the single exception to both halves of that: it
// comes back *with* a result, because the process did run, and it comes back as
// an error because the caller's answer to it is not to report the run but to
// rerun it cold. Nothing else returns both.
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
func (r *Runner) Run(ctx context.Context, req agent.Request) (result *RunResult, runErr error) {
	req = retainedValidationRequest(req)
	if err := r.validateRequest(req); err != nil {
		return nil, err
	}
	if err := agent.VerifyRequestDelivery(ctx, req); err != nil {
		return nil, err
	}
	if req.Invocation == agent.InvocationGraphNode {
		rolePath := "plugin/agents/" + req.Delivery.MainRole + ".md"
		if _, ok := req.Delivery.RenderedFiles[rolePath]; !ok {
			return nil, agent.DeliveryRefusal(req, fmt.Errorf("missing Claude main-role resource: %s", rolePath))
		}
	}

	logPath, err := naming.RunLogPath(r.home, req.Ref, req.RunOrdinal)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), runArtefactDirMode); err != nil {
		return nil, fmt.Errorf("create run log directory: %w", err)
	}
	if created, err := EnsureClaudeConfigDir(r.claudeConfigDir, r.osHome); err != nil {
		return nil, fmt.Errorf("prepare claude config dir: %w", err)
	} else if created {
		r.logger.Info("created the agent's claude config dir",
			slog.String("path", r.claudeConfigDir))
	}
	if written, err := EnsureSettings(r.settingsPath, r.home); err != nil {
		return nil, fmt.Errorf("prepare claude settings: %w", err)
	} else if written {
		r.logger.Info("wrote claude settings file", slog.String("path", r.settingsPath))
	}

	// O_APPEND rather than O_TRUNC: the ordinal is allocated once per run and
	// never reused, so this file should not exist — and if it somehow does, a
	// previous run's partial evidence is not ours to delete (S-9).
	// #nosec G304 -- logPath is not user input: naming.RunLogPath derives it from
	// r.home and a validated task ref plus the run ordinal, and rejects anything
	// that would escape the run artefact directory.
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

	args := r.args(req)
	mcpPath, cleanupMCP, err := r.prepareMCP(req.MCPLaunch)
	if err != nil {
		return nil, err
	}
	defer func() { runErr = errors.Join(runErr, cleanupMCP()) }()
	if mcpPath != "" {
		args = append(args, "--mcp-config", mcpPath)
	}
	logger := r.logger.With(
		slog.String("task", req.Ref.String()),
		slog.Int("run", req.RunOrdinal),
		slog.String("claude_session_id", req.Session.Ref),
		slog.String("path", req.Path.String()))

	// exec's result is returned even when its error is not nil: a refused
	// resume is both a run that happened and a condition the caller has to act
	// on, and the result is the evidence for it.
	return r.exec(ctx, req, args, logPath, logFile, logger)
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
//   - --session-id names the conversation a *first* run is about to start. The
//     id is the one carried on the request, which is the one recorded on the
//     segments row, so the row and the conversation claude keeps on disk cannot
//     drift apart. Passing it a second time does not resume anything: claude
//     checks the id against the transcript on disk and exits non-zero with
//     "Session ID <uuid> is already in use."
//   - --resume takes that same id on every run after the first, and
//     --session-id is dropped — the two are mutually exclusive unless
//     --fork-session is also given, which would write a second transcript per
//     run and need a fresh id for each. --resume fails *closed*: an id with no
//     conversation behind it exits non-zero rather than inventing one, which is
//     what makes a lost transcript detectable and the cold rerun possible.
//     claude's --continue fails *open* — with nothing to continue it silently
//     starts a fresh conversation and exits 0, indistinguishable from having
//     resumed — so it is never constructed here or anywhere else.
//   - --tools is the real tool boundary (SD-2).
//   - --settings points at the litefactory-controlled file, so a run does not
//     inherit the operator's personal permissions.
//   - --permission-mode decides everything the rules do not name. See
//     PermissionMode.
//   - --allowedTools is the narrower permission list, additive to the
//     operator's settings. Under --permission-mode auto it is a fast path: an
//     action it names is permitted without consulting the classifier, and one
//     it does not name is judged rather than refused outright. That is a
//     change from the behaviour this comment used to describe — while runs
//     were in "default" the list was the effective boundary, because anything
//     outside it needed a prompt that a headless run could not answer.
//   - --strict-mcp-config makes claude ignore every MCP configuration except
//     the ones passed with --mcp-config. Only the attempt registration is
//     reachable; a .mcp.json committed on the branch is excluded.
//
// claude's permission-skipping flag — the one config.forbiddenFlag names — is
// never constructed here or anywhere else in the tree. That is a grep-able
// review invariant enforced by a repository-wide test, which is also why the
// flag is not written out as a literal in this comment.
//
// Native tool lists are the request's when it carries mode-selected permissions
// and the runner's configured ones otherwise — see [Runner.surface]. MCP rules
// come separately from the validated installed catalog. GitHub supplies neither.
func (r *Runner) args(req agent.Request) []string {
	// The session flag is the only difference between the two paths. Everything
	// after it — and the tool boundary in particular — is identical, because a
	// resumed session takes its surface from this argv and does not inherit a
	// wider one from the transcript it is continuing.
	sessionFlag := "--session-id"
	if req.Path.Resuming() {
		sessionFlag = "--resume"
	}
	tools, allowedTools := r.surface(req)
	// Delivered skills are loadable only through claude's Skill tool; the
	// bundle itself stays unreadable under the $LF_HOME deny. Skill loads the
	// run's own plugin skills and grants no file, shell or network access.
	if req.Delivery != nil && len(req.Delivery.Skills) > 0 {
		if !slices.Contains(tools, "Skill") {
			tools = append(slices.Clone(tools), "Skill")
		}
		if len(allowedTools) > 0 && !slices.Contains(allowedTools, "Skill") {
			allowedTools = append(slices.Clone(allowedTools), "Skill")
		}
	}
	if req.MCPLaunch != nil {
		allowedTools = slices.Clone(allowedTools)
		for _, name := range req.MCPLaunch.PermittedTools() {
			allowedTools = append(allowedTools, "mcp__"+mcpServerName+"__"+name)
		}
	}
	// A Validate reviewer's read-only shell refuses everything --allowedTools
	// does not name; every other run keeps PermissionMode.
	permissionMode := PermissionMode
	if toolSurface(req).ReadOnlyShell {
		permissionMode = ReadOnlyShellPermissionMode
	}
	model := r.model
	if req.Delivery != nil && req.Delivery.MainRole != "" {
		model = req.Delivery.MainModel
	}
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		sessionFlag, req.Session.Ref,
		// Unconditional, in both cases. An omitted --tools is allow-all, which
		// is the exact opposite of what an empty list looks like it asks for.
		"--tools", strings.Join(tools, ","),
		"--settings", r.settingsPath,
		// Unconditional, and next to --settings because the two are read
		// together: the settings document supplies the deny rules and this
		// says how everything they do not name is decided. See PermissionMode
		// for why the value is a constant rather than configuration.
		"--permission-mode", permissionMode,
		// Unconditional, so that the model a run used is a property of this
		// argv and readable in the recorded command line, rather than of
		// whatever the CLI defaults to. It sets the top-level session only:
		// a sub-agent definition carrying its own `model:` frontmatter still
		// runs on that, which is what lets a cheap router dispatch to
		// expensive orchestrators.
		"--model", model,
		"--autocompact", r.autoCompact,
	}
	if len(allowedTools) > 0 {
		// Comma-joined into a single argument rather than passed variadically:
		// --allowedTools takes <tools...>, and a following flag would otherwise
		// be swallowed as another tool rule.
		args = append(args, "--allowedTools", strings.Join(allowedTools, ","))
	}
	if req.Delivery != nil {
		args = append(args, "--setting-sources", "", "--plugin-dir", filepath.Join(req.Delivery.Root, "plugin"), "--append-system-prompt-file", filepath.Join(req.Delivery.Root, "bootstrap.md"))
	}
	if req.Delivery != nil && req.Delivery.MainRole != "" {
		args = append(args, "--agent", agent.DeliveryPlugin+":"+req.Delivery.MainRole)
	}
	if req.Usage != nil {
		args = append(args, "--forward-subagent-text")
	}
	return append(args, "--strict-mcp-config")
}

// InstructionIdentity invalidates a conversation when operator-owned parent
// model settings change. Remote team content cannot override this value.
func (r *Runner) InstructionIdentity() string { return r.model + "/" + r.autoCompact }

// surface picks the pair of tool lists one run is invoked with. It is the only
// place the native surface is chosen, shared by argv and native-tool logging.
//
// A request carrying tools is a request a profile was resolved for: its lists
// are used exactly as they arrive, AllowedTools verbatim. Empty AllowedTools
// there is a profile that genuinely has no permission rules — the flag is
// omitted rather than backfilled from the configured list, because empty and
// absent mean different things to claude.
//
// A request with no tools is every run that has no profile, and takes the
// runner's configured lists unchanged. That is what keeps an unprofiled argv
// byte-identical to the one litefactory built before profiles existed (AC-2.3).
//
// Tools is the discriminator rather than the profile name because Tools is
// what argv is built from: a profile name with an empty surface behind it
// would otherwise omit --tools, and an omitted --tools grants everything.
func (r *Runner) surface(req agent.Request) (tools, allowedTools []string) {
	if ts := toolSurface(req); len(ts.Tools) > 0 {
		return ts.Tools, ts.AllowedTools
	}
	return r.tools, r.allowedTools
}

// exec starts the process, streams it, and classifies how it ended.
func (r *Runner) exec(
	ctx context.Context,
	req agent.Request,
	args []string,
	logPath string,
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

	// #nosec G204 -- This exec is the product's security boundary and every part
	// of it is chosen, not inherited. r.binary.Path is an absolute path resolved
	// and stat'ed at New time from the operator's own config, never from an
	// issue or a comment. args comes from r.args above: fixed flags, the
	// segment's session id — which validate has already established is a
	// canonical UUID and so cannot smuggle in a flag — and one of two tool
	// surfaces.
	//
	// The tool lists now arrive on the request rather than only from the runner,
	// and that is the part worth being precise about. They are copies of
	// operator-authored strings out of config.yaml, selected by the resolved
	// run mode, and validate
	// has just checked every one of them against r.ceiling — so nothing here can
	// exceed a bound set outside GitHub. The mode-profile name stays off argv
	// entirely: it is a lookup key, never a value.
	//
	// So no string originating from GitHub is ever joined into this argv. The
	// untrusted input — the prompt built from the issue — goes on stdin below,
	// never on argv, so there is no shell and nothing here for it to influence.
	// cmd.Env is the explicit allowlist buildEnv assembles, not the daemon's
	// environment.
	var cmd *exec.Cmd
	var stdoutR, stdoutW, stderrR, stderrW *os.File
	stackEnv, err := req.StackEnvironment(buildEnv(r.environ, r.forwardEnv, r.claudeConfigDir))
	if err != nil {
		return nil, err
	}
	usage := newUsageCollector(runCtx, req)
	if usage != nil {
		usage.prepareTranscripts(r.claudeConfigDir, req.WorktreePath)
	}
	startedAt := time.Now()
	err = providerprocess.Start(runCtx, r.binary.Path, req.WorktreePath, func() error {
		//nolint:gosec // Configured executable and validated arguments; prompt stays on stdin.
		cmd = exec.CommandContext(runCtx, r.binary.Path, args...)
		cmd.Dir = req.WorktreePath
		cmd.Env = stackEnv
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

		stdoutR, stdoutW, err = os.Pipe()
		if err != nil {
			return fmt.Errorf("%w: stdout pipe: %w", ErrStartFailed, err)
		}
		stderrR, stderrW, err = os.Pipe()
		if err != nil {
			closeAll(stdoutR, stdoutW)
			return fmt.Errorf("%w: stderr pipe: %w", ErrStartFailed, err)
		}
		// Assigning *os.File values means os/exec passes the descriptors straight
		// to the child and starts no copier goroutines of its own for them — and,
		// equally important, never closes the read ends behind this package's back.
		cmd.Stdout = stdoutW
		cmd.Stderr = stderrW

		if err := agent.VerifyRequestDelivery(runCtx, req); err != nil {
			closeAll(stdoutR, stdoutW, stderrR, stderrW)
			return err
		}
		if err := cmd.Start(); err != nil {
			closeAll(stdoutR, stdoutW, stderrR, stderrW)
			return fmt.Errorf("%w: %s: %w", ErrStartFailed, r.binary.Path, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// The child holds its own copies now. Without this the read ends never see
	// EOF, because this process would still hold a writer open.
	closeAll(stdoutW, stderrW)
	defer closeAll(stdoutR, stderrR)

	// The three tool attributes report the native surface this run was actually
	// invoked with, not the runner's configured defaults: with a mode profile in
	// play the two differ, and the log line is the only place the difference is
	// visible after the fact. tool_profile is empty when no mode profile was supplied.
	tools, allowedTools := r.surface(req)
	logger.Info("coding agent started",
		slog.String("agent", "claude"),
		slog.String("binary", r.binary.Path),
		slog.Int("pid", cmd.Process.Pid),
		slog.String("worktree", req.WorktreePath),
		slog.String("tools", strings.Join(tools, ",")),
		slog.String("allowed_tools", strings.Join(allowedTools, ",")),
		slog.String("tool_profile", req.Permissions.Profile),
		slog.String("settings", r.settingsPath),
		slog.Int("env_vars", len(cmd.Env)),
		slog.String("timeout", r.timeout.String()))

	// The package's entire goroutine budget: one stdout reader, one stderr
	// reader. Both are joined below before anything they wrote is read, and
	// neither can outlive this call — if a survivor holds a pipe open past
	// ioGrace the read ends are closed from here and both return at once.
	stream := &eventStream{sink: sink, logger: logger, usage: usage}
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
		SessionID:   req.Session.Ref,
		LogPath:     logPath,
		Command:     append([]string{r.binary.Path}, args...),
		Model:       stream.model,
		Tools:       stream.tools,
		Events:      stream.events,
		Malformed:   stream.malformed,
		LongestLine: stream.longest,
		Stderr:      stderrTail.String(),
		StreamError: firstError(stream.err(), stdoutErr, stderrErr),
		Usage:       stream.usage.finish(),
		StartedAt:   startedAt,
		FinishedAt:  finishedAt,
		Duration:    finishedAt.Sub(startedAt),
	}
	if stream.sessionID != "" && stream.sessionID != req.Session.Ref {
		// The id came off the segments row. If claude answered with a different
		// one, the row and the conversation on disk have parted company, and a
		// later run would resume the wrong one.
		logger.Warn("claude reported a different session id than it was given",
			slog.String("requested", req.Session.Ref),
			slog.String("reported", stream.sessionID))
	}

	if event := stream.result; event != nil {
		result.HasResult = true
		result.HasCost = event.HasCost
		result.ResultText = event.Result
		result.CostUSD = event.TotalCostUSD
		result.IsError = event.IsError
		result.NumTurns = event.NumTurns
		result.HasNumTurns = event.HasNumTurns
	}

	if !abandoned && !timedOut {
		result.Interruption = stream.quota.interruption(stream.result, finishedAt)
		if result.Interruption != nil {
			result.Outcome, result.Reason = agent.OutcomeInterrupted, "session quota limit reached"
			result.ResumeToken = stream.sessionID
		}
	}
	if result.Outcome == agent.OutcomeFailed && result.Interruption == nil && stream.result != nil && stream.result.IsError {
		result.Transient = agent.ProviderCapacity(strings.Join(stream.result.Errors, "\n"))
		if result.Transient != nil {
			result.Reason = result.Transient.Cause
			result.Transient.Model = r.model
			if req.Delivery != nil && req.Delivery.MainRole != "" {
				result.Transient.Model = req.Delivery.MainModel
			}
		}
	}
	r.logCompletion(logger, result)

	result.ProviderReportedZeroCostRefusal = provenZeroCostRefusal(req.Path.Resuming(), result, stream)
	if result.Transient == nil && resumeRefused(req.Path.Resuming(), result) {
		logger.Warn("claude refused to resume the conversation",
			slog.String("stderr", excerpt(result.Stderr)))
		return result, fmt.Errorf("%w: no conversation to resume for session id %s (exit %d)",
			ErrSessionNotResumable, req.Session.Ref, result.ExitCode)
	}
	if result.Transient == nil && resumeUnusable(req.Path.Resuming(), result) {
		logger.Warn("the resumed conversation no longer fits; rerunning this run cold",
			slog.Int("num_turns", result.NumTurns),
			slog.String("result", excerpt(result.ResultText)))
		return result, fmt.Errorf("%w: conversation %s failed before its first turn (exit %d)",
			ErrSessionNotResumable, req.Session.Ref, result.ExitCode)
	}
	return result, nil
}

// resumeRefused reports whether this run is claude declining to resume, which
// is the one failure the caller answers by rerunning rather than by reporting.
//
// It is decided structurally. The stderr line claude prints ("No conversation
// found with session ID: …") is logged as evidence and never matched on: it is
// prose from another project's binary, and a release that rewords it would turn
// a handled condition into a permanently wedged task.
//
// Every clause earns its place:
//
//   - resume only. A cold run cannot be refused for this reason, and a cold run
//     that exits non-zero having emitted nothing is a different problem — a
//     session id already in use, for one — that rerunning would not fix.
//   - OutcomeFailed. It excludes OutcomeTimedOut and OutcomeAbandoned, neither
//     of which says anything about whether the conversation exists.
//   - no init event, so Model is empty. A run that got as far as an init event
//     had its conversation and then failed at something else; rerunning it cold
//     would throw that conversation away. Note that the refusal is *not* silent
//     on stdout — it emits a single terminal result event with num_turns 0 — so
//     the absence of an init event is the test, not the absence of events.
//   - a positive exit code. It excludes [SignalExitCode], which is a killed
//     process rather than a status claude chose, and the SD-7 missing-`--verbose`
//     failure, which is zero events with a *clean* exit and classifies as
//     OutcomeNoResult.
func resumeRefused(resuming bool, result *RunResult) bool {
	return resuming &&
		result.Outcome == OutcomeFailed &&
		result.Model == "" &&
		result.ExitCode > 0
}

// resumeUnusable reports whether a resumed conversation was rejected before it
// could do any work, which the caller answers by rerunning cold exactly as it
// answers [resumeRefused].
//
// The condition this was written for: a conversation grew past the model's
// context window across earlier runs, so claude declines it up front rather
// than at the API. On issue #207 that ended run 18 in five seconds with one
// turn, no tokens and "Prompt is too long" — after the prompt itself had been
// cut from 2.8MB to 125KB, so the prompt was no longer the problem and the
// history was.
//
// It is decided structurally, and deliberately not by matching the message.
// "Prompt is too long" is prose from another project's binary, and its
// terminal_reason is a vocabulary that project can extend; neither is a
// contract. What is load-bearing is that nothing happened:
//
//   - resume only, and OutcomeFailed, for the reasons [resumeRefused] gives.
//   - a terminal error result. A run killed or timed out says nothing about
//     whether its conversation is usable.
//   - at most one turn, and a reported cost of exactly zero. Together these
//     are the claim that no work was done and no tokens were spent, which is
//     what makes rerunning cold safe: [resumeRefused] must not throw away a
//     conversation that got somewhere, and neither must this. A resumed run
//     that burned tokens and then failed is a different problem and is
//     reported, not retried.
func resumeUnusable(resuming bool, result *RunResult) bool {
	return resuming &&
		result.Outcome == OutcomeFailed &&
		result.HasResult && result.IsError &&
		result.ExitCode > 0 &&
		result.NumTurns <= 1 &&
		result.HasCost && result.CostUSD == 0
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
	if result.HasCost {
		attrs = append(attrs, slog.Float64("cost_usd", result.CostUSD))
	}
	if result.StreamError != nil {
		attrs = append(attrs, slog.String("stream_error", result.StreamError.Error()))
	}
	if result.Succeeded() {
		logger.Info("coding agent finished", append([]any{slog.String("agent", "claude")}, attrs...)...)
		return
	}
	logger.Error("coding agent failed", append([]any{slog.String("agent", "claude")}, append(attrs, slog.String("reason", result.Reason))...)...)
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
				fmt.Sprintf("claude was terminated by a signal: %s", exitErr.String()),
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
