package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// The remote steps each do one owner-authorized effect. They act only on an
// effect the resolve step recorded as intent from the owner's own command; a run
// that reaches one without such an intent does nothing. Each checks the real
// remote state first, so a restart or a lost reply never repeats an effect. A
// failed or uncertain effect is recorded and shown; only the owner repeating the
// command retries it.

type validateRemote struct {
	b      ValidateFlowBindings
	opts   Options
	req    ActivityRequest
	st     ValidateState
	target ValidateDeliveryTarget
}

func (b ValidateFlowBindings) remote(ctx context.Context, opts Options, req ActivityRequest, action string) (*validateRemote, error) {
	if _, err := loadValidateStep(ctx, opts, req, b.Profile, action); err != nil {
		return nil, err
	}
	if !b.Delivery.ready() {
		return nil, errors.New("validate owner steps lack delivery bindings")
	}
	st, err := LoadValidateState(ctx, opts, b.Profile, req.InstanceID)
	if err != nil {
		return nil, err
	}
	target, err := b.Delivery.Target(ctx, opts, req)
	if err != nil {
		return nil, fmt.Errorf("resolve the delivery target: %w", err)
	}
	if action != graphexec.ValidateIssueAction && st.Waiver != nil && !st.waiverCurrent() {
		return nil, errors.New("shipping exception is stale: changed evidence requires a fresh owner decision")
	}
	return &validateRemote{b: b, opts: opts, req: req, st: st, target: target}, nil
}

// settle records the end of an effect attempt. It never loses the reason.
func (r *validateRemote) settle(ctx context.Context, e store.ValidateEffect, state string, result any, reason string) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if (state == store.EffectFailed || state == store.EffectUncertain) && r.opts.Logger != nil {
		r.opts.Logger.Warn("validate effect did not succeed",
			"instance", r.req.InstanceID, "effect", e.Kind, "commit", r.st.Commit, "state", state, "reason", reason)
	}
	return r.opts.DB.SettleValidateEffect(ctx, e.ID, state, raw, reason, r.b.now(r.opts))
}

// headMatches re-checks the confirmed head before any remote action.
func (r *validateRemote) headMatches(ctx context.Context) (string, bool, error) {
	head, err := r.b.Delivery.Head(ctx)
	if err != nil {
		return "", false, fmt.Errorf("read the branch head: %w", err)
	}
	return head, head == r.st.deliveryCommit(), nil
}

// intent returns the effect of a kind that waits to be carried out.
func (r *validateRemote) intent(ctx context.Context, kind, target string) (store.ValidateEffect, bool, error) {
	e, err := r.opts.DB.GetValidateEffect(ctx, r.req.InstanceID, kind, target)
	if errors.Is(err, store.ErrNotFound) {
		return e, false, nil
	}
	return e, err == nil && e.State == store.EffectIntent, err
}

// done ends a step that has no remote failure of its own to raise. A failure of
// recording the settlement itself is raised, because the record is the truth.
func done(payload map[string]any, errs ...error) (ActivityResult, error) {
	if err := errors.Join(errs...); err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	return validateJSONResult("done", payload)
}

// ---- issue ------------------------------------------------------------------

var validateMarkerPattern = regexp.MustCompile(`[^A-Za-z0-9_.:-]`)

// ValidateIssueMarker is the stable marker in the body of a follow-up issue. A
// replay finds the issue by it and creates no second one.
func ValidateIssueMarker(instanceID int64, finding string) string {
	return fmt.Sprintf("<!-- lf:validate-issue run=%d finding=%s -->", instanceID, validateMarkerPattern.ReplaceAllString(finding, "_"))
}

// ValidateIssueActivity creates or recovers the follow-up issue of every
// finding the owner chose an issue for.
type ValidateIssueActivity struct{ b ValidateFlowBindings }

// Execute handles each issue effect that waits.
func (h ValidateIssueActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	r, err := h.b.remote(ctx, opts, req, graphexec.ValidateIssueAction)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	var handled []string
	for _, f := range r.st.Findings {
		if f.Choice == nil || f.Choice.Choice != store.ChoiceIssue {
			continue
		}
		e, waiting, err := r.intent(ctx, store.EffectIssue, f.ID)
		if err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		if !waiting {
			continue
		}
		if err := r.createIssue(ctx, e, f); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		handled = append(handled, f.ID)
	}
	return done(map[string]any{"findings": handled})
}

func (r *validateRemote) createIssue(ctx context.Context, e store.ValidateEffect, f ValidateFinding) error {
	marker := ValidateIssueMarker(r.req.InstanceID, f.ID)
	fail := func(state string, err error) error {
		return r.settle(ctx, e, state, map[string]any{}, "The follow-up issue was not confirmed: "+err.Error())
	}
	issues, err := r.b.Delivery.tickets().ListFollowupIssues(ctx, r.target.ticketRepo())
	if err != nil {
		return fail(store.EffectUncertain, err)
	}
	found, matches := github.Issue{}, 0
	for _, i := range issues {
		if (r.target.BotID == 0 || i.Author.ID == r.target.BotID) && bodyHasMarkerLine(i.Body, marker) {
			found, matches = i, matches+1
		}
	}
	if matches > 1 {
		return fail(store.EffectFailed, errors.New("more than one issue carries the marker"))
	}
	if matches == 0 {
		title, body, err := r.issueContent(ctx, f, marker)
		if err != nil {
			return fail(store.EffectFailed, err)
		}
		created, createErr := createTicketEffect(ctx, r.b.Delivery.tickets(), "validate-issue:"+marker, r.target.ticketRepo(), 0, title, body)
		if createErr != nil {
			// A lost reply may still have created it: look before saying it failed.
			again, listErr := r.b.Delivery.tickets().ListFollowupIssues(ctx, r.target.ticketRepo())
			if listErr != nil {
				return fail(store.EffectUncertain, errors.Join(createErr, listErr))
			}
			for _, i := range again {
				if (r.target.BotID == 0 || i.Author.ID == r.target.BotID) && bodyHasMarkerLine(i.Body, marker) {
					created, createErr = i, nil
					break
				}
			}
			if createErr != nil {
				return fail(store.EffectFailed, createErr)
			}
		}
		found = created
	}
	if found.Number < 1 {
		return fail(store.EffectFailed, errors.New("the created issue has no number"))
	}
	if r.labelsBug(f) && !slices.Contains(found.Labels, "bug") {
		if err := r.labelBug(ctx, found); err != nil {
			return r.settle(ctx, e, store.EffectFailed, map[string]any{}, fmt.Sprintf("created %s but could not label it bug: %v", displayIssue(r.target.Task, found).Reference(), err))
		}
	}
	return r.settle(ctx, e, store.EffectSucceeded, validateEffectResult{Issue: found.Number, IssueReference: displayIssue(r.target.Task, found).Reference()}, "")
}

// labelsBug says whether the follow-up gets the bug label: a bug on GitHub.
// Linear accepts only mapped mode labels, so a Linear follow-up keeps none.
func (r *validateRemote) labelsBug(f ValidateFinding) bool {
	return issueKind(f) == "bug" && r.target.Task.Forge != "linear"
}

// labelBug adds the bug label. After an error it reads the issue again and
// succeeds only when the label landed anyway.
func (r *validateRemote) labelBug(ctx context.Context, issue github.Issue) error {
	tickets := r.b.Delivery.tickets()
	err := tickets.AddIssueLabel(ctx, r.target.ticketRepo(), issue.Number, "bug")
	if err == nil {
		return nil
	}
	again, listErr := tickets.ListFollowupIssues(ctx, r.target.ticketRepo())
	if listErr != nil {
		return errors.Join(err, listErr)
	}
	for _, i := range again {
		if i.Number == issue.Number && slices.Contains(i.Labels, "bug") {
			return nil
		}
	}
	return err
}

func issueKind(f ValidateFinding) string {
	if f.Choice != nil && f.Choice.IssueKind != "" {
		return f.Choice.IssueKind
	}
	return "bug"
}

func bodyHasMarkerLine(body, marker string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		if strings.TrimSpace(line) == marker {
			return true
		}
	}
	return false
}

func (r *validateRemote) issueContent(ctx context.Context, f ValidateFinding, marker string) (redact.Body, redact.Body, error) {
	kind := issueKind(f)
	titleText := fmt.Sprintf("Validate follow-up (%s): %s %s", kind, f.Story, f.Name)
	if f.Defect != "" {
		titleText = firstSentence(f.Defect)
	}
	title, err := r.b.Delivery.Redactor.Apply(ctx, graphDeliveryTitle(titleText), 256)
	if err != nil {
		return redact.Body{}, redact.Body{}, err
	}
	var b strings.Builder
	if f.Defect != "" {
		b.WriteString("## What happens\n\n" + f.Defect + "\n\n")
	}
	b.WriteString("## Source\n\n")
	fmt.Fprintf(&b, "- Follow-up %s from Validate of issue %s, tested commit `%s`.\n", kind, r.target.Parent.display().Reference(), r.st.Commit)
	fmt.Fprintf(&b, "- Finding `%s` is %s (%s).\n", f.ID, f.Status, f.Class)
	b.WriteString("- The owner chose to track this as its own issue. Creating it does not clear a blocking finding.\n\n" + marker)
	body, err := r.b.Delivery.Redactor.Apply(ctx, b.String(), 65536)
	return title, body, err
}

// ---- push -------------------------------------------------------------------

// ValidatePushActivity pushes the displayed delivery commit guarded by the expected remote
// head, then finds or creates the pull request into the base.
type ValidatePushActivity struct{ b ValidateFlowBindings }

// Execute pushes and opens the pull request, each only once.
func (h ValidatePushActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	r, err := h.b.remote(ctx, opts, req, graphexec.ValidatePushAction)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	commit := r.st.deliveryCommit()
	push, pushing, err := r.intent(ctx, store.EffectPush, commit)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	pr, opening, err := r.intent(ctx, store.EffectPR, commit)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	if !pushing && !opening {
		return done(map[string]any{"acted": false})
	}
	if head, ok, err := r.headMatches(ctx); err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	} else if !ok {
		why := fmt.Sprintf("The branch head moved to %s; the displayed delivery commit is %s. Nothing was pushed.", short(head), short(commit))
		for _, e := range []store.ValidateEffect{push, pr} {
			if e.State == store.EffectIntent {
				if err := r.settle(ctx, e, store.EffectFailed, map[string]any{}, why); err != nil {
					return ActivityResult{}, err
				}
			}
		}
		return done(map[string]any{"acted": false, "stale": true})
	}
	if pushing {
		if err := r.push(ctx, push); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		if push, err = r.opts.DB.GetValidateEffect(ctx, req.InstanceID, store.EffectPush, commit); err != nil {
			return ActivityResult{}, err
		}
	}
	if push.State != store.EffectSucceeded {
		return done(map[string]any{"acted": true, "push": push.State})
	}
	if !opening {
		// The owner confirmed push; the pull request is the second half of it.
		if pr.ID == 0 {
			if pr, _, err = r.opts.DB.BeginValidateEffect(ctx, store.ValidateEffect{InstanceID: req.InstanceID, Kind: store.EffectPR, Commit: commit, Target: commit, ConfirmCommentID: push.ConfirmCommentID, CreatedAt: r.b.now(opts)}); err != nil {
				return ActivityResult{Diagnostic: err.Error()}, err
			}
			opening = pr.State == store.EffectIntent
		}
	}
	if opening {
		if err := r.openPR(ctx, pr); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
	}
	return done(map[string]any{"acted": true})
}

// push publishes the delivery commit unless the remote branch already holds it.
func (r *validateRemote) push(ctx context.Context, e store.ValidateEffect) error {
	commit := r.st.deliveryCommit()
	remote, err := r.b.Delivery.code().GetDeliveryHead(ctx, r.target.Repo, r.target.Branch)
	if err != nil {
		if !errors.Is(err, github.ErrNotFound) {
			return r.settle(ctx, e, store.EffectUncertain, map[string]any{}, "The remote branch head could not be read: "+err.Error())
		}
		remote = ""
	}
	if remote == commit {
		return r.settle(ctx, e, store.EffectSucceeded, validateEffectResult{Head: commit}, "")
	}
	// A pull request from the branch merged at the delivery commit already
	// delivered it (the merge may have deleted the remote branch): nothing to push.
	if prs, err := r.b.Delivery.code().ListDeliveryPRs(ctx, r.target.Repo, r.target.Branch); err == nil {
		for _, p := range prs {
			if p.Merged && p.Number > 0 && p.Branch == r.target.Branch && p.Target == r.target.Base && p.Head == commit {
				return r.settle(ctx, e, store.EffectSucceeded, validateEffectResult{Head: commit}, "")
			}
		}
	}
	pushErr := r.b.Delivery.Publish(ctx, commit, remote)
	after, readErr := r.b.Delivery.code().GetDeliveryHead(ctx, r.target.Repo, r.target.Branch)
	switch {
	case readErr == nil && after == commit:
		return r.settle(ctx, e, store.EffectSucceeded, validateEffectResult{Head: commit}, "")
	case pushErr != nil:
		return r.settle(ctx, e, store.EffectFailed, map[string]any{}, "Pushing the delivery commit failed: "+pushErr.Error())
	}
	return r.settle(ctx, e, store.EffectUncertain, map[string]any{}, "The push reported success but the remote branch does not hold the delivery commit yet.")
}

// openPR finds the pull request at the displayed delivery commit or creates it.
func (r *validateRemote) openPR(ctx context.Context, e store.ValidateEffect) error {
	commit := r.st.deliveryCommit()
	find := func() (github.DeliveryPR, bool, error) {
		prs, err := r.b.Delivery.code().ListDeliveryPRs(ctx, r.target.Repo, r.target.Branch)
		if err != nil {
			return github.DeliveryPR{}, false, err
		}
		var open github.DeliveryPR
		for _, p := range prs {
			if p.Branch != r.target.Branch || p.Target != r.target.Base || p.Number < 1 {
				continue
			}
			if p.Merged && p.Head == commit {
				return p, true, nil
			}
			if p.State == "open" && open.Number == 0 {
				open = p
			}
		}
		return open, open.Number > 0, nil
	}
	pr, found, err := find()
	if err != nil {
		return r.settle(ctx, e, store.EffectUncertain, map[string]any{}, "The pull requests could not be listed: "+err.Error())
	}
	if !found {
		titleText := graphDeliveryTitle(fmt.Sprintf("Deliver issue %s", r.target.Parent.display().Label))
		title, err := r.b.Delivery.Redactor.Apply(ctx, titleText, 256)
		if err != nil {
			return r.settle(ctx, e, store.EffectFailed, map[string]any{}, err.Error())
		}
		accepted := ""
		for _, f := range r.st.Findings {
			if f.acceptedDeviation() {
				accepted += fmt.Sprintf("- `%s`: %s\n", f.ID, strings.Join(strings.Fields(clipText(f.Verdict.Evidence, 1000)), " "))
			}
		}
		if accepted != "" {
			accepted = "Accepted deviations (blocking review findings the owner accepted):\n\n" + accepted + "\n"
		}
		authorization := fmt.Sprintf("Owner-authorized delivery commit `%s`. Suite and review evidence is retained at tested commit `%s` (suite: %s).\n\n", commit, r.st.Commit, r.st.Suite.Outcome)
		if r.st.Waiver != nil {
			authorization += fmt.Sprintf("Owner waiver by actor %d, source comment %d: %s\n\nDisplayed blockers (facts remain unchanged):\n", r.st.Waiver.ActorID, r.st.Waiver.CommentID, r.st.Waiver.Reason)
			for _, blocker := range r.st.Waiver.Blockers {
				authorization += "- " + blocker + "\n"
			}
		}
		body, err := r.b.Delivery.Redactor.Apply(ctx, fmt.Sprintf("%s\nIssue %s.\n\n%s<!-- lf:validate-pr run=%d -->", authorization, r.target.Parent.display().Reference(), accepted, r.req.InstanceID), 65536)
		if err != nil {
			return r.settle(ctx, e, store.EffectFailed, map[string]any{}, err.Error())
		}
		created, createErr := r.b.Delivery.code().CreateDeliveryPR(ctx, r.target.Repo, r.target.Branch, r.target.Base, title, body)
		if createErr != nil {
			// A lost reply may still have created it: look before saying it failed.
			if pr, found, err = find(); err != nil || !found {
				return r.settle(ctx, e, store.EffectFailed, map[string]any{}, "Creating the pull request failed: "+createErr.Error())
			}
		} else {
			pr = created
		}
	}
	if pr.Head != commit {
		return r.settle(ctx, e, store.EffectFailed, map[string]any{}, fmt.Sprintf("Pull request #%d is at head %s, not the displayed delivery commit %s.", pr.Number, short(pr.Head), short(commit)))
	}
	return r.settle(ctx, e, store.EffectSucceeded, validateEffectResult{Number: pr.Number, Head: pr.Head}, "")
}

// ---- merge ------------------------------------------------------------------

// ValidateMergeActivity merges the pull request only at the confirmed head.
type ValidateMergeActivity struct{ b ValidateFlowBindings }

// Execute merges once.
func (h ValidateMergeActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	r, err := h.b.remote(ctx, opts, req, graphexec.ValidateMergeAction)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	pr := decodeEffectResult(r.st.Effects.PR)
	if !effectDone(r.st.Effects.Push) || !effectDone(r.st.Effects.PR) || pr.Number < 1 {
		return done(map[string]any{"acted": false})
	}
	e, waiting, err := r.intent(ctx, store.EffectMerge, fmt.Sprintf("%d@%s", pr.Number, pr.Head))
	if err != nil || !waiting {
		if err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		return done(map[string]any{"acted": false})
	}
	// Replay checks the merged state first.
	current, err := r.b.Delivery.code().GetDeliveryPR(ctx, r.target.Repo, pr.Number)
	if err != nil {
		return done(map[string]any{"acted": false}, r.settle(ctx, e, store.EffectUncertain, map[string]any{}, "The pull request could not be read: "+err.Error()))
	}
	if current.Merged {
		return mergeSettled(ctx, r, e, current, pr)
	}
	if head, ok, err := r.headMatches(ctx); err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	} else if !ok {
		return done(map[string]any{"acted": false, "stale": true}, r.settle(ctx, e, store.EffectFailed, map[string]any{}, fmt.Sprintf("The branch head moved to %s; the confirmed head is %s. Nothing was merged.", short(head), short(pr.Head))))
	}
	if current.Head != pr.Head {
		return done(map[string]any{"acted": false, "stale": true}, r.settle(ctx, e, store.EffectFailed, map[string]any{}, fmt.Sprintf("Pull request #%d is at head %s, not the confirmed %s. Nothing was merged.", pr.Number, short(current.Head), short(pr.Head))))
	}
	if mergeErr := r.b.Delivery.code().MergeDeliveryPR(ctx, r.target.Repo, pr.Number, pr.Head); mergeErr != nil {
		// The merge may have happened despite the error: look before saying so.
		again, getErr := r.b.Delivery.code().GetDeliveryPR(ctx, r.target.Repo, pr.Number)
		if getErr != nil {
			return done(map[string]any{"acted": true}, r.settle(ctx, e, store.EffectUncertain, map[string]any{}, "Merging reported "+mergeErr.Error()+" and the result could not be read."))
		}
		if !again.Merged {
			return done(map[string]any{"acted": true}, r.settle(ctx, e, store.EffectFailed, map[string]any{}, fmt.Sprintf("Merging pull request #%d failed: %s", pr.Number, mergeErr.Error())))
		}
		current = again
	} else if current, err = r.b.Delivery.code().GetDeliveryPR(ctx, r.target.Repo, pr.Number); err != nil {
		return done(map[string]any{"acted": true}, r.settle(ctx, e, store.EffectUncertain, map[string]any{}, "The merge was sent but the result could not be read: "+err.Error()))
	}
	if !current.Merged {
		return done(map[string]any{"acted": true}, r.settle(ctx, e, store.EffectUncertain, map[string]any{}, fmt.Sprintf("Pull request #%d is not merged yet.", pr.Number)))
	}
	return mergeSettled(ctx, r, e, current, pr)
}

func mergeSettled(ctx context.Context, r *validateRemote, e store.ValidateEffect, current github.DeliveryPR, pr validateEffectResult) (ActivityResult, error) {
	if current.Head != "" && current.Head != pr.Head {
		return done(map[string]any{"acted": true}, r.settle(ctx, e, store.EffectFailed, map[string]any{}, fmt.Sprintf("Pull request #%d merged at %s, not the confirmed head %s.", pr.Number, short(current.Head), short(pr.Head))))
	}
	return done(map[string]any{"acted": true, "merged": true}, r.settle(ctx, e, store.EffectSucceeded, validateEffectResult{Number: pr.Number, Head: pr.Head, MergeSHA: current.MergeSHA}, ""))
}

// ---- close ------------------------------------------------------------------

// ValidateCloseActivity closes the parent and the accepted story issues as
// completed, each checked by numeric id, and only after a succeeded merge for the
// same run and head. Validate succeeds only through it.
type ValidateCloseActivity struct{ b ValidateFlowBindings }

// Execute closes every listed issue.
func (h ValidateCloseActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	r, err := h.b.remote(ctx, opts, req, graphexec.ValidateCloseAction)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	if !effectDone(r.st.Effects.Merge) || r.st.Effects.Close == nil {
		return validateJSONResult("open", map[string]any{"acted": false})
	}
	e := *r.st.Effects.Close
	if e.State == store.EffectSucceeded {
		if err := opts.DB.ResolveOwnerDecisionInstance(ctx, req.InstanceID); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		if _, err := r.b.RefreshValidateReport(ctx, opts, req); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		return validateJSONResult("closed", map[string]any{"acted": false})
	}
	if e.State != store.EffectIntent {
		return validateJSONResult("open", map[string]any{"acted": false})
	}
	set := r.target.CloseSet()
	if e.Target != r.st.closeEffectTarget(set) {
		if err := r.settle(ctx, e, store.EffectFailed, map[string]any{}, "The issues to close changed since the command: "+validateNumbers(set)); err != nil {
			return ActivityResult{}, err
		}
		return validateJSONResult("open", map[string]any{"acted": false})
	}
	if head, ok, err := r.headMatches(ctx); err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	} else if !ok {
		if err := r.settle(ctx, e, store.EffectFailed, map[string]any{}, fmt.Sprintf("The branch head moved to %s; the confirmed head is %s. Nothing was closed.", short(head), short(r.st.deliveryCommit()))); err != nil {
			return ActivityResult{}, err
		}
		return validateJSONResult("open", map[string]any{"acted": false, "stale": true})
	}
	var closed []int
	var failures []string
	forge := r.b.Delivery.tickets()
	for _, s := range r.target.Stories {
		if err := forge.SetStoryState(ctx, r.target.ticketRepo(), r.target.Parent.Number, r.target.Parent.ID, s.Number, s.ID, true); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %s", s.display().Reference(), err.Error()))
			continue
		}
		closed = append(closed, s.Number)
	}
	// The parent closes last, so it never looks done while a story is open.
	if len(failures) == 0 {
		if err := forge.CloseDeliveredParent(ctx, r.target.ticketRepo(), r.target.Parent.Number, r.target.Parent.ID); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %s", r.target.Parent.display().Reference(), err.Error()))
		} else {
			closed = append(closed, r.target.Parent.Number)
		}
	}
	if len(failures) > 0 {
		if err := r.settle(ctx, e, store.EffectFailed, validateEffectResult{Closed: closed}, "Closing did not finish: "+strings.Join(failures, "; ")); err != nil {
			return ActivityResult{}, err
		}
		return validateJSONResult("open", map[string]any{"acted": true, "closed": closed})
	}
	if err := r.settle(ctx, e, store.EffectSucceeded, validateEffectResult{Closed: closed}, ""); err != nil {
		return ActivityResult{}, err
	}
	if err := opts.DB.ResolveOwnerDecisionInstance(ctx, req.InstanceID); err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	if _, err := r.b.RefreshValidateReport(ctx, opts, req); err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	return validateJSONResult("closed", map[string]any{"acted": true, "closed": closed})
}
