package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/bugrepair"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// BugPRAction is the Bug-owned PR preparation and review activity.
const BugPRAction = "lf_bug_pr"

// BugPRSpec declares every permitted domain outcome.
func BugPRSpec() graphexec.ActionSpec {
	return graphexec.ActionSpec{Name: BugPRAction, Workflow: agent.WorkflowBug, Outcomes: []string{"review-ready", "approved", "clarify", "repair", "reshape", "stale", "blocked"}}
}

// BugPRForge grants only PR reads and creation, never merge or closure.
type BugPRForge interface {
	GetDeliveryHead(context.Context, github.Repo, string) (string, error)
	ListDeliveryPRs(context.Context, github.Repo, string) ([]github.DeliveryPR, error)
	GetDeliveryPR(context.Context, github.Repo, int) (github.DeliveryPR, error)
	CreateDeliveryPR(context.Context, github.Repo, string, string, redact.Body, redact.Body) (github.DeliveryPR, error)
}

// BugPRInput comes from pinned parent state. Operation identifies this logical
// observation; IntentOperation is unchanged through all successive PR reviews.
type BugPRInput struct {
	SessionID, ChildCallID                           int64
	Operation, IntentOperation, Branch, ReviewDigest string
	Reply                                            *store.GraphOwnerReply
	At                                               time.Time
	Presentation                                     []store.BugDeliveryClarification
}

// BugPROutput separates retained review, observed publication and owner decision.
type BugPROutput struct {
	Outcome, Reason string
	Review          *store.BugDeliveryReview
	Decision        *store.BugDeliveryDecision
}

// BugPRActivity prepares one tested PR under factory-selected bindings.
type BugPRActivity struct {
	// Publish is the parent-owned durable review publication adapter.
	Publish  func(context.Context, store.BugDeliveryReview) (store.BugDeliveryReview, error)
	Profile  *graphexec.Profile
	Forge    BugPRForge
	Redactor *redact.Redactor
	BotID    int64
	Resolve  func(context.Context, ActivityRequest) (BugPRInput, error)
	// Observe must reobserve committed source, pinned target recipe and execution
	// context through the existing check boundary; it grants no new authority.
	Observe func(context.Context, store.BugCheckBinding) error
}

// RegisterBugPR requires complete factory wiring before graph admission.
func RegisterBugPR(specs *graphexec.Registry, activities *ActivityRegistry, h *BugPRActivity) error {
	if specs == nil || activities == nil || h == nil || h.Profile == nil || h.Forge == nil || h.Redactor == nil || h.BotID <= 0 || h.Resolve == nil || h.Observe == nil {
		return errors.New("incomplete Bug PR factory bindings")
	}
	if _, ok := activities.Resolve(BugPRAction); ok {
		return errors.New("bug PR already registered")
	}
	if err := specs.Register(BugPRSpec()); err != nil {
		return err
	}
	return activities.Register(BugPRAction, h)
}

// Execute validates the pinned running graph before resolving domain inputs.
func (h *BugPRActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if opts.DB == nil || h.Profile == nil || h.Resolve == nil || req.Workflow != agent.WorkflowBug || req.Mode != "" {
		return ActivityResult{}, errors.New("invalid Bug PR activity binding")
	}
	i, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	d, err := opts.DB.GetGraphDefinition(ctx, i.DefinitionID)
	if err != nil {
		return ActivityResult{}, err
	}
	a, err := opts.DB.GetGraphNodeAttempt(ctx, req.NodeAttemptID)
	if err != nil {
		return ActivityResult{}, err
	}
	n, ok := h.Profile.Node(req.Node)
	if !ok || n.Action != BugPRAction || d.Workflow != "bug" || d.Mode != "" || d.Digest != h.Profile.Digest || i.CurrentNode != req.Node || a.InstanceID != i.ID || a.Node != req.Node || a.Status != store.GraphRunning {
		return ActivityResult{}, errors.New("bug PR graph binding mismatch")
	}
	in, err := h.Resolve(ctx, req)
	if err != nil {
		return ActivityResult{}, err
	}
	if in.SessionID != i.SessionID {
		return ActivityResult{}, errors.New("bug PR session mismatch")
	}
	out, err := h.Prepare(ctx, opts.DB, in)
	if err != nil {
		return ActivityResult{}, err
	}
	raw, err := json.Marshal(out)
	return ActivityResult{Outcome: out.Outcome, Kind: store.GraphOutputJSON, Output: raw}, err
}
func bugPRProjection(p github.DeliveryPR, body string) store.BugPullRequest {
	return store.BugPullRequest{ID: p.ID, CreatorID: p.CreatorID, Number: p.Number, Head: p.Head, Branch: p.Branch, Target: p.Target, HeadRepo: p.HeadRepo, BaseRepo: p.BaseRepo, State: p.State, Body: body, Merged: p.Merged}
}
func bugPRMatches(p store.BugPullRequest, i store.BugPRIntent, e store.BugDeliveryEvidence) bool {
	return i.Marker != "" && i.CreatorID > 0 && p.ID > 0 && p.Number > 0 && p.CreatorID == i.CreatorID && p.Branch == i.Branch && p.HeadRepo == e.Identity.Repository && p.BaseRepo == e.Identity.Repository && p.Target == "main" && p.State == "open" && !p.Merged && p.Head == e.Repair.SourceCommit && strings.Contains(p.Body, i.Marker) && (i.PR == nil || (i.PR.ID == p.ID && i.PR.Number == p.Number))
}
func (h *BugPRActivity) observePR(ctx context.Context, db *store.DB, in BugPRInput, suffix string, p github.DeliveryPR, reason string) (store.BugPullRequest, error) {
	body, err := h.Redactor.Apply(ctx, p.Body, 65536)
	if err == nil && body.Truncated() {
		err = errors.New("bug PR provenance body truncated")
	}
	if err != nil {
		identity := store.BugPullRequest{ID: p.ID, Number: p.Number}
		retained := db.ObserveBugPR(ctx, in.SessionID, store.BugPRObservation{Operation: in.Operation + ":" + suffix + ":identity", Reason: "PR content could not be safely retained", PR: &identity, At: in.At})
		return identity, errors.Join(err, retained)
	}
	safeReason, err := h.Redactor.Apply(ctx, reason, 8192)
	if err != nil {
		identity := store.BugPullRequest{ID: p.ID, Number: p.Number}
		retained := db.ObserveBugPR(ctx, in.SessionID, store.BugPRObservation{Operation: in.Operation + ":" + suffix + ":identity", Reason: "PR diagnostic could not be safely retained", PR: &identity, At: in.At})
		return identity, errors.Join(err, retained)
	}

	projection := bugPRProjection(p, body.String())
	err = db.ObserveBugPR(ctx, in.SessionID, store.BugPRObservation{Operation: in.Operation + ":" + suffix, Reason: safeReason.String(), PR: &projection, At: in.At})
	return projection, err
}
func (h *BugPRActivity) stale(ctx context.Context, db *store.DB, in BugPRInput, reason string) (BugPROutput, error) {
	safe, err := h.Redactor.Apply(ctx, reason, 8192)
	if err != nil {
		return BugPROutput{}, err
	}
	if err = db.InvalidateBugTestCall(ctx, in.ChildCallID, safe.String()); err != nil {
		return BugPROutput{}, err
	}
	if in.ReviewDigest != "" {
		if err = db.InvalidateBugDeliveryReview(ctx, in.SessionID, in.ReviewDigest, safe.String()); err != nil {
			return BugPROutput{}, err
		}
	}
	return BugPROutput{Outcome: "stale", Reason: safe.String()}, nil
}

// Prepare executes one bounded PR operation. It never publishes an issue
// comment, pushes source, reserves repair work, runs checks or merges a PR.
func (h *BugPRActivity) Prepare(ctx context.Context, db *store.DB, in BugPRInput) (BugPROutput, error) {
	if db == nil || h.Forge == nil || h.Redactor == nil || h.BotID <= 0 || h.Observe == nil || in.SessionID <= 0 || in.ChildCallID <= 0 || in.Operation == "" || len(in.Operation) > 128 || in.At.IsZero() {
		return BugPROutput{}, errors.New("invalid Bug PR input")
	}
	if err := ctx.Err(); err != nil {
		return BugPROutput{}, err
	}
	if in.Reply != nil {
		recovered, replayErr := db.RecoverBugDeliveryReply(ctx, in.SessionID, in.ReviewDigest, *in.Reply)
		if replayErr == nil {
			r, readErr := db.GetBugDeliveryReview(ctx, in.SessionID, in.ReviewDigest)
			if readErr != nil {
				return BugPROutput{}, readErr
			}
			outcome := recovered.Kind
			if outcome == "discussion" {
				outcome = "clarify"
			}
			return BugPROutput{Outcome: outcome, Review: &r, Decision: &recovered}, nil
		}
		if !errors.Is(replayErr, store.ErrNotFound) {
			return BugPROutput{}, replayErr
		}
	}
	// The public child handoff is mandatory, and is repeated atomically below
	// when state is persisted. Receipt or graph success alone cannot pass it.
	if _, err := db.PassedBugTestCall(ctx, in.ChildCallID); err != nil {
		return BugPROutput{}, err
	}
	e, revision, err := db.BugDeliveryPrerequisites(ctx, in.SessionID, in.ChildCallID)
	if err != nil {
		return BugPROutput{}, err
	}
	if err = h.Observe(ctx, e.Child.Request.Binding); err != nil {
		return h.stale(ctx, db, in, err.Error())
	}
	ref, err := naming.ParseRef(fmt.Sprintf("github:%s#%d", e.Identity.Repository, e.Identity.IssueNumber))
	if err != nil {
		return BugPROutput{}, err
	}
	repo := github.RepoFromRef(ref)
	i, err := db.EnsureBugPRIntent(ctx, store.BugPRIntent{SessionID: in.SessionID, Operation: in.IntentOperation, Branch: in.Branch, CreatorID: h.BotID}, in.ChildCallID, revision)
	if err != nil {
		return BugPROutput{}, err
	}
	if i.Cancelled {
		return BugPROutput{Outcome: "blocked", Reason: "Bug delivery cancelled"}, nil
	}
	head, err := h.Forge.GetDeliveryHead(ctx, repo, i.Branch)
	if err != nil {
		return BugPROutput{}, err
	}
	target, err := h.Forge.GetDeliveryHead(ctx, repo, "main")
	if err != nil {
		return BugPROutput{}, err
	}
	if head != e.Repair.SourceCommit || target != e.Child.Request.Binding.TargetCommit {
		return h.stale(ctx, db, in, "remote tested head or main target changed")
	}
	var p github.DeliveryPR
	if i.PR != nil {
		p, err = h.Forge.GetDeliveryPR(ctx, repo, i.PR.Number)
	} else {
		candidates, readErr := h.Forge.ListDeliveryPRs(ctx, repo, i.Branch)
		if readErr != nil {
			return BugPROutput{}, readErr
		}
		if len(candidates) > 1 {
			return BugPROutput{Outcome: "blocked", Reason: "multiple Bug task PR candidates require reconciliation"}, nil
		}
		if len(candidates) == 1 {
			p = candidates[0]
		} else {
			if i.Dispatched {
				return BugPROutput{Outcome: "blocked", Reason: "PR creation remains uncertain; an empty listing cannot authorize another create"}, nil
			}
			title, redErr := h.Redactor.Apply(ctx, fmt.Sprintf("Bug repair for issue #%d", e.Identity.IssueNumber), 256)
			if redErr != nil {
				return BugPROutput{}, redErr
			}
			body, redErr := h.Redactor.Apply(ctx, fmt.Sprintf("Tested Bug repair for issue #%d. Delivery requires the separate exact owner review.\n\n%s", e.Identity.IssueNumber, i.Marker), 65536)
			if redErr != nil {
				return BugPROutput{}, redErr
			}
			claimed, claimErr := db.ClaimBugPRCreate(ctx, in.SessionID, in.ChildCallID, revision)
			if claimErr != nil {
				return BugPROutput{}, claimErr
			}
			if !claimed {
				return BugPROutput{Outcome: "blocked", Reason: "Bug PR create already claimed or cancelled"}, nil
			}
			p, err = h.Forge.CreateDeliveryPR(ctx, repo, i.Branch, "main", title, body)
			reason := "create response received"
			if err != nil {
				reason = err.Error()
			}
			if _, saveErr := h.observePR(ctx, db, in, "create", p, reason); saveErr != nil {
				return BugPROutput{}, saveErr
			}
			if err != nil {
				// The persisted create intent owns reconciliation; transport uncertainty is a domain block.
				return BugPROutput{Outcome: "blocked", Reason: "Bug PR creation uncertain; retained request requires reconciliation"}, nil //nolint:nilerr // Remote effects must be reconciled before retry.
			}
		}
	}
	if err != nil {
		return BugPROutput{}, err
	}
	projected, err := h.observePR(ctx, db, in, "candidate", p, "PR candidate observed")
	if err != nil {
		return BugPROutput{}, err
	}
	if !bugPRMatches(projected, i, e) {
		return BugPROutput{Outcome: "blocked", Reason: "Bug PR identity, provenance, state or tested head mismatch"}, nil
	}
	// Always independently read back even a successful create or branch listing.
	p, err = h.Forge.GetDeliveryPR(ctx, repo, p.Number)
	if err != nil {
		return BugPROutput{}, err
	}
	projected, err = h.observePR(ctx, db, in, "readback", p, "independent PR readback")
	if err != nil {
		return BugPROutput{}, err
	}
	if !bugPRMatches(projected, i, e) {
		return BugPROutput{Outcome: "blocked", Reason: "independent Bug PR readback conflicts"}, nil
	}
	if err = h.Observe(ctx, e.Child.Request.Binding); err != nil {
		return h.stale(ctx, db, in, err.Error())
	}
	if in.Reply != nil {
		return h.applyReply(ctx, db, in, e, projected)
	}
	return h.prepareReview(ctx, db, in, e, projected, revision)
}

func (h *BugPRActivity) applyReply(ctx context.Context, db *store.DB, in BugPRInput, e store.BugDeliveryEvidence, p store.BugPullRequest) (BugPROutput, error) {
	r, err := db.GetBugDeliveryReview(ctx, in.SessionID, in.ReviewDigest)
	if err != nil {
		return BugPROutput{}, err
	}
	facts, err := db.BugFacts(ctx, in.SessionID, "")
	if err != nil {
		return BugPROutput{}, err
	}
	selfChecks, err := db.BugSelfChecks(ctx, in.SessionID, e.Repair.Request)
	if err != nil {
		return BugPROutput{}, err
	}
	digest, err := store.BugDeliveryReviewDigest(e, p, facts, store.SummarizeBugSelfChecks(selfChecks), r.Presentation...)
	if err != nil {
		return BugPROutput{}, err
	}
	if digest != r.Digest {
		return h.stale(ctx, db, in, "Bug delivery review no longer matches observed evidence")
	}
	_, revision, err := db.BugDeliveryPrerequisites(ctx, in.SessionID, in.ChildCallID)
	if err != nil {
		return BugPROutput{}, err
	}
	decision, err := db.ApplyBugDeliveryReply(ctx, in.SessionID, r.Digest, *in.Reply, revision)
	if err != nil {
		return BugPROutput{}, err
	}
	outcome := decision.Kind
	if outcome == "discussion" {
		outcome = "clarify"
	}
	return BugPROutput{Outcome: outcome, Review: &r, Decision: &decision}, nil
}
func (h *BugPRActivity) prepareReview(ctx context.Context, db *store.DB, in BugPRInput, e store.BugDeliveryEvidence, p store.BugPullRequest, revision int64) (BugPROutput, error) {
	facts, err := db.BugFacts(ctx, in.SessionID, "")
	if err != nil {
		return BugPROutput{}, err
	}
	selfChecks, err := db.BugSelfChecks(ctx, in.SessionID, e.Repair.Request)
	if err != nil {
		return BugPROutput{}, err
	}
	selfCheckSummary := store.SummarizeBugSelfChecks(selfChecks)
	digest, err := store.BugDeliveryReviewDigest(e, p, facts, selfCheckSummary, in.Presentation...)
	if err != nil {
		return BugPROutput{}, err
	}
	r, err := db.GetBugDeliveryReview(ctx, in.SessionID, digest)
	if err == nil {
		if !r.Active {
			return BugPROutput{Outcome: "blocked", Reason: r.Reason, Review: &r}, nil
		}
		confirmed, confirmErr := db.ConfirmBugDeliveryReview(ctx, in.SessionID, digest)
		if confirmErr != nil {
			if h.Publish != nil {
				published, e := h.Publish(ctx, r)
				if e != nil {
					return BugPROutput{}, e
				}
				return BugPROutput{Outcome: "review-ready", Review: &published}, nil
			}
			if errors.Is(confirmErr, store.ErrNotFound) {
				return BugPROutput{Outcome: "blocked", Reason: "Bug review publication pending", Review: &r}, nil
			}
			return BugPROutput{}, confirmErr
		}
		return BugPROutput{Outcome: "review-ready", Review: &confirmed}, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return BugPROutput{}, err
	}
	r = store.BugDeliveryReview{SessionID: in.SessionID, Digest: digest, Evidence: e, PR: p, Presentation: in.Presentation, DeliveryKey: "bug-delivery:" + digest, BotID: h.BotID}
	view := report.BugDeliveryReview{Task: fmt.Sprintf("github:%s#%d", e.Identity.Repository, e.Identity.IssueNumber), Digest: digest, Head: p.Head, Target: e.Child.Request.Binding.TargetCommit, Recipe: e.Child.Request.Binding.RecipeDigest, PR: p.Number, Analysis: renderBugAnalysis(e.Analysis.Revision, e.Analysis.Digest, e.Analysis.Analysis), Repair: renderBugRepair(e.Coverage), Facts: bugFactLines(facts), ApprovalCommand: r.ApprovalCommand()}
	var known []string
	for n, phase := range []string{"regression", "focused", "full"} {
		b := e.Child.PhaseBinding(phase)
		record, readErr := db.GetBugCheckPhase(ctx, in.SessionID, b.Operation)
		if readErr != nil {
			return BugPROutput{}, readErr
		}
		view.Phases[n] = bugCheckRow(phase, record)
		if bugKnownFailurePhase(record) {
			known = append(known, phase)
		}
	}
	view.Lead = bugDeliveryLead(p.Number, e, known...)
	for _, c := range in.Presentation {
		references := strings.Join(c.References, ", ")
		if references == "" {
			references = "Unanswered: owner resolution or explicit reshape remains required"
		}
		view.Clarifications = append(view.Clarifications, fmt.Sprintf("Owner reply %d: %s\nRelevant retained evidence: %s", c.CommentID, c.Body, references))
	}
	rendered, err := report.RenderBugDeliveryReview(view)
	if err != nil {
		return BugPROutput{}, err
	}
	body, err := h.Redactor.Apply(ctx, rendered, 60000)
	if err != nil {
		return BugPROutput{}, err
	}
	if body.Truncated() {
		return BugPROutput{}, errors.New("bug review required evidence truncated")
	}
	r.Body = body.String()
	sum := sha256.Sum256([]byte(r.Body))
	r.ContentDigest = hex.EncodeToString(sum[:])
	if err = db.RetainBugDeliveryReview(ctx, r, revision); err != nil {
		return BugPROutput{}, err
	}
	if h.Publish != nil {
		published, err := h.Publish(ctx, r)
		if err != nil {
			return BugPROutput{}, err
		}
		return BugPROutput{Outcome: "review-ready", Review: &published}, nil
	}
	return BugPROutput{Outcome: "blocked", Reason: "Bug review retained; publication is pending", Review: &r}, nil
}

// bugDeliveryLead is what the owner scans first: the PR is ready, what it
// fixes, that every check phase passed and whether every criterion is covered.
// known names the phases that passed only with known failures the repair did
// not cause, so the owner reads them in the check table before approving.
func bugDeliveryLead(pr int, e store.BugDeliveryEvidence, known ...string) string {
	var b strings.Builder
	if len(known) == 0 {
		fmt.Fprintf(&b, "**The fix is ready for your review in PR #%d, and all project checks passed.**\n\n", pr)
	} else {
		fmt.Fprintf(&b, "**The fix is ready for your review in PR #%d; the project checks passed, with known failures the repair did not cause.**\n\n", pr)
	}
	fmt.Fprintf(&b, "- **Bug:** %s\n", firstSentence(e.Analysis.Analysis.Actual))
	fmt.Fprintf(&b, "- **Fix:** %s\n", firstSentence(e.Coverage.Summary))
	var gaps []string
	for _, c := range e.Coverage.Coverage {
		if strings.TrimSpace(c.Gap) != "" {
			gaps = append(gaps, "`"+c.Criterion+"`")
		}
	}
	total := len(e.Coverage.Coverage)
	if len(gaps) == 0 {
		fmt.Fprintf(&b, "- **Success criteria:** all %d covered by changes and tests.\n", total)
	} else {
		fmt.Fprintf(&b, "- **Success criteria:** %d of %d covered; gaps in %s (see below).\n", total-len(gaps), total, strings.Join(gaps, ", "))
	}
	if extra := bugFilesBeyondAnalysis(e); len(extra) > 0 {
		fmt.Fprintf(&b, "- **Also changed:** %s, beyond the files the analysis expected; check these in the PR.\n", strings.Join(extra, ", "))
	}
	if len(known) == 0 {
		b.WriteString("- **Checks:** regression, focused and full all passed on the tested head.\n")
	} else {
		fmt.Fprintf(&b, "- **Checks:** regression, focused and full passed on the tested head; %s passed with known failures: flaky tests or tests that also fail on main. They are listed under Checks below.\n", strings.Join(known, " and "))
	}
	b.WriteString("\n**Next:** review the PR; to merge it, reply with the approval line at the end of this comment. Otherwise reply with `/bug-review` and what should change.\n")
	return b.String()
}

// bugFilesBeyondAnalysis lists files the repair changed or tested that the
// approved analysis did not expect; the list is guidance, so the owner sees them.
func bugFilesBeyondAnalysis(e store.BugDeliveryEvidence) []string {
	b := e.Analysis.Analysis.Boundaries
	if b == nil {
		return nil
	}
	expected := b.AllowedPaths
	seen := map[string]bool{}
	var extra []string
	for _, c := range e.Coverage.Coverage {
		for _, p := range append(slices.Clone(c.Changes), c.Tests...) {
			if !seen[p] && !slices.Contains(expected, p) {
				seen[p] = true
				extra = append(extra, "`"+p+"`")
			}
		}
	}
	return extra
}

// renderBugRepair lays out the fixer's summary and per-criterion coverage.
func renderBugRepair(e bugrepair.Evidence) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n| Criterion | Changed | Tested by | Gap |\n|---|---|---|---|\n", strings.TrimSpace(e.Summary))
	codes := func(paths []string) string {
		if len(paths) == 0 {
			return "—"
		}
		out := make([]string, len(paths))
		for i, p := range paths {
			out[i] = "`" + p + "`"
		}
		return strings.Join(out, "<br>")
	}
	for _, c := range e.Coverage {
		gap := bugCell(c.Gap)
		if gap == "" {
			gap = "none"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", c.Criterion, codes(c.Changes), codes(c.Tests), gap)
	}
	return b.String()
}

// bugKnownFailurePhase reports a phase that passed only with flaky or
// pre-existing failing tests, which the review must show.
func bugKnownFailurePhase(record store.BugCheckPhaseRecord) bool {
	return record.Outcome == "passed" && strings.HasPrefix(record.Reason, BugKnownFailuresPrefix)
}

// bugCheckRow is one row of the delivery review's check table.
func bugCheckRow(phase string, record store.BugCheckPhaseRecord) string {
	var spent time.Duration
	for _, c := range record.Commands {
		if !c.Observation.Ended.IsZero() && c.Observation.Ended.After(c.Observation.Started) {
			spent += c.Observation.Ended.Sub(c.Observation.Started)
		}
	}
	result := record.Outcome
	if reason := strings.TrimSpace(record.Reason); bugKnownFailurePhase(record) {
		// The reason already says "passed with known failures: ...".
		result = bugCell(reason)
	} else if reason != "" {
		result += ": " + bugCell(reason)
	}
	return fmt.Sprintf("| %s | %s | %d | %s |", phase, result, len(record.Commands), spent.Round(time.Second))
}
