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
//	push          the owner confirmed push
//	merge         the owner confirmed merge
//	close         the owner confirmed close
//	stale         the branch head or the report revision moved
//
// A refused command changes nothing but the notice shown to the owner.
type ValidateResolveActivity struct{ b ValidateFlowBindings }

// Execute resolves one entry of the step.
func (h ValidateResolveActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if _, err := loadValidateStep(ctx, opts, req, h.b.Profile, graphexec.ValidateResolveAction); err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	if !h.b.Delivery.ready() {
		return ActivityResult{Diagnostic: "validate owner steps lack delivery bindings"}, errors.New("validate owner steps lack delivery bindings")
	}
	st, err := LoadValidateState(ctx, opts, h.b.Profile, req.InstanceID)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	wait, err := opts.DB.GetGraphOwnerWait(ctx, req.InstanceID)
	if errors.Is(err, store.ErrNotFound) || err == nil && (wait.State != "allocated" || wait.ContinuationAttemptID != req.NodeAttemptID) {
		// The first pass after the outcome, or a pass with no reply of its own.
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

	owner, err := h.b.Delivery.OwnerID(ctx, opts)
	if err != nil || owner < 1 {
		return ActivityResult{Diagnostic: "the owner identity cannot be resolved"}, errors.Join(errors.New("the owner identity cannot be resolved"), err)
	}
	if wait.ReplyAuthorID != owner {
		return refuse("wait", "Only the owner can give Validate commands.")
	}
	// A reply this run already acted on (a restart replays the step) goes to the
	// same step again. Every step recovers on its own, so nothing is done twice.
	if outcome, ok := replayedValidateReply(ctx, opts, st, wait.ReplyID); ok {
		return validateResolveResult(outcome, "replayed", st)
	}
	head, err := h.b.Delivery.Head(ctx)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, fmt.Errorf("read the branch head: %w", err)
	}
	if head != st.Commit {
		return refuse("stale", fmt.Sprintf("The branch head is now `%s`, not the tested commit `%s`. Verdicts, choices and confirmations belong to the tested commit and are stale; run Validate again.", short(head), short(st.Commit)))
	}
	cmd, err := ParseValidateCommand(wait.ReplyBody)
	if err != nil {
		return refuse("wait", err.Error())
	}
	if cmd.Revision != st.Revision {
		return refuse("stale", fmt.Sprintf("The command names revision `%s`, but the current report revision is `%s`. Use the commands in the latest report.", cmd.Revision, st.Revision))
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
		if !ok || !f.Criterion || f.Status != "unverified" {
			return refuse("wait", fmt.Sprintf("`%s` is not a criterion that awaits an owner check.", cmd.Criterion))
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
		case cmd.Choice == store.ChoiceDefer && !f.Optional:
			return refuse("wait", fmt.Sprintf("`%s` is blocking. Defer is allowed only for optional suggestions; choose repair or issue.", f.ID))
		}
		if _, err := opts.DB.PutValidateChoice(ctx, store.ValidateChoice{InstanceID: req.InstanceID, Commit: st.Commit, Finding: f.ID, Choice: cmd.Choice, IssueKind: cmd.IssueKind, CommentID: wait.ReplyID, AuthorID: wait.ReplyAuthorID, CreatedAt: now}); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
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
	}
	return refuse("wait", "Unknown Validate command.")
}

// accept clears the notice and returns the outcome with the revision the state
// has after the records just written.
func (h ValidateResolveActivity) accept(ctx context.Context, opts Options, req ActivityRequest, _ ValidateState, now time.Time, outcome, what string) (ActivityResult, error) {
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

// replayedValidateReply finds what an owner reply already did: the step that
// carries its confirmation on, or wait when it only recorded a verdict or choice.
func replayedValidateReply(ctx context.Context, opts Options, st ValidateState, commentID int64) (string, bool) {
	effects, err := opts.DB.ListValidateEffects(ctx, st.InstanceID)
	if err == nil {
		for _, e := range effects {
			if e.ConfirmCommentID != commentID || e.Commit != st.Commit {
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
		if f.Choice != nil && f.Choice.CommentID == commentID || f.Verdict != nil && f.Verdict.CommentID == commentID {
			return "wait", true
		}
	}
	return "", false
}
