package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// ValidateReviewAccess is the access level every reviewer runs under. Reviewers
// only read: no edit, no command, no approval.
const ValidateReviewAccess = "read"

// ValidateEvidence is one suite command of the tested commit, offered to every
// reviewer as a retained check it may cite. Only a passed one proves anything.
type ValidateEvidence struct {
	ID     string
	Argv   []string
	State  string
	Reason string
}

// ValidateReviewRequest is everything one reviewer needs. It is built from
// factory state only.
type ValidateReviewRequest struct {
	// InstanceID and NodeAttemptID name the review step that starts the
	// reviewer.
	InstanceID    int64
	NodeAttemptID int64
	Role          string
	Attempt       string
	Commit        string
	Tree          string
	Checkout      string
	Access        string
	Stories       []ValidateStory
	Evidence      []ValidateEvidence
	// SnapshotID is the snapshot every story's findings must name.
	SnapshotID string
	// Scope is the trusted result context the reviewer's submission is judged
	// against: its role's coverage, the frozen criteria and the retained checks.
	Scope agentresult.Context
	// StandardsRole is the reviewer role that reads the standards applying to
	// it, when the review node's lf_standards names it. Optional and advisory.
	StandardsRole string
}

// ValidateReviewRunner starts one reviewer and returns its result as retained by
// the shared result route. Run takes one run slot's worth of work; the step has
// already acquired the slot.
type ValidateReviewRunner interface {
	// Run starts the reviewer and waits for its submission. A reviewer that
	// ended without submitting returns a nil result and an error.
	Run(ctx context.Context, req ValidateReviewRequest) (*agentresult.Result, error)
	// Recover resolves a lost reply: the result already retained for the
	// attempt, or false. It never starts anything.
	Recover(ctx context.Context, attempt string) (*agentresult.Result, bool, error)
}

// ValidateSlots meters the run-slot pool. A reviewer takes one slot.
type ValidateSlots interface {
	// TryAcquire takes a slot without waiting. ok is false when none is free.
	TryAcquire(ctx context.Context) (release func(), ok bool, err error)
}

// ValidateReviewActivity runs the three read-only reviewers side by side on a
// scratch checkout detached at the final tested commit, each in its own run
// slot, and records each result. It returns
//
//	done   every reviewer has a recorded answer for the commit
//	retry  a reviewer is still waiting for a run slot; nothing recorded is lost
//
// A step that runs again keeps every recorded answer and starts only the
// reviewers that have none. A reviewer is never started twice for one commit.
type ValidateReviewActivity struct {
	b              ValidateFlowBindings
	initialRelease func()
}

// admit reserves the first missing child's capacity before an attempt is spent.
// The held grant is handed to Execute; later children are acquired without waiting.
func (h ValidateReviewActivity) admit(ctx context.Context, opts Options, req ActivityRequest) (bool, func(), error) {
	if h.b.Slots == nil {
		return true, nil, nil
	}
	suite, err := latestValidateSuite(ctx, opts, req.InstanceID, req.Attrs["lf_suite"])
	if err != nil {
		return false, nil, err
	}
	rows, err := opts.DB.ListValidateReviews(ctx, req.InstanceID, suite.Output.Commit)
	if err != nil {
		return false, nil, err
	}
	finished := map[string]bool{}
	for _, row := range rows {
		finished[row.Role] = row.State != store.ReviewPending
		if finished[row.Role] || row.Attempt == "" || h.b.Reviewers == nil {
			continue
		}
		result, found, err := h.b.Reviewers.Recover(ctx, row.Attempt)
		if err != nil {
			return false, nil, err
		}
		if !found {
			continue
		}
		if h.b.Stories == nil {
			continue
		}
		stories, err := h.b.Stories(ctx, opts, req)
		if err != nil {
			return false, nil, err
		}
		if err = h.record(ctx, opts, row, row.Role, stories, suite, result, nil); err != nil {
			return false, nil, err
		}
		finished[row.Role] = true
	}
	needed := false
	for _, role := range ValidateReviewRoles {
		if !finished[role] {
			needed = true
			break
		}
	}
	if !needed {
		return true, nil, nil
	}
	if weighted, ok := h.b.Slots.(WeightedValidateSlots); ok {
		_, total := weighted.Capacity()
		parent := 0
		if stepCapacity(ctx) != nil {
			parent, err = graphexec.SlotCost(h.b.Profile, req.Node)
			if err != nil {
				return false, nil, err
			}
		}
		if parent >= total {
			return false, nil, nil
		}
	}
	var release func()
	var ok bool
	if weighted, supports := h.b.Slots.(WeightedValidateSlots); supports {
		release, ok, err = weighted.TryAcquireWeighted(ctx, 1)
	} else {
		release, ok, err = h.b.Slots.TryAcquire(ctx)
	}
	if !ok || err != nil {
		return ok, nil, err
	}
	return true, sync.OnceFunc(release), nil
}

func (h ValidateReviewActivity) capacityDiagnostic(ctx context.Context, profile *graphexec.Profile, node string) string {
	used, total := 0, 0
	if weighted, ok := h.b.Slots.(WeightedValidateSlots); ok {
		used, total = weighted.Capacity()
	}
	cost := 1
	if stepCapacity(ctx) != nil {
		if parent, err := graphexec.SlotCost(profile, node); err == nil {
			cost += parent
		}
	}
	return capacityDiagnostic("shared", "validate", node+"/reviewer", cost, used, total)
}

// ValidateReviewRoles are the reviewer role names the graph's review step names,
// in order, mapped to their review roles.
var ValidateReviewRoles = []string{agent.ReviewRoleAcceptance, agent.ReviewRoleCode, agent.ReviewRoleArchitecture}

// Execute reviews the commit of the latest suite result.
func (h ValidateReviewActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	b := h.b
	step, err := loadValidateStep(ctx, opts, req, b.Profile, graphexec.ValidateReviewAction)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	if b.Reviewers == nil || b.Slots == nil || b.Repo == nil || b.Stories == nil {
		err := errors.New("validate review requires reviewers, run slots, a repository and the stories under review")
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	suite, err := latestValidateSuite(ctx, opts, req.InstanceID, step.node.Attrs["lf_suite"])
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	commit := suite.Output.Commit
	rows, err := opts.DB.ListValidateReviews(ctx, req.InstanceID, commit)
	if err != nil {
		return ActivityResult{}, err
	}
	have := map[string]store.ValidateReview{}
	for _, r := range rows {
		have[r.Role] = r
	}
	var missing []string
	for _, role := range ValidateReviewRoles {
		if r, ok := have[role]; !ok || r.State == store.ReviewPending {
			missing = append(missing, role)
		}
	}
	if len(missing) == 0 {
		return h.result("done", suite, rows)
	}
	stories, err := b.Stories(ctx, opts, req)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, fmt.Errorf("read the stories under review: %w", err)
	}

	// Resolve lost replies first, before any slot or checkout is spent: a
	// reviewer that already submitted is recorded, never started again.
	var toStart []string
	for _, role := range missing {
		row, pending := have[role]
		if pending && row.Attempt != "" {
			recovered, found, err := b.Reviewers.Recover(ctx, row.Attempt)
			if err != nil {
				return ActivityResult{Diagnostic: err.Error()}, fmt.Errorf("resolve the %s reviewer's lost reply: %w", role, err)
			}
			if found {
				if err := h.record(ctx, opts, row, role, stories, suite, recovered, nil); err != nil {
					return ActivityResult{}, err
				}
				continue
			}
		}
		toStart = append(toStart, role)
	}
	if len(toStart) > 0 {
		if err := h.start(ctx, opts, req, suite, stories, have, toStart); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
	}
	rows, err = opts.DB.ListValidateReviews(ctx, req.InstanceID, commit)
	if err != nil {
		return ActivityResult{}, err
	}
	finished := map[string]bool{}
	for _, r := range rows {
		finished[r.Role] = r.State != store.ReviewPending
	}
	for _, role := range ValidateReviewRoles {
		if !finished[role] {
			result, err := h.result("retry", suite, rows)
			result.Deferred = true
			result.Diagnostic = h.capacityDiagnostic(ctx, b.Profile, req.Node)
			return result, err
		}
	}
	return h.result("done", suite, rows)
}

func (h ValidateReviewActivity) result(outcome string, suite validateSuiteView, rows []store.ValidateReview) (ActivityResult, error) {
	states := map[string]string{}
	for _, r := range rows {
		states[r.Role] = r.State
	}
	return validateJSONResult(outcome, map[string]any{"commit": suite.Output.Commit, "reviewers": states})
}

// start takes a slot per reviewer, one by one, opens the scratch checkout once
// and runs the reviewers that got a slot side by side. A reviewer without a slot
// is left for the next dispatch.
func (h ValidateReviewActivity) start(ctx context.Context, opts Options, req ActivityRequest, suite validateSuiteView, stories []ValidateStory, _ map[string]store.ValidateReview, roles []string) error {
	b := h.b
	type slot struct {
		role    string
		release func()
	}
	var granted []slot
	releaseAll := func() {
		for _, s := range granted {
			s.release()
		}
	}
	for index, role := range roles {
		var release func()
		var ok bool
		var err error
		if index == 0 && h.initialRelease != nil {
			release, ok = h.initialRelease, true
		} else if weighted, supports := b.Slots.(WeightedValidateSlots); supports {
			release, ok, err = weighted.TryAcquireWeighted(ctx, 1)
		} else {
			release, ok, err = b.Slots.TryAcquire(ctx)
		}
		if err != nil {
			releaseAll()
			return fmt.Errorf("take a run slot for the %s reviewer: %w", role, err)
		}
		if !ok {
			// Reviewers queue one by one: no later reviewer jumps the queue.
			break
		}
		granted = append(granted, slot{role, release})
	}
	if len(granted) == 0 {
		return nil
	}
	defer releaseAll()

	scratch, err := b.Repo.Begin(ctx, suite.Output.Commit)
	if err != nil {
		return fmt.Errorf("prepare the review checkout: %w", err)
	}
	defer func() { _ = scratch.Close(context.WithoutCancel(ctx)) }()
	tree, err := scratch.Observe(ctx)
	var refusal *git.SourceSymlinkError
	if err != nil && ctx.Err() == nil && errors.As(err, &refusal) {
		// A symlink refusal is a repository finding: each granted reviewer is
		// recorded as failed with the reason, and no reviewer runs, so Validate
		// ends blocked with an owner-visible reason instead of a stopped turn.
		for _, g := range granted {
			attempt := validateReviewAttempt(req.InstanceID, g.role, suite.Output.Commit)
			row, _, err := opts.DB.BeginValidateReview(ctx, store.ValidateReview{InstanceID: req.InstanceID, Role: g.role, Commit: suite.Output.Commit, SuiteRunID: suite.Output.RunID, Attempt: attempt, StartedAt: b.now(opts)})
			if err != nil {
				return err
			}
			if row.State != store.ReviewPending {
				continue
			}
			if err := opts.DB.FinishValidateReview(ctx, row.ID, store.ReviewFailed, clipText(refusal.Error(), 500), nil, b.now(opts)); err != nil {
				return err
			}
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("observe the review checkout: %w", err)
	}
	drift := suite.Tree != "" && tree != suite.Tree

	evidence, err := h.evidence(ctx, opts, suite)
	if err != nil {
		return err
	}
	snapshot := validateSnapshotID(suite.Output.Commit)
	digest := validateImplementationDigest(suite.Output.Commit, suite.Tree)
	type started struct {
		row store.ValidateReview
		req ValidateReviewRequest
	}
	var runs []started
	for _, g := range granted {
		attempt := validateReviewAttempt(req.InstanceID, g.role, suite.Output.Commit)
		row, _, err := opts.DB.BeginValidateReview(ctx, store.ValidateReview{InstanceID: req.InstanceID, Role: g.role, Commit: suite.Output.Commit, TreeDigest: tree, SuiteRunID: suite.Output.RunID, Attempt: attempt, StartedAt: b.now(opts)})
		if err != nil {
			return err
		}
		if row.State != store.ReviewPending {
			continue
		}
		if drift {
			if err := opts.DB.FinishValidateReview(ctx, row.ID, store.ReviewFailed, "snapshot drift: the checked-out source differs from the tested source", nil, b.now(opts)); err != nil {
				return err
			}
			continue
		}
		scope := validateReviewScope(g.role, stories, snapshot, digest, evidence)
		runs = append(runs, started{row, ValidateReviewRequest{
			InstanceID: req.InstanceID, NodeAttemptID: req.NodeAttemptID,
			Role: g.role, Attempt: attempt, Commit: suite.Output.Commit, Tree: tree, Checkout: scratch.Path(), Access: ValidateReviewAccess,
			Stories: stories, Evidence: evidence, SnapshotID: snapshot, Scope: scope,
			StandardsRole: graphexec.StandardsRole(req.Attrs, "validate-"+g.role),
		}})
	}
	results := make([]*agentresult.Result, len(runs))
	errs := make([]error, len(runs))
	var wg sync.WaitGroup
	for i := range runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = b.Reviewers.Run(ctx, runs[i].req)
		}()
	}
	wg.Wait()
	var faults []error
	for i, r := range runs {
		if ctx.Err() != nil {
			// A shutdown is not the reviewer's failure; its row stays pending.
			return ctx.Err()
		}
		if errs[i] != nil && results[i] == nil {
			// The reply may have been lost after the reviewer submitted.
			if recovered, found, rerr := b.Reviewers.Recover(ctx, r.row.Attempt); rerr == nil && found {
				results[i], errs[i] = recovered, nil
			}
		}
		if err := h.record(ctx, opts, r.row, r.req.Role, stories, suite, results[i], errs[i]); err != nil {
			faults = append(faults, err)
		}
	}
	return errors.Join(faults...)
}

// record judges one reviewer's result and retains it. Only a complete, valid,
// completed submission fills findings; anything else is failed or malformed and
// leaves its part unresolved.
func (h ValidateReviewActivity) record(ctx context.Context, opts Options, row store.ValidateReview, role string, stories []ValidateStory, suite validateSuiteView, result *agentresult.Result, runErr error) error {
	now := h.b.now(opts)
	finish := func(state, reason string, findings json.RawMessage) error {
		return opts.DB.FinishValidateReview(context.WithoutCancel(ctx), row.ID, state, clipText(reason, 500), findings, now)
	}
	if result == nil {
		reason := "the reviewer gave no result"
		if runErr != nil {
			reason += ": " + runErr.Error()
		}
		return finish(store.ReviewFailed, reason, nil)
	}
	if result.Outcome != "completed" {
		return finish(store.ReviewFailed, "the reviewer could not finish: "+strings.TrimSpace(result.Reason), nil)
	}
	evidence, err := h.evidence(ctx, opts, suite)
	if err != nil {
		return err
	}
	scope := validateReviewScope(role, stories, validateSnapshotID(suite.Output.Commit), validateImplementationDigest(suite.Output.Commit, suite.Tree), evidence)
	if d := agentresult.ValidateResult(*result, scope); !d.Valid || !d.Complete {
		return finish(store.ReviewMalformed, "the reviewer's result is not valid: "+clipText(diagnosticSummary(d), 300), nil)
	}
	if result.Payload.Validate == nil {
		return finish(store.ReviewMalformed, "the reviewer's result carries no assessment", nil)
	}
	findings, err := json.Marshal(result.Payload.Validate.Stories)
	if err != nil {
		return err
	}
	return finish(store.ReviewAccepted, "", findings)
}

func diagnosticSummary(d agentresult.Diagnostics) string {
	var parts []string
	for _, v := range d.Diagnostics {
		parts = append(parts, v.Path+" "+v.Code)
	}
	if len(parts) == 0 {
		return "incomplete result"
	}
	return strings.Join(parts, "; ")
}

// evidence lists the suite commands of the tested run as retained checks.
func (h ValidateReviewActivity) evidence(ctx context.Context, opts Options, suite validateSuiteView) ([]ValidateEvidence, error) {
	if suite.Output.RunID == 0 {
		return nil, nil
	}
	commands, err := opts.DB.SuiteCommands(ctx, suite.Output.RunID)
	if err != nil {
		return nil, err
	}
	out := make([]ValidateEvidence, 0, len(commands))
	for _, c := range commands {
		out = append(out, ValidateEvidence{ID: validateCheckID(suite.Output.RunID, c.Position), Argv: c.Argv, State: c.State, Reason: c.Reason})
	}
	return out, nil
}

func validateCheckID(runID int64, position int) string {
	return fmt.Sprintf("suite-%d-%d", runID, position)
}

func validateSnapshotID(commit string) string { return "commit-" + commit }

// validateImplementationDigest binds the evidence to the commit and the content
// digest of its tree.
func validateImplementationDigest(commit, tree string) string {
	sum := sha256.Sum256([]byte("commit:" + commit + "\ntree:" + tree))
	return hex.EncodeToString(sum[:])
}

func validateReviewAttempt(instance int64, role, commit string) string {
	return fmt.Sprintf("validate-review-%d-%s-%s", instance, role, commit[:12])
}

// validateReviewScope builds the trusted context a reviewer's result is judged
// against: required coverage for its role, the criteria whose frozen text
// needs executed proof (all but explicit (Manual) ones), and the suite commands of the commit as the only citable checks. A
// check proves only when it passed.
func validateReviewScope(role string, stories []ValidateStory, snapshot, digest string, evidence []ValidateEvidence) agentresult.Context {
	scope := agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: string(agent.ModeValidate), WorkflowVersion: store.WorkflowSeparated, ReviewRole: role, Stories: map[string]agent.ContributionStory{}}
	for _, s := range stories {
		checks := map[string]agent.RetainedContributionCheck{}
		for _, e := range evidence {
			checks[e.ID] = agent.RetainedContributionCheck{
				Story: s.Key, Revision: s.Revision, SnapshotID: snapshot, ImplementationDigest: digest,
				Executed: e.State == store.SuitePassed || e.State == store.SuiteFailed, Passed: e.State == store.SuitePassed, Retained: true,
			}
		}
		scope.Stories[s.Key] = agent.ContributionStory{
			Revision: s.Revision, SnapshotID: snapshot, ImplementationDigest: digest, SnapshotRetained: true,
			Criteria: append([]string(nil), s.Criteria...), ExecutionRequired: append([]string(nil), s.ExecutionRequired...), Checks: checks,
		}
	}
	return scope
}
