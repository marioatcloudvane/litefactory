package turn

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

// BugSelfCheckWait bounds how long bug_self_check_status waits for a change
// before answering, staying under every provider's MCP tool timeout (the
// factory does not set Codex's, which defaults to 60 seconds).
const BugSelfCheckWait = 30 * time.Second

// BugSelfCheckOutputCap bounds the redacted tail of a failing command's
// output returned to the fixer, per command (AC-011).
const BugSelfCheckOutputCap = 8 << 10

// bugSelfCheckPollInterval is how often Status re-reads durable state while
// waiting for a change; the DB, not an in-process signal, is authoritative.
const bugSelfCheckPollInterval = 250 * time.Millisecond

// bugSelfCheckStopWait bounds how long Stop waits for a locally running self-
// check's process group to end before it gives up waiting (the underlying
// runner itself bounds cancellation to a few seconds; this is slack on top).
const bugSelfCheckStopWait = 15 * time.Second

// BugSelfCheckService executes fixer-requested self-checks for one dispatched
// repair attempt, scoped to a single fixer MCP launch. The factory runs every
// command with the existing bugchecks.Isolation runner, in the fixer's own
// worktree; the fixer only asks through bug_self_check_start and polls
// through bug_self_check_status. It takes no worktree guard of its own, never
// reserves, counts against or releases the three-attempt repair budget, and
// never reads the recipe from the fixer's live worktree — only from the
// repair's own pinned source commit, through git plumbing.
type BugSelfCheckService struct {
	DB         *store.DB
	Isolation  bugchecks.Isolation
	Worktree   *git.Worktree
	Session    int64
	Repair     string
	Repository string

	mu  sync.Mutex
	run *bugSelfCheckActive
}

type bugSelfCheckActive struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// bugSelfCheckStartResponse mirrors bug_self_check_start's exact reply shape:
// exactly one of RunID (started or already pending), Unavailable or
// LimitReached is populated.
type bugSelfCheckStartResponse struct {
	RunID        string `json:"run_id,omitempty"`
	Ordinal      int    `json:"ordinal,omitempty"`
	Remaining    int    `json:"remaining,omitempty"`
	Unavailable  string `json:"unavailable,omitempty"`
	LimitReached *int   `json:"limit_reached,omitempty"`
}

func unavailableResponse(err error) (json.RawMessage, error) {
	return json.Marshal(bugSelfCheckStartResponse{Unavailable: err.Error()})
}

// HandleStart implements the bug_self_check_start MCP tool. It takes no
// arguments: the fixer cannot choose commands or phases (AC-001).
func (s *BugSelfCheckService) HandleStart(ctx context.Context, _ string, args []byte) (json.RawMessage, error) {
	if s == nil {
		return unavailableResponse(errors.New("self-check sandbox is not configured for this attempt"))
	}
	var empty struct{}
	if err := decodeMCPArguments(args, &empty); err != nil {
		return nil, err
	}
	return s.Start(ctx)
}

// HandleStatus implements the bug_self_check_status MCP tool.
func (s *BugSelfCheckService) HandleStatus(ctx context.Context, _ string, args []byte) (json.RawMessage, error) {
	if s == nil {
		return nil, errors.New("self-check sandbox is not configured for this attempt")
	}
	var in struct {
		RunID string `json:"run_id"`
	}
	if err := decodeMCPArguments(args, &in); err != nil {
		return nil, err
	}
	if in.RunID == "" || len(in.RunID) > 256 {
		return nil, errors.New("bug self-check status requires run_id")
	}
	return s.Status(ctx, in.RunID)
}

// Start implements bug_self_check_start's business logic (AC-001..AC-004,
// AC-007, AC-008). Every failure mode, including a precondition refusal, an
// unavailable sandbox or a stale recipe, is returned as data rather than a
// tool error, so it never fails the repair attempt (AC-011).
func (s *BugSelfCheckService) Start(ctx context.Context) (json.RawMessage, error) {
	pending, ok, err := s.pendingRun(ctx)
	if err != nil {
		return unavailableResponse(err)
	}
	if ok {
		return json.Marshal(bugSelfCheckStartResponse{RunID: pending.RunID, Ordinal: pending.Ordinal, Remaining: pending.Remaining})
	}
	repair, err := s.DB.GetBugRepair(ctx, s.Session, s.Repair)
	if err != nil {
		return unavailableResponse(err)
	}
	if err := s.Isolation.Validate(); err != nil {
		return unavailableResponse(err)
	}
	recipe, err := s.Worktree.ReadBugCheckRecipe(ctx, repair.SourceCommit)
	if err != nil {
		return unavailableResponse(err)
	}
	parsed, err := config.ParseBugCheckRecipe(recipe)
	if err != nil {
		return unavailableResponse(err)
	}
	regressionPhase, err := parsed.Phase("regression")
	if err != nil {
		return unavailableResponse(err)
	}
	template := store.BugCheckBinding{Repair: s.Repair, SourceCommit: repair.SourceCommit, SourceTree: repair.SourceTree, TargetRepository: s.Repository, TargetBranch: "main", TargetCommit: repair.SourceCommit, RecipeDigest: config.BugRecipeDigest(recipe), EnvironmentRevision: bugRuntimeDigest(s.Isolation.Environment), PermissionRevision: s.Isolation.Revision}
	regression := template
	regression.Phase = "regression"
	started, err := s.DB.StartBugSelfCheck(ctx, s.Session, s.Repair, 0, regression, recipe, len(regressionPhase.Commands))
	if err != nil {
		return unavailableResponse(err)
	}
	switch started.Status {
	case "limit_reached":
		return json.Marshal(bugSelfCheckStartResponse{LimitReached: &started.Count})
	case "pending":
		return json.Marshal(bugSelfCheckStartResponse{RunID: started.RunID, Ordinal: started.Ordinal, Remaining: started.Remaining})
	case "started":
		s.launch(ctx, started, parsed, template, recipe)
		return json.Marshal(bugSelfCheckStartResponse{RunID: started.RunID, Ordinal: started.Ordinal, Remaining: started.Remaining})
	default:
		return unavailableResponse(errors.New("unrecognized bug self-check start outcome"))
	}
}

// pendingRun reports the bug's one currently unresolved self-check run, if
// any, honoring "starting again while a run is pending returns that same
// pending run" (AC-003) before any sandbox or recipe work.
func (s *BugSelfCheckService) pendingRun(ctx context.Context) (store.BugSelfCheckStart, bool, error) {
	runs, err := s.DB.BugSelfChecks(ctx, s.Session, s.Repair)
	if err != nil {
		return store.BugSelfCheckStart{}, false, err
	}
	remaining := store.BugSelfCheckMaxRuns - len(runs)
	for _, run := range runs {
		if !run.Done() {
			return store.BugSelfCheckStart{Status: "pending", RunID: run.RunID, Ordinal: run.Ordinal, Remaining: remaining}, true, nil
		}
	}
	return store.BugSelfCheckStart{}, false, nil
}

// launch starts the run's background execution: regression, then focused only
// if regression passed, stopping at the first failed phase (AC-002). It owns
// the goroutine it starts: Stop cancels it and waits for it to end.
func (s *BugSelfCheckService) launch(ctx context.Context, started store.BugSelfCheckStart, recipe config.BugCheckRecipe, template store.BugCheckBinding, recipeBytes []byte) {
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	active := &bugSelfCheckActive{cancel: cancel, done: make(chan struct{})}
	s.mu.Lock()
	s.run = active
	s.mu.Unlock()
	go func() {
		defer close(active.done)
		defer cancel()
		defer func() {
			s.mu.Lock()
			if s.run == active {
				s.run = nil
			}
			s.mu.Unlock()
		}()
		s.runPhases(runCtx, started, recipe, template, recipeBytes)
	}()
}

// runPhases runs the regression phase and, only if it passes, the focused
// phase, recording each through FinishBugSelfCheckPhase. It never runs the
// full phase.
func (s *BugSelfCheckService) runPhases(ctx context.Context, started store.BugSelfCheckStart, recipe config.BugCheckRecipe, template store.BugCheckBinding, recipeBytes []byte) {
	regressionPhase, err := recipe.Phase("regression")
	if err != nil {
		return
	}
	outcome, reason, commands := s.runCommands(ctx, regressionPhase)
	if err := s.DB.FinishBugSelfCheckPhase(context.WithoutCancel(ctx), s.Session, started.Operation, outcome, reason, commands); err != nil {
		return
	}
	if outcome != "passed" {
		return
	}
	focused := template
	focused.Phase = "focused"
	focusedPhase, err := recipe.Phase("focused")
	if err != nil {
		return
	}
	// WithoutCancel: the focused row is always added once regression passes,
	// even if Stop is racing this moment, so the run always reaches a terminal
	// state instead of being stuck "not done" forever with nothing left to run
	// it. runCommands below still uses the (possibly already cancelled) ctx, so
	// a racing Stop still ends the focused phase "cancelled" promptly rather
	// than actually running it.
	next, err := s.DB.StartBugSelfCheck(context.WithoutCancel(ctx), s.Session, s.Repair, started.Ordinal, focused, recipeBytes, len(focusedPhase.Commands))
	if err != nil || next.Status != "started" {
		// A genuine store failure here (never expected in ordinary operation)
		// leaves this run with a passed regression phase and no focused row,
		// permanently "not done"; StopBugSelfChecks and restart recovery only
		// sweep rows still outcome=pending, so this rare case needs an operator
		// to notice and clear it, same as any other unexpected store error would.
		return
	}
	fOutcome, fReason, fCommands := s.runCommands(ctx, focusedPhase)
	_ = s.DB.FinishBugSelfCheckPhase(context.WithoutCancel(ctx), s.Session, next.Operation, fOutcome, fReason, fCommands)
}

// runCommands runs one phase's commands in order, stopping at the first
// non-passing command, reusing the same checkObservation projection the
// factory's own checks use.
func (s *BugSelfCheckService) runCommands(ctx context.Context, phase config.BugCheckPhase) (outcome, reason string, commands []store.BugCheckCommandRecord) {
	for _, command := range phase.Commands {
		result := s.Isolation.Run(ctx, s.Worktree.Path(), command, func(bugchecks.CommandResult) error { return nil })
		commands = append(commands, store.BugCheckCommandRecord{Ordinal: len(commands), State: "completed", Observation: checkObservation(result)})
		if result.Outcome != "passed" {
			return result.Outcome, result.Reason, commands
		}
	}
	return "passed", "", commands
}

// Stop cancels any locally running self-check and waits for its process group
// to end, then marks any row still pending cancelled. It is safe to call on a
// nil service or when no run is active (AC-007, AC-011).
func (s *BugSelfCheckService) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	run := s.run
	s.mu.Unlock()
	if run != nil {
		run.cancel()
		select {
		case <-run.done:
		case <-time.After(bugSelfCheckStopWait):
		}
	}
	_, err := s.DB.StopBugSelfChecks(context.WithoutCancel(ctx), s.Session, s.Repair)
	return err
}

// bugSelfCheckStatusResponse mirrors bug_self_check_status's exact reply shape.
type bugSelfCheckStatusResponse struct {
	State     string                  `json:"state"`
	Phases    []bugSelfCheckPhaseView `json:"phases,omitempty"`
	Remaining int                     `json:"remaining"`
}
type bugSelfCheckPhaseView struct {
	Name            string                       `json:"name"`
	Outcome         string                       `json:"outcome"`
	FailingCommands []bugSelfCheckFailingCommand `json:"failing_commands,omitempty"`
}
type bugSelfCheckFailingCommand struct {
	ArgvSummary string `json:"argv_summary"`
	Exit        *int   `json:"exit,omitempty"`
	OutputTail  string `json:"output_tail"`
}

// Status implements bug_self_check_status: the run's state and, once done,
// each phase's outcome and the redacted, capped tail of any failing command's
// output (AC-003, AC-011). It waits up to BugSelfCheckWait for a change,
// re-reading durable state rather than an in-process signal, before
// answering. Unknown or foreign run ids are refused.
func (s *BugSelfCheckService) Status(ctx context.Context, runID string) (json.RawMessage, error) {
	deadline := time.Now().Add(BugSelfCheckWait)
	for {
		runs, err := s.DB.BugSelfChecks(ctx, s.Session, s.Repair)
		if err != nil {
			return nil, err
		}
		var run *store.BugSelfCheckRun
		for i := range runs {
			if runs[i].RunID == runID {
				run = &runs[i]
				break
			}
		}
		if run == nil {
			return nil, errors.New("unknown or foreign bug self-check run")
		}
		if run.Done() || !time.Now().Before(deadline) {
			state := "pending"
			if run.Done() {
				state = "done"
			}
			return json.Marshal(bugSelfCheckStatusResponse{State: state, Phases: bugSelfCheckPhaseViews(run.Phases), Remaining: store.BugSelfCheckMaxRuns - len(runs)})
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(bugSelfCheckPollInterval):
		}
	}
}

// newBugSelfCheckService builds the fixer-attempt-scoped self-check runner
// for a dispatched repair attempt. A nil result and nil error together mean
// self-checking is unavailable for this attempt (no dispatched repair could
// be resolved); the caller attaches no self-check tools rather than a broken
// service, so the fixer still finishes and hands back normally (AC-008).
func newBugSelfCheckService(ctx context.Context, runtime *MCPRuntime, prep *preparation) (*BugSelfCheckService, error) {
	if runtime == nil || runtime.Authority == nil || prep == nil || prep.worktree == nil || prep.session.ID <= 0 {
		return nil, errors.New("bug self-check requires a prepared session worktree")
	}
	repair, err := runtime.Authority.db.LatestBugRepair(ctx, prep.session.ID)
	if err != nil {
		return nil, err
	}
	if repair.State != "dispatched" {
		return nil, errors.New("bug self-check requires a dispatched repair attempt")
	}
	return &BugSelfCheckService{
		DB:         runtime.Authority.db,
		Isolation:  runtime.BugChecksIsolation,
		Worktree:   prep.worktree,
		Session:    prep.session.ID,
		Repair:     repair.Request,
		Repository: prep.ref.Owner + "/" + prep.ref.Repo,
	}, nil
}

// RecoverBugSelfChecks cancels every self-check run a crash left pending,
// across every bug, without rerunning any of them (AC-011). It is meant to
// run once at daemon startup, before any run is dispatched.
func RecoverBugSelfChecks(ctx context.Context, db *store.DB) error {
	pending, err := db.PendingBugSelfCheckRepairs(ctx)
	if err != nil {
		return err
	}
	for _, p := range pending {
		if _, err := db.StopBugSelfChecks(ctx, p.SessionID, p.Repair); err != nil {
			return err
		}
	}
	return nil
}

func bugSelfCheckPhaseViews(phases []store.BugCheckPhaseRecord) []bugSelfCheckPhaseView {
	out := make([]bugSelfCheckPhaseView, 0, len(phases))
	for _, phase := range phases {
		view := bugSelfCheckPhaseView{Name: phase.Binding.Phase, Outcome: phase.Outcome}
		for _, c := range phase.Commands {
			if c.Observation.Outcome == "" || c.Observation.Outcome == "passed" {
				continue
			}
			text := c.Observation.Output
			if len(text) > BugSelfCheckOutputCap {
				text = "…" + text[len(text)-BugSelfCheckOutputCap:]
			}
			view.FailingCommands = append(view.FailingCommands, bugSelfCheckFailingCommand{ArgvSummary: c.Observation.Executable, Exit: c.Observation.Exit, OutputTail: text})
		}
		out = append(out, view)
	}
	return out
}
