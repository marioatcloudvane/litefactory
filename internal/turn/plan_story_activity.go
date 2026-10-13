package turn

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// PlanStoryForge is the existing redacted forge boundary plus not-planned closure.
type PlanStoryForge interface {
	GetIssue(context.Context, github.Repo, int) (github.Issue, error)
	GetParentIssue(context.Context, github.Repo, int) (github.Issue, error)
	ListFollowupIssues(context.Context, github.Repo) ([]github.Issue, error)
	CreateIssue(context.Context, github.Repo, redact.Body, redact.Body) (github.Issue, error)
	UpdateIssue(context.Context, github.Repo, int, redact.Body, redact.Body) (github.Issue, error)
	AddSubIssue(context.Context, github.Repo, int, int64) error
	ListCommentsAfter(context.Context, github.Repo, int, int64) ([]github.Comment, error)
	PostComment(context.Context, github.Repo, int, redact.Body) (github.Comment, error)
	RetirePlanStory(context.Context, github.Repo, int, int64, int, int64) error
}

// PlanStoryAuthority is factory-only context, never decoded from agent output.
// Retirement requires numeric owner evidence for the current displayed revision.
type PlanStoryAuthority struct {
	Parent                                  string
	Repo                                    github.Repo
	ParentNumber                            int
	ParentID                                int64
	SelectedKey, Candidate, Stage           string
	ContentScope                            planbrief.ContentScope
	OwnerID, ActorID                        int64
	RetirementCandidate, RetirementRevision string
	Confirmation                            *planbrief.PlanOwnerReply
	ReviewRevision                          string
	Recovery                                bool
}

// PlanStoryReceipt describes draft publication only, never implementation readiness.
type PlanStoryReceipt struct {
	RequestID   string `json:"request_id"`
	Story       string `json:"story"`
	Candidate   string `json:"candidate"`
	Revision    string `json:"revision"`
	IssueID     int64  `json:"issue_id"`
	IssueNumber int    `json:"issue_number"`
	Outcome     string `json:"outcome"`
	Detail      string `json:"detail,omitempty"`
}

// PlanStoryActivity obtains authority and a structured request from the admitted
// policy. Attrs and agent prose cannot choose a parent, owner or remote target.
type PlanStoryActivity struct {
	DB       *store.DB
	Forge    PlanStoryForge
	Bot      ticket.Identity
	Redactor *redact.Redactor
	LockDir  string
	Bind     func(context.Context, Options, ActivityRequest) (PlanStoryAuthority, graphexec.PlanStoryRequest, error)
}

// RegisterPlanStory registers exactly one new Plan-specific capability.
func RegisterPlanStory(specs *graphexec.Registry, activities *ActivityRegistry, h PlanStoryActivity) error {
	if h.DB == nil || h.Forge == nil || h.Redactor == nil || h.LockDir == "" || h.Bind == nil {
		return errors.New("plan story activity dependencies are required")
	}
	if err := specs.Register(graphexec.PlanStorySpec()); err != nil {
		return err
	}
	return activities.Register(graphexec.PlanStoryAction, h)
}

// Execute implements the shared graph activity boundary.
func (h PlanStoryActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if req.Mode != agent.ModePlan || h.Bind == nil {
		return ActivityResult{}, errors.New("plan story requires admitted Plan authority")
	}
	a, r, err := h.Bind(ctx, opts, req)
	if err != nil {
		return ActivityResult{}, err
	}
	receipt, err := h.Apply(ctx, a, r)
	if err != nil {
		return ActivityResult{}, err
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return ActivityResult{}, err
	}
	return ActivityResult{Outcome: receipt.Outcome, Kind: store.GraphOutputJSON, Output: data}, nil
}

// PlanIssueRevision hashes all editable content, including factory provenance.
func PlanIssueRevision(title, body string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(title+"\x00"+body)))
}

func planStoryMarker(a PlanStoryAuthority, key string) string {
	return fmt.Sprintf("<!-- lf:plan-story parent=%d key=%s -->", a.ParentID, key)
}
func planPartMarker(a PlanStoryAuthority, key string, r graphexec.PlanStoryRequest, n, total int) string {
	return fmt.Sprintf("<!-- lf:plan-part parent=%d key=%s digest=%x part=%d/%d -->", a.ParentID, key, sha256.Sum256([]byte(r.RequestID)), n, total)
}
func planRequestMarker(r graphexec.PlanStoryRequest) string {
	return fmt.Sprintf("<!-- lf:plan-request digest=%x -->", sha256.Sum256([]byte(r.RequestID)))
}

// Apply serializes the parent without holding a SQL transaction across I/O.
func (h PlanStoryActivity) Apply(ctx context.Context, a PlanStoryAuthority, r graphexec.PlanStoryRequest) (receipt PlanStoryReceipt, err error) {
	receipt = PlanStoryReceipt{RequestID: r.RequestID, Story: r.Draft.Key, Candidate: r.Candidate}
	if h.DB == nil || h.Forge == nil || h.Redactor == nil || h.LockDir == "" {
		return receipt, errors.New("plan story dependencies missing")
	}
	if err = r.Validate(); err != nil {
		return receipt, err
	}
	if err = a.Repo.Validate(); err != nil {
		return receipt, err
	}
	if a.Parent == "" || a.ParentNumber < 1 || a.ParentID < 1 || a.SelectedKey != r.Draft.Key || a.Candidate != r.Candidate || a.Stage != r.Stage {
		return receipt, errors.New("foreign or unbound Plan story request")
	}
	unlock, err := lockPlanStory(ctx, 250*time.Millisecond, 60*time.Second, func() (func() error, error) {
		return h.DB.LockStoryProgress(ctx, a.Parent, h.LockDir)
	})
	if err != nil {
		return receipt, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	raw, err := json.Marshal(r)
	if err != nil {
		return receipt, err
	}
	retained, lookupErr := h.DB.GetPlanStoryCandidate(ctx, a.Parent, r.RequestID)
	if lookupErr == nil && retained.RequestJSON != string(raw) {
		return receipt, errors.New("plan request identity reused with changed input")
	}
	if lookupErr != nil && !errors.Is(lookupErr, store.ErrNotFound) {
		return receipt, lookupErr
	}
	d, err := h.DB.GetPlanStoryDraft(ctx, a.Parent, r.Draft.Key)
	if errors.Is(err, store.ErrNotFound) && r.Operation == "create" {
		if err = r.Draft.Validate(r.Stage, a.ContentScope); err != nil {
			return receipt, err
		}
		d, err = h.DB.ReservePlanStory(ctx, a.Parent, r.Draft.Key)
	}
	if err != nil {
		return receipt, err
	}
	receipt.IssueID = d.IssueID
	receipt.IssueNumber = d.IssueNumber
	receipt.Revision = d.Revision
	// A story created at promotion has an issue with a short drafting body and no
	// candidate yet. Its first draft is a create that updates that issue, whose
	// content revision is the one recorded when it was created.
	bound := r.Operation == "create" && d.IssueID > 0 && d.Candidate == ""
	priorRevision := d.Revision
	latest, effectErr := h.DB.LatestPlanStoryEffect(ctx, a.Parent, r.RequestID)
	if effectErr != nil && !errors.Is(effectErr, store.ErrNotFound) {
		return receipt, effectErr
	}
	if effectErr == nil && latest.Outcome == "applied" {
		if err = h.repairCompletion(ctx, a, r, d, latest); err != nil {
			return receipt, err
		}
		receipt.Outcome = latest.Outcome
		receipt.Detail = latest.Detail
		receipt.Revision = latest.Revision
		receipt.IssueID = latest.IssueID
		receipt.IssueNumber = latest.IssueNumber
		return receipt, nil
	}
	if effectErr == nil && (latest.Outcome == "conflict" || latest.Outcome == "failed") {
		receipt.Outcome = latest.Outcome
		receipt.Detail = latest.Detail
		receipt.Revision = latest.Revision
		receipt.IssueID = latest.IssueID
		receipt.IssueNumber = latest.IssueNumber
		return receipt, nil
	}
	candidate := r.Draft
	if r.Operation != "create" {
		if d.Retired || d.IssueID == 0 || d.Revision != r.ExpectedRevision {
			return receipt, heldPlanStory("plan story retired, unpublished or stale")
		}
		var prior planbrief.PlanDraft
		if err = json.Unmarshal([]byte(d.DraftJSON), &prior); err != nil {
			return receipt, err
		}
		if r.Operation == "update" {
			candidate, err = planbrief.MergePlanDraft(prior, r.Draft, r.ChangedSections)
			if err != nil {
				return receipt, err
			}
		} else {
			candidate = prior
		}
	} else if d.Candidate != "" {
		return receipt, heldPlanStory("duplicate Plan story identity")
	}
	if r.Operation == "create" {
		err = candidate.Validate(r.Stage, a.ContentScope)
	} else {
		err = candidate.ValidateRetained(r.Stage, a.ContentScope)
	}
	if err != nil {
		return receipt, err
	}
	if r.Operation == "retire" && (a.OwnerID < 1 || a.ActorID != a.OwnerID || a.RetirementCandidate != d.Candidate || a.RetirementRevision != d.Revision) {
		return receipt, heldPlanStory("retirement lacks authenticated exact-candidate owner confirmation")
	}
	for _, key := range r.Replacements {
		other, getErr := h.DB.GetPlanStoryDraft(ctx, a.Parent, key)
		if getErr != nil {
			if errors.Is(getErr, store.ErrNotFound) {
				return receipt, heldPlanStory("retirement replacement missing")
			}
			return receipt, getErr
		}
		if other.Retired || other.IssueID == 0 || key == d.Key {
			return receipt, heldPlanStory("retirement replacement is missing, foreign, retired or self")
		}
	}
	if err = h.DB.PutPlanStoryCandidate(ctx, store.PlanStoryCandidate{Parent: a.Parent, RequestID: r.RequestID, Key: r.Draft.Key, Candidate: r.Candidate, RequestJSON: string(raw)}); err != nil {
		return receipt, err
	}
	if r.Operation == "retire" {
		if err = h.retainRetirement(ctx, &a, r, d); err != nil {
			return receipt, err
		}
	} else {
		original := a
		original.Recovery = false
		authorityRaw, marshalErr := json.Marshal(original)
		if marshalErr != nil {
			return receipt, marshalErr
		}
		if err = h.DB.RetainPlanStoryAuthority(ctx, a.Parent, r.RequestID, string(authorityRaw)); err != nil {
			return receipt, err
		}
	}
	if err = h.prepareCompletion(ctx, a, r, candidate); err != nil {
		return receipt, err
	}
	record := func(phase, outcome, detail string) (PlanStoryReceipt, error) {
		receipt.Outcome = outcome
		receipt.Detail = detail
		return receipt, h.DB.AppendPlanStoryEffect(ctx, store.PlanStoryEffect{Parent: a.Parent, RequestID: r.RequestID, Phase: phase, Outcome: outcome, IssueID: receipt.IssueID, IssueNumber: receipt.IssueNumber, Detail: detail})
	}
	remoteFailure := func(phase string, cause error) (PlanStoryReceipt, error) {
		result, saveErr := record(phase, "uncertain", cause.Error())
		if saveErr != nil {
			return result, saveErr
		}
		if errors.Is(cause, github.ErrNotFound) {
			return result, nil
		}
		return result, cause
	}
	parent, getErr := h.Forge.GetIssue(ctx, a.Repo, a.ParentNumber)
	if getErr != nil {
		result, saveErr := record("observe", "uncertain", getErr.Error())
		return result, errors.Join(getErr, saveErr)
	}
	if parent.ID != a.ParentID || parent.State != github.StateOpen {
		return receipt, errors.New("authorized parent identity or state changed")
	}
	render := report.RenderPlanDraftParts
	if r.Operation != "create" {
		render = report.RenderRetainedPlanDraftParts
	}
	parentRef, err := naming.ParseRef(a.Parent)
	if err != nil {
		return receipt, err
	}
	rendered, err := render(int64(a.ParentNumber), d.Ordinal, candidate, r.Stage, a.ContentScope, displayIssue(parentRef, parent).TitleKey)
	title, body := rendered.Title, rendered.Body
	if err != nil {
		// Rendering is deterministic and precedes every remote mutation, so the
		// request can never publish: record it as failed rather than erroring,
		// which recovery would otherwise retry on every turn.
		return record("render", "failed", err.Error())
	}
	body = planStoryMarker(a, d.Key) + "\n" + planRequestMarker(r) + "\n" + body
	safeTitle, err := h.Redactor.Apply(ctx, title, 256)
	if err != nil {
		return receipt, err
	}
	safeBody, err := h.Redactor.Apply(ctx, body, planbrief.MaxStoryBodyBytes)
	if err != nil {
		return receipt, err
	}
	if safeTitle.Truncated() || safeBody.Truncated() {
		return record("render", "failed", "complete draft cannot be truncated")
	}
	safeParts := make([]redact.Body, len(rendered.Parts))
	for i, part := range rendered.Parts {
		text := planPartMarker(a, d.Key, r, i+1, len(rendered.Parts)) + "\n**Full draft, part " + fmt.Sprintf("%d/%d", i+1, len(rendered.Parts)) + "**\n\n" + part
		if safeParts[i], err = h.Redactor.Apply(ctx, text, github.CommentSizeLimit); err != nil {
			return receipt, err
		}
		if safeParts[i].Truncated() {
			return record("render", "failed", "complete draft part cannot be truncated")
		}
	}
	// Retain the exact redacted bytes in the revision comparison.
	title, body = safeTitle.String(), safeBody.String()
	if r.Operation == "create" && d.IssueID == 0 {
		createIntent, intentErr := h.DB.HasPlanStoryMutation(ctx, a.Parent, r.RequestID, "create")
		if intentErr != nil {
			return receipt, intentErr
		}
		if createIntent {
			var found *github.Issue
			qualified := false
			if recoverer, ok := h.Forge.(interface {
				RecoverIssueEffect(context.Context, string, github.Repo, int, string, string) (github.Issue, bool, error)
			}); ok {
				issue, handled, recoverErr := recoverer.RecoverIssueEffect(ctx, "plan-create:"+a.Parent+":"+r.RequestID, a.Repo, a.ParentNumber, title, body)
				if recoverErr != nil {
					return remoteFailure("create", recoverErr)
				}
				qualified = handled
				if handled {
					found = &issue
				}
			}
			if !qualified {
				issues, listErr := h.Forge.ListFollowupIssues(ctx, a.Repo)
				if listErr != nil {
					return remoteFailure("create", listErr)
				}
				for i := range issues {
					v := &issues[i]
					if strings.HasPrefix(v.Body, planStoryMarker(a, d.Key)+"\n") && strings.Contains(v.Body, planRequestMarker(r)) {
						if found != nil {
							return record("create", "conflict", "duplicate remote request identities")
						}
						found = v
					}
				}
			}
			if found == nil {
				return record("create", "uncertain", "unresolved create result; no blind recreation")
			}
			if found.State != github.StateOpen || found.Title != title || !h.planContentEquivalent(body, *found) {
				return record("create", "conflict", "created story changed or closed during recovery")
			}
			d.IssueID, d.IssueNumber = found.ID, found.Number
		} else {
			if _, err = record("create", "uncertain", "create intent recorded before remote mutation"); err != nil {
				return receipt, err
			}
			created, createErr := createTicketEffect(ctx, h.Forge, "plan-create:"+a.Parent+":"+r.RequestID, a.Repo, a.ParentNumber, safeTitle, safeBody)
			if createErr != nil {
				return remoteFailure("create", createErr)
			}
			d.IssueID, d.IssueNumber = created.ID, created.Number
		}
		receipt.IssueID, receipt.IssueNumber = d.IssueID, d.IssueNumber
		if err = h.DB.BindPlanStoryIssue(ctx, a.Parent, d.Key, d.IssueID, d.IssueNumber); err != nil {
			return receipt, err
		}
	}
	child, getErr := h.Forge.GetIssue(ctx, a.Repo, d.IssueNumber)
	if getErr != nil {
		return remoteFailure("observe", getErr)
	}
	if child.ID != d.IssueID || !strings.HasPrefix(child.Body, planStoryMarker(a, d.Key)+"\n") {
		return record("observe", "conflict", "story identity or provenance changed")
	}
	observedParent, parentErr := h.Forge.GetParentIssue(ctx, a.Repo, d.IssueNumber)
	if r.Operation == "create" && errors.Is(parentErr, github.ErrNotFound) {
		attachIntent, intentErr := h.DB.HasPlanStoryMutation(ctx, a.Parent, r.RequestID, "attach")
		if intentErr != nil {
			return receipt, intentErr
		}
		if attachIntent {
			return record("attach", "uncertain", "attachment outcome or later detachment is ambiguous; verify the original request before reattachment")
		}
		if child.State != github.StateOpen || (!bound && (child.Title != title || !h.planContentEquivalent(body, child))) || (bound && PlanIssueRevision(child.Title, child.Body) != priorRevision) {
			return record("attach", "conflict", "created issue was edited or closed")
		}
		if _, err = record("attach", "uncertain", "known creation retained before attachment"); err != nil {
			return receipt, err
		}
		if err = h.Forge.AddSubIssue(ctx, a.Repo, a.ParentNumber, d.IssueID); err != nil {
			return remoteFailure("attach", err)
		}
		observedParent, parentErr = h.Forge.GetParentIssue(ctx, a.Repo, d.IssueNumber)
	}
	if parentErr != nil {
		return remoteFailure("observe", parentErr)
	}
	if observedParent.ID != a.ParentID || observedParent.Number != a.ParentNumber {
		return record("observe", "conflict", "story was reparented")
	}
	if r.Operation == "retire" {
		retireIntent, intentErr := h.DB.HasPlanStoryMutation(ctx, a.Parent, r.RequestID, "retire")
		if intentErr != nil {
			return receipt, intentErr
		}
		if child.State == "closed" && child.StateReason == "not_planned" && retireIntent && PlanIssueRevision(child.Title, child.Body) == r.ExpectedRevision {
			// An exact retained retirement intent permits lost-response reconciliation.
		} else {
			if retireIntent || a.Recovery {
				return record("retire", "uncertain", "retirement ordering is ambiguous; confirm current lifecycle before issuing a new request")
			}
			if child.State != github.StateOpen || PlanIssueRevision(child.Title, child.Body) != r.ExpectedRevision {
				return record("retire", "conflict", "retirement target changed or closed")
			}
			if _, err = record("retire", "uncertain", "owner-confirmed retirement intent recorded"); err != nil {
				return receipt, err
			}
			if err = h.Forge.RetirePlanStory(ctx, a.Repo, a.ParentNumber, a.ParentID, d.IssueNumber, d.IssueID); err != nil {
				return remoteFailure("retire", err)
			}
			child, getErr = h.Forge.GetIssue(ctx, a.Repo, d.IssueNumber)
			if getErr != nil {
				return remoteFailure("retire", getErr)
			}
			if child.ID != d.IssueID || child.State != "closed" || child.StateReason != "not_planned" || PlanIssueRevision(child.Title, child.Body) != r.ExpectedRevision {
				return record("retire", "uncertain", "retirement readback did not confirm not-planned closure")
			}
		}
		d.Retired = true
	} else {
		if child.State != github.StateOpen {
			if child.State == "closed" {
				if err = h.DB.ObservePlanStoryLifecycle(ctx, a.Parent, d.Key, planbrief.PlanLifecycle{State: child.State, Reason: child.StateReason, Revision: PlanIssueRevision(child.Title, child.Body)}, ""); err != nil {
					return receipt, err
				}
			}
			return record("observe", "conflict", "story was closed")
		}
		if (r.Operation == "update" || bound) && (effectErr != nil || child.Title != title || !h.planContentEquivalent(body, child)) {
			if PlanIssueRevision(child.Title, child.Body) != priorRevision {
				return record("update", "conflict", "owner edits conflict with expected content revision")
			}
			if _, err = record("update", "uncertain", "update intent retained; remote API has no atomic compare-and-swap"); err != nil {
				return receipt, err
			}
			if _, err = h.updatePlanStoryContent(ctx, a.Repo, d.IssueNumber, priorRevision, safeTitle, safeBody); err != nil {
				return remoteFailure("update", err)
			}
		}
		child, getErr = h.Forge.GetIssue(ctx, a.Repo, d.IssueNumber)
		if getErr != nil {
			return remoteFailure("verify", getErr)
		}
		if child.ID != d.IssueID || child.State != github.StateOpen || child.Title != title || !h.planContentEquivalent(body, child) {
			return record("verify", "uncertain", "remote concurrent edit or incomplete mutation; no atomic CAS guarantee")
		}
		d.Revision = PlanIssueRevision(child.Title, child.Body)
	}
	verifiedParent, verifyErr := h.Forge.GetParentIssue(ctx, a.Repo, d.IssueNumber)
	if verifyErr != nil {
		return remoteFailure("verify", verifyErr)
	}
	if verifiedParent.ID != a.ParentID || verifiedParent.Number != a.ParentNumber {
		return record("verify", "uncertain", "parent changed during mutation")
	}
	if r.Operation != "retire" && len(safeParts) > 0 {
		bot := h.Bot
		if contextual, ok := h.Forge.(interface {
			BotIdentityFor(context.Context, github.Repo, int) (ticket.Identity, error)
		}); ok {
			var e error
			bot, e = contextual.BotIdentityFor(ctx, a.Repo, a.ParentNumber)
			if e != nil {
				return receipt, e
			}
		} else if configured, ok := h.Forge.(interface{ BotIdentity() ticket.Identity }); ok {
			bot = configured.BotIdentity()
			if err := bot.Validate(); err != nil {
				return receipt, err
			}
		}
		comments, listErr := h.Forge.ListCommentsAfter(ctx, a.Repo, d.IssueNumber, 0)
		if listErr != nil {
			return remoteFailure("parts", listErr)
		}
		for i, part := range safeParts {
			marker := planPartMarker(a, d.Key, r, i+1, len(safeParts))
			posted := false
			for _, c := range comments {
				if strings.HasPrefix(c.Body, marker+"\n") && c.Body == part.String() && (bot == (ticket.Identity{}) || commentAuthorMatches(c, bot)) {
					posted = true
					break
				}
			}
			if posted {
				continue
			}

			phase := fmt.Sprintf("parts-%d", i+1)
			intent, intentErr := h.DB.HasPlanStoryMutation(ctx, a.Parent, r.RequestID, phase)
			if intentErr != nil {
				return receipt, intentErr
			}
			if intent {
				return record(phase, "uncertain", "original part dispatch remains unconfirmed; no duplicate send")
			}
			if _, intentErr = record(phase, "uncertain", "part dispatch intent retained before publication"); intentErr != nil {
				return receipt, intentErr
			}
			postedReply, postErr := postCommentEffect(ctx, h.Forge, "plan-part:"+a.Parent+":"+r.RequestID+":"+phase, a.Repo, d.IssueNumber, part)
			if postErr != nil {
				return remoteFailure(phase, postErr)
			}
			if postedReply.ID < 1 || postedReply.Body != part.String() || (bot != (ticket.Identity{}) && !commentAuthorMatches(postedReply, bot)) {
				return record(phase, "uncertain", "part receipt did not confirm prepared bytes and bot identity")
			}

		}
	}
	d.Candidate = r.Candidate
	d.Stage = r.Stage
	draftRaw, err := json.Marshal(candidate)
	if err != nil {
		return receipt, err
	}
	d.DraftJSON = string(draftRaw)
	receipt.Revision = d.Revision
	receipt.Outcome = "applied"
	receipt.Detail = "confirmed by readback; GitHub provides no atomic content compare-and-swap"
	effect := store.PlanStoryEffect{Parent: a.Parent, RequestID: r.RequestID, Phase: r.Operation, Outcome: "applied", IssueID: d.IssueID, IssueNumber: d.IssueNumber, Detail: receipt.Detail, Revision: d.Revision}
	// The state is read fresh and saved with its revision check together with the
	// draft, and re-read when a sibling story saved in between, so no revision is
	// held across the GitHub calls above.
	err = h.DB.WithPlanWorkflowState(ctx, a.Parent, func(state *planbrief.PlanState, revision int64) error {
		if state.SharedRevision == "" {
			state.SharedRevision = "initial"
		}
		if e := completePlanState(state, a, r, d, child); e != nil {
			return e
		}
		return h.DB.CompletePlanStoryEffect(ctx, d, priorRevision, effect, *state, revision, false)
	})
	if err != nil {
		return receipt, err
	}
	return receipt, nil
}

// lockPlanStory tolerates brief sibling effect contention within this invocation
// (AC-520-4). Only acquisition is retried; the effect and its context are unchanged.
func lockPlanStory(ctx context.Context, interval, limit time.Duration, acquire func() (func() error, error)) (func() error, error) {
	deadline := time.Now().Add(limit)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		unlock, err := acquire()
		if !errors.Is(err, store.ErrStoryProgressBusy) {
			return unlock, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, err
		}
		timer := time.NewTimer(min(interval, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !time.Now().Before(deadline) {
			return nil, err
		}
	}
}

// Legacy fixtures keep their existing boundary; production binds through the
// neutral bridge, which refreshes the original expected Plan revision.
func (h *PlanStoryActivity) updatePlanStoryContent(ctx context.Context, repo github.Repo, number int, expected string, title, body redact.Body) (github.Issue, error) {
	if guarded, ok := h.Forge.(interface {
		UpdateIssueExpected(context.Context, github.Repo, int, string, redact.Body, redact.Body) (github.Issue, error)
	}); ok {
		return guarded.UpdateIssueExpected(ctx, repo, number, expected, title, body)
	}
	return h.Forge.UpdateIssue(ctx, repo, number, title, body)
}

func (h *PlanStoryActivity) planContentEquivalent(expected string, observed github.Issue) bool {
	if comparer, ok := h.Forge.(interface {
		PlanContentEquivalent(string, github.Issue) bool
	}); ok {
		return comparer.PlanContentEquivalent(expected, observed)
	}
	return expected == observed.Body
}
