package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// BugMergeAction identifies the guarded delivery integration activity.
const BugMergeAction = "lf_bug_merge"

// BugMergeSpec declares every supported integration outcome.
func BugMergeSpec() graphexec.ActionSpec {
	return graphexec.ActionSpec{Name: BugMergeAction, Workflow: agent.WorkflowBug, Outcomes: []string{"confirmed", "stale", "blocked", "failed", "pending"}}
}

// BugMergeForge restricts integration to the existing delivery API and independent observations.
type BugMergeForge interface {
	GetDeliveryPR(context.Context, github.Repo, int) (github.DeliveryPR, error)
	GetDeliveryHead(context.Context, github.Repo, string) (string, error)
	GetDeliveryCommit(context.Context, github.Repo, string) (github.DeliveryCommit, error)
	MergeDeliveryPR(context.Context, github.Repo, int, string) error
}

// BugMergeInput names the exact tested review and durable integration operation.
type BugMergeInput struct {
	SessionID               int64
	Operation, ReviewDigest string
	At                      time.Time
}

// BugMergeOutput distinguishes confirmed integration from stale, pending and blocked effects.
type BugMergeOutput struct {
	Outcome, Reason string
	Merge           *store.BugMergeOperation
}

// BugMergeActivity rechecks exact authorization and reconciles the guarded merge request.
type BugMergeActivity struct {
	Profile  *graphexec.Profile
	Forge    BugMergeForge
	Redactor *redact.Redactor
	Resolve  func(context.Context, ActivityRequest) (BugMergeInput, error)
	// Observe rechecks immutable source/recipe/environment/permission bindings.
	// Remote main is observed separately because integration changes it to M.
	Observe func(context.Context, store.BugCheckBinding) error
}

// RegisterBugMerge binds the concrete integration handler to the Bug namespace.
func RegisterBugMerge(specs *graphexec.Registry, activities *ActivityRegistry, h *BugMergeActivity) error {
	if specs == nil || activities == nil || h == nil || h.Profile == nil || h.Forge == nil || h.Redactor == nil || h.Resolve == nil || h.Observe == nil {
		return errors.New("incomplete Bug merge factory bindings")
	}
	if _, ok := activities.Resolve(BugMergeAction); ok {
		return errors.New("bug merge already registered")
	}
	if err := specs.Register(BugMergeSpec()); err != nil {
		return err
	}
	return activities.Register(BugMergeAction, h)
}

// Execute validates the pinned running graph before resolving domain inputs.
func (h *BugMergeActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if opts.DB == nil || h.Profile == nil || h.Resolve == nil || req.Workflow != agent.WorkflowBug || req.Mode != "" {
		return ActivityResult{}, errors.New("invalid Bug merge activity binding")
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
	if !ok || n.Action != BugMergeAction || d.Workflow != "bug" || d.Mode != "" || d.Digest != h.Profile.Digest || i.CurrentNode != req.Node || a.InstanceID != i.ID || a.Node != req.Node || a.Status != store.GraphRunning {
		return ActivityResult{}, errors.New("bug merge graph binding mismatch")
	}
	in, err := h.Resolve(ctx, req)
	if err != nil {
		return ActivityResult{}, err
	}
	if in.SessionID != i.SessionID {
		return ActivityResult{}, errors.New("bug merge session mismatch")
	}
	out, err := h.Merge(ctx, opts.DB, in)
	if err != nil {
		return ActivityResult{}, err
	}
	raw, err := json.Marshal(out)
	return ActivityResult{Outcome: out.Outcome, Kind: store.GraphOutputJSON, Output: raw}, err
}

func mergeOutput(m store.BugMergeOperation) BugMergeOutput {
	return BugMergeOutput{Outcome: m.Outcome, Reason: m.Reason, Merge: &m}
}
func (h *BugMergeActivity) observe(ctx context.Context, db *store.DB, in BugMergeInput, o store.BugIntegrationObservation) (BugMergeOutput, error) {
	o.At = in.At
	if o.RequestError != "" {
		safe, err := h.Redactor.Apply(ctx, o.RequestError, 8192)
		if err != nil {
			return BugMergeOutput{}, err
		}
		o.RequestError = safe.String()
	}
	if o.BindingError != "" {
		safe, err := h.Redactor.Apply(ctx, o.BindingError, 8192)
		if err != nil {
			return BugMergeOutput{}, err
		}
		o.BindingError = safe.String()
	}
	// PR body is not integration proof and is excluded from this observation.
	o.PR.Body = ""
	m, err := db.ObserveBugMerge(ctx, in.SessionID, in.Operation, o)
	return mergeOutput(m), err
}
func mergePRMatches(p github.DeliveryPR, w store.BugPullRequest) bool {
	return p.ID == w.ID && p.Number == w.Number && p.CreatorID == w.CreatorID && p.Head == w.Head && p.Branch == w.Branch && p.HeadRepo == w.HeadRepo && p.BaseRepo == w.BaseRepo && p.Target == "main"
}

// Merge performs at most one guarded forge mutation. All dispatched uncertainty
// is reconciled by independent reads; an open PR alone never admits a retry.
func (h *BugMergeActivity) Merge(ctx context.Context, db *store.DB, in BugMergeInput) (BugMergeOutput, error) {
	if db == nil || h.Forge == nil || h.Redactor == nil || h.Observe == nil || in.At.IsZero() {
		return BugMergeOutput{}, errors.New("incomplete Bug merge input")
	}
	m, err := db.EnsureBugMerge(ctx, in.SessionID, in.Operation, in.ReviewDigest)
	if err != nil {
		return BugMergeOutput{}, err
	}
	if m.Outcome == "confirmed" {
		_, err = db.ConfirmedBugIntegration(ctx, in.SessionID, in.Operation)
		if err != nil {
			safe, e := h.Redactor.Apply(ctx, err.Error(), 8192)
			if e != nil {
				return BugMergeOutput{}, e
			}
			return BugMergeOutput{Outcome: "blocked", Reason: safe.String(), Merge: &m}, nil
		}
		return mergeOutput(m), nil
	}
	r := m.Binding.Review
	parts := strings.Split(r.Evidence.Identity.Repository, "/")
	if len(parts) != 2 {
		return BugMergeOutput{}, errors.New("invalid retained Bug repository")
	}
	repo := github.Repo{Owner: parts[0], Name: parts[1]}
	p, err := h.Forge.GetDeliveryPR(ctx, repo, r.PR.Number)
	if err != nil {
		return h.observe(ctx, db, in, store.BugIntegrationObservation{Kind: "unavailable", RequestError: err.Error()})
	}
	if p.Merged {
		return h.confirm(ctx, db, in, m, repo, p)
	}
	if m.Dispatched {
		return h.observe(ctx, db, in, store.BugIntegrationObservation{Kind: "integration", PR: bugPRProjection(p, ""), RequestError: "prior merge dispatch remains uncertain; open PR does not prove nonexecution"})
	}
	if m.Outcome != "pending" || m.Exception != "" {
		return mergeOutput(m), nil
	}
	if !mergePRMatches(p, r.PR) || p.State != "open" {
		return h.stop(ctx, db, in, m, "blocked", "remote PR identity, state, head or target differs from approved review")
	}
	if err = h.Observe(ctx, r.Evidence.Child.Request.Binding); err != nil {
		return h.stop(ctx, db, in, m, "stale", err.Error())
	}
	_, revision, err := db.BugMergePrerequisites(ctx, in.SessionID, in.Operation)
	if err != nil {
		return h.stop(ctx, db, in, m, "blocked", err.Error())
	}
	// Observe again immediately before the durable claim, after local guards.
	p, err = h.Forge.GetDeliveryPR(ctx, repo, r.PR.Number)
	if err != nil {
		return h.observe(ctx, db, in, store.BugIntegrationObservation{Kind: "unavailable", RequestError: err.Error()})
	}
	if p.Merged {
		return h.confirm(ctx, db, in, m, repo, p)
	}
	if !mergePRMatches(p, r.PR) || p.State != "open" {
		return h.stop(ctx, db, in, m, "stale", "PR changed immediately before merge")
	}
	tip, err := h.Forge.GetDeliveryHead(ctx, repo, "main")
	if err != nil {
		return h.observe(ctx, db, in, store.BugIntegrationObservation{Kind: "unavailable", RequestError: err.Error()})
	}
	pre, err := h.observe(ctx, db, in, store.BugIntegrationObservation{Kind: "precheck", PR: bugPRProjection(p, ""), Main: tip})
	if err != nil {
		return pre, err
	}
	if tip != r.Evidence.Child.Request.Binding.TargetCommit {
		return h.stop(ctx, db, in, m, "stale", "main changed before merge dispatch")
	}
	if err = ctx.Err(); err != nil {
		return BugMergeOutput{}, err
	}
	claimed, err := db.ClaimBugMerge(ctx, in.SessionID, in.Operation, revision)
	if err != nil {
		return BugMergeOutput{}, err
	}
	if !claimed {
		latest, e := db.GetBugMerge(ctx, in.SessionID, in.Operation)
		return mergeOutput(latest), e
	}
	requestErr := h.Forge.MergeDeliveryPR(ctx, repo, r.PR.Number, r.PR.Head)
	o := store.BugIntegrationObservation{Kind: "request"}
	if requestErr != nil {
		o.RequestError = requestErr.Error()
		var api *github.APIError
		if errors.As(requestErr, &api) && api.Method == http.MethodPut && api.RequestID != "" {
			endpoint, e := url.Parse(api.URL)
			if e == nil && endpoint.Path == fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", repo.Owner, repo.Name, r.PR.Number) {
				switch api.StatusCode {
				case http.StatusForbidden, http.StatusMethodNotAllowed, http.StatusConflict, http.StatusUnprocessableEntity:
					o.Kind = "request_refused"
				}
			}
		}
	}
	result, err := h.observe(ctx, db, in, o)
	if err != nil {
		return result, errors.Join(requestErr, err)
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	p, err = h.Forge.GetDeliveryPR(ctx, repo, r.PR.Number)
	if err != nil {
		return h.observe(ctx, db, in, store.BugIntegrationObservation{Kind: "unavailable", RequestError: err.Error()})
	}
	return h.confirm(ctx, db, in, *result.Merge, repo, p)
}

func (h *BugMergeActivity) stop(ctx context.Context, db *store.DB, in BugMergeInput, m store.BugMergeOperation, outcome, reason string) (BugMergeOutput, error) {
	safe, err := h.Redactor.Apply(ctx, reason, 8192)
	if err != nil {
		return BugMergeOutput{}, err
	}
	if outcome == "stale" {
		if err = db.InvalidateBugTestCall(ctx, m.Binding.Review.Evidence.Child.ID, safe.String()); err != nil {
			return BugMergeOutput{}, err
		}
		if err = db.InvalidateBugDeliveryReview(ctx, in.SessionID, in.ReviewDigest, safe.String()); err != nil {
			return BugMergeOutput{}, err
		}
	}
	updated, err := db.StopBugMerge(ctx, in.SessionID, in.Operation, outcome, safe.String())
	return mergeOutput(updated), err
}
func (h *BugMergeActivity) confirm(ctx context.Context, db *store.DB, in BugMergeInput, m store.BugMergeOperation, repo github.Repo, p github.DeliveryPR) (BugMergeOutput, error) {
	o := store.BugIntegrationObservation{Kind: "integration", PR: bugPRProjection(p, ""), MergeCommit: p.MergeSHA, MergedAt: p.MergedAt}
	// Retain the remote fact even if further reads fail or authorization is absent.
	initial, err := h.observe(ctx, db, in, o)
	if err != nil {
		return initial, err
	}
	if !p.Merged || p.MergeSHA == "" {
		return initial, nil
	}
	commit, err := h.Forge.GetDeliveryCommit(ctx, repo, p.MergeSHA)
	if err != nil {
		o.RequestError = err.Error()
		return h.observe(ctx, db, in, o)
	}
	if commit.SHA != p.MergeSHA {
		return h.stop(ctx, db, in, m, "blocked", "merge commit read returned a different identity")
	}
	o.Parents = commit.Parents
	// Persist commit proof independently before the main read.
	if _, err = h.observe(ctx, db, in, o); err != nil {
		return BugMergeOutput{}, err
	}
	tip, err := h.Forge.GetDeliveryHead(ctx, repo, "main")
	if err != nil {
		o.RequestError = err.Error()
		return h.observe(ctx, db, in, o)
	}
	o.Main = tip
	if err = h.Observe(ctx, m.Binding.Review.Evidence.Child.Request.Binding); err != nil {
		o.BindingError = err.Error()
	}
	return h.observe(ctx, db, in, o)
}
