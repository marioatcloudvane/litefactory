package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// ValidateSuiteAuthority is factory-only context: the exact commit to test.
type ValidateSuiteAuthority struct {
	Commit string
}

// ValidateSuiteBind supplies the commit from factory state, never from agent text.
type ValidateSuiteBind func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error)

// ValidateSuiteRepo opens scratch worktrees detached at an exact commit.
type ValidateSuiteRepo interface {
	Begin(ctx context.Context, commit string) (ValidateSuiteScratch, error)
}

// ValidateSuiteScratch is a throwaway worktree detached at the tested commit.
// The activity owns it and closes it on every path.
type ValidateSuiteScratch interface {
	Path() string
	// Recipe reads the check recipe from the tested commit, never from the
	// working files. A missing recipe is git.ErrBugCheckRecipe.
	Recipe(ctx context.Context) ([]byte, error)
	// Observe returns the content digest of the checked-out tree.
	Observe(ctx context.Context) (string, error)
	Close(ctx context.Context) error
}

// ValidateSuiteRunner runs one command. started retains the process identity
// right after launch.
type ValidateSuiteRunner func(ctx context.Context, worktree string, command config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult

// ValidateSuiteBindings carries what the suite activity needs.
type ValidateSuiteBindings struct {
	StackProfile *config.ResolvedStackProfile
	DB           *store.DB
	LockDir      string
	Repo         ValidateSuiteRepo
	Bind         ValidateSuiteBind
	// Run overrides the configured check sandbox; nil uses it.
	Run ValidateSuiteRunner
	// Stopped overrides the process-stopped probe; nil uses bugchecks.ProcessStopped.
	Stopped func(ctx context.Context, identity string) (bool, error)
	Now     func() time.Time
}

// RegisterValidateSuiteActivity registers the suite activity in both registries.
func RegisterValidateSuiteActivity(specs *graphexec.Registry, activities *ActivityRegistry, b ValidateSuiteBindings) error {
	if specs == nil || activities == nil || b.DB == nil || b.LockDir == "" || b.Repo == nil || b.Bind == nil {
		return errors.New("validate suite activity requires registries, store, lock directory, repository and bind")
	}
	spec := graphexec.ValidateSuiteSpec()
	if err := specs.Register(spec); err != nil {
		return err
	}
	return activities.Register(spec.Name, ValidateSuiteActivity{b})
}

// ValidateSuiteActivity runs the full phase of the recipe of one commit.
type ValidateSuiteActivity struct{ b ValidateSuiteBindings }

// validateSuiteOutput is the one stable result later steps read.
type validateSuiteOutput struct {
	RunID        int64  `json:"run_id,omitempty"`
	Key          string `json:"key,omitempty"`
	Outcome      string `json:"outcome"`
	Commit       string `json:"commit"`
	RecipeDigest string `json:"recipe_digest,omitempty"`
	TreeBefore   string `json:"tree_before,omitempty"`
	TreeAfter    string `json:"tree_after,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

func suiteOutput(run store.SuiteRun) validateSuiteOutput {
	return validateSuiteOutput{
		RunID: run.ID, Key: fmt.Sprintf("%d/%s/%s/%d", run.InstanceID, run.Commit, run.RecipeDigest, run.Attempt), Outcome: run.State,
		Commit: run.Commit, RecipeDigest: run.RecipeDigest, TreeBefore: run.TreeBefore, TreeAfter: run.TreeAfter, Reason: run.Reason,
	}
}

func isFullSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func replayable(state string) bool {
	return state == store.SuitePassed || state == store.SuiteFailed || state == store.SuiteBlocked
}

// Execute returns
//
//	passed   every full-phase command exited 0 on an unchanged snapshot
//	failed   a command exited 1; later commands did not run
//	blocked  no recipe, an empty full phase, a non-0/1 exit, a changed snapshot,
//	         or an earlier run that could not be confirmed stopped
//	retry    another suite holds the lock; nothing was started
//
// A finished result for the same graph instance and commit is returned
// unchanged without running anything, and a passed suite of another instance
// on the same commit is copied instead of run again. A cancelled context is an error.
func (h ValidateSuiteActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (res ActivityResult, err error) {
	b := h.b
	if req.Mode != agent.ModeValidate || b.Bind == nil {
		return ActivityResult{}, errors.New("validate suite activity requires admitted Validate authority")
	}
	auth, err := b.Bind(ctx, opts, req)
	if err != nil {
		return ActivityResult{}, err
	}
	if !isFullSHA(auth.Commit) {
		return ActivityResult{}, errors.New("validate suite needs the full commit SHA")
	}
	if done, ok, err := h.finished(ctx, req.InstanceID, auth.Commit); err != nil || ok {
		return done, err
	}
	if reused, err := b.DB.ReuseSuiteRun(ctx, req.InstanceID, req.Node, auth.Commit, h.now(opts)); err == nil {
		return suiteResult(reused.State, suiteOutput(reused))
	} else if !errors.Is(err, store.ErrNotFound) {
		return ActivityResult{}, err
	}
	unlock, err := b.DB.LockSuite(ctx, b.LockDir)
	if errors.Is(err, store.ErrSuiteBusy) {
		return suiteResult("retry", validateSuiteOutput{Outcome: "retry", Commit: auth.Commit, Reason: "another suite is running"})
	}
	if err != nil {
		return ActivityResult{}, fmt.Errorf("validate suite lock: %w", err)
	}
	defer func() { err = errors.Join(err, unlock()) }()

	if done, ok, err := h.finished(ctx, req.InstanceID, auth.Commit); err != nil || ok {
		return done, err
	}
	if reason, err := h.recover(ctx, opts); err != nil {
		return ActivityResult{}, err
	} else if reason != "" {
		return suiteResult("blocked", validateSuiteOutput{Outcome: "blocked", Commit: auth.Commit, Reason: reason})
	}
	return h.run(ctx, opts, req, auth.Commit)
}

func (h ValidateSuiteActivity) finished(ctx context.Context, instance int64, commit string) (ActivityResult, bool, error) {
	run, err := h.b.DB.LatestSuiteRun(ctx, instance, commit)
	if errors.Is(err, store.ErrNotFound) {
		return ActivityResult{}, false, nil
	}
	if err != nil {
		return ActivityResult{}, false, err
	}
	if !replayable(run.State) {
		return ActivityResult{}, false, nil
	}
	res, err := suiteResult(run.State, suiteOutput(run))
	return res, true, err
}

func suiteResult(outcome string, out validateSuiteOutput) (ActivityResult, error) {
	data, err := json.Marshal(out)
	if err != nil {
		return ActivityResult{}, err
	}
	return ActivityResult{Outcome: outcome, Kind: store.GraphOutputJSON, Output: data}, nil
}

// recover settles an unfinished run a restart left behind. Holding the lock
// proves no live daemon owns it. The old attempt is marked interrupted only
// once every recorded process group is confirmed stopped; otherwise it returns
// the reason and nothing new starts.
func (h ValidateSuiteActivity) recover(ctx context.Context, opts Options) (string, error) {
	old, err := h.b.DB.RunningSuiteRun(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	commands, err := h.b.DB.SuiteCommands(ctx, old.ID)
	if err != nil {
		return "", err
	}
	probe := h.b.Stopped
	if probe == nil {
		probe = bugchecks.ProcessStopped
	}
	for _, c := range commands {
		if c.State != store.SuiteRunning {
			continue
		}
		stopped, perr := probe(ctx, c.ProcessIdentity)
		if perr != nil || !stopped {
			// An unconfirmed stop is a refusal reason, not a fault of this step.
			return "an earlier suite run was interrupted and its test processes could not be confirmed stopped; no new suite was started", nil //nolint:nilerr
		}
	}
	if err := h.b.DB.InterruptSuiteRun(ctx, old.ID, h.now(opts)); err != nil {
		return "", err
	}
	return "", nil
}

func (h ValidateSuiteActivity) now(opts Options) time.Time {
	if h.b.Now != nil {
		return h.b.Now().UTC()
	}
	return spikeNow(opts)
}

func (h ValidateSuiteActivity) runner(opts Options) (ValidateSuiteRunner, string) {
	if h.b.Run != nil {
		return h.b.Run, ""
	}
	if opts.MCP == nil {
		return nil, "the check sandbox is not configured"
	}
	isolation, err := opts.MCP.BugChecksIsolation.WithStackProfile(h.b.StackProfile)
	if err != nil {
		return nil, "stack.environment: retained configuration is invalid"
	}
	if err := isolation.Validate(); err != nil {
		return nil, "the check sandbox is unavailable: " + err.Error()
	}
	return isolation.Run, ""
}

func (h ValidateSuiteActivity) run(ctx context.Context, opts Options, req ActivityRequest, commit string) (res ActivityResult, err error) {
	b := h.b
	scratch, err := b.Repo.Begin(ctx, commit)
	if err != nil {
		return ActivityResult{}, fmt.Errorf("prepare suite checkout: %w", err)
	}
	defer func() {
		if closeErr := scratch.Close(context.WithoutCancel(ctx)); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("discard suite checkout: %w", closeErr))
		}
	}()
	before, err := scratch.Observe(ctx)
	if err != nil {
		return ActivityResult{}, fmt.Errorf("observe suite snapshot: %w", err)
	}
	recipeRaw, recipeErr := scratch.Recipe(ctx)
	if recipeErr != nil && !errors.Is(recipeErr, git.ErrBugCheckRecipe) {
		return ActivityResult{}, fmt.Errorf("read build check recipe: %w", recipeErr)
	}
	digest := ""
	if recipeErr == nil {
		digest = config.BugRecipeDigest(recipeRaw)
	}
	run, err := b.DB.BeginSuiteRun(ctx, store.SuiteRun{InstanceID: req.InstanceID, Node: req.Node, Commit: commit, RecipeDigest: digest, TreeBefore: before, StartedAt: h.now(opts)})
	if err != nil {
		return ActivityResult{}, err
	}
	// finish retains the outcome with a context that survives cancellation.
	finish := func(state, after, reason string) (ActivityResult, error) {
		if err := b.DB.FinishSuiteRun(context.WithoutCancel(ctx), run.ID, state, after, reason, h.now(opts)); err != nil {
			return ActivityResult{}, err
		}
		final, err := b.DB.GetSuiteRun(context.WithoutCancel(ctx), run.ID)
		if err != nil {
			return ActivityResult{}, err
		}
		if state == store.SuiteCancelled {
			return ActivityResult{}, fmt.Errorf("validate suite cancelled: %w", errors.Join(ctx.Err(), errors.New(reason)))
		}
		return suiteResult(final.State, suiteOutput(final))
	}
	blocked := func(reason string) (ActivityResult, error) { return finish(store.SuiteBlocked, before, reason) }

	if recipeErr != nil {
		return blocked("no build check recipe in the tested commit")
	}
	recipe, perr := config.ParseBugCheckRecipe(recipeRaw)
	if perr != nil {
		return blocked("the build check recipe is invalid: " + clipText(perr.Error(), 300))
	}
	commands := recipe.Phases.Full.Commands
	if len(commands) == 0 {
		return blocked("the recipe declares no full-phase command")
	}
	runCommand, reason := h.runner(opts)
	if runCommand == nil {
		return blocked(reason)
	}

	state, why := store.SuitePassed, ""
	for i, command := range commands {
		command, err := pinnedStackCommand(b.StackProfile, command)
		if err != nil {
			return blocked(err.Error())
		}
		started := func(r bugchecks.CommandResult) error {
			return b.DB.ClaimSuiteCommand(ctx, store.SuiteCommand{RunID: run.ID, Position: i, Argv: command.Argv, StartedAt: r.Started, ProcessIdentity: r.ProcessIdentity})
		}
		result := runCommand(ctx, scratch.Path(), command, started)
		cmdState := result.Outcome
		switch {
		case cmdState == "passed" && (!result.CleanupConfirmed || !result.CaptureComplete || result.Exit == nil || *result.Exit != 0):
			cmdState, result.Reason = store.SuiteBlocked, "the command ended without a confirmed clean exit"
		case cmdState != "passed" && cmdState != "failed" && cmdState != "cancelled":
			cmdState = store.SuiteBlocked
		}
		if err := b.DB.FinishSuiteCommand(context.WithoutCancel(ctx), suiteCommandOf(run.ID, i, command, result, cmdState)); err != nil {
			return ActivityResult{}, err
		}
		if cmdState == store.SuitePassed {
			continue
		}
		state, why = cmdState, result.Reason
		break
	}

	after, obsErr := scratch.Observe(context.WithoutCancel(ctx))
	switch {
	case state == store.SuiteCancelled:
		return finish(store.SuiteCancelled, after, why)
	case obsErr != nil:
		after = ""
		state, why = store.SuiteBlocked, "the snapshot could not be observed after the run: "+clipText(obsErr.Error(), 300)
	case after != before:
		state, why = store.SuiteBlocked, "snapshot drift: the tested source changed while the suite ran"
	case state == store.SuiteFailed && why == "":
		why = "a suite command reported test failure"
	}
	return finish(state, after, why)
}

func suiteCommandOf(runID int64, position int, command config.BugCheckCommand, r bugchecks.CommandResult, state string) store.SuiteCommand {
	sum := sha256.Sum256([]byte(r.Output))
	tail, truncated := r.Output, false
	if len(tail) > store.MaxSuiteOutputTail {
		tail, truncated = strings.ToValidUTF8(tail[len(tail)-store.MaxSuiteOutputTail:], ""), true
	}
	finished := r.Ended
	if finished.IsZero() {
		finished = time.Now().UTC()
	}
	started := r.Started
	if started.IsZero() {
		started = finished
	}
	return store.SuiteCommand{
		RunID: runID, Position: position, Argv: command.Argv, State: state, Exit: r.Exit, Reason: clipText(r.Reason, 500),
		StartedAt: started, FinishedAt: finished, ProcessIdentity: r.ProcessIdentity, CleanupConfirmed: r.CleanupConfirmed,
		OutputTail: tail, OutputTruncated: truncated || len(tail) < len(r.Output), OutputDigest: hex.EncodeToString(sum[:]),
	}
}

// GitValidateSuiteRepo is the real repository behind ValidateSuiteRepo.
type GitValidateSuiteRepo struct {
	Clone       *git.Clone
	ClonePath   string
	Remote      string
	ScratchRoot string
	Gate        CloneGate
}

// Begin fetches the remote and checks out the commit detached in a scratch worktree.
func (r GitValidateSuiteRepo) Begin(ctx context.Context, commit string) (ValidateSuiteScratch, error) {
	if r.Clone == nil || r.Remote == "" || r.ScratchRoot == "" || !isFullSHA(commit) {
		return nil, errors.New("git suite repository is not configured")
	}
	if r.Gate != nil {
		release, err := r.Gate.Acquire(ctx, r.ClonePath)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	if err := r.Clone.Fetch(ctx, r.Remote); err != nil {
		return nil, fmt.Errorf("fetch %s: %w", r.Remote, err)
	}
	got, err := r.Clone.RevParse(ctx, commit+"^{commit}", git.RevParseOptions{})
	if err != nil || got != commit {
		return nil, fmt.Errorf("commit %s is not available: %w", commit, err)
	}
	if err := os.MkdirAll(r.ScratchRoot, 0o700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(r.ScratchRoot, "validate-suite-")
	if err != nil {
		return nil, err
	}
	wt, err := r.Clone.WorktreeAdd(ctx, filepath.Join(dir, "tree"), git.WorktreeAddOptions{Branch: commit, Detach: true})
	if err != nil {
		return nil, errors.Join(fmt.Errorf("scratch worktree: %w", err), os.RemoveAll(dir))
	}
	return &gitSuiteScratch{repo: r, wt: wt, dir: dir, commit: commit}, nil
}

type gitSuiteScratch struct {
	repo   GitValidateSuiteRepo
	wt     *git.Worktree
	dir    string
	commit string
}

func (s *gitSuiteScratch) Path() string { return s.wt.Path() }

func (s *gitSuiteScratch) Recipe(ctx context.Context) ([]byte, error) {
	return s.wt.ReadBugCheckRecipe(ctx, s.commit)
}

func (s *gitSuiteScratch) Observe(ctx context.Context) (string, error) {
	id, err := s.wt.ObserveSource(ctx, git.SourceOptions{})
	if err != nil {
		return "", err
	}
	return id.Digest, nil
}

func (s *gitSuiteScratch) Close(ctx context.Context) error {
	if s.repo.Gate != nil {
		release, err := s.repo.Gate.Acquire(ctx, s.repo.ClonePath)
		if err != nil {
			return err
		}
		defer release()
	}
	removeErr := s.repo.Clone.WorktreeRemove(ctx, s.wt.Path(), git.WorktreeRemoveOptions{Force: true})
	return errors.Join(removeErr, os.RemoveAll(s.dir))
}
