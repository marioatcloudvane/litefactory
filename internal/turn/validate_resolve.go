package turn

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// ValidateResolveActivity applies the owner's reply. It runs no remote effect.
// It checks who wrote the reply, the exact grammar and the report revision;
// records a verdict or a choice, or the owner's confirmation of one effect; and
// returns one outcome:
//
//	wait          nothing to act on (no reply yet, a refused command, or a
//	              recorded verdict or choice)
//	create_issue  the owner chose an issue for a finding
//	repair        the owner chose repair, and every blocking finding of the
//	              tested commit now has a choice: one repair round starts for
//	              all findings chosen for repair (only when the graph declares
//	              the outcome; otherwise the choice waits as before)
//	push          the owner confirmed push
//	merge         the owner confirmed merge
//	close         the owner confirmed close
//	stale         the branch head or the report revision moved
//	implement     the owner sent the blocking findings back to Implement (only
//	              when the graph declares the outcome)
//
// /validate-ship retains an exact reasoned exception without changing factual
// evidence, then returns the first undone delivery step. Push and merge come
// back here; only while the reply is that /validate-ship does a pass without a
// new owner comment return the next step, so one confirmation never implies the
// next except the explicit ship command.
//
// A refused command changes nothing but the notice shown to the owner.
type ValidateResolveActivity struct{ b ValidateFlowBindings }

// Execute resolves one entry of the step.
func (h ValidateResolveActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	step, err := loadValidateStep(ctx, opts, req, h.b.Profile, graphexec.ValidateResolveAction)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	repairs := slices.Contains(step.node.Outcomes, "repair")
	implements := slices.Contains(step.node.Outcomes, "implement")
	if !h.b.Delivery.ready() {
		return ActivityResult{Diagnostic: "validate owner steps lack delivery bindings"}, errors.New("validate owner steps lack delivery bindings")
	}
	st, err := LoadValidateState(ctx, opts, h.b.Profile, req.InstanceID)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	wait, err := opts.DB.GetGraphOwnerWait(ctx, req.InstanceID)
	if errors.Is(err, store.ErrNotFound) {
		// The first pass after the outcome.
		return validateResolveResult("wait", "no owner reply", st)
	}
	if err != nil {
		return ActivityResult{}, err
	}
	now := h.b.now(opts)
	refuse := func(outcome, why string) (ActivityResult, error) {
		if err := saveValidateNotice(ctx, opts, req.InstanceID, st.Revision, why, now); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		return validateResolveResult(outcome, why, st)
	}
	if wait.State != "allocated" || wait.ContinuationAttemptID != req.NodeAttemptID {
		// A pass with no reply of its own. Only an explicit /validate-ship that
		// an earlier pass of this step accepted carries on to the next delivery
		// step; after any other command the run waits for the owner again.
		reentered, err := validateShipReentry(ctx, opts, req, wait)
		if err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		if !reentered {
			return validateResolveResult("wait", "no owner reply", st)
		}
		return h.continueShip(ctx, opts, req, st, wait, now, refuse)
	}

	owner, err := h.b.Delivery.OwnerID(ctx, opts)
	if opts.ProjectApprovers && !isValidateCodeControlBody(wait.ReplyBody) {
		owner, err = ticketOwnerID(ctx, opts)
	} else if alt, ok := ticketCodeOwnerID(ctx, opts); ok && wait.ReplyAuthorID == alt {
		owner, err = alt, nil
	}
	if err != nil || owner < 1 {
		return ActivityResult{Diagnostic: "the owner identity cannot be resolved"}, errors.Join(errors.New("the owner identity cannot be resolved"), err)
	}
	if wait.ReplyAuthorID != owner {
		approver := "configured owner"
		if opts.ProjectApprovers {
			approver = "configured code approver"
			if !isValidateCodeControlBody(wait.ReplyBody) {
				approver = "configured ticket approver"
			}
		}
		return refuse("wait", "Only the owner can give Validate commands. Ask the "+approver+" to make their own decision using the current report's choices; this reply gave no permission. "+validateCurrentGuidance(ctx, opts, req, st))
	}
	if st.Waiver != nil && st.Waiver.CommentID == wait.ReplyID {
		return h.continueShip(ctx, opts, req, st, wait, now, refuse)
	}
	// A reply this run already acted on (a restart replays the step) goes to the
	// same step again. Every step recovers on its own, so nothing is done twice.
	if outcome, ok := replayedValidateReply(ctx, opts, st, wait.ReplyID, repairs); ok {
		if isValidateShipReply(wait.ReplyBody) {
			// A ship reply may have recorded its checks and choices without
			// reaching its first step: carry on from what is recorded.
			return h.continueShip(ctx, opts, req, st, wait, now, refuse)
		}
		return validateResolveResult(outcome, "replayed", st)
	}
	cmd, binding, err := ResolveValidateDecision(ctx, opts, req, st, wait)
	if err != nil {
		return refuse("wait", err.Error())
	}
	if cmd.Revision != st.Revision {
		return refuse("stale", fmt.Sprintf("The command names revision `%s`, but the current report revision is `%s`. %s", cmd.Revision, st.Revision, validateCurrentGuidance(ctx, opts, req, st)))
	}
	delivery := st.Commit
	if cmd.Name == ValidateShipCommand && binding.Display.DeliveryCommit != "" {
		delivery = binding.Display.DeliveryCommit
	}
	head, err := h.b.Delivery.Head(ctx)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, fmt.Errorf("read the branch head: %w", err)
	}
	if head != delivery {
		return refuse("stale", fmt.Sprintf("The branch head is now `%s`, not the displayed delivery commit `%s`. That decision is stale. Comment `%s` to validate the current head before making a new delivery choice. Nothing was approved or delivered.", short(head), short(delivery), ValidateStartCommand))
	}
	notReviewed := func(id string) (ActivityResult, error) {
		return refuse("wait", fmt.Sprintf("`%s` was not reviewed: its reviewer has no usable result, so it takes no check or choice. %s", id, validateRetryStep(st.Commit)))
	}
	switch cmd.Name {
	case ValidateCheckCommand:
		f, ok := st.finding(cmd.Criterion)
		if ok && f.State == findingNotReviewed {
			return notReviewed(f.ID)
		}
		if !ok || !f.checkable() {
			return refuse("wait", fmt.Sprintf("`%s` does not await an eligible owner check.", cmd.Criterion))
		}
		if _, err := opts.DB.PutValidateVerdict(ctx, store.ValidateVerdict{InstanceID: req.InstanceID, Commit: st.Commit, Criterion: f.ID, Verdict: cmd.Verdict, Evidence: cmd.Evidence, CommentID: wait.ReplyID, AuthorID: wait.ReplyAuthorID, CreatedAt: now}); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		return h.accept(ctx, opts, req, st, now, "wait", "check recorded")
	case ValidateFindingCommand:
		f, ok := st.finding(cmd.Finding)
		switch {
		case !ok:
			return refuse("wait", fmt.Sprintf("`%s` is not a finding of this run.", cmd.Finding))
		case f.State == findingNotReviewed:
			return notReviewed(f.ID)
		case f.State == findingAwaitingCheck:
			return refuse("wait", fmt.Sprintf("`%s` awaits your check first: record pass or fail with evidence.", f.ID))
		case !f.needsChoice():
			return refuse("wait", fmt.Sprintf("`%s` is %s and takes no choice.", f.ID, f.State))
		case cmd.Choice == validateChoiceAccept && !f.acceptable():
			return refuse("wait", fmt.Sprintf("`%s` cannot be accepted. Accept is only for blocking review findings (architecture, plan, code), not criteria or the suite.", f.ID))
		case cmd.Choice == validateChoiceAccept:
			// The owner accepts a deliberate deviation: an owner pass verdict on
			// the tested commit, with the reason as its evidence.
			if _, err := opts.DB.PutValidateVerdict(ctx, store.ValidateVerdict{InstanceID: req.InstanceID, Commit: st.Commit, Criterion: f.ID, Verdict: store.VerdictPass, Evidence: cmd.Evidence, CommentID: wait.ReplyID, AuthorID: wait.ReplyAuthorID, CreatedAt: now}); err != nil {
				return ActivityResult{Diagnostic: err.Error()}, err
			}
			return h.accept(ctx, opts, req, st, now, "wait", "deviation accepted")
		case cmd.Choice == store.ChoiceDefer && !f.Optional:
			return refuse("wait", fmt.Sprintf("`%s` is blocking. Defer is allowed only for optional suggestions; choose repair or issue.", f.ID))
		}
		if _, err := opts.DB.PutValidateChoice(ctx, store.ValidateChoice{InstanceID: req.InstanceID, Commit: st.Commit, Finding: f.ID, Choice: cmd.Choice, IssueKind: cmd.IssueKind, CommentID: wait.ReplyID, AuthorID: wait.ReplyAuthorID, CreatedAt: now}); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		if cmd.Choice == store.ChoiceRepair && repairs {
			after, err := LoadValidateState(ctx, opts, h.b.Profile, req.InstanceID)
			if err != nil {
				return ActivityResult{Diagnostic: err.Error()}, err
			}
			if after.ownerRepairReady() {
				return h.accept(ctx, opts, req, after, now, "repair", "repair chosen")
			}
		}
		if cmd.Choice != store.ChoiceIssue {
			return h.accept(ctx, opts, req, st, now, "wait", "choice recorded")
		}
		if _, _, err := opts.DB.BeginValidateEffect(ctx, store.ValidateEffect{InstanceID: req.InstanceID, Kind: store.EffectIssue, Commit: st.Commit, Target: f.ID, ConfirmCommentID: wait.ReplyID, CreatedAt: now}); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		return h.accept(ctx, opts, req, st, now, "create_issue", "issue chosen")
	case ValidatePushCommand:
		switch {
		case !st.Ready:
			return refuse("wait", "Push is not offered yet: "+strings.Join(st.Open, " "))
		case effectDone(st.Effects.Push) && effectDone(st.Effects.PR):
			return refuse("wait", "The tested commit is already pushed and its pull request exists.")
		}
		if _, _, err := opts.DB.BeginValidateEffect(ctx, store.ValidateEffect{InstanceID: req.InstanceID, Kind: store.EffectPush, Commit: st.Commit, Target: st.Commit, ConfirmCommentID: wait.ReplyID, CreatedAt: now}); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		if st.Effects.PR != nil && !effectDone(st.Effects.PR) {
			if _, _, err := opts.DB.BeginValidateEffect(ctx, store.ValidateEffect{InstanceID: req.InstanceID, Kind: store.EffectPR, Commit: st.Commit, Target: st.Commit, ConfirmCommentID: wait.ReplyID, CreatedAt: now}); err != nil {
				return ActivityResult{Diagnostic: err.Error()}, err
			}
		}
		return h.accept(ctx, opts, req, st, now, "push", "push confirmed")
	case ValidateMergeCommand:
		pr := decodeEffectResult(st.Effects.PR)
		switch {
		case !effectDone(st.Effects.Push) || !effectDone(st.Effects.PR) || pr.Number < 1:
			return refuse("wait", "Merge is refused: the tested commit is not pushed yet.")
		case effectDone(st.Effects.Merge):
			return refuse("wait", "The pull request is already merged.")
		case cmd.PR != pr.Number || cmd.Head != pr.Head || pr.Head != st.Commit:
			return refuse("wait", fmt.Sprintf("Merge is refused: the pull request is #%d at head `%s`; the command named #%d at `%s`.", pr.Number, pr.Head, cmd.PR, short(cmd.Head)))
		}
		if _, _, err := opts.DB.BeginValidateEffect(ctx, store.ValidateEffect{InstanceID: req.InstanceID, Kind: store.EffectMerge, Commit: st.Commit, Target: fmt.Sprintf("%d@%s", pr.Number, pr.Head), ConfirmCommentID: wait.ReplyID, CreatedAt: now}); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		return h.accept(ctx, opts, req, st, now, "merge", "merge confirmed")
	case ValidateCloseCommand:
		target, err := h.b.Delivery.Target(ctx, opts, req)
		if err != nil {
			return ActivityResult{Diagnostic: err.Error()}, fmt.Errorf("resolve the issues to close: %w", err)
		}
		set := target.CloseSet()
		listed := slices.Clone(cmd.Issues)
		slices.Sort(listed)
		switch {
		case !effectDone(st.Effects.Merge):
			return refuse("wait", "Close is refused: the pull request is not merged yet.")
		case effectDone(st.Effects.Close):
			return refuse("wait", "The accepted issues are already closed.")
		case !slices.Equal(listed, set):
			return refuse("wait", fmt.Sprintf("Close must name exactly the displayed issues: %s.", validateNumbers(set)))
		}
		if _, _, err := opts.DB.BeginValidateEffect(ctx, store.ValidateEffect{InstanceID: req.InstanceID, Kind: store.EffectClose, Commit: st.Commit, Target: validateNumbers(set), ConfirmCommentID: wait.ReplyID, CreatedAt: now}); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		return h.accept(ctx, opts, req, st, now, "close", "close confirmed")
	case ValidateReturnCommand:
		switch {
		case !implements:
			return refuse("wait", "This Validate run has no return to Implement step.")
		case st.Reimplementing:
			return refuse("wait", "This reviewed work was already returned to Implement.")
		case !st.Assessed || len(st.Stories) == 0:
			return refuse("wait", "Return requires retained reviewed work.")
		}
		return h.accept(ctx, opts, req, st, now, "implement", "return confirmed")
	case ValidateImplementCommand:
		switch {
		case !implements:
			return refuse("wait", "This Validate run has no step that hands findings back to Implement. "+validateNewSnapshotStep)
		case st.Reimplementing:
			return refuse("wait", fmt.Sprintf("The findings of commit `%s` were already sent back to Implement.", short(st.Commit)))
		case !st.Assessed:
			return refuse("wait", "/validate-implement is refused: the assessment of this commit is not recorded yet.")
		case len(st.implementTargets()) == 0:
			return refuse("wait", fmt.Sprintf("/validate-implement is refused: no story has a failed or unverified criterion or a blocking review, and Validate handed back no failed suite, on commit `%s`, so Implement has nothing to repair.", short(st.Commit)))
		}
		return h.accept(ctx, opts, req, st, now, "implement", "implement confirmed")
	case ValidateShipCommand:
		if opts.ProjectApprovers && validateShipSettlesTicketDecisions(st) {
			approver, err := ticketOwnerID(ctx, opts)
			if err != nil || wait.ReplyAuthorID != approver {
				return refuse("wait", "The project ticket approver must settle acceptance checks and finding choices before the code owner can ship.")
			}
		}
		if !st.Assessed {
			return refuse("wait", "The assessment of this commit is not recorded yet.")
		}
		if effectDone(st.Effects.Close) {
			return refuse("wait", "Delivery is already complete.")
		}
		if err := retainValidateShippingWaiver(ctx, opts, req, st, wait, delivery, cmd.Evidence, now); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		after, err := LoadValidateState(ctx, opts, h.b.Profile, req.InstanceID)
		if err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		return h.shipStep(ctx, opts, req, after, wait, now)
	}
	return refuse("wait", "Unknown Validate command.")
}

// validateShipReentry says this pass re-enters the step on a /validate-ship
// reply an earlier, finished pass of the same step in this run accepted: the
// wait is still allocated to that pass and no new owner comment arrived.
func validateShipReentry(ctx context.Context, opts Options, req ActivityRequest, wait store.GraphOwnerWait) (bool, error) {
	if wait.State != "allocated" || wait.ContinuationAttemptID < 1 || wait.ContinuationAttemptID == req.NodeAttemptID {
		return false, nil
	}
	if !isValidateShipReply(wait.ReplyBody) {
		return false, nil
	}
	previous, err := opts.DB.GetGraphNodeAttempt(ctx, wait.ContinuationAttemptID)
	if err != nil {
		return false, err
	}
	current, err := opts.DB.GetGraphNodeAttempt(ctx, req.NodeAttemptID)
	if err != nil {
		return false, err
	}
	return previous.InstanceID == req.InstanceID && current.InstanceID == req.InstanceID && previous.Node == current.Node &&
		previous.Status == store.GraphSucceeded && current.ID > previous.ID, nil
}

// isValidateShipReply says the reply is a well-formed /validate-ship command.
func isValidateShipReply(body string) bool {
	cmd, err := ParseValidateCommand(body)
	return err == nil && cmd.Name == ValidateShipCommand
}

// continueShip carries an accepted /validate-ship on to the next delivery step.
// The owner and the branch head are checked again; the report revision is not,
// because the ship's own records and effects have moved it since the command.
func (h ValidateResolveActivity) continueShip(ctx context.Context, opts Options, req ActivityRequest, st ValidateState, wait store.GraphOwnerWait, now time.Time, refuse func(outcome, why string) (ActivityResult, error)) (ActivityResult, error) {
	owner, err := h.b.Delivery.OwnerID(ctx, opts)
	if alt, ok := ticketCodeOwnerID(ctx, opts); ok && wait.ReplyAuthorID == alt {
		owner, err = alt, nil
	}
	if err != nil || owner < 1 {
		return ActivityResult{Diagnostic: "the owner identity cannot be resolved"}, errors.Join(errors.New("the owner identity cannot be resolved"), err)
	}
	if wait.ReplyAuthorID != owner {
		approver := "configured owner"
		if opts.ProjectApprovers {
			approver = "configured code approver"
		}
		return refuse("wait", "Only the owner can give Validate commands. Ask the "+approver+" to give their own reasoned shipping decision using the current report's choices; this reply gave no permission. "+validateCurrentGuidance(ctx, opts, req, st))
	}
	head, err := h.b.Delivery.Head(ctx)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, fmt.Errorf("read the branch head: %w", err)
	}
	if head != st.deliveryCommit() {
		return refuse("stale", fmt.Sprintf("The branch head is now `%s`, not the displayed delivery commit `%s`. A fresh displayed decision is required.", short(head), short(st.deliveryCommit())))
	}
	if !st.waiverCurrent() || st.Waiver.CommentID != wait.ReplyID || st.Waiver.Body != wait.ReplyBody {
		return refuse("stale", "The retained shipping decision no longer matches the evidence or owner input; give a fresh decision.")
	}
	return h.shipStep(ctx, opts, req, st, wait, now)
}

// shipStep records the intent of the first undone delivery step of a
// /validate-ship reply and returns it: push with the pull request, then merge
// at the recorded pull request number and head, then close of the displayed
// issues. Each intent carries the ship reply as its confirmation, so a later
// ship comment re-arms a failed or uncertain step and finished steps are never
// repeated. A step this reply already tried and that failed stops the ship.
func (h ValidateResolveActivity) shipStep(ctx context.Context, opts Options, req ActivityRequest, st ValidateState, wait store.GraphOwnerWait, now time.Time) (ActivityResult, error) {
	stop := func(why string) (ActivityResult, error) {
		if err := saveValidateNotice(ctx, opts, req.InstanceID, st.Revision, why, now); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		return validateResolveResult("wait", why, st)
	}
	tried := func(e *store.ValidateEffect) bool {
		return e != nil && e.ConfirmCommentID == wait.ReplyID && (e.State == store.EffectFailed || e.State == store.EffectUncertain)
	}
	retry := fmt.Sprintf(" Repeat %s to continue from this step.", strings.Trim(st.shipLine(), "`"))
	begin := func(kind, target string) error {
		_, _, err := opts.DB.BeginValidateEffect(ctx, store.ValidateEffect{InstanceID: req.InstanceID, Kind: kind, Commit: st.deliveryCommit(), Target: target, ConfirmCommentID: wait.ReplyID, CreatedAt: now})
		return err
	}
	switch {
	case !st.Ready:
		return stop("/validate-ship stopped: " + strings.Join(st.Open, " "))
	case !effectDone(st.Effects.Push) || !effectDone(st.Effects.PR):
		if tried(st.Effects.Push) || tried(st.Effects.PR) {
			return stop("/validate-ship stopped at push: the push or the pull request did not succeed." + retry)
		}
		if err := begin(store.EffectPush, st.deliveryCommit()); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		if st.Effects.PR != nil && !effectDone(st.Effects.PR) {
			if err := begin(store.EffectPR, st.deliveryCommit()); err != nil {
				return ActivityResult{Diagnostic: err.Error()}, err
			}
		}
		return h.accept(ctx, opts, req, st, now, "push", "ship: push")
	case !effectDone(st.Effects.Merge):
		pr := decodeEffectResult(st.Effects.PR)
		if pr.Number < 1 || pr.Head != st.deliveryCommit() {
			return stop(fmt.Sprintf("/validate-ship stopped at merge: pull request #%d is at head `%s`, not the displayed delivery commit `%s`.", pr.Number, short(pr.Head), short(st.deliveryCommit())))
		}
		if tried(st.Effects.Merge) && st.mergeConflicted() {
			return stop("The ship stopped at merge: " + st.mergeConflictStep())
		}
		if tried(st.Effects.Merge) {
			return stop("/validate-ship stopped at merge: the merge did not succeed." + retry)
		}
		if err := begin(store.EffectMerge, fmt.Sprintf("%d@%s", pr.Number, pr.Head)); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		return h.accept(ctx, opts, req, st, now, "merge", "ship: merge")
	case !effectDone(st.Effects.Close):
		if tried(st.Effects.Close) {
			return stop("/validate-ship stopped at close: closing the issues did not finish." + retry)
		}
		target, err := h.b.Delivery.Target(ctx, opts, req)
		if err != nil {
			return ActivityResult{Diagnostic: err.Error()}, fmt.Errorf("resolve the issues to close: %w", err)
		}
		if err := begin(store.EffectClose, st.closeEffectTarget(target.CloseSet())); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		return h.accept(ctx, opts, req, st, now, "close", "ship: close")
	}
	return stop("Delivery is already complete.")
}

// accept clears the notice and returns the outcome with the revision the state
// has after the records just written.
func (h ValidateResolveActivity) accept(ctx context.Context, opts Options, req ActivityRequest, _ ValidateState, now time.Time, outcome, what string) (ActivityResult, error) {
	if wait, e := opts.DB.GetGraphOwnerWait(ctx, req.InstanceID); e == nil && wait.ReplyID > 0 {
		instance, e := opts.DB.GetGraphInstance(ctx, req.InstanceID)
		if e != nil {
			return ActivityResult{Diagnostic: e.Error()}, e
		}
		session, e := opts.DB.GetSession(ctx, instance.SessionID)
		if e != nil {
			return ActivityResult{Diagnostic: e.Error()}, e
		}
		if e = opts.DB.ResolveOwnerDecisionBinding(ctx, session.TaskRef, wait.ReplyID); e != nil {
			return ActivityResult{Diagnostic: e.Error()}, e
		}
	}

	after, err := LoadValidateState(ctx, opts, h.b.Profile, req.InstanceID)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	if err := saveValidateNotice(ctx, opts, req.InstanceID, after.Revision, "", now); err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	return validateResolveResult(outcome, what, after)
}

func validateResolveResult(outcome, what string, st ValidateState) (ActivityResult, error) {
	return validateJSONResult(outcome, map[string]any{"outcome": outcome, "detail": what, "commit": st.Commit, "revision": st.Revision, "ready": st.Ready})
}

// ownerRepairReady says a repair round may start for the owner's choices: at
// least one finding is chosen for repair and every blocking finding has a
// choice. Pending owner checks do not hold the round back, because a kept repair
// is a new snapshot on which the checks are asked again.
func (s ValidateState) ownerRepairReady() bool {
	chosen := false
	for _, f := range s.Findings {
		if !f.needsChoice() {
			continue
		}
		if f.Choice == nil {
			if f.Blocking {
				return false
			}
			continue
		}
		chosen = chosen || f.Choice.Choice == store.ChoiceRepair
	}
	return chosen
}

// replayedValidateReply finds what an owner reply already did: the step that
// carries its confirmation on, repair when it was the repair choice that
// started a round, or wait when it only recorded a verdict or choice.
func replayedValidateReply(ctx context.Context, opts Options, st ValidateState, commentID int64, repairs bool) (string, bool) {
	effects, err := opts.DB.ListValidateEffects(ctx, st.InstanceID)
	if err == nil {
		for _, e := range effects {
			if e.ConfirmCommentID != commentID || e.Commit != st.Commit && e.Commit != st.deliveryCommit() {
				continue
			}
			switch e.Kind {
			case store.EffectIssue:
				return "create_issue", true
			case store.EffectPush, store.EffectPR:
				return "push", true
			case store.EffectMerge:
				return "merge", true
			case store.EffectClose:
				return "close", true
			}
		}
	}
	for _, f := range st.Findings {
		if f.Choice != nil && f.Choice.CommentID == commentID && f.Choice.Choice == store.ChoiceRepair && repairs && st.ownerRepairReady() {
			return "repair", true
		}
		if f.Choice != nil && f.Choice.CommentID == commentID || f.Verdict != nil && f.Verdict.CommentID == commentID {
			return "wait", true
		}
	}
	return "", false
}

func validateShipSettlesTicketDecisions(st ValidateState) bool {
	for _, f := range st.Findings {
		if f.State == findingSatisfied || f.State == findingOwnerPassed || f.State == "passed" || (f.Optional && f.Choice != nil) {
			continue
		}
		return true
	}
	return false
}
