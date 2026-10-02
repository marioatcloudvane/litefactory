package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

type bugRetainedNode struct {
	ResultID       int64                    `json:"result_id"`
	ResultRevision int64                    `json:"result_revision"`
	Node           agentresult.ResearchNode `json:"node"`
}

func (r *BugRuntime) retained(ctx context.Context, instance int64, node string) (bugRetainedNode, error) {
	var out bugRetainedNode
	a, e := r.opts.DB.LatestGraphNodeAttempt(ctx, instance, node)
	if e != nil {
		return out, e
	}
	if a.Status != store.GraphSucceeded {
		return out, errors.New("bug source invocation did not succeed")
	}
	outputs, e := r.opts.DB.ListGraphOutputsBounded(ctx, a.ID, agentresult.MaxResearchNodeBytes, 16)
	if e != nil {
		return out, e
	}
	count := 0
	for _, o := range outputs {
		if o.Kind == store.GraphOutputJSON {
			count++
			if e = json.Unmarshal(o.Payload, &out); e != nil {
				return out, e
			}
		}
	}
	if count != 1 || out.ResultID <= 0 || out.ResultRevision <= 0 || out.Node.Result.Payload.Bug == nil {
		return out, errors.New("bug retained source is absent or ambiguous")
	}
	return out, nil
}
func (r *BugRuntime) parentPolicy(w *researchWorkflow) BugParentPolicy {
	profile := r.definition.Profile
	session := w.req.SessionID
	analyst := BugAnalystActivity{Resolve: func(ctx context.Context, a ActivityRequest) (BugAnalystInput, error) {
		state, _, _, e := r.opts.DB.CurrentBugAnalysis(ctx, session)
		if e != nil {
			return BugAnalystInput{}, e
		}
		invocation := fmt.Sprintf("analysis:%d:%d", session, a.NodeAttemptID)
		_, retained, e := BugAnalystContext(ctx, r.opts.DB, session, invocation)
		if e != nil {
			return BugAnalystInput{}, e
		}
		return BugAnalystInput{SessionID: session, ExpectedRevision: state.Revision, Invocation: invocation, InputDigest: bugRuntimeDigest(retained)}, nil
	}, Prepare: func(ctx context.Context, _ Options, a ActivityRequest, c agentresult.Context, s string) (ResearchInvocation, error) {
		return r.prepareRole(ctx, w, a, c, s)
	}}
	fixer := BugFixerActivity{Resolve: func(ctx context.Context, a ActivityRequest) (BugFixerInput, error) {
		repair, e := r.opts.DB.LatestBugRepair(ctx, session)
		if e != nil {
			return BugFixerInput{}, e
		}
		tree, e := r.worktree(ctx, session)
		if e != nil {
			return BugFixerInput{}, e
		}
		if e = tree.ObserveBugCheckSource(ctx, repair.SourceCommit, repair.SourceTree); e != nil {
			return BugFixerInput{}, e
		}
		return BugFixerInput{SessionID: session, Reservation: repair.Request, SourceCommit: repair.SourceCommit, SourceTree: repair.SourceTree}, nil
	}, Prepare: func(ctx context.Context, _ Options, a ActivityRequest, c agentresult.Context, s string) (ResearchInvocation, error) {
		return r.prepareRole(ctx, w, a, c, s)
	}}
	record := BugRecordActivity{Profile: profile, Resolve: func(ctx context.Context, a ActivityRequest) (BugRecordInput, error) {
		identity, e := r.identity(ctx, session)
		if e != nil {
			return BugRecordInput{}, e
		}
		out := BugRecordInput{Identity: identity}
		if a.Node != "record_analysis" {
			return out, nil
		}
		result, e := r.retained(ctx, a.InstanceID, "analyst")
		if e != nil {
			return out, e
		}
		invocation := result.Node.Result.Payload.Bug.Invocation
		if e = ReconcileBugAnalystSuccess(ctx, r.opts.DB, session, invocation); e != nil {
			return out, e
		}
		state, _, _, e := r.opts.DB.CurrentBugAnalysis(ctx, session)
		if e != nil {
			return out, e
		}
		out.Application = &store.BugAnalysisApplication{SessionID: session, ExpectedRevision: state.Revision, Invocation: invocation, Operation: fmt.Sprintf("analysis-result:%d:%d", result.ResultID, result.ResultRevision), InputDigest: bugRuntimeDigest(result), ResultID: result.ResultID, ResultRevision: result.ResultRevision}
		return out, nil
	}}
	attempt := BugAttemptActivity{Profile: profile, Resolve: func(ctx context.Context, a ActivityRequest) (BugAttemptInput, error) {
		state, analysis, approval, e := r.opts.DB.CurrentBugAnalysis(ctx, session)
		if e != nil {
			return BugAttemptInput{}, e
		}
		if approval == nil {
			return BugAttemptInput{}, store.ErrBugRepairUnauthorized
		}
		tree, e := r.worktree(ctx, session)
		if e != nil {
			return BugAttemptInput{}, e
		}
		request := fmt.Sprintf("repair:%d:%d", session, a.NodeAttemptID)
		// Main is merged before HEAD is read, so the repair's source commit,
		// and every check bound to it, already includes current main.
		blocked, e := r.syncBugTarget(ctx, session, tree)
		if e != nil {
			return BugAttemptInput{}, e
		}
		if blocked != "" {
			return BugAttemptInput{ExpectedRevision: state.Revision, Request: store.BugRepairRequest{SessionID: session, Request: request, GraphAttemptID: a.NodeAttemptID}, BlockedReason: blocked}, nil
		}
		commit, e := tree.RevParse(ctx, "HEAD", git.RevParseOptions{})
		if e != nil {
			return BugAttemptInput{}, e
		}
		sha, e := tree.RevParse(ctx, "HEAD^{tree}", git.RevParseOptions{})
		if e != nil {
			return BugAttemptInput{}, e
		}
		if e = tree.ObserveBugCheckSource(ctx, commit, sha); e != nil {
			return BugAttemptInput{}, e
		}
		life, le := r.opts.DB.GetBugLifecycle(ctx, session)
		if le != nil {
			return BugAttemptInput{}, le
		}
		grantReference := ""
		if strings.HasPrefix(life.Reference, "exhaustion:") {
			// A granted attempt re-enters here through the budget-grant
			// continuation, whose own reference names the exhaustion, not the
			// review reply (or other cause) that originally asked for this
			// repair. Recover that from the exhausted reservation's retained
			// failure evidence instead of trusting life.Reference for cause
			// below (builder note on re-entry cause recomputation).
			grantReference = life.Reference
		}
		cause, evidence := "initial", approval.Review
		prior, pe := r.opts.DB.LatestBugRepair(ctx, session)
		if pe == nil {
			if prior.State == "reshape" && prior.AnalysisDigest != analysis.Digest {
				cause, evidence = "reshape", prior.Request
			} else {
				child, ce := r.opts.DB.LatestBugTestCall(ctx, session)
				if ce == nil && child.Request.Binding.Repair == prior.Request && child.Outcome == "failed" {
					cause, evidence = "test_failure", fmt.Sprintf("child:%d", child.ID)
				} else {
					reference := life.Reference
					if grantReference != "" {
						reference, le = r.opts.DB.BugExhaustedReservationEvidence(ctx, session, grantReference)
						if le != nil {
							return BugAttemptInput{}, le
						}
					}
					if reference == "" {
						return BugAttemptInput{}, errors.New("bug repair lacks retained feedback cause")
					}
					cause, evidence = "pr_feedback", reference
				}
			}
		} else if !errors.Is(pe, store.ErrNotFound) {
			return BugAttemptInput{}, pe
		}
		return BugAttemptInput{ExpectedRevision: state.Revision, Request: store.BugRepairRequest{SessionID: session, Request: request, GraphAttemptID: a.NodeAttemptID, AnalysisRevision: analysis.Revision, AnalysisDigest: analysis.Digest, ApprovalReview: approval.Review, Cause: cause, Evidence: evidence, SourceCommit: commit, SourceTree: sha, GrantReference: grantReference}}, nil
	}}
	tests := &BugTestGraphActivity{Observe: r.observe, Resolve: func(ctx context.Context, a ActivityRequest) (store.BugTestRequest, error) {
		return r.testRequest(ctx, session, a)
	}}
	pr := &BugPRActivity{Profile: profile, Forge: r.cfg.Forge, Redactor: r.opts.MCP.Redactor, BotID: r.opts.MCP.BotID, Observe: r.observe, Resolve: func(ctx context.Context, a ActivityRequest) (BugPRInput, error) {
		c, e := r.opts.DB.LatestBugTestCall(ctx, session)
		if e != nil {
			return BugPRInput{}, e
		}
		s, e := r.opts.DB.GetSession(ctx, session)
		if e != nil {
			return BugPRInput{}, e
		}
		review, e := r.opts.DB.CurrentBugDeliveryReview(ctx, session)
		if e != nil && !errors.Is(e, store.ErrNotFound) {
			return BugPRInput{}, e
		}
		presentation, pe := BugDeliveryPresentation(ctx, r.opts.DB, session, c.ID)
		if pe != nil {
			return BugPRInput{}, pe
		}
		return BugPRInput{Presentation: presentation, SessionID: session, ChildCallID: c.ID, Operation: fmt.Sprintf("pr:%d:%d", session, a.NodeAttemptID), IntentOperation: fmt.Sprintf("bug-pr:%d", session), Branch: s.Branch, ReviewDigest: review.Digest, At: r.opts.MCP.Now()}, nil
	}}
	merge := &BugMergeActivity{Profile: profile, Forge: r.cfg.Forge, Redactor: r.opts.MCP.Redactor, Observe: r.observeSource, Resolve: func(ctx context.Context, a ActivityRequest) (BugMergeInput, error) {
		review, e := r.opts.DB.CurrentBugDeliveryReview(ctx, session)
		if e != nil {
			return BugMergeInput{}, e
		}
		return BugMergeInput{SessionID: session, Operation: "merge:" + review.Digest, ReviewDigest: review.Digest, At: r.opts.MCP.Now()}, nil
	}}
	closeActivity := &BugCloseActivity{Profile: profile, Forge: r.cfg.Forge, Redactor: r.opts.MCP.Redactor, Resolve: func(ctx context.Context, a ActivityRequest) (BugCloseInput, error) {
		review, e := r.opts.DB.CurrentBugDeliveryReview(ctx, session)
		if e != nil {
			return BugCloseInput{}, e
		}
		return BugCloseInput{SessionID: session, Operation: "close:" + review.Digest, MergeOperation: "merge:" + review.Digest, At: r.opts.MCP.Now()}, nil
	}}
	return BugParentPolicy{Profile: profile, Analyst: analyst, Fixer: fixer, Record: record, Attempt: attempt, Tests: tests, PR: pr, Merge: merge, Close: closeActivity, Reporter: r.opts.ResearchReporter, RepairRecord: func(ctx context.Context, _ Options, a ActivityRequest) (ActivityResult, error) {
		v, e := r.retained(ctx, a.InstanceID, "fixer")
		if errors.Is(e, store.ErrNotFound) {
			// An owner retry of this step starts a new instance at
			// record_repair; the fixer's result is in the one it replaced.
			prior, pe := r.opts.DB.BugContinuationPrior(ctx, session, a.InstanceID)
			if pe != nil {
				return ActivityResult{}, errors.Join(e, pe)
			}
			v, e = r.retained(ctx, prior, "fixer")
		}
		if e != nil {
			return ActivityResult{}, e
		}
		repair, e := r.opts.DB.LatestBugRepair(ctx, session)
		if e != nil {
			return ActivityResult{}, e
		}
		completion := store.BugRepairCompletion{SessionID: session, Request: repair.Request, ResultID: v.ResultID, ResultRevision: v.ResultRevision}
		response := v.Node.Result.Payload.Bug.Response
		if response != "done" {
			state, _, _, e := r.opts.DB.CurrentBugAnalysis(ctx, session)
			if e != nil {
				return ActivityResult{}, e
			}
			e = r.opts.DB.ApplyBugRepairStop(ctx, completion, state.Revision)
			return ActivityResult{Outcome: response}, e
		}
		tree, e := r.worktree(ctx, session)
		if e != nil {
			return ActivityResult{}, e
		}
		if e = r.recoverRepairPublication(ctx, repair, tree); e != nil {
			return ActivityResult{}, e
		}
		handoff, e := NewBugRepairHandoff(r.opts.DB, tree, nil)
		if e != nil {
			return ActivityResult{}, e
		}
		completed, e := handoff.Complete(ctx, r.opts.DB, completion)
		if e != nil {
			return ActivityResult{}, e
		}
		raw, e := json.Marshal(completed)
		return ActivityResult{Outcome: "done", Kind: store.GraphOutputJSON, Output: raw}, e
	}}
}
func (r *BugRuntime) observeSource(ctx context.Context, b store.BugCheckBinding) error {
	if b.EnvironmentRevision != bugRuntimeDigest(r.cfg.Isolation.Environment) || b.PermissionRevision != r.cfg.Isolation.Revision {
		return errors.New("bug check environment or permission revision changed")
	}
	w, e := r.worktree(ctx, b.SessionID)
	if e != nil {
		return e
	}
	if e = w.ObserveBugCheckSource(ctx, b.SourceCommit, b.SourceTree); e != nil {
		return e
	}
	raw, e := w.ReadBugCheckRecipe(ctx, b.TargetCommit)
	if e != nil {
		return e
	}
	if config.BugRecipeDigest(raw) != b.RecipeDigest {
		return errors.New("bug target recipe digest changed")
	}
	return nil
}
func (r *BugRuntime) observe(ctx context.Context, b store.BugCheckBinding) error {
	if e := r.observeSource(ctx, b); e != nil {
		return e
	}
	s, e := r.opts.DB.GetSession(ctx, b.SessionID)
	if e != nil {
		return e
	}
	ref, e := naming.ParseRef(s.TaskRef)
	if e != nil {
		return e
	}
	if b.TargetRepository != ref.Owner+"/"+ref.Repo || b.TargetBranch != "main" {
		return errors.New("bug target differs from task main")
	}
	head, e := r.cfg.Forge.GetDeliveryHead(ctx, github.Repo{Owner: ref.Owner, Name: ref.Repo}, "main")
	if e != nil {
		return e
	}
	if head != b.TargetCommit {
		return errors.New("bug target tip changed")
	}
	return nil
}
func (r *BugRuntime) testRequest(ctx context.Context, session int64, a ActivityRequest) (store.BugTestRequest, error) {
	repair, e := r.opts.DB.LatestBugRepair(ctx, session)
	if e != nil {
		return store.BugTestRequest{}, e
	}
	tree, e := r.worktree(ctx, session)
	if e != nil {
		return store.BugTestRequest{}, e
	}
	commit, e := tree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if e != nil {
		return store.BugTestRequest{}, e
	}
	sha, e := tree.RevParse(ctx, "HEAD^{tree}", git.RevParseOptions{})
	if e != nil {
		return store.BugTestRequest{}, e
	}
	done, e := r.opts.DB.CompletedBugRepair(ctx, session, repair.Request, commit, sha)
	if e != nil {
		return store.BugTestRequest{}, e
	}
	s, e := r.opts.DB.GetSession(ctx, session)
	if e != nil {
		return store.BugTestRequest{}, e
	}
	ref, e := naming.ParseRef(s.TaskRef)
	if e != nil {
		return store.BugTestRequest{}, e
	}
	target, e := r.cfg.Forge.GetDeliveryHead(ctx, github.Repo{Owner: ref.Owner, Name: ref.Repo}, "main")
	if e != nil {
		return store.BugTestRequest{}, e
	}
	if e = r.fetchTarget(ctx, session); e != nil {
		return store.BugTestRequest{}, e
	}
	raw, e := tree.ReadBugCheckRecipe(ctx, target)
	if e != nil {
		if errors.Is(e, git.ErrBugCheckRecipe) {
			return store.BugTestRequest{}, &BugTestSetupBlocked{Reason: e.Error()}
		}
		return store.BugTestRequest{}, e
	}
	if _, e = config.ParseBugCheckRecipe(raw); e != nil {
		return store.BugTestRequest{}, &BugTestSetupBlocked{Reason: e.Error()}
	}
	life, e := r.opts.DB.GetBugLifecycle(ctx, session)
	if e != nil {
		return store.BugTestRequest{}, e
	}
	operation := fmt.Sprintf("test:%d:%d", session, a.NodeAttemptID)
	request := store.BugTestRequest{SessionID: session, Operation: operation, ParentInstance: a.InstanceID, ParentAttempt: a.NodeAttemptID, ChildDefinition: life.ChildDefinition, DefinitionDigest: r.childDefinition.Profile.Digest, Binding: store.BugCheckBinding{SessionID: session, Operation: operation, Generation: operation, Repair: repair.Request, SourceCommit: done.SourceCommit, SourceTree: done.SourceTree, TargetRepository: ref.Owner + "/" + ref.Repo, TargetBranch: "main", TargetCommit: target, RecipeDigest: config.BugRecipeDigest(raw), EnvironmentRevision: bugRuntimeDigest(r.cfg.Isolation.Environment), PermissionRevision: r.cfg.Isolation.Revision}}
	prior, pe := r.opts.DB.LatestBugTestCall(ctx, session)
	if pe == nil && prior.Request.Binding.Repair == repair.Request {
		previous := prior.Request.Binding
		changed := previous.TargetCommit != target || previous.RecipeDigest != request.Binding.RecipeDigest || previous.EnvironmentRevision != request.Binding.EnvironmentRevision || previous.PermissionRevision != request.Binding.PermissionRevision
		if prior.Outcome == "blocked" {
			resolution, re := r.opts.DB.BugTestResolution(ctx, session, prior.ID)
			if re != nil {
				return store.BugTestRequest{}, re
			}
			request.PreviousCall, request.Resolution = prior.ID, resolution
			return request, nil
		}
		if !changed {
			return store.BugTestRequest{}, errors.New("bug repeat checks require recorded resolved infrastructure continuation")
		}
		if prior.Outcome != "passed" || !prior.Consumed {
			return store.BugTestRequest{}, errors.New("bug stale checks have unresolved prior execution")
		}
		reason := "Factory observed target, recipe or execution-context drift for the same confirmed repair"
		if e = r.opts.DB.InvalidateBugTestCall(ctx, prior.ID, reason); e != nil {
			return store.BugTestRequest{}, e
		}
		request.PreviousCall, request.Resolution = prior.ID, reason
	} else if pe != nil && !errors.Is(pe, store.ErrNotFound) {
		return store.BugTestRequest{}, pe
	}
	return request, nil
}
