package turn

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// planCandidate names the exact candidate one graph Plan attempt would produce.
func planCandidate(instanceID, attemptID int64) string {
	return fmt.Sprintf("plan-%d-%d", instanceID, attemptID)
}

// planRequestID is the payload identity the factory binds to one attempt's story
// request. It is distinct from the transport request identity of a submission:
// resending the same bytes keeps the transport identity and this stays the same.
func planRequestID(instanceID, attemptID int64) string {
	return fmt.Sprintf("plan-request-%d-%d", instanceID, attemptID)
}

// planGraphBinding derives every factory-owned value of one graph Plan node from
// retained state. None of it comes from agent output, so a result that supplies
// any of it is refused and the recorded values are these.
func planGraphBinding(state planbrief.PlanState, story, stage string, instanceID, attemptID int64, existingRevision string) (agentresult.PlanGraphBinding, error) {
	// The set stage is about no story, so it has no upstream candidates to bind.
	upstream := []string{}
	if stage != planSetStage {
		var err error
		if upstream, err = state.UpstreamReferences(story, stage); err != nil {
			return agentresult.PlanGraphBinding{}, fmt.Errorf("resolve Plan upstream references: %w", err)
		}
	}
	binding := agentresult.PlanGraphBinding{Story: story, Stage: stage, Candidate: planCandidate(instanceID, attemptID), RequestID: planRequestID(instanceID, attemptID), Upstream: upstream, SharedRevision: state.SharedRevision, ExistingRevision: existingRevision}
	if promoted, ok := state.PromotedStory(story); ok && stage == "story" {
		binding.Promoted, binding.PromotedDependencies = true, promoted.Dependencies
	}
	return binding, nil
}

// resultContext resolves the trusted application-result context of one graph
// Plan node from current state. The shared result service calls it afresh for
// every preflight and every submit, and both reach the same validation through
// it, so identical bytes get the same outcome while authority, revisions and
// bound state are unchanged, and a change refuses stale bytes without retaining
// them. It reads state and writes nothing: no event, reservation or approval.
func (r *planGraphRuntime) resultContext(ctx context.Context, a ActivityRequest) (agentresult.Context, error) {
	state, _, err := r.state(ctx)
	if err != nil {
		return agentresult.Context{}, err
	}
	existing := ""
	if r.stage != planSetStage {
		draft, err := r.opts.DB.GetPlanStoryDraft(ctx, r.parentRef.String(), r.selected)
		switch {
		case err == nil && draft.Candidate != "":
			// An issue created at promotion has no draft yet: its first draft is a create.
			existing = draft.Revision
		case err == nil:
		case !errors.Is(err, store.ErrNotFound):
			return agentresult.Context{}, err
		}
	}
	binding, err := planGraphBinding(state, r.selected, r.stage, a.InstanceID, a.NodeAttemptID, existing)
	if err != nil {
		r.notePublishing(a, err)
		return agentresult.Context{}, err
	}
	binding.Upstream = r.reboundUpstream(state, a, binding.Upstream)
	binding.Discussion = r.discussion
	return agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: "plan", WorkflowVersion: store.WorkflowSeparated, QuestionsAllowed: questionPermission(r.req.mode(), r.req.Command), PlanScope: r.scope, PlanGraph: &binding}, nil
}

// planPreparedBasis is what one agent attempt of a story stage was prepared
// against: the exact upstream references and the story's review revision for
// that stage. It lives only in the process that prepared the attempt.
type planPreparedBasis struct {
	attempt  int64
	upstream []string
	review   string
}

// planUpstreamDeferral carries, from the result resolver to the failure path of
// the same run, the first refusal met because an upstream story was only
// publishing its stage, and whether the failed step was ended quietly for it.
type planUpstreamDeferral struct {
	// mu guards publishing: the resolver runs on the result service's goroutine.
	mu         sync.Mutex
	publishing error
	deferred   bool
	// cause is the failed step's cause joined with the publishing refusal.
	cause error
}

// recorded returns the kept publishing refusal, if any.
func (d *planUpstreamDeferral) recorded() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.publishing
}

// notePublishing keeps the first upstream-publishing refusal the resolver met
// during an attempt (preflight, submit or acceptance). A refused submit retains
// nothing, so without this the cause would be lost by acceptance time.
func (r *planGraphRuntime) notePublishing(a ActivityRequest, err error) {
	if !errors.Is(err, planbrief.ErrPlanUpstreamPublishing) {
		return
	}
	r.upstream.mu.Lock()
	defer r.upstream.mu.Unlock()
	if r.upstream.publishing == nil {
		r.upstream.publishing = fmt.Errorf("attempt %d: %w", a.NodeAttemptID, err)
	}
}

// reboundUpstream keeps the upstream references the attempt was prepared with
// when only a dependency's candidate name moved since: the story's own
// candidate is unchanged and so is its review revision for the stage, so the
// result was written against the same reviewed basis (#1172). Any other change,
// including an owner edit of the story itself, keeps the current list, and the
// recorded result is then refused as stale.
func (r *planGraphRuntime) reboundUpstream(state planbrief.PlanState, a ActivityRequest, current []string) []string {
	basis := r.prepared
	if r.stage == planSetStage || basis.attempt != a.NodeAttemptID || basis.review == "" || slices.Equal(basis.upstream, current) {
		return current
	}
	own := func(refs []string) []string {
		var out []string
		for _, ref := range refs {
			if strings.HasPrefix(ref, r.selected+":") {
				out = append(out, ref)
			}
		}
		return out
	}
	if !slices.Equal(own(basis.upstream), own(current)) {
		return current
	}
	if review, err := state.ReviewRevision(r.selected, r.stage); err != nil || review != basis.review {
		return current
	}
	return slices.Clone(basis.upstream)
}

// resultResolver adapts resultContext to the shared service's resolver hook for
// one node attempt.
func (r *planGraphRuntime) resultResolver(a ActivityRequest) func(context.Context) (agentresult.Context, error) {
	return func(ctx context.Context) (agentresult.Context, error) { return r.resultContext(ctx, a) }
}

// planGraphResultTargets are the fields a graph Plan node may read and edit: the
// legacy Plan targets plus the story request and story set.
func planGraphResultTargets() []agentresult.Target {
	return append(agentresult.EditTargets("plan"), agentresult.PlanGraphEditTargets()...)
}
