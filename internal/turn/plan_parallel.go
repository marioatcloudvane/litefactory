package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

func parallelStory(s *planbrief.PlanState, key string) *planbrief.PlanStoryState {
	for i := range s.Stories {
		if s.Stories[i].Key == key {
			return &s.Stories[i]
		}
	}
	return nil
}

func planStoryFacts(s planbrief.PlanState, key string) map[string]string {
	facts := map[string]string{"story.sizing_valid": "false", "story.fallback": "false"}
	if entry, ok := s.StorySet.EntryForStory(key); ok {
		facts["story.size"] = entry.Size
		reason := strings.TrimSpace(entry.SizeReason)
		if slices.Contains([]string{"S", "M", "L"}, entry.Size) && reason != "" && utf8.ValidString(entry.SizeReason) && utf8.RuneCountInString(reason) <= 280 && !strings.ContainsAny(entry.SizeReason, "\r\n") {
			facts["story.sizing_valid"] = "true"
		}
	}
	if item := parallelStory(&s, key); item != nil && item.Route != nil {
		if item.Route.Fallback != "" {
			facts["story.fallback"] = "true"
		}
		if item.Route.Version != planbrief.PlanParallelVersion {
			facts["story.sizing_valid"] = "false"
		}
	}
	return facts
}

func (r *planGraphRuntime) parallelFact(ctx context.Context, a ActivityRequest) (ActivityResult, error) {
	s, _, err := r.state(ctx)
	if err != nil {
		return ActivityResult{}, err
	}
	condition, err := graphexec.DecodeCondition(graphexec.ResearchIfAction, a.Attrs, graphexec.PlanControlPolicy())
	if err != nil {
		return ActivityResult{}, err
	}
	outcome, err := condition.EvaluatePlanFacts(planStoryFacts(s, r.selected))
	return ActivityResult{Outcome: outcome}, err
}

// parallelBasis binds only selected-story inputs and its transitive dependency
// semantics. Sibling progress alone does not invalidate independent work.
func (r *planGraphRuntime) parallelBasis(s planbrief.PlanState, g *planbrief.PlanParallelGroup) string {
	item := parallelStory(&s, r.selected)
	draft := planbrief.PlanDraft{Key: r.selected}
	if item != nil {
		draft = item.Draft
	}
	own := draft.Revision()
	if g != nil && own != g.ExpectedDraftRevision {
		for _, raw := range g.Projections {
			var request graphexec.PlanStoryRequest
			if json.Unmarshal(raw, &request) == nil && request.Draft.Revision() == own && item != nil && item.Candidate == request.Candidate && item.EffectRequest == request.RequestID && item.Effect == "applied" {
				own = g.ExpectedDraftRevision
				break
			}
		}
	}
	if g != nil && own == g.ExpectedDraftRevision {
		own = g.SourceDraftRevision
	}
	refs := map[string]string{}
	seen := map[string]bool{}
	var walk func(string)
	walk = func(key string) {
		if seen[key] {
			return
		}
		seen[key] = true
		d := parallelStory(&s, key)
		if d == nil {
			refs[key] = "missing"
			return
		}
		raw, _ := json.Marshal(d.Approvals)
		refs[key] = d.Draft.Revision() + string(raw)
		if d.Retired || d.Blocked() {
			refs[key] += "blocked"
		}
		for _, dep := range d.Draft.Dependencies.StoryKeys {
			walk(dep)
		}
	}
	for _, dep := range draft.Dependencies.StoryKeys {
		walk(dep)
	}
	entry, _ := s.StorySet.EntryForStory(r.selected)
	promoted := planbrief.PromotedStory{Entry: entry}
	if s.StorySet != nil {
		for _, id := range entry.DependsOn {
			promoted.Dependencies = append(promoted.Dependencies, s.StorySet.Keys[id])
		}
	}
	raw, _ := json.Marshal(struct {
		Capture, Shared, Own string
		Promoted             planbrief.PromotedStory
		Dependencies         map[string]string
		Scope                planbrief.ContentScope
	}{r.definition.Digest, s.SharedRevision, own, promoted, refs, r.scope})
	return hashBytes(raw)
}

func (r *planGraphRuntime) parallel(ctx context.Context, w *researchWorkflow, a ActivityRequest) (ActivityResult, error) {
	node, ok := r.definition.Profile.Node(a.Node)
	if !ok {
		return ActivityResult{}, errors.New("parallel node missing")
	}
	declaration, err := graphexec.DecodePlanParallel(r.definition.Profile, node)
	if err != nil {
		return ActivityResult{}, err
	}
	if r.selected == "" || r.stage == planSetStage || r.discussion {
		return ActivityResult{}, errors.New("parallel Plan requires a selected story stage")
	}
	if w.prep == nil {
		return ActivityResult{}, errors.New("parallel coordinator preparation missing")
	}
	// Admission is one atomic weighted replacement for coordinator and all members.
	// The grant remains owned by the outer driver until every invocation joins.
	if err = r.opts.DB.UpdatePlanWorkflowState(ctx, r.parentRef.String(), func(s *planbrief.PlanState) (bool, error) {
		if s.StorySet == nil || !s.StorySet.Promoted {
			return false, errors.New("parallel Plan requires a promoted story set")
		}
		item := parallelStory(s, r.selected)
		if item == nil {
			s.Stories = append(s.Stories, planbrief.PlanStoryState{Key: r.selected, Stage: "story", Draft: planbrief.PlanDraft{Key: r.selected}, Approvals: map[string]string{}})
			item = &s.Stories[len(s.Stories)-1]
		}
		if (item.Route != nil && item.Route.Version != planbrief.PlanParallelVersion) || (item.Parallel != nil && item.Parallel.Version != planbrief.PlanParallelVersion) {
			return false, errors.New("unsupported parallel Plan state version")
		}
		if item.Route != nil && item.Route.Fallback != "" {
			return false, nil
		}
		basis := r.parallelBasis(*s, item.Parallel)
		if item.Parallel == nil || item.Parallel.Basis != basis {
			g, e := planbrief.NewPlanParallelGroup(fmt.Sprintf("group-%d-%d", a.InstanceID, a.NodeAttemptID), basis, declaration.Rounds, item.Draft)
			if e != nil {
				return false, e
			}
			g.ParentAttemptID = a.NodeAttemptID
			g.Blockers = append(g.Blockers, item.Draft.Questions...)
			item.Parallel = g
		}
		entry, _ := s.StorySet.EntryForStory(r.selected)
		e := s.RecordPlanRoute(r.selected, planbrief.PlanRouteState{Cycle: fmt.Sprintf("plan-%s", s.StorySet.PromotedRevision), Capture: r.definition.Digest, SourceRevision: s.StorySet.PromotedRevision, EntryID: entry.ID}, planbrief.PlanRouteEvent{Route: "parallel", Node: a.Node, Fact: "story.size", Value: entry.Size})
		return true, e
	}); err != nil {
		return ActivityResult{}, err
	}
	if err = r.recoverParallelMembers(ctx, a); err != nil {
		return ActivityResult{}, err
	}
	for round := 0; round < declaration.Rounds; round++ {
		s, _, e := r.state(ctx)
		if e != nil {
			return ActivityResult{}, e
		}
		item := parallelStory(&s, r.selected)
		if item.Route != nil && item.Route.Fallback != "" {
			return ActivityResult{Outcome: "fallback"}, nil
		}
		g := item.Parallel
		if g == nil || g.Version != planbrief.PlanParallelVersion || r.parallelBasis(s, g) != g.Basis {
			return ActivityResult{Outcome: "superseded"}, nil
		}
		if g.Sealed {
			return ActivityResult{Outcome: "ready"}, nil
		}
		if outcome := parallelBlockerOutcome(g); outcome != "" {
			if outcome == "fallback" {
				err = r.opts.DB.UpdatePlanWorkflowState(ctx, r.parentRef.String(), func(s *planbrief.PlanState) (bool, error) {
					item := parallelStory(s, r.selected)
					for _, m := range item.Parallel.Members {
						if m.Contribution.Growth != "" {
							return true, s.RecordPlanGrowth(r.selected, item.Parallel.GrowthStage(r.stage), m.Contribution.Growth, m.Role)
						}
					}
					return false, nil
				})
				if err != nil {
					return ActivityResult{}, err
				}
			}
			return r.parallelOutcome(ctx, outcome), nil
		}
		roles := []string{}
		for _, role := range planReviewStages {
			m, present := g.Members[role]
			if !present || m.Contribution.Agreement != g.Digest || g.Digest == "" {
				roles = append(roles, role)
			}
		}
		frozen, _ := json.Marshal(s)
		var wg sync.WaitGroup
		errs := make(chan error, len(roles))
		for _, role := range roles {
			wg.Add(1)
			go func(role string) {
				defer wg.Done()
				errs <- r.parallelMember(ctx, w, a, declaration.Members[role], role, *g, frozen)
			}(role)
		}
		wg.Wait()
		close(errs)
		var combined error
		for e := range errs {
			combined = errors.Join(combined, e)
		}
		outcome := ""
		e = r.opts.DB.UpdatePlanWorkflowState(context.WithoutCancel(ctx), r.parentRef.String(), func(s *planbrief.PlanState) (bool, error) {
			item := parallelStory(s, r.selected)
			if item == nil || item.Parallel == nil || item.Parallel.ID != g.ID || r.parallelBasis(*s, item.Parallel) != g.Basis {
				outcome = "superseded"
				return false, nil
			}
			current := item.Parallel
			for _, role := range planReviewStages {
				if m, ok := current.Members[role]; ok && m.Contribution.Growth != "" {
					outcome = "fallback"
					return true, s.RecordPlanGrowth(r.selected, current.GrowthStage(r.stage), m.Contribution.Growth, declaration.Members[role])
				}
			}
			if len(current.Blockers) > 0 {
				outcome = "stopped"
				for _, m := range current.Members {
					if len(m.Contribution.Questions) > 0 || len(m.Contribution.Draft.Questions) > 0 {
						outcome = "ask"
					}
				}
				return true, nil
			}
			if combined != nil {
				return retainParallelFailureOutcome(current, combined, &outcome)
			}
			ready, reconcileErr := current.Reconcile(r.scope)
			if reconcileErr != nil {
				return retainParallelStop(current, reconcileErr, &outcome)
			}
			if !slices.Equal(current.Proposal.Dependencies.StoryKeys, current.Base.Dependencies.StoryKeys) || !slices.Equal(current.Proposal.Dependencies.IssueRefs, current.Base.Dependencies.IssueRefs) {
				current.ReviewRequired = true
				current.Blockers = append(current.Blockers, "Proposed dependencies changed and require owner review.")
				outcome = "ask"
				return true, nil
			}
			if ready {
				if e := current.Seal(current.Basis, r.scope); e != nil {
					return false, e
				}
				outcome = "ready"
			}
			return true, nil
		})
		if e != nil {
			return ActivityResult{}, e
		}
		if outcome != "" {
			return r.parallelOutcome(ctx, outcome), nil
		}
	}
	err = r.opts.DB.UpdatePlanWorkflowState(ctx, r.parentRef.String(), func(s *planbrief.PlanState) (bool, error) {
		item := parallelStory(s, r.selected)
		if item == nil || item.Parallel == nil {
			return false, nil
		}
		item.Parallel.Blockers = append(item.Parallel.Blockers, "parallel Plan continuation limit exhausted")
		return true, nil
	})
	return r.parallelOutcome(ctx, "stopped"), err
}

func (r *planGraphRuntime) parallelMember(ctx context.Context, parent *researchWorkflow, a ActivityRequest, node, role string, g planbrief.PlanParallelGroup, frozen []byte) (returnedErr error) {
	policy := PlanWorkflowPolicy()
	// The member contract is supplied by its captured Markdown resources; the
	// legacy sequential result shape is not a member instruction.
	policy.Guidance = ""
	req := r.req
	req.researchDefinition = r.definition
	req.researchNode = node
	req.workflowPolicy = &policy
	captured, _ := r.definition.Profile.Node(node)
	var err error
	req, err = applyNodeAccess(policy, captured, req)
	if err != nil {
		return err
	}
	deliveryPlan, err := planWorkflowDelivery(r.opts, req, r.definition, node, policy)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(r.opts.Paths.Home(), filepath.Join(r.opts.Paths.BundlesDir(), r.result.Ref.Forge, r.result.Ref.Owner, r.result.Ref.Repo, strconv.Itoa(r.result.Ref.Issue)))
	if err != nil {
		return err
	}
	delivery, err := writeDelivery(ctx, r.opts.Paths.Home(), relative, deliveryPlan)
	if err != nil {
		return err
	}
	run, segment, attempt, _, err := r.opts.DB.AllocatePlanGroupMemberRun(ctx, store.PlanGroupMemberRunStart{ParentAttemptID: a.NodeAttemptID, Role: role, Node: node, Basis: g.Basis, Agent: string(req.Agent), SessionRefForOrdinal: func(ordinal int) (string, error) { return naming.SessionRef(r.result.Ref, ordinal) }, StartedAt: r.opts.MCP.Now(), LogPath: func(ordinal int) string {
		p, _ := naming.RunLogPath(r.opts.Paths.Home(), r.result.Ref, ordinal)
		return p
	}})
	if err != nil {
		return err
	}
	prep := *parent.prep
	prep.run, prep.segment = run, segment
	prep.metaPath, err = naming.RunMetaPath(r.opts.Paths.Home(), r.result.Ref, run.Ordinal)
	var process *agent.RunResult
	defer func() {
		cleanup := context.WithoutCancel(ctx)
		current, e := r.opts.DB.GetGraphNodeAttempt(cleanup, attempt.ID)
		if e == nil && current.Status == store.GraphRunning {
			e = r.opts.DB.CompletePlanGroupMember(cleanup, attempt.ID, store.GraphFailed, "", nil, r.opts.MCP.Now())
		}
		returnedErr = errors.Join(returnedErr, e)
		status := store.RunSucceeded
		if returnedErr != nil {
			status = store.RunFailed
		}
		outcome := store.RunOutcome{Status: status, FinishedAt: r.opts.MCP.Now(), Error: parallelErrorText(returnedErr)}
		if process != nil {
			outcome = runOutcome(process)
			if returnedErr != nil {
				outcome.Status, outcome.Error = store.RunFailed, returnedErr.Error()
			}
		}
		_, e = r.opts.DB.FinishRun(cleanup, run.ID, outcome)
		returnedErr = errors.Join(returnedErr, e)
		if prep.metaPath != "" {
			meta := turnMeta(&prep, process)
			if returnedErr != nil {
				reason := returnedErr.Error()
				meta.Error = &reason
			}
			returnedErr = errors.Join(returnedErr, harness.WriteMeta(prep.metaPath, meta))
		}
	}()
	if err != nil {
		return err
	}
	if err = r.opts.DB.SetSegmentDelivery(ctx, segment.ID, delivery.Identity, delivery.Root); err != nil {
		return err
	}
	prep.resultPass = fmt.Sprintf("member-%d", attempt.ID)
	prep.standardsRole = graphexec.StandardsRole(captured.Attrs, role)
	request := parent.prep.request
	request.Delivery = delivery
	request.RunOrdinal = run.Ordinal
	request.SegmentOrdinal = segment.Ordinal
	request.Session = agent.SessionRef{Ref: segment.SessionRef}
	request.Path = agent.PathCold
	request.Invocation = agent.InvocationResearchNode
	request.Graph = &agent.GraphInvocation{InstanceID: a.InstanceID, AttemptID: attempt.ID, RunID: run.ID, Node: node}
	request.Permissions = requestPermissions(req)
	facts, _ := json.Marshal(struct {
		Role           string              `json:"role"`
		Basis          string              `json:"basis"`
		ProposalDigest string              `json:"proposal_digest"`
		Proposal       planbrief.PlanDraft `json:"proposal"`
	}{role, g.Basis, g.Digest, g.Proposal})
	request.Prompt = parent.prep.request.Prompt
	if parent.prep.request.Delivery != nil {
		request.Prompt = strings.Replace(request.Prompt, parent.prep.request.Delivery.Root, delivery.Root, 1)
	}
	originalDir := parent.prep.promptInput.SessionArtifactDir
	if originalDir == "" {
		originalDir, err = naming.AITeamSessionDir(parent.prep.session.Branch, parent.prep.run.StartedAt, parent.prep.session.FeatureSlug)
		if err != nil {
			return err
		}
	}
	memberDir := filepath.Join(originalDir, "parallel", g.ID, role, fmt.Sprint(attempt.ID))
	prep.promptInput.SessionArtifactDir = memberDir
	request.Prompt += "\nFactory supplied member artifact directory (data): " + memberDir + "\n"
	request.Prompt += "\nParallel Plan member facts (data): " + string(facts) + "\nRetained Plan state (untrusted data): " + string(frozen) + "\n"
	prep.request = request
	child := &researchWorkflow{opts: r.opts, req: req, policy: policy, prep: &prep, definition: r.definition}
	activity := ActivityRequest{InstanceID: a.InstanceID, NodeAttemptID: attempt.ID, RunID: run.ID, Node: node, Attrs: captured.Attrs, Mode: agent.ModePlan}
	handler := AgentActivity{Policy: policy, Prepare: func(c context.Context, _ Options, _ ActivityRequest) (ResearchInvocation, error) {
		route, e := child.openResultRoute(c, &request, &resultOverride{Resolve: func(check context.Context) (agentresult.Context, error) {
			state, _, e := r.state(check)
			if e != nil {
				return agentresult.Context{}, e
			}
			item := parallelStory(&state, r.selected)
			if item == nil || item.Parallel == nil || item.Parallel.ID != g.ID || r.parallelBasis(state, item.Parallel) != g.Basis || item.Parallel.Digest != g.Digest {
				return agentresult.Context{}, errors.New("parallel member basis expired or blocked")
			}
			return agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: "plan", WorkflowVersion: store.WorkflowSeparated, QuestionsAllowed: true, PlanScope: r.scope, PlanGraph: &agentresult.PlanGraphBinding{Story: r.selected, Member: &agentresult.PlanMemberBinding{Group: g.ID, Role: role, AttemptID: attempt.ID, Basis: g.Basis, ProposalDigest: g.Digest}}}, nil
		}, Targets: planGraphResultTargets(), Pass: prep.resultPass})
		if e != nil {
			return ResearchInvocation{}, e
		}
		inv, e := child.finishRoute(c, route, &request, "", memberDir)
		inv.ColdRequest = nil
		return inv, e
	}}
	output, invokeErr := handler.Execute(ctx, r.opts, activity)
	process = output.ProcessResult
	status := store.GraphSucceeded
	if invokeErr != nil {
		status = store.GraphFailed
		output.Outcome = ""
	}
	if e := r.opts.DB.CompletePlanGroupMember(context.WithoutCancel(ctx), attempt.ID, status, output.Outcome, output.Output, r.opts.MCP.Now()); e != nil {
		return errors.Join(invokeErr, e)
	}
	if invokeErr != nil {
		return invokeErr
	}
	retained, e := r.loadParallelMember(ctx, attempt.ID)
	if e != nil {
		return e
	}
	return r.opts.DB.UpdatePlanWorkflowState(ctx, r.parentRef.String(), func(s *planbrief.PlanState) (bool, error) {
		item := parallelStory(s, r.selected)
		if item == nil || item.Parallel == nil || item.Parallel.ID != g.ID || r.parallelBasis(*s, item.Parallel) != g.Basis {
			return false, errors.New("parallel collection superseded")
		}
		c := planbrief.PlanParallelContribution{}
		if retained.Result.Payload.Plan.Contribution != nil {
			c = *retained.Result.Payload.Plan.Contribution
		}
		for _, q := range retained.Result.Questions {
			c.Questions = append(c.Questions, q.ID+": "+q.Text)
		}
		if retained.Result.Outcome != "completed" && len(c.Questions) == 0 {
			c.Failure = retained.Result.Reason
		}
		return true, item.Parallel.Collect(g.Basis, role, attempt.ID, fmt.Sprint(retained.Revision), c)
	})
}

type parallelRetained struct {
	Result   agentresult.Result
	Revision int64
}

func (r *planGraphRuntime) loadParallelMember(ctx context.Context, id int64) (parallelRetained, error) {
	outputs, err := r.opts.DB.ListGraphOutputsBounded(ctx, id, agentresult.MaxResearchNodeBytes, 16)
	if err != nil {
		return parallelRetained{}, err
	}
	var ref planResultRef
	found := false
	for _, o := range outputs {
		if o.Kind == store.GraphOutputJSON {
			var ok bool
			ref, ok = decodePlanResultRef(o.Payload)
			if !ok || found {
				return parallelRetained{}, errors.New("ambiguous parallel member result")
			}
			found = true
		}
	}
	if !found {
		return parallelRetained{}, errors.New("parallel member result missing")
	}
	attempt, err := r.opts.DB.GetGraphNodeAttempt(ctx, id)
	if err != nil {
		return parallelRetained{}, err
	}
	if attempt.Status != store.GraphSucceeded || attempt.RunID != ref.Binding.RunID {
		return parallelRetained{}, errors.New("parallel member reference attempt mismatch")
	}
	result, err := r.opts.DB.GetResultRevision(ctx, ref.Binding, ref.Revision)
	if err != nil {
		return parallelRetained{}, err
	}
	snapshot, _, err := decodeRetainedResult(result)
	if err == nil && (snapshot.Meta.AttemptID != ref.Binding.Attempt || snapshot.Meta.RunID != ref.Binding.RunID || snapshot.Meta.ResultRevision != ref.Revision || snapshot.Meta.Mode != "plan") {
		return parallelRetained{}, errors.New("parallel member shared metadata mismatch")
	}
	return parallelRetained{Result: snapshot.Result, Revision: ref.Revision}, err
}

// parallelQuestions projects retained shared questions without discarding choices.
// Role-local IDs are replaced deterministically for the combined owner batch.
func (r *planGraphRuntime) parallelQuestions(ctx context.Context, g *planbrief.PlanParallelGroup) ([]agentresult.Question, error) {
	var questions []agentresult.Question
	handled := map[string]bool{}
	for _, role := range []string{"story", "technical", "acceptance"} {
		member, ok := g.Members[role]
		if !ok {
			continue
		}
		binding, err := r.opts.DB.PlanGroupMemberResultBinding(ctx, member.AttemptID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		retained, err := r.opts.DB.GetResultRevision(ctx, binding, 0)
		if err != nil {
			return nil, err
		}
		snapshot, _, err := decodeRetainedResult(retained)
		if err != nil {
			return nil, err
		}
		for _, question := range snapshot.Result.Questions {
			handled[question.ID+": "+question.Text] = true
			question.ID = fmt.Sprintf("q%d", len(questions)+1)
			questions = append(questions, question)
		}
	}
	for _, blocker := range g.Blockers {
		if !handled[blocker] {
			questions = append(questions, agentresult.Question{ID: fmt.Sprintf("q%d", len(questions)+1), Text: blocker, Audience: "technical"})
			handled[blocker] = true
		}
	}
	return questions, nil
}

// parallelProjection journals the ordinary stage request before its first
// effect. Receipt recovery reuses these exact bytes and request identities.
func (r *planGraphRuntime) parallelProjection(ctx context.Context, a ActivityRequest) (planRetained, store.GraphNodeAttempt, error) {
	source, err := r.opts.DB.LatestGraphNodeAttempt(ctx, a.InstanceID, a.Attrs["lf_source"])
	if err != nil {
		return planRetained{}, source, err
	}
	if source.Status != store.GraphSucceeded {
		return planRetained{}, source, errors.New("parallel source is not complete")
	}
	var out planRetained
	err = r.opts.DB.UpdatePlanWorkflowState(ctx, r.parentRef.String(), func(s *planbrief.PlanState) (bool, error) {
		item := parallelStory(s, r.selected)
		if item == nil || item.Parallel == nil {
			return false, errors.New("parallel candidate missing")
		}
		g := item.Parallel
		stage := a.Attrs["lf_stage"]
		if stage == "" {
			out = planRetained{Response: "ask", Comment: "Planning needs an owner answer."}
			out.Questions, err = r.parallelQuestions(ctx, g)
			return false, err
		}
		if g.Version != planbrief.PlanParallelVersion || item.Route == nil || item.Route.Version != planbrief.PlanParallelVersion || item.Route.Fallback != "" || r.parallelBasis(*s, g) != g.Basis || !g.Sealed || len(g.Blockers) > 0 {
			return false, errors.New("parallel candidate is no longer current")
		}
		if item.Hold != "" || item.LifecycleHold != "" || item.Wait != nil {
			return false, errors.New("parallel candidate has an unresolved hold or owner wait")
		}
		var request graphexec.PlanStoryRequest
		if raw := g.Projections[stage]; len(raw) > 0 {
			if e := json.Unmarshal(raw, &request); e != nil {
				return false, e
			}
			out = planRetained{Response: "done", Comment: "Coherent parallel plan; all three roles agreed.", Request: &request}
			return false, nil
		}
		if item.Blocked() || item.Wait != nil {
			return false, errors.New("parallel candidate has an unresolved hold or owner wait")
		}
		draft, e := g.Projection(g.Basis, stage)
		if e != nil {
			return false, e
		}
		upstream, e := s.UpstreamReferences(r.selected, stage)
		if e != nil {
			return false, e
		}
		operation := "create"
		revision := ""
		existing, e := r.opts.DB.GetPlanStoryDraft(ctx, r.parentRef.String(), r.selected)
		if e != nil && !errors.Is(e, store.ErrNotFound) {
			return false, e
		}
		changed := []string{}
		if e == nil && existing.Candidate != "" {
			operation = "update"
			revision = existing.Revision
			switch stage {
			case "story":
				changed = []string{"intent", "dependencies", "conversation"}
			case "technical":
				changed = []string{"architecture", "design", "guidance", "conversation"}
			case "acceptance":
				changed = []string{"tasks", "criteria", "conversation"}
			}
		}
		request = graphexec.PlanStoryRequest{RequestID: g.ID + "-" + stage, Operation: operation, Stage: stage, Candidate: g.ID + "-" + stage, ExpectedRevision: revision, Upstream: upstream, SharedRevision: s.SharedRevision, Draft: draft, ChangedSections: changed}
		raw, e := json.Marshal(request)
		if e != nil {
			return false, e
		}
		g.Projections[stage] = raw
		out = planRetained{Response: "done", Comment: "Coherent parallel plan; all three roles agreed.", Request: &request}
		return true, nil
	})
	return out, source, err
}

func (r *planGraphRuntime) parallelApplied(ctx context.Context, a ActivityRequest, receipt PlanStoryReceipt) error {
	source, ok := r.definition.Profile.Node(a.Attrs["lf_source"])
	if !ok || source.Action != graphexec.PlanParallelAction || receipt.Outcome != "applied" {
		return nil
	}
	return r.opts.DB.UpdatePlanWorkflowState(ctx, r.parentRef.String(), func(s *planbrief.PlanState) (bool, error) {
		item := parallelStory(s, r.selected)
		if item == nil || item.Parallel == nil {
			return false, errors.New("applied parallel group disappeared")
		}
		item.Parallel.ExpectedDraftRevision = item.Draft.Revision()
		item.Parallel.Applied[a.Attrs["lf_stage"]] = receipt.RequestID
		if item.Parallel.Applied[a.Attrs["lf_stage"]] == "" {
			item.Parallel.Applied[a.Attrs["lf_stage"]] = receipt.Candidate
		}
		return true, nil
	})
}

// parallelJudgeContext pins the exact sealed projection and its contribution
// manifest as advisory evidence. It creates no result or approval authority.
func (r *planGraphRuntime) parallelJudgeContext(ctx context.Context, a ActivityRequest, input JudgeRequest) (JudgeRequest, error) {
	source, ok := r.definition.Profile.Node(a.Attrs["lf_source"])
	if !ok || source.Action != graphexec.PlanParallelAction {
		return input, nil
	}
	retained, attempt, err := r.parallelProjection(ctx, a)
	if err != nil {
		return input, err
	}
	raw, err := json.Marshal(map[string]any{"result": map[string]any{"outcome": "completed", "payload": map[string]any{"plan": map[string]any{"storyrequest": retained.Request}}}})
	if err != nil {
		return input, err
	}
	state, _, err := r.state(ctx)
	if err != nil {
		return input, err
	}
	item := parallelStory(&state, r.selected)
	manifest, err := json.Marshal(item.Parallel)
	if err != nil {
		return input, err
	}
	input.SourceNodeAttemptID = attempt.ID
	input.PinnedSource = &store.JudgeSource{Task: r.result.Ref.String(), Kind: "graph", Provider: string(r.req.Agent), ID: attempt.ID, Revision: 1, Reference: item.Parallel.ID + ":" + a.Attrs["lf_stage"], Snapshot: raw, PlanContext: manifest, Digest: hashBytes(raw)}
	return input, nil
}

func parallelBlockerOutcome(g *planbrief.PlanParallelGroup) string {
	for _, m := range g.Members {
		if m.Contribution.Growth != "" {
			return "fallback"
		}
	}
	if g.ReviewRequired || len(g.Base.Questions) > 0 {
		return "ask"
	}
	if len(g.Blockers) == 0 {
		return ""
	}
	for _, m := range g.Members {
		if len(m.Contribution.Questions) > 0 || len(m.Contribution.Draft.Questions) > 0 {
			return "ask"
		}
	}
	return "stopped"
}

// recoverParallelMembers closes only abandoned children, then recovers accepted
// shared revisions before deciding whether a role is missing. It never treats
// terminal output or an incomplete shared snapshot as a contribution.
func (r *planGraphRuntime) recoverParallelMembers(ctx context.Context, a ActivityRequest) error {
	if err := r.opts.DB.CloseOrphanPlanGroupMembers(ctx, a.InstanceID, a.NodeAttemptID, r.opts.MCP.Now()); err != nil {
		return err
	}
	state, _, err := r.state(ctx)
	if err != nil {
		return err
	}
	item := parallelStory(&state, r.selected)
	if item == nil || item.Parallel == nil {
		return errors.New("parallel recovery lacks group")
	}
	g := item.Parallel
	members, err := r.opts.DB.ListPlanGroupMembersByBasis(ctx, a.InstanceID, g.Basis)
	if err != nil {
		return err
	}
	for _, m := range members {
		if g.ParentAttemptID > 0 && m.ParentAttemptID < g.ParentAttemptID {
			continue
		}
		if old, ok := g.Members[m.Role]; ok && old.AttemptID >= m.AttemptID {
			continue
		}
		binding, e := r.opts.DB.PlanGroupMemberResultBinding(ctx, m.AttemptID)
		if errors.Is(e, store.ErrNotFound) {
			attempt, readErr := r.opts.DB.GetGraphNodeAttempt(ctx, m.AttemptID)
			if readErr != nil {
				return readErr
			}
			if attempt.Status == store.GraphFailed {
				run, readErr := r.opts.DB.GetRun(ctx, m.RunID)
				if readErr != nil {
					return readErr
				}
				cause := run.Error
				if cause == "" {
					cause = "parallel member failed without a retained result"
				}
				if err = r.opts.DB.UpdatePlanWorkflowState(ctx, r.parentRef.String(), func(s *planbrief.PlanState) (bool, error) {
					item := parallelStory(s, r.selected)
					if item == nil || item.Parallel == nil || item.Parallel.ID != g.ID {
						return false, errors.New("parallel recovery changed")
					}
					return true, item.Parallel.Collect(g.Basis, m.Role, m.AttemptID, "failed", planbrief.PlanParallelContribution{Failure: cause})
				}); err != nil {
					return err
				}
			}
			continue
		}
		if e != nil {
			return e
		}
		retained, e := r.opts.DB.GetResultRevision(ctx, binding, 0)
		if e != nil {
			return e
		}
		snapshot, _, e := decodeRetainedResult(retained)
		if e != nil {
			return e
		}
		proposalDigest := ""
		if plan := snapshot.Result.Payload.Plan; plan != nil && plan.Contribution != nil {
			proposalDigest = plan.Contribution.Agreement
		}
		scope := agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: "plan", WorkflowVersion: store.WorkflowSeparated, QuestionsAllowed: true, PlanScope: r.scope, PlanGraph: &agentresult.PlanGraphBinding{Story: r.selected, Member: &agentresult.PlanMemberBinding{Group: g.ID, Role: m.Role, AttemptID: m.AttemptID, Basis: g.Basis, ProposalDigest: proposalDigest}}}
		diagnostics := agentresult.ValidateResult(snapshot.Result, scope)
		if !diagnostics.Valid || !diagnostics.Complete {
			attempt, e := r.opts.DB.GetGraphNodeAttempt(ctx, m.AttemptID)
			if e != nil {
				return e
			}
			if attempt.Status == store.GraphFailed {
				run, e := r.opts.DB.GetRun(ctx, m.RunID)
				if e != nil {
					return e
				}
				cause := run.Error
				if cause == "" {
					cause = "parallel member failed with an incomplete shared result"
				}
				if e = r.retainParallelFailure(ctx, g, m, cause); e != nil {
					return e
				}
			}
			continue
		}
		plan := snapshot.Result.Payload.Plan
		if plan == nil {
			return errors.New("retained member lacks Plan contribution")
		}
		c := planbrief.PlanParallelContribution{}
		if plan.Contribution != nil {
			c = *plan.Contribution
		}
		for _, q := range snapshot.Result.Questions {
			c.Questions = append(c.Questions, q.ID+": "+q.Text)
		}
		if snapshot.Result.Outcome != "completed" && len(c.Questions) == 0 {
			c.Failure = snapshot.Result.Reason
		}
		if err = r.opts.DB.UpdatePlanWorkflowState(ctx, r.parentRef.String(), func(s *planbrief.PlanState) (bool, error) {
			item := parallelStory(s, r.selected)
			if item == nil || item.Parallel == nil || item.Parallel.ID != g.ID || r.parallelBasis(*s, item.Parallel) != g.Basis {
				return false, errors.New("parallel recovery superseded")
			}
			if item.Parallel.Sealed {
				return false, nil
			}
			return true, item.Parallel.Collect(g.Basis, m.Role, m.AttemptID, fmt.Sprint(retained.Revision), c)
		}); err != nil {
			return err
		}
	}
	return nil
}

func parallelErrorText(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}

func (r *planGraphRuntime) parallelOutcome(ctx context.Context, outcome string) ActivityResult {
	out := ActivityResult{Outcome: outcome}
	s, _, err := r.state(ctx)
	if err != nil {
		out.Diagnostic = err.Error()
		return out
	}
	item := parallelStory(&s, r.selected)
	if item == nil || item.Parallel == nil {
		return out
	}
	out.Diagnostic = strings.Join(item.Parallel.Blockers, "; ")
	if outcome == "fallback" && item.Route != nil {
		r.stage = item.Stage
		r.rewindReason = "Story " + r.selected + " grew beyond S: " + item.Route.Fallback + ". Normal owner reviews apply for the rest of this planning cycle."
	}
	return out
}

func (r *planGraphRuntime) recordNormalRoute(ctx context.Context, a ActivityRequest) error {
	if r.stage == planSetStage || r.selected == "" {
		return nil
	}
	if !parallelGraphEnabled(r.definition.Profile) {
		return nil
	}
	return r.opts.DB.UpdatePlanWorkflowState(ctx, r.parentRef.String(), func(s *planbrief.PlanState) (bool, error) {
		item := parallelStory(s, r.selected)
		if item == nil || s.StorySet == nil {
			return false, nil
		}
		entry, _ := s.StorySet.EntryForStory(r.selected)
		return true, s.RecordPlanRoute(r.selected, planbrief.PlanRouteState{Cycle: "plan-" + s.StorySet.PromotedRevision, Capture: r.definition.Digest, SourceRevision: s.StorySet.PromotedRevision, EntryID: entry.ID}, planbrief.PlanRouteEvent{Route: "normal", Node: a.Node, Reason: "Captured graph reached the sequential role."})
	})
}

func (r *planGraphRuntime) retainParallelFailure(ctx context.Context, g *planbrief.PlanParallelGroup, m store.PlanGroupMember, cause string) error {
	return r.opts.DB.UpdatePlanWorkflowState(ctx, r.parentRef.String(), func(s *planbrief.PlanState) (bool, error) {
		item := parallelStory(s, r.selected)
		if item == nil || item.Parallel == nil || item.Parallel.ID != g.ID || r.parallelBasis(*s, item.Parallel) != g.Basis {
			return false, errors.New("parallel failure recovery changed")
		}
		return true, item.Parallel.Collect(g.Basis, m.Role, m.AttemptID, "failed", planbrief.PlanParallelContribution{Failure: cause})
	})
}

// An already current approval is preserved on successor replay. This skips no
// new approval: the manifest, applied stage semantics and upstream guards must
// still identify the exact stage that was approved earlier.
func parallelStageApproved(s *planbrief.PlanState, a ActivityRequest, request *graphexec.PlanStoryRequest) bool {
	if a.Attrs["lf_stage"] == "" || request == nil {
		return false
	}
	item := parallelStory(s, request.Draft.Key)
	if item == nil || item.Parallel == nil || item.Route == nil || item.Route.Version != planbrief.PlanParallelVersion || item.Parallel.Version != planbrief.PlanParallelVersion || item.Route.Fallback != "" || !item.Parallel.Sealed || len(item.Parallel.Blockers) > 0 || item.Blocked() || item.Wait != nil {
		return false
	}
	stage := a.Attrs["lf_stage"]
	if item.Parallel.Applied[stage] == "" || request.Draft.StageRevision(stage) != item.Draft.StageRevision(stage) {
		return false
	}
	var saved graphexec.PlanStoryRequest
	if json.Unmarshal(item.Parallel.Projections[stage], &saved) != nil || saved.RequestID != request.RequestID || saved.Candidate != request.Candidate {
		return false
	}
	for _, prior := range planReviewStages {
		revision, err := s.ReviewRevision(item.Key, prior)
		if err != nil || item.Approvals[prior] != revision || !s.UpstreamApproved(item.Key, prior) {
			return false
		}
		if prior == stage {
			return true
		}
	}
	return false
}

func retainParallelStop(g *planbrief.PlanParallelGroup, cause error, outcome *string) (bool, error) {
	g.Blockers = append(g.Blockers, cause.Error())
	*outcome = "stopped"
	return true, nil
}

func retainParallelFailureOutcome(g *planbrief.PlanParallelGroup, cause error, outcome *string) (bool, error) {
	g.Blockers = append(g.Blockers, cause.Error())
	*outcome = "failed"
	return true, nil
}

func parallelGraphEnabled(p *graphexec.Profile) bool {
	if p != nil {
		for _, n := range p.Nodes {
			if n.Action == graphexec.PlanParallelAction {
				return true
			}
		}
	}
	return false
}
