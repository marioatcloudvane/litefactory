package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// BugChecksAction names the trusted recipe phase activity.
const BugChecksAction = "lf_bug_checks"

// BugChecksSpec declares passing, ordinary failure and blocked phase outcomes.
func BugChecksSpec() graphexec.ActionSpec {
	return graphexec.ActionSpec{Name: BugChecksAction, Workflow: agent.WorkflowBug, Outcomes: []string{"passed", "failed", "blocked", "cancelled"}, Attributes: map[string]graphexec.AttributeSpec{"lf_phase": {Required: true, Values: []string{"regression", "focused", "full"}}}}
}

// BugChecksInput binds trusted recipe execution to source, target and isolation observations.
type BugChecksInput struct {
	Binding   store.BugCheckBinding
	Worktree  *git.Worktree
	Isolation bugchecks.Isolation
	// Observe validates target repository/branch/tip and environment/permission
	// revisions through factory-owned observations. It must not trust graph attrs.
	Observe func(context.Context, store.BugCheckBinding) error
	// Base checks the target commit out in a temporary detached worktree for
	// rerun attribution and returns its cleanup. Nil leaves every failure
	// still failing on the repair unattributed.
	Base func(context.Context, string) (*git.Worktree, func(context.Context) error, error)
}

// BugChecksOutput reports a phase outcome with retained evidence references.
type BugChecksOutput struct {
	SessionID int64  `json:"session_id"`
	Operation string `json:"operation"`
	Reason    string `json:"reason,omitempty"`
}

// BugChecksActivity owns active cancellation handles. The store retains intent
// across restart; no recovered operation is relaunched based on a bare PID.
type BugChecksActivity struct {
	// ObserveStopped optionally supplies an operator-owned process observer. Nil
	// selects the concrete process-group observer; graph input cannot set it.
	ObserveStopped func(context.Context, string) (bool, error)
	Profile        *graphexec.Profile
	Resolve        func(context.Context, ActivityRequest) (BugChecksInput, error)
	mu             sync.Mutex
	active         map[string]context.CancelFunc
	// run replaces Isolation.Run in tests; production leaves it nil.
	run bugCheckRunner
}

// bugCheckRunner has the signature of bugchecks.Isolation.Run.
type bugCheckRunner func(context.Context, string, config.BugCheckCommand, func(bugchecks.CommandResult) error) bugchecks.CommandResult

// RegisterBugChecks registers the concrete trusted recipe phase handler.
func RegisterBugChecks(specs *graphexec.Registry, activities *ActivityRegistry, h *BugChecksActivity) error {
	if specs == nil || activities == nil || h == nil || h.Profile == nil || h.Resolve == nil {
		return errors.New("bug checks require complete factory bindings")
	}
	if _, ok := activities.Resolve(BugChecksAction); ok {
		return errors.New("bug checks already registered")
	}
	if err := specs.Register(BugChecksSpec()); err != nil {
		return err
	}
	return activities.Register(BugChecksAction, h)
}
func bugCheckKey(session int64, operation string) string {
	return fmt.Sprintf("%d:%s", session, operation)
}

// Cancel records intent before stopping the locally active command.
func (h *BugChecksActivity) Cancel(ctx context.Context, db *store.DB, session int64, operation string) error {
	if db == nil {
		return errors.New("bug check cancellation requires store")
	}
	if err := db.CancelBugCheckPhase(ctx, session, operation); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if cancel := h.active[bugCheckKey(session, operation)]; cancel != nil {
		cancel()
	}
	return nil
}

// Execute executes the bound activity while preserving its durable authorization and recovery checks.
func (h *BugChecksActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if opts.DB == nil || h.Profile == nil || h.Resolve == nil || req.Workflow != agent.WorkflowBug || req.Mode != "" {
		return ActivityResult{}, errors.New("bug checks require standalone factory context")
	}
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	pin, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return ActivityResult{}, err
	}
	node, ok := h.Profile.Node(req.Node)
	if !ok || node.Action != BugChecksAction || pin.Workflow != "bug" || pin.Mode != "" || pin.Digest != h.Profile.Digest || instance.CurrentNode != req.Node {
		return ActivityResult{}, errors.New("bug check graph binding mismatch")
	}
	attempt, err := opts.DB.GetGraphNodeAttempt(ctx, req.NodeAttemptID)
	if err != nil {
		return ActivityResult{}, err
	}
	if attempt.InstanceID != instance.ID || attempt.Node != req.Node || attempt.Status != store.GraphRunning {
		return ActivityResult{}, errors.New("bug check attempt binding mismatch")
	}
	in, err := h.Resolve(ctx, req)
	if err != nil {
		return ActivityResult{}, err
	}
	if in.Binding.SessionID != instance.SessionID || in.Binding.Phase != node.Attrs["lf_phase"] {
		return ActivityResult{}, errors.New("bug check task/phase mismatch")
	}
	return h.RunPhase(ctx, opts.DB, in)
}
func checkActivityResult(b store.BugCheckBinding, outcome, reason string) (ActivityResult, error) {
	raw, err := json.Marshal(BugChecksOutput{b.SessionID, b.Operation, reason})
	return ActivityResult{Outcome: outcome, Kind: store.GraphOutputJSON, Output: raw}, err
}

// RunPhase runs the existing generation once. It does not allocate children,
// generations or repair slots and performs no retries.
func (h *BugChecksActivity) RunPhase(ctx context.Context, db *store.DB, in BugChecksInput) (ActivityResult, error) {
	b := in.Binding
	if db == nil || in.Worktree == nil || in.Observe == nil || b.PermissionRevision != in.Isolation.Revision {
		return ActivityResult{}, errors.New("incomplete Bug check factory input")
	}
	key := bugCheckKey(b.SessionID, b.Operation)
	runCtx, cancel := context.WithCancel(ctx)
	h.mu.Lock()
	if h.active == nil {
		h.active = map[string]context.CancelFunc{}
	}
	if h.active[key] != nil {
		h.mu.Unlock()
		cancel()
		return ActivityResult{}, errors.New("bug check already executing")
	}
	h.active[key] = cancel
	h.mu.Unlock()
	defer func() { cancel(); h.mu.Lock(); delete(h.active, key); h.mu.Unlock() }()
	observe := func() error {
		if err := in.Worktree.ObserveBugCheckSource(runCtx, b.SourceCommit, b.SourceTree); err != nil {
			return err
		}
		return in.Observe(runCtx, b)
	}
	// Replays resolve the retained recipe, never the mutable repair branch.
	retained, err := db.GetBugCheckPhase(runCtx, b.SessionID, b.Operation)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return ActivityResult{}, err
	}
	var recipe []byte
	if err == nil {
		recipe = retained.Recipe
	} else {
		recipe, err = in.Worktree.ReadBugCheckRecipe(runCtx, b.TargetCommit)
		if err != nil {
			return retainCheckSetupBlock(runCtx, db, b, err)
		}
	}
	parsed, err := config.ParseBugCheckRecipe(recipe)
	if err != nil {
		return retainCheckSetupBlock(runCtx, db, b, err)
	}
	phase, err := parsed.Phase(b.Phase)
	if err != nil {
		return ActivityResult{}, err
	}
	retained, err = db.EnsureBugCheckPhase(runCtx, b, recipe)
	if err != nil {
		return ActivityResult{}, err
	}
	if !retained.Current {
		return checkActivityResult(b, "blocked", "retained check evidence is stale or unreliable")
	}
	if retained.Outcome != "pending" {
		if retained.Outcome == "passed" {
			if err = observe(); err != nil {
				if invalidErr := db.InvalidateBugCheckPhase(runCtx, b.SessionID, b.Operation, err.Error()); invalidErr != nil {
					return ActivityResult{}, invalidErr
				}
				return retainCheckSetupBlock(runCtx, db, b, err)
			}
		}
		return checkActivityResult(b, retained.Outcome, retained.Reason)
	}
	claimed, err := db.ClaimBugWork(runCtx, b.SessionID, b.Operation, "checks")
	if err != nil {
		return ActivityResult{}, err
	}
	if !claimed {
		return checkActivityResult(b, "blocked", "worktree is owned by repair, save or another child")
	}
	finish := func(outcome, reason string, current bool) (ActivityResult, error) {
		cleanupCtx := context.WithoutCancel(ctx)
		if err := db.FinishBugCheckPhase(cleanupCtx, b.SessionID, b.Operation, outcome, reason, current); err != nil {
			return ActivityResult{}, err
		}
		saved, err := db.GetBugCheckPhase(cleanupCtx, b.SessionID, b.Operation)
		if err != nil {
			return ActivityResult{}, err
		}
		if saved.Cleanup {
			if err = db.ReleaseBugWork(cleanupCtx, b.SessionID, b.Operation); err != nil {
				return ActivityResult{}, err
			}
		}
		return checkActivityResult(b, outcome, reason)
	}
	if retained.Cancelled {
		return finish("cancelled", "check cancellation requested", false)
	}
	if retained.Unreliable {
		return finish("blocked", "known unreliable checks require a changed binding", false)
	}
	if err = in.Isolation.Validate(); err != nil {
		return finish("blocked", err.Error(), false)
	}
	if err = observe(); err != nil {
		return finish("blocked", err.Error(), false)
	}
	run := h.run
	if run == nil {
		run = in.Isolation.Run
	}
	var known []string
	for ordinal, command := range phase.Commands {
		if ordinal < len(retained.Commands) {
			old := retained.Commands[ordinal]
			if old.Ordinal != ordinal {
				return ActivityResult{}, errors.New("bug check command ordinal mismatch")
			}
			if old.State != "completed" || old.RerunActive() {
				return checkActivityResult(b, "blocked", "command launch or cleanup remains uncertain; reconciliation required")
			}
			if outcome, reason := bugCommandVerdict(old); outcome != "passed" {
				return finish(outcome, reason, false)
			}
			if old.Attribution != nil {
				known = append(known, bugKnownFailures(*old.Attribution))
			}
			continue
		}
		if err = observe(); err != nil {
			return finish("blocked", err.Error(), false)
		}
		latest, e := db.GetBugCheckPhase(runCtx, b.SessionID, b.Operation)
		if e != nil {
			return ActivityResult{}, e
		}
		if latest.Cancelled {
			return finish("cancelled", "check cancellation requested", false)
		}
		launched, e := db.ClaimBugCheckCommand(runCtx, b.SessionID, b.Operation, ordinal)
		if e != nil {
			return ActivityResult{}, e
		}
		if !launched {
			return checkActivityResult(b, "blocked", "command claim refused or already retained")
		}
		observation := run(runCtx, in.Worktree.Path(), command, func(o bugchecks.CommandResult) error {
			return db.ObserveBugCheckCommand(runCtx, b.SessionID, b.Operation, ordinal, false, checkObservation(o))
		})
		cleanupCtx := context.WithoutCancel(ctx)
		if runCtx.Err() != nil {
			if e = db.CancelBugCheckPhase(cleanupCtx, b.SessionID, b.Operation); e != nil {
				return ActivityResult{}, e
			}
		}
		if e = db.ObserveBugCheckCommand(cleanupCtx, b.SessionID, b.Operation, ordinal, true, checkObservation(observation)); e != nil {
			return ActivityResult{}, e
		}
		// Only an ordinary test failure of a completed, uncancelled command is
		// attributed; a blocked, cancelled or timed-out command never reruns.
		if observation.Outcome == "failed" && command.Rerun != nil && runCtx.Err() == nil {
			outcome, reason, e := h.attribute(ctx, runCtx, db, in, run, ordinal, command, observation, observe)
			if e != nil {
				return ActivityResult{}, e
			}
			if outcome != "passed" {
				return finish(outcome, reason, false)
			}
			known = append(known, reason)
		} else if observation.Outcome != "passed" {
			return finish(observation.Outcome, observation.Reason, false)
		}
		if e = observe(); e != nil {
			return finish("blocked", e.Error(), false)
		}
	}
	if err = observe(); err != nil {
		return finish("blocked", err.Error(), false)
	}
	if len(known) > 0 {
		return finish("passed", BugKnownFailuresPrefix+strings.Join(known, "; "), true)
	}
	return finish("passed", "", true)
}

// BugKnownFailuresPrefix opens the reason of a phase that passed only because
// every failing test was flaky or also failed on the target commit.
const BugKnownFailuresPrefix = "passed with known failures: "

// bugCommandVerdict reads a retained command as the phase counts it.
func bugCommandVerdict(c store.BugCheckCommandRecord) (string, string) {
	if c.Passed() {
		return "passed", ""
	}
	if c.Attribution != nil && c.Attribution.Outcome == "failed" {
		return "failed", c.Attribution.Reason
	}
	return c.Observation.Outcome, c.Observation.Reason
}

// bugKnownFailures names a passing attribution's flaky and pre-existing tests.
func bugKnownFailures(a store.BugCheckAttribution) string {
	var parts []string
	if len(a.Flaky) > 0 {
		parts = append(parts, "flaky "+bugTestList(a.Flaky))
	}
	if len(a.PreExisting) > 0 {
		parts = append(parts, "pre-existing "+bugTestList(a.PreExisting))
	}
	return strings.Join(parts, "; ")
}
func bugTestList(tests []store.BugCheckTest) string {
	out := make([]string, len(tests))
	for i, t := range tests {
		out[i] = t.Name + " (" + t.Package + ")"
	}
	return strings.Join(out, ", ")
}
func bugTests(failed map[string][]string) []store.BugCheckTest {
	var out []store.BugCheckTest
	for _, pkg := range slices.Sorted(maps.Keys(failed)) {
		for _, name := range failed[pkg] {
			out = append(out, store.BugCheckTest{Package: pkg, Name: name})
		}
	}
	return out
}

// bugFailedWithout lists the tests of all that are not in some.
func bugFailedWithout(all, some map[string][]string) map[string][]string {
	out := map[string][]string{}
	for pkg, tests := range all {
		for _, name := range tests {
			if !slices.Contains(some[pkg], name) {
				out[pkg] = append(out[pkg], name)
			}
		}
	}
	return out
}

// attribute reruns a failed command's named failing tests once on the repair
// and, for those still failing, once on the target commit. Tests passing on
// rerun are flaky and tests also failing on the target are pre-existing; only
// tests that fail on the repair and pass on the target are the repair's. The
// command passes only when no repair failure remains. Output that cannot be
// attributed leaves the failure exactly as observed. The returned reason is
// the known failures of a passing command or the phase reason otherwise.
func (h *BugChecksActivity) attribute(ctx, runCtx context.Context, db *store.DB, in BugChecksInput, run bugCheckRunner, ordinal int, command config.BugCheckCommand, failed bugchecks.CommandResult, observe func() error) (string, string, error) {
	b := in.Binding
	settle := func(a store.BugCheckAttribution) (string, string, error) {
		if err := db.AttributeBugCheckCommand(context.WithoutCancel(ctx), b.SessionID, b.Operation, ordinal, a); err != nil {
			return "", "", err
		}
		if a.Outcome == "passed" {
			return "passed", bugKnownFailures(a), nil
		}
		return a.Outcome, a.Reason, nil
	}
	unattributed := func(cause string) (string, string, error) {
		return settle(store.BugCheckAttribution{Outcome: "failed", Reason: failed.Reason + " (not attributed to named tests: " + cause + ")"})
	}
	// ended maps a rerun that ended attribution early to the phase outcome.
	ended := func(v rerunVerdict) (string, string, error) {
		if v.outcome == "unattributed" {
			return unattributed(v.reason)
		}
		return v.outcome, v.reason, nil
	}
	report, err := bugchecks.ParseGoTestFailures(failed.Output, failed.CaptureComplete)
	if err != nil {
		return unattributed(err.Error())
	}
	still, verdict, err := h.rerunTests(ctx, runCtx, db, in, run, ordinal, command, "repair", in.Worktree.Path(), report.Failed, observe)
	if err != nil {
		return "", "", err
	}
	if verdict.outcome != "" {
		return ended(verdict)
	}
	flaky := bugTests(bugFailedWithout(report.Failed, still))
	if len(still) == 0 {
		return settle(store.BugCheckAttribution{Outcome: "passed", Reason: "every failing test passed on rerun", Flaky: flaky})
	}
	if in.Base == nil {
		return unattributed("no target worktree to rerun on")
	}
	base, cleanup, err := in.Base(runCtx, b.TargetCommit)
	if err != nil {
		if runCtx.Err() != nil {
			if err = db.CancelBugCheckPhase(context.WithoutCancel(ctx), b.SessionID, b.Operation); err != nil {
				return "", "", err
			}
			return "cancelled", "check cancellation requested", nil
		}
		return unattributed("target worktree unavailable: " + err.Error())
	}
	// The temporary target worktree holds no evidence; its removal is best
	// effort and runs after cancellation too.
	defer func() { _ = cleanup(context.WithoutCancel(ctx)) }()
	onTarget, verdict, err := h.rerunTests(ctx, runCtx, db, in, run, ordinal, command, "target", base.Path(), still, observe)
	if err != nil {
		return "", "", err
	}
	if verdict.outcome != "" {
		return ended(verdict)
	}
	pre := bugTests(onTarget)
	repair := bugTests(bugFailedWithout(still, onTarget))
	if len(repair) > 0 {
		reason := failed.Reason + "; failing with the repair but passing on " + b.TargetBranch + ": " + bugTestList(repair)
		if known := bugKnownFailures(store.BugCheckAttribution{Flaky: flaky, PreExisting: pre}); known != "" {
			reason += "; also " + known
		}
		return settle(store.BugCheckAttribution{Outcome: "failed", Reason: reason, Flaky: flaky, PreExisting: pre, Repair: repair})
	}
	return settle(store.BugCheckAttribution{Outcome: "passed", Reason: "every remaining failing test also fails on " + b.TargetBranch, Flaky: flaky, PreExisting: pre})
}

// rerunVerdict says why a rerun ended attribution early: "blocked" and
// "cancelled" end the phase so, "unattributed" keeps the command's failure.
type rerunVerdict struct{ outcome, reason string }

// rerunTests runs the named tests once in root under the same isolation and
// cwd, retaining the rerun under the failed command, and returns the tests
// still failing. Every named package must be reported and no unnamed test may
// fail, or the rerun is unattributed. Source and target observations bracket
// the rerun exactly as they bracket a command.
func (h *BugChecksActivity) rerunTests(ctx, runCtx context.Context, db *store.DB, in BugChecksInput, run bugCheckRunner, ordinal int, command config.BugCheckCommand, target, root string, tests map[string][]string, observe func() error) (map[string][]string, rerunVerdict, error) {
	b := in.Binding
	if err := observe(); err != nil {
		return nil, rerunVerdict{"blocked", err.Error()}, nil
	}
	latest, err := db.GetBugCheckPhase(runCtx, b.SessionID, b.Operation)
	if err != nil {
		return nil, rerunVerdict{}, err
	}
	if latest.Cancelled {
		return nil, rerunVerdict{"cancelled", "check cancellation requested"}, nil
	}
	argv, err := bugchecks.ExpandGoTestRerun(command.Rerun.Argv, tests)
	if err != nil {
		return nil, rerunVerdict{"unattributed", err.Error()}, nil
	}
	claimed, err := db.ClaimBugCheckRerun(runCtx, b.SessionID, b.Operation, ordinal, target, bugTests(tests))
	if err != nil {
		return nil, rerunVerdict{}, err
	}
	if !claimed {
		return nil, rerunVerdict{"blocked", "rerun claim refused or already retained"}, nil
	}
	rerun := command
	rerun.Argv, rerun.TimeoutSeconds, rerun.Rerun = argv, command.Rerun.TimeoutSeconds, nil
	result := run(runCtx, root, rerun, func(o bugchecks.CommandResult) error {
		return db.ObserveBugCheckRerun(runCtx, b.SessionID, b.Operation, ordinal, false, checkObservation(o))
	})
	cleanupCtx := context.WithoutCancel(ctx)
	if runCtx.Err() != nil {
		if err = db.CancelBugCheckPhase(cleanupCtx, b.SessionID, b.Operation); err != nil {
			return nil, rerunVerdict{}, err
		}
	}
	if err = db.ObserveBugCheckRerun(cleanupCtx, b.SessionID, b.Operation, ordinal, true, checkObservation(result)); err != nil {
		return nil, rerunVerdict{}, err
	}
	switch result.Outcome {
	case "cancelled":
		return nil, rerunVerdict{"cancelled", "check cancellation requested"}, nil
	case "passed", "failed":
	default:
		return nil, rerunVerdict{"unattributed", target + " rerun " + result.Outcome + ": " + result.Reason}, nil
	}
	report, err := bugchecks.ParseGoTestReport(result.Output, result.CaptureComplete)
	if err != nil {
		return nil, rerunVerdict{"unattributed", target + " rerun: " + err.Error()}, nil
	}
	if result.Outcome == "failed" && len(report.Failed) == 0 {
		return nil, rerunVerdict{"unattributed", target + " rerun failed without a failing package"}, nil
	}
	for pkg, names := range tests {
		failing, reported := report.Failed[pkg]
		if !reported && !slices.Contains(report.Passed, pkg) {
			return nil, rerunVerdict{"unattributed", target + " rerun did not report " + pkg}, nil
		}
		for _, name := range failing {
			if !slices.Contains(names, name) {
				return nil, rerunVerdict{"unattributed", target + " rerun failed an unnamed test"}, nil
			}
		}
	}
	for pkg := range report.Failed {
		if _, named := tests[pkg]; !named {
			return nil, rerunVerdict{"unattributed", target + " rerun failed an unnamed package"}, nil
		}
	}
	if err = observe(); err != nil {
		return nil, rerunVerdict{"blocked", err.Error()}, nil
	}
	return report.Failed, rerunVerdict{}, nil
}
func checkObservation(o bugchecks.CommandResult) store.BugCheckObservation {
	return store.BugCheckObservation{ExecutableDigest: o.ExecutableDigest, Executable: o.Executable, CWD: o.CWD, Environment: o.Environment, Started: o.Started, Ended: o.Ended, Exit: o.Exit, Outcome: o.Outcome, Reason: o.Reason, Output: o.Output, CaptureComplete: o.CaptureComplete, CleanupConfirmed: o.CleanupConfirmed, ProcessID: o.ProcessID, ProcessIdentity: o.ProcessIdentity}
}
func retainCheckSetupBlock(ctx context.Context, db *store.DB, b store.BugCheckBinding, cause error) (ActivityResult, error) {
	state, _, _, err := db.CurrentBugAnalysis(ctx, b.SessionID)
	if err != nil {
		return ActivityResult{}, err
	}
	reason := cause.Error()
	err = db.RecordBugFailure(ctx, b.SessionID, state.Revision, "checks:"+b.Operation+":"+config.BugRecipeDigest([]byte(reason)), reason, []string{b.Operation})
	if err != nil {
		return ActivityResult{}, err
	}
	return checkActivityResult(b, "blocked", reason)
}
