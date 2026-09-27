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

// BugCloseAction identifies the guarded issue closure activity.
const BugCloseAction = "lf_bug_close"

// BugCloseSpec declares every supported closure outcome.
func BugCloseSpec() graphexec.ActionSpec {
	return graphexec.ActionSpec{Name: BugCloseAction, Workflow: agent.WorkflowBug, Outcomes: []string{"confirmed", "blocked", "failed", "pending"}}
}

// BugCloseForge restricts closure to issue observation and the existing close request.
type BugCloseForge interface {
	BaseURL() string
	GetIssue(context.Context, github.Repo, int) (github.Issue, error)
	CloseDeliveredParent(context.Context, github.Repo, int, int64) error
}

// BugCloseInput binds closure to a task, durable operation and observed time.
type BugCloseInput struct {
	SessionID                 int64
	Operation, MergeOperation string
	At                        time.Time
}

// BugCloseOutput distinguishes confirmed closure from pending or blocked effects.
type BugCloseOutput struct {
	Outcome, Reason string
	Closure         *store.BugClosureOperation
}

// BugCloseActivity executes and reconciles authorized closure using retained evidence.
type BugCloseActivity struct {
	Profile  *graphexec.Profile
	Forge    BugCloseForge
	Redactor *redact.Redactor
	Resolve  func(context.Context, ActivityRequest) (BugCloseInput, error)
}

// RegisterBugClose binds the concrete closure handler to the Bug namespace.
func RegisterBugClose(specs *graphexec.Registry, activities *ActivityRegistry, h *BugCloseActivity) error {
	if specs == nil || activities == nil || h == nil || h.Profile == nil || h.Forge == nil || h.Redactor == nil || h.Resolve == nil {
		return errors.New("incomplete Bug close factory bindings")
	}
	if _, ok := activities.Resolve(BugCloseAction); ok {
		return errors.New("bug close already registered")
	}
	if err := specs.Register(BugCloseSpec()); err != nil {
		return err
	}
	return activities.Register(BugCloseAction, h)
}

// Execute validates the pinned running graph before resolving domain inputs.
func (h *BugCloseActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if opts.DB == nil || h.Profile == nil || h.Resolve == nil || req.Workflow != agent.WorkflowBug || req.Mode != "" {
		return ActivityResult{}, errors.New("invalid Bug close activity binding")
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
	if !ok || n.Action != BugCloseAction || d.Workflow != "bug" || d.Mode != "" || d.Digest != h.Profile.Digest || i.CurrentNode != req.Node || a.InstanceID != i.ID || a.Node != req.Node || a.Status != store.GraphRunning {
		return ActivityResult{}, errors.New("bug close graph binding mismatch")
	}
	in, err := h.Resolve(ctx, req)
	if err != nil {
		return ActivityResult{}, err
	}
	if in.SessionID != i.SessionID {
		return ActivityResult{}, errors.New("bug close session mismatch")
	}
	out, err := h.Close(ctx, opts.DB, in)
	if err != nil {
		return ActivityResult{}, err
	}
	raw, err := json.Marshal(out)
	return ActivityResult{Outcome: out.Outcome, Kind: store.GraphOutputJSON, Output: raw}, err
}

func closeOutput(c store.BugClosureOperation) BugCloseOutput {
	return BugCloseOutput{Outcome: c.Outcome, Reason: c.Reason, Closure: &c}
}
func (h *BugCloseActivity) observe(ctx context.Context, db *store.DB, in BugCloseInput, o store.BugClosureObservation) (BugCloseOutput, error) {
	o.At = in.At
	if o.Reason != "" {
		safe, e := h.Redactor.Apply(ctx, o.Reason, 8192)
		if e != nil {
			return BugCloseOutput{}, e
		}
		o.Reason = safe.String()
	}
	c, e := db.ObserveBugClosure(ctx, in.SessionID, o)
	return closeOutput(c), e
}
func (h *BugCloseActivity) read(ctx context.Context, db *store.DB, in BugCloseInput, repo github.Repo, number int) (BugCloseOutput, error) {
	i, e := h.Forge.GetIssue(ctx, repo, number)
	if e != nil {
		return h.observe(ctx, db, in, store.BugClosureObservation{Kind: "unavailable", Reason: e.Error()})
	}
	return h.observe(ctx, db, in, store.BugClosureObservation{Kind: "issue", ID: i.ID, Number: i.Number, Endpoint: i.APIURL, State: i.State})
}

// Close observes only the issue recorded in confirmed integration. Remote closed
// state never grants missing authorization, and confirmed closure never causes a
// second write or an automatic response to external reopening.
func (h *BugCloseActivity) Close(ctx context.Context, db *store.DB, in BugCloseInput) (BugCloseOutput, error) {
	if db == nil || h.Forge == nil || h.Redactor == nil || in.At.IsZero() {
		return BugCloseOutput{}, errors.New("incomplete Bug close input")
	}
	m, err := db.GetBugMerge(ctx, in.SessionID, in.MergeOperation)
	if err != nil {
		return BugCloseOutput{}, err
	}
	id := m.Binding.Review.Evidence.Identity
	parts := strings.Split(id.Repository, "/")
	if len(parts) != 2 {
		return BugCloseOutput{}, errors.New("invalid Bug issue repository")
	}
	repo := github.Repo{Owner: parts[0], Name: parts[1]}
	endpoint := strings.TrimRight(h.Forge.BaseURL(), "/") + fmt.Sprintf("/repos/%s/issues/%d", id.Repository, id.IssueNumber)
	if _, err = db.EnsureBugClosure(ctx, in.SessionID, in.Operation, in.MergeOperation, endpoint); err != nil {
		return BugCloseOutput{}, err
	}
	result, err := h.read(ctx, db, in, repo, int(id.IssueNumber))
	if err != nil {
		return result, err
	}
	c := result.Closure
	if c == nil || c.Outcome == "confirmed" || c.Outcome == "blocked" || c.Conflict || c.Dispatched || c.Confirmed {
		return result, nil
	}
	// Only a matching open observation can be claimed, never an unavailable read.
	if len(c.Observations) == 0 {
		return result, nil
	}
	last := c.Observations[len(c.Observations)-1]
	if last.Kind != "issue" || last.State != "open" {
		return result, nil
	}
	revision, err := db.BugClosurePrerequisites(ctx, in.SessionID)
	if err != nil {
		return h.observe(ctx, db, in, store.BugClosureObservation{Kind: "blocked", Reason: err.Error()})
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	claimed, err := db.ClaimBugClosure(ctx, in.SessionID, revision)
	if err != nil {
		return BugCloseOutput{}, err
	}
	if !claimed {
		latest, e := db.GetBugClosure(ctx, in.SessionID)
		return closeOutput(latest), e
	}
	requestErr := h.Forge.CloseDeliveredParent(ctx, repo, int(id.IssueNumber), id.IssueID)
	o := store.BugClosureObservation{Kind: "request"}
	if requestErr != nil {
		o.Reason = requestErr.Error()
		var api *github.APIError
		if errors.As(requestErr, &api) && api.Method == http.MethodPatch && api.RequestID != "" {
			u, e := url.Parse(api.URL)
			if e == nil && u.String() == endpoint {
				switch api.StatusCode {
				case http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity:
					o.Kind = "request_refused"
				}
			}
		}
	}
	result, err = h.observe(ctx, db, in, o)
	if err != nil {
		return result, errors.Join(requestErr, err)
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	return h.read(ctx, db, in, repo, int(id.IssueNumber))
}
