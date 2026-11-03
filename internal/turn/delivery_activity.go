package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// DeliveryActivity binds the shared delivery loop to a retained graph candidate.
// Review authority is read from factory records, never from the agent's prose.
type DeliveryActivity struct {
	w        *researchWorkflow
	profile  *graphexec.Profile
	bug      *GraphBugFinalization
	validate *ValidateFlowBindings
	// operation is set only by legacy compatibility wrappers.
	operation string
}

var errDeliveryCandidateChanged = errors.New("reviewed source or base changed")

func registerDelivery(specs *graphexec.Registry, activities *ActivityRegistry, w *researchWorkflow) error {
	if _, exists := activities.Resolve(graphexec.DeliverAction); exists {
		return nil
	}
	delivery := graphexec.DeliverSpec(w.policy.Mode)
	delivery.Workflow = w.policy.Workflow
	if err := specs.Register(delivery); err != nil {
		return err
	}
	return activities.Register(graphexec.DeliverAction, DeliveryActivity{w: w})
}

func preparedDelivery(ctx context.Context, db *store.DB, instance int64) (graphDeliveryProgress, error) {
	var out graphDeliveryProgress
	raw, err := db.LatestGraphRecord(ctx, instance, "delivery_prepared")
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	if err == nil && (out.Outcome != "ready" || out.Commit == "" || out.Binding.BaseCommit == "" || out.PR.Number < 1) {
		err = errors.New("no current prepared delivery")
	}
	return out, err
}

func (h DeliveryActivity) checkout(ctx context.Context, binding GraphDeliveryBinding, fn func(*git.Clone, *git.Worktree) error) error {
	w := h.w
	s, err := w.opts.DB.GetSession(ctx, binding.SessionID)
	if err != nil {
		return err
	}
	if s.LockOwner == "" || s.LockOwner != w.opts.InstanceID || s.Branch != binding.Branch {
		return errors.New("delivery requires current task writer exclusion")
	}
	repo, err := executionRepository(ctx, w.opts.DB, s)
	if err != nil {
		return err
	}
	release, err := w.opts.CloneGate.Acquire(ctx, repo.Path)
	if err != nil {
		return err
	}
	defer release()
	clone, err := w.opts.Git.Open(ctx, repo.Path)
	if err != nil {
		return err
	}
	if err = verifyExecutionClone(ctx, w.opts, s, clone); err != nil {
		return err
	}
	tree, err := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: s.WorktreePath, Branch: s.Branch, BaseBranch: repo.BaseBranch})
	if err != nil {
		return err
	}
	return fn(clone, tree.Worktree)
}

// Execute prepares without merging, or ships only the exact displayed candidate.
func (h DeliveryActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if h.validate != nil {
		node, ok := h.validate.Profile.Node(req.Node)
		if !ok {
			return ActivityResult{}, errors.New("delivery node absent from pinned graph")
		}
		operation := node.Attrs["lf_operation"]
		if h.operation != "" {
			operation = h.operation
		}
		switch operation {
		case "push":
			return (ValidatePushActivity{*h.validate}).execute(ctx, opts, req)
		case "merge":
			return (ValidateMergeActivity{*h.validate}).execute(ctx, opts, req)
		case "close":
			return (ValidateCloseActivity{*h.validate}).execute(ctx, opts, req)
		default:
			return ActivityResult{}, errors.New("invalid delivery operation")
		}
	}
	if h.bug != nil {
		node, ok := h.profile.Node(req.Node)
		if !ok || node.Action != graphexec.DeliverAction {
			return ActivityResult{}, errors.New("delivery node differs from pinned graph")
		}
		handler := *h.bug
		handler.Operation = node.Attrs["lf_operation"]
		return handler.Execute(ctx, opts, req)
	}
	w := h.w
	node, ok := w.definition.Profile.Node(req.Node)
	if !ok || node.Action != graphexec.DeliverAction {
		return ActivityResult{}, errors.New("delivery node differs from pinned graph")
	}
	finish := func(outcome, reason string) (ActivityResult, error) {
		out := ActivityResult{Outcome: outcome, Diagnostic: reason}
		if outcome == "revalidate" {
			raw, e := json.Marshal(map[string]string{"record": "delivery_revalidation", "reason": reason})
			if e != nil {
				return out, e
			}
			out.Kind = store.GraphOutputJSON
			out.Output = raw
		}
		return out, nil
	}
	source, err := latestResearchAgentAttempt(ctx, opts, w.definition.Profile, req.InstanceID, node.Attrs["lf_source"])
	if err != nil {
		return ActivityResult{}, err
	}
	scope := w.policy.baseContext(true)
	scope.WriteAccess = true
	result, err := w.policy.load(ctx, opts.DB, source.ID, scope)
	if err != nil {
		return ActivityResult{}, err
	}
	if result.Result.Outcome != "completed" || len(result.Result.Questions) != 0 {
		return finish("blocked", "Delivery requires an accepted completed result")
	}
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	session, err := opts.DB.GetSession(ctx, instance.SessionID)
	if err != nil {
		return ActivityResult{}, err
	}
	repo, err := executionRepository(ctx, opts.DB, session)
	if err != nil {
		return ActivityResult{}, err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return ActivityResult{}, err
	}
	ticketRepo := github.Repo{Owner: repo.Owner, Name: repo.Name}
	if ref.Owner != "" {
		ticketRepo = github.RepoFromRef(ref)
	}
	binding := GraphDeliveryBinding{Record: "delivery_binding", InstanceID: instance.ID, SessionID: session.ID, IssueNumber: session.IssueNumber, Repository: github.Repo{Owner: repo.Owner, Name: repo.Name}, TicketRepository: ticketRepo, Branch: session.Branch, Target: repo.BaseBranch, Summary: result.Result.Comment, RunID: source.RunID, SourceAttemptID: source.ID, Marker: fmt.Sprintf("<!-- lf:delivery instance=%d -->", instance.ID)}
	binding.IssueReference = session.TaskRef
	operation := node.Attrs["lf_operation"]
	code, _ := opts.MCP.Comments.(BugCodeForge)
	tickets, _ := opts.ResearchReporter.forge.(BugCloseForge)
	if opts.Bug != nil {
		if opts.Bug.cfg.Code != nil {
			code = opts.Bug.cfg.Code
		}
		if opts.Bug.cfg.Tickets != nil {
			tickets = opts.Bug.cfg.Tickets
		}
	}
	var head string
	expectedPR := 0
	switch operation {
	case "push":
		if w.prep != nil && !w.saved {
			if _, err = w.saveNow(ctx); err != nil {
				return finish("blocked", err.Error())
			}
		}
		err = h.checkout(ctx, binding, func(clone *git.Clone, tree *git.Worktree) error {
			if err := clone.Fetch(ctx, opts.remote()); err != nil {
				return err
			}
			dirty, e := tree.Status(ctx)
			if e != nil {
				return e
			}
			if len(dirty) > 0 {
				return errors.New("candidate has unsaved changes")
			}
			head, e = tree.RevParse(ctx, "HEAD", git.RevParseOptions{})
			if e != nil {
				return e
			}
			binding.BaseCommit, e = tree.RevParse(ctx, "refs/remotes/"+opts.remote()+"/"+binding.Target, git.RevParseOptions{})
			return e
		})
		if err != nil {
			return finish("blocked", err.Error())
		}
		if head == binding.BaseCommit {
			return finish("unchanged", "No branch changes to deliver")
		}
	case "merge", "close":
		prepared, e := preparedDelivery(ctx, opts.DB, instance.ID)
		if e != nil {
			return finish("blocked", e.Error())
		}
		if prepared.Binding.SessionID != binding.SessionID || prepared.Binding.Repository != binding.Repository || prepared.Binding.TicketRepository != binding.TicketRepository || prepared.Binding.Branch != binding.Branch || prepared.Binding.Target != binding.Target {
			return finish("revalidate", "The reviewed delivery target changed")
		}
		if prepared.Binding.SourceAttemptID != source.ID {
			return finish("revalidate", "The reviewed source result changed")
		}
		wait, e := opts.DB.ApprovedGraphReview(ctx, instance.ID, source.ID)
		if e != nil {
			return finish("blocked", e.Error())
		}
		intent, e := opts.DB.GetGraphComment(ctx, wait.DeliveryKey)
		if e != nil {
			return finish("blocked", e.Error())
		}
		review, exists := w.definition.Profile.Node(intent.Node)
		target, released := graphexec.Decide(w.definition.Profile, intent.Node, "approved")
		if !exists || !released || !deliveryApprovalReaches(w.definition.Profile, target, req.Node) || review.Attrs["lf_approve_reply"] == "" || wait.State != "allocated" || wait.SourceAttemptID != source.ID || !strings.EqualFold(strings.TrimSpace(wait.ReplyBody), review.Attrs["lf_approve_reply"]) {
			return finish("blocked", "No consumed owner approval for this delivery")
		}
		// The published review itself must name this prepared commit.
		var shown agentresult.Result
		if e = json.Unmarshal(intent.Candidate, &shown); e != nil {
			return finish("blocked", e.Error())
		}
		if !strings.HasSuffix(shown.Comment, deliveryReviewNotice(prepared, review.Attrs["lf_approve_reply"])) {
			return finish("blocked", "The owner review did not display this commit")
		}
		binding = prepared.Binding
		head = prepared.Commit
		expectedPR = prepared.PR.Number
		if code == nil {
			return finish("blocked", "Delivery requires factory code gateway")
		}
		remotePR, e := code.GetDeliveryPR(ctx, binding.Repository, prepared.PR.Number)
		if e != nil {
			return finish("blocked", e.Error())
		}
		if remotePR.Head != head || remotePR.Branch != binding.Branch || remotePR.Target != binding.Target {
			return finish("revalidate", "The reviewed PR changed")
		}
		alreadyMerged := remotePR.Merged
		err = h.checkout(ctx, binding, func(clone *git.Clone, tree *git.Worktree) error {
			if e := clone.Fetch(ctx, opts.remote()); e != nil {
				return e
			}
			actual, e := tree.RevParse(ctx, "HEAD", git.RevParseOptions{})
			if e != nil {
				return e
			}
			base, e := tree.RevParse(ctx, "refs/remotes/"+opts.remote()+"/"+binding.Target, git.RevParseOptions{})
			if e != nil {
				return e
			}
			dirty, e := tree.Status(ctx)
			if e != nil {
				return e
			}
			if actual != head || (!alreadyMerged && base != binding.BaseCommit) || len(dirty) > 0 {
				return errDeliveryCandidateChanged
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, errDeliveryCandidateChanged) {
				return finish("revalidate", err.Error())
			}
			return finish("blocked", err.Error())
		}
	default:
		return ActivityResult{}, errors.New("invalid delivery operation")
	}
	if tickets == nil || code == nil {
		return finish("blocked", "Delivery requires factory code and ticket gateways")
	}
	issue, e := tickets.GetIssue(ctx, binding.TicketRepository, binding.IssueNumber)
	if e != nil {
		return finish("blocked", e.Error())
	}
	if issue.ID <= 0 || issue.Number != binding.IssueNumber || (binding.IssueID > 0 && issue.ID != binding.IssueID) {
		return finish("blocked", "Delivery issue identity mismatch")
	}
	binding.IssueID = issue.ID
	runtime := &BugRuntime{opts: opts, definition: w.definition}
	handler := GraphDelivery{ExpectedPR: expectedPR, Operation: operation, Code: code, Tickets: tickets, Redactor: opts.MCP.Redactor, Bind: func(context.Context, Options, ActivityRequest) (GraphDeliveryBinding, error) { return binding, nil }, Save: func(context.Context, Options, ActivityRequest, GraphDeliveryBinding) (string, error) {
		return head, nil
	}, PublishSaved: runtime.pushGraphDelivery, SyncBase: func(ctx context.Context, o Options, a ActivityRequest, b GraphDeliveryBinding, head string) (syncBaseOutput, error) {
		sync := SyncBaseBinding{Repo: sessionGraphSyncRepo{runtime: runtime, sessionID: session.ID}, Bind: func(context.Context, Options, ActivityRequest) (SyncBaseTarget, error) {
			return SyncBaseTarget{Working: b.Branch, Base: b.Target, ExpectedHead: head, Checkout: sessionSyncCheckout{runtime: runtime, session: session}, Lock: func(context.Context) (func() error, bool, error) {
				current, e := opts.DB.GetSession(ctx, b.SessionID)
				if e != nil {
					return nil, false, e
				}
				if current.LockOwner == "" || current.LockOwner != opts.InstanceID {
					return nil, false, errors.New("delivery lost task writer exclusion")
				}
				return func() error { return nil }, true, nil
			}}, nil
		}}
		res, e := (SyncBaseActivity{sync}).Execute(ctx, o, a)
		var out syncBaseOutput
		if e == nil {
			e = json.Unmarshal(res.Output, &out)
		}
		return out, e
	}}
	out, e := handler.Execute(ctx, opts, req)
	if e == nil && out.Outcome == "revalidate" {
		return finish("revalidate", out.Diagnostic)
	}
	return out, e
}

// deliveryRevalidationContext carries recorded state, not agent instructions.
func deliveryRevalidationContext(ctx context.Context, db *store.DB, p *graphexec.Profile, a ActivityRequest) (string, error) {
	attempts, e := db.ListGraphNodeAttempts(ctx, a.InstanceID)
	if e != nil {
		return "", e
	}
	for i := len(attempts) - 1; i >= 0; i-- {
		prior := attempts[i]
		if prior.ID >= a.NodeAttemptID {
			continue
		}
		node, ok := p.Node(prior.Node)
		if !ok || node.Action != graphexec.DeliverAction || prior.Outcome != "revalidate" {
			return "", nil
		}
		raw, e := db.LatestGraphRecord(ctx, a.InstanceID, "delivery_revalidation")
		if e != nil {
			return "", e
		}
		return "\n<note>Recorded factory delivery state; no current shipping approval.\n" + string(raw) + "\n</note>\n", nil
	}
	return "", nil
}

func deliveryReviewNotice(p graphDeliveryProgress, reply string) string {
	return fmt.Sprintf("\n\nFactory candidate: commit `%s`, base `%s`, PR #%d into `%s`. Reply `%s` to merge and close.", p.Commit, p.Binding.BaseCommit, p.PR.Number, p.Binding.Target, reply)
}

// A consumed review can authorize closure only through its merge node's declared
// successful edge. It never authorizes another candidate or an unrelated node.
func deliveryApprovalReaches(p *graphexec.Profile, target, current string) bool {
	if target == current {
		return true
	}
	n, ok := p.Node(target)
	next, routed := graphexec.Decide(p, target, "merged")
	return ok && n.Action == graphexec.DeliverAction && n.Attrs["lf_operation"] == "merge" && routed && next == current
}
