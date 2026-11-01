package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// AC-001 crosses the captured graph, the live shared-result service and its
// receipt boundary, SQLite state, and the production story effects over HTTP.
// The controlled reasoning roles meet at a barrier before any can complete.
func TestPlanParallelEndToEnd_ConcurrentAgreementAndOwnerFinalStop(t *testing.T) {
	runParallelMainPath(t, false, "")
}

func TestPlanParallelEndToEnd_MixedTwoSmallOneLarge(t *testing.T) {
	runParallelMainPath(t, true, "")
}

func TestPlanParallelEndToEnd_DependencyWaitPreservesIndependentSibling(t *testing.T) {
	runParallelMainPath(t, true, "dependency")
}

func TestPlanParallelEndToEnd_UncertainEffectRecovery(t *testing.T) {
	runParallelMainPath(t, false, "uncertain")
}

func TestPlanParallelEndToEnd_DurableSignalsAndRestart(t *testing.T) {
	for _, signal := range []string{"question", "growth", "failure", "link", "retry"} {
		t.Run(signal, func(t *testing.T) { runParallelMainPath(t, false, signal) })
	}
}

// A starved shared pool (total 4) must still start the group on its minimum
// and reach the same sealed, guarded result, running at most as many members
// at once as it holds slots, and returning every borrowed slot.
func TestPlanParallelEndToEnd_MeteredPoolRunsMembersWithinFreeSlots(t *testing.T) {
	for _, free := range []int{1, 2} {
		t.Run(fmt.Sprintf("free_%d", free), func(t *testing.T) {
			pool := &slotStoryPool{total: 4}
			competitor, ok, err := pool.acquire(pool.total - free)
			if err != nil || !ok {
				t.Fatal(err)
			}
			defer competitor()
			parent := &slotStoryParent{pool: pool}
			peak := runParallelMain(t, false, "", parent)
			if peak < 1 || peak > free {
				t.Fatalf("members ran %d at once with %d free slot(s)", peak, free)
			}
			pool.mu.Lock()
			observed := slicesMax(pool.observed)
			pool.mu.Unlock()
			if observed > pool.total {
				t.Fatalf("pool overbooked: %d > %d", observed, pool.total)
			}
			parent.Release()
			if used, _ := pool.Capacity(); used != pool.total-free {
				t.Fatalf("slots leaked: used=%d want=%d", used, pool.total-free)
			}
		})
	}
}

func slicesMax(values []int) int {
	peak := 0
	for _, v := range values {
		peak = max(peak, v)
	}
	return peak
}

func runParallelMainPath(t *testing.T, mixed bool, signal string) {
	t.Helper()
	runParallelMain(t, mixed, signal, nil)
}

// runParallelMain drives the main path; with capacity set every turn runs
// under that step reservation and the barrier gives way to a concurrency
// count, whose peak it returns.
func runParallelMain(t *testing.T, mixed bool, signal string, capacity *slotStoryParent) int {
	t.Helper()
	metered := capacity != nil
	active, peak := 0, 0
	dependency := signal == "dependency"
	recovery := signal == "uncertain"
	if dependency || recovery {
		signal = ""
	}
	entries := []planbrief.PlanStorySetEntry{{ID: "small", Name: "Small plan", Intent: "Retain a coherent plan", Size: "S", SizeReason: "One local change; no migration or unknowns."}}
	if mixed {
		entries = append(entries, planbrief.PlanStorySetEntry{ID: "second", Name: "Second small plan", Intent: "Retain independent scope", Size: "S", SizeReason: "One separate local change."}, planbrief.PlanStorySetEntry{ID: "large", Name: "Large plan", Intent: "Retain normal review", Size: "L", SizeReason: "Several interacting components."})
	}
	if dependency {
		entries[1].DependsOn = []string{"large"}
	}
	var mu sync.Mutex
	started := make(map[string]bool)
	receipts := make(map[string]string)
	launchPaths := make(map[string]string)
	bases := make(map[string]string)
	initialProposals := make(map[string]string)
	allStarted := make(map[string]chan struct{})
	// firstRounds counts first-round member invocations per role; retrying is
	// set once the owner retries a stopped group (signal "retry").
	firstRounds := make(map[string]int)
	retrying := false
	var concurrentRetry func() error
	var f *fixture
	sequential := (&creationAgent{}).behave(t, &f)
	runner := lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		if req.Graph.Node == "set_agent" {
			proposed := make([]ar.PlanStorySetEntry, 0, len(entries))
			for _, entry := range entries {
				proposed = append(proposed, ar.PlanStorySetEntry{ID: entry.ID, Name: entry.Name, Intent: entry.Intent, Size: entry.Size, SizeReason: entry.SizeReason, DependsOn: entry.DependsOn})
			}
			result := ar.Result{Outcome: "completed", Comment: "Review the proposed story set.", Payload: ar.Payload{Plan: &ar.Plan{StorySet: &ar.PlanStorySet{Entries: proposed}}}}
			envelope, err := json.Marshal(ar.ResearchNode{Version: 1, Response: "done", Result: result})
			if err != nil {
				return nil, err
			}
			submitLiveResult(t, req, string(envelope))
			return agentDone(t, result), nil
		}
		var facts struct {
			Role           string              `json:"role"`
			Basis          string              `json:"basis"`
			ProposalDigest string              `json:"proposal_digest"`
			Proposal       planbrief.PlanDraft `json:"proposal"`
		}
		const label = "Parallel Plan member facts (data): "
		found := false
		for _, line := range strings.Split(req.Prompt, "\n") {
			if raw, ok := strings.CutPrefix(line, label); ok {
				if err := json.Unmarshal([]byte(raw), &facts); err != nil {
					return nil, err
				}
				found = true
				break
			}
		}
		if !found {
			matched := setFlowStoryPattern.FindStringSubmatch(req.Prompt)
			if (!mixed && signal != "growth") || len(matched) != 3 {
				return nil, fmt.Errorf("expected bound parallel member facts for node %s", req.Graph.Node)
			}
			return sequential(planAgentStep{f: f, t: t, req: req, client: submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}, story: matched[1], stage: matched[2]})
		}
		key := facts.Proposal.Key + ":" + facts.Role
		mu.Lock()
		active++
		peak = max(peak, active)
		mu.Unlock()
		defer func() {
			mu.Lock()
			active--
			mu.Unlock()
		}()
		if metered {
			// Give a concurrent sibling, if one was wrongly admitted, time to overlap.
			time.Sleep(20 * time.Millisecond)
		}
		mu.Lock()
		if allStarted[facts.Proposal.Key] == nil {
			allStarted[facts.Proposal.Key] = make(chan struct{})
		}
		barrier := allStarted[facts.Proposal.Key]
		if basis, ok := bases[facts.Proposal.Key]; ok && basis != facts.Basis {
			mu.Unlock()
			return nil, fmt.Errorf("members received different immutable bases")
		}
		if facts.Basis == "" {
			mu.Unlock()
			return nil, fmt.Errorf("member basis missing")
		}
		bases[facts.Proposal.Key] = facts.Basis
		if facts.ProposalDigest == "" {
			revision := facts.Proposal.Revision()
			if prior, ok := initialProposals[facts.Proposal.Key]; ok && prior != revision {
				mu.Unlock()
				return nil, fmt.Errorf("members received different initial scopes")
			}
			initialProposals[facts.Proposal.Key] = revision
		}
		if facts.ProposalDigest == "" {
			firstRounds[facts.Role]++
			started[key] = true
			launchPaths[key] = req.MCPLaunch.DescriptorPath()
			count := 0
			for member := range started {
				if strings.HasPrefix(member, facts.Proposal.Key+":") {
					count++
				}
			}
			if count == 3 {
				close(barrier)
			}
		}
		mu.Unlock()
		if facts.ProposalDigest == "" && !metered {
			select {
			case <-barrier:
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(20 * time.Second):
				return nil, fmt.Errorf("three member runners did not overlap")
			}
		}
		if facts.ProposalDigest != "" {
			if facts.Proposal.Revision() != facts.ProposalDigest || facts.Proposal.Architecture == nil || len(facts.Proposal.Tasks) != 1 || len(facts.Proposal.Criteria) != 1 {
				return nil, fmt.Errorf("member received an incomplete or mismatched proposal")
			}
		}
		contribution := planbrief.PlanParallelContribution{Agreement: facts.ProposalDigest}
		switch facts.Role {
		case "story":
			contribution.Draft = integrationCreateRequest(facts.Proposal.Key).Draft
			contribution.Draft.Dependencies = facts.Proposal.Dependencies
			contribution.Draft.UserStory.Notes = []string{"Covers: O-01"}
		case "technical":
			contribution.Draft.Architecture = &planbrief.PlanArchitecture{General: []planbrief.GeneralDecision{{Area: "Planning", Decision: "Reuse existing effects", Rationale: "Preserve guards"}}}
		case "acceptance":
			// The member learns the task owner roles only from the factory
			// binding, as a real agent does (MAR-63 guessed "engineer").
			role, err := boundOwnerRole(req.Prompt)
			if err != nil {
				return nil, err
			}
			contribution.Draft.Criteria = []planbrief.PlanCriterion{{ID: "AC-001", Text: "You can retain a coherent plan. (Auto)", ProofMethod: "End-to-end captured graph check. Covers: O-01"}}
			contribution.Draft.Tasks = []planbrief.TaskContent{{ID: "T-001", Description: "Kind: code — Build the retained plan.", OwnerRole: role, CriterionIDs: []string{"AC-001"}, DependsOn: []string{}}}
			if facts.ProposalDigest == "" {
				if err := assertUnboundRoleRefused(ctx, req, contribution); err != nil {
					return nil, err
				}
			}
			mu.Lock()
			retried, check := retrying, concurrentRetry
			mu.Unlock()
			if retried && check != nil && facts.ProposalDigest == "" {
				if err := check(); err != nil {
					return nil, err
				}
			}
			if signal == "link" || (signal == "retry" && !retried) {
				// Passes the member's own check but leaves a criterion that no
				// task delivers, which only the join can see.
				contribution.Draft.Criteria = append(contribution.Draft.Criteria, planbrief.PlanCriterion{ID: "AC-002", Text: "You can see the plan kept. (Manual)", ProofMethod: "Owner check: reopen the plan. Covers: O-01"})
			}
		default:
			return nil, fmt.Errorf("unexpected member role %q", facts.Role)
		}
		if facts.ProposalDigest != "" {
			// Agreement-only continuations must retain all previously collected
			// role sections while binding agreement to the complete proposal.
			contribution.Draft = planbrief.PlanDraft{}
		}
		if facts.Role == "technical" {
			switch signal {
			case "growth":
				contribution.Growth = "Storage migration adds several interacting components."
			case "failure":
				contribution.Failure = "Technical evidence is unavailable."
			}
		}
		result := ar.Result{Outcome: "completed", Comment: "Retained role-owned contribution.", Payload: ar.Payload{Plan: &ar.Plan{Contribution: &contribution}}}
		if signal == "question" && facts.Role == "technical" {
			result.Outcome = "incomplete"
			result.Reason = "The retention boundary needs an owner decision."
			result.Questions = []ar.Question{{ID: "q1", Audience: "technical", Text: "Which retention boundary does the owner want?", RecommendedOption: "a", Options: []ar.Option{{ID: "a", Text: "Use bounded local retention"}, {ID: "b", Text: "Keep every revision"}}}}
		}
		data := submitBytes(t, result)
		client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
		pre, err := client.Call(ctx, "communication_preflight", data)
		if err != nil {
			return nil, fmt.Errorf("preflight: %w", err)
		}
		if pre.Error != "" {
			return nil, fmt.Errorf("preflight: %s", pre.Error)
		}
		var diagnostics ar.UpdateDiagnostics
		if err = json.Unmarshal(pre.Result, &diagnostics); err != nil {
			return nil, err
		}
		if !diagnostics.Valid {
			return nil, fmt.Errorf("preflight invalid: %s", diagnosticPaths(diagnostics))
		}
		submitted, err := client.Call(ctx, "communication_submit", data)
		if err != nil {
			return nil, fmt.Errorf("submit: %w", err)
		}
		if submitted.Error != "" {
			return nil, fmt.Errorf("submit: %s", submitted.Error)
		}
		var accepted AgentResultSubmission
		if err = json.Unmarshal(submitted.Result, &accepted); err != nil {
			return nil, err
		}
		if accepted.Receipt == nil || !accepted.Validation.Valid {
			return nil, fmt.Errorf("contribution refused: %+v", accepted)
		}
		receiptData, err := json.Marshal(map[string]string{"request_id": accepted.Receipt.RequestID})
		if err != nil {
			return nil, err
		}
		lookedUp, err := client.Call(ctx, "communication_receipt", receiptData)
		if err != nil {
			return nil, fmt.Errorf("receipt: %w", err)
		}
		if lookedUp.Error != "" {
			return nil, fmt.Errorf("receipt: %s", lookedUp.Error)
		}
		var recovered AgentResultSubmission
		if err = json.Unmarshal(lookedUp.Result, &recovered); err != nil {
			return nil, err
		}
		if recovered.Receipt == nil || recovered.Receipt.RequestID != accepted.Receipt.RequestID || recovered.Receipt.InputState != "accepted" {
			return nil, fmt.Errorf("receipt mismatch: %+v", recovered)
		}
		mu.Lock()
		receipts[key+":"+facts.ProposalDigest] = accepted.Receipt.RequestID
		mu.Unlock()
		if result.Outcome == "incomplete" {
			raw, err := json.Marshal(ar.ResearchNode{Version: 1, Response: "ask", Result: result})
			if err != nil {
				return nil, err
			}
			return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: string(raw), FinishedAt: time.Now()}, nil
		}
		process := agentDone(t, result)
		process.Model = "observed-" + facts.Role
		return process, nil
	})
	var base Request
	var remote *graphPlanHTTP
	var forge *fakeForge
	f, remote, base, forge = setupGraphPlanFixture(t, runner, false)
	if metered {
		f.capacity = capacity
	}

	executeGraphPlan(t, f, base)
	revision := graphPlanState(t, f).StorySet.Revision
	// The set remains unpromoted until its authenticated owner command.
	before := graphPlanState(t, f)
	if before.StorySet.Promoted || len(before.Stories) != 0 {
		t.Fatal("set advanced before owner promotion")
	}
	executeGraphPlan(t, f, promoteTrigger(base, 900, 7, promoteCommand(revision)))
	if recovery {
		remote.mu.Lock()
		remote.losePatch = true
		remote.mu.Unlock()
		out, ran, err := runDueGraphStep(t, f, base)
		if !ran {
			out, ran, err = runOwedStory(t, f, base)
		}
		if !ran {
			t.Fatal("no parallel effect launched")
		}
		if err == nil && out.Err == nil {
			t.Fatal("lost effect response did not retain an uncertain stop")
		}
		interrupted := graphPlanState(t, f).Stories[0]
		if len(interrupted.Approvals) > 0 {
			t.Fatal("uncertain effect approved a stage before recovery")
		}
		if interrupted.Parallel == nil || !interrupted.Parallel.Sealed || len(interrupted.Parallel.Projections) == 0 {
			t.Fatalf("uncertain effect lost sealed manifest: %+v", interrupted)
		}
		groupID := interrupted.Parallel.ID
		request := append([]byte{}, interrupted.Parallel.Projections["story"]...)
		remote.mu.Lock()
		creates, patches := remote.creates, remote.patches
		remote.mu.Unlock()
		if creates != 1 || patches != 1 {
			t.Fatalf("uncertain story effect was not applied once: creates=%d patches=%d", creates, patches)
		}
		reopened, e := store.Open(t.Context(), f.paths.DBPath())
		mustProgress(t, e)
		saved, _, e := reopened.GetPlanWorkflowState(t.Context(), f.ref().String())
		mustProgress(t, e)
		mustProgress(t, reopened.Close())
		if saved.Stories[0].Parallel.ID != groupID || string(saved.Stories[0].Parallel.Projections["story"]) != string(request) {
			t.Fatal("restart changed uncertain request identity")
		}
		resume := asStoryTask(t, f, parallelAutomationContinuation(base, "Recover the retained stage effect."), interrupted.Key)
		executeGraphPlan(t, f, resume)
		resumed := graphPlanState(t, f).Stories[0]
		if resumed.Parallel.ID != groupID || string(resumed.Parallel.Projections["story"]) != string(request) {
			t.Fatal("recovery replaced uncertain request identity")
		}
	}
	if signal != "" {
		for i := 0; i < 6; i++ {
			out, ran, err := runDueGraphStep(t, f, base)
			if !ran {
				out, ran, err = runOwedStory(t, f, base)
			}
			if err != nil || out.Err != nil {
				t.Fatalf("signal workflow: %v %+v", err, out)
			}
			state := graphPlanState(t, f)
			if len(state.Stories) > 0 && state.Stories[0].Parallel != nil {
				assertParallelSignalRestart(t, f, base, remote, forge, signal)
				if signal == "retry" {
					mu.Lock()
					delete(allStarted, state.Stories[0].Key)
					for member := range started {
						if strings.HasPrefix(member, state.Stories[0].Key+":") {
							delete(started, member)
						}
					}
					retrying = true
					mu.Unlock()
					assertOwnerRetryRerunsStoppedGroup(t, f, base, state.Stories[0].Key, func() map[string]int {
						mu.Lock()
						defer mu.Unlock()
						return maps.Clone(firstRounds)
					}, func(check func() error) {
						mu.Lock()
						concurrentRetry = check
						mu.Unlock()
					})
				}
				return peak
			}
			if !ran {
				break
			}
		}
		t.Fatal("signal never reached parallel collection")
	}
	drainParallelGraphSteps(t, f, base)
	state := graphPlanState(t, f)
	if len(state.Stories) != len(entries) {
		t.Fatalf("stories: %+v", state.Stories)
	}
	smallCount := 1
	if mixed {
		smallCount = 2
	}
	if dependency {
		if state.Stories[1].Parallel != nil || len(state.Stories[1].Approvals) > 0 {
			t.Fatal("dependent S story advanced before upstream owner review")
		}
		independent := state.Stories[0].Approvals["acceptance"]
		if independent == "" {
			t.Fatal("independent S sibling did not finish while dependency waited")
		}
		large := state.Stories[2]
		for i, stage := range []string{"story", "technical", "acceptance"} {
			current := graphPlanState(t, f).Stories[2]
			if current.Wait == nil || current.Wait.Stage != stage {
				t.Fatalf("upstream missing %s owner wait", stage)
			}
			executeGraphPlan(t, f, graphPlanApproveKey(t, f, base, large.Key, int64(1500+i)))
		}
		drainParallelGraphSteps(t, f, base)
		state = graphPlanState(t, f)
		if state.Stories[0].Approvals["acceptance"] != independent {
			t.Fatal("dependency progress invalidated independent sibling")
		}
	}
	for _, story := range state.Stories[:smallCount] {
		if story.Parallel == nil || !story.Parallel.Sealed {
			t.Fatalf("group did not seal: %+v logs=%s", story.Parallel, f.logs.String())
		}
		assertParallelDispatchClassification(t, f, asStoryTask(t, f, base, story.Key).SessionID)
		childSession := asStoryTask(t, f, base, story.Key).SessionID
		instance, err := f.db.LatestWorkflowInstance(t.Context(), childSession, "plan")
		mustProgress(t, err)
		members, err := f.db.ListPlanGroupMembersByBasis(t.Context(), instance.ID, story.Parallel.Basis)
		mustProgress(t, err)
		paths := make(map[string]bool)
		for _, member := range members {
			path := assertParallelMemberMetadata(t, f, member.AttemptID, "observed-"+member.Role)
			if paths[path] {
				t.Fatal("parallel members shared a metadata file")
			}
			paths[path] = true
		}
		if len(paths) < 6 {
			t.Fatalf("metadata missing for reasoning/agreement members: %d", len(paths))
		}
		for _, stage := range []string{"story", "technical", "acceptance"} {
			if story.Approvals[stage] == "" || story.ApprovedBy[stage] != planbrief.ApprovalByGraph {
				t.Fatalf("stage %s not guarded graph approved: %+v", stage, story)
			}
		}
	}
	if mixed && !dependency {
		large := state.Stories[2]
		if large.Parallel != nil || large.Wait == nil || large.Wait.Stage != "story" {
			t.Fatalf("large sibling missed normal story review: %+v", large)
		}
		for i, stage := range []string{"story", "technical", "acceptance"} {
			current := graphPlanState(t, f).Stories[2]
			if current.Wait == nil || current.Wait.Stage != stage {
				t.Fatalf("large sibling missed %s review: %+v", stage, current)
			}
			executeGraphPlan(t, f, graphPlanApproveKey(t, f, base, large.Key, int64(1500+i)))
			drainParallelGraphSteps(t, f, base)
		}
	}
	mu.Lock()
	if len(started) != 3*smallCount || len(receipts) < 6*smallCount {
		t.Fatalf("members=%v receipts=%v", started, receipts)
	}
	for role, path := range launchPaths {
		for other, otherPath := range launchPaths {
			if role != other && path == otherPath {
				t.Fatalf("members share launch descriptor: %s %s", role, other)
			}
		}
	}
	mu.Unlock()
	remote.mu.Lock()
	creates, patches := remote.creates, remote.patches
	remote.mu.Unlock()
	if recovery && patches != 3 {
		t.Fatalf("recovery repeated a known effect: patches=%d", patches)
	}
	if creates != len(entries) || patches < 2*smallCount {
		t.Fatalf("ordered stage effects: creates=%d patches=%d", creates, patches)
	}
	runParent(t, f, base)
	state = graphPlanState(t, f)
	if state.AssembledWait == "" || state.AssembledApproval != "" {
		t.Fatalf("final owner approval was bypassed: %+v", state)
	}
	mu.Lock()
	defer mu.Unlock()
	return peak
}

// Missing and invalid sizing must exercise the same real owner waits as M/L,
// rather than silently borrowing S from another story or the parent request.
func TestPlanParallelEndToEnd_NormalSizingKeepsEveryOwnerReview(t *testing.T) {
	for _, size := range []string{"M", "L", "", "XL"} {
		t.Run("size_"+size, func(t *testing.T) {
			c := &creationAgent{}
			var f *fixture
			behave := c.behave(t, &f)
			runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				if strings.Contains(req.Prompt, "Parallel Plan member facts (data): ") {
					return nil, fmt.Errorf("normal sizing entered parallel reasoning")
				}
				matched := setFlowStoryPattern.FindStringSubmatch(req.Prompt)
				if len(matched) != 3 {
					return nil, fmt.Errorf("missing selected sequential stage")
				}
				return behave(planAgentStep{f: f, t: t, req: req, client: submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}, story: matched[1], stage: matched[2]})
			})
			var base Request
			f, _, base, _ = setupGraphPlanFixture(t, runner, false)
			rev := saveProposedSet(t, f, []planbrief.PlanStorySetEntry{{ID: "normal", Name: "Normal story", Intent: "Keep owner review", Size: size, SizeReason: "Normal planning evidence."}})
			executeGraphPlan(t, f, promoteTrigger(base, 900, 7, promoteCommand(rev)))
			for i, stage := range []string{"story", "technical", "acceptance"} {
				drainParallelGraphSteps(t, f, base)
				state := graphPlanState(t, f)
				item := state.Stories[0]
				if item.Wait == nil || item.Wait.Stage != stage || item.Approvals[stage] != "" || item.Parallel != nil {
					t.Fatalf("%s owner stop missing: %+v", stage, item)
				}
				executeGraphPlan(t, f, graphPlanApprove(t, f, base, int64(901+i)))
			}
			state := graphPlanState(t, f)
			for _, stage := range []string{"story", "technical", "acceptance"} {
				if state.Stories[0].ApprovedBy[stage] != planbrief.ApprovalByOwner {
					t.Fatalf("%s did not retain owner provenance", stage)
				}
			}
		})
	}
}

func drainParallelGraphSteps(t *testing.T, f *fixture, base Request) {
	t.Helper()
	for i := 0; i < 20; i++ {
		out, ran, err := runDueGraphStep(t, f, base)
		if !ran {
			out, ran, err = runOwedStory(t, f, base)
		}
		if !ran {
			return
		}
		for _, item := range graphPlanState(t, f).Stories {
			if item.Parallel != nil && len(item.Parallel.Blockers) > 0 {
				t.Fatalf("parallel blocked: %v logs=%s", item.Parallel.Blockers, f.logs.String())
			}
		}
		if err != nil || out.Err != nil {
			t.Fatalf("parallel graph step %d: err=%v result=%+v state=%+v logs=%s", i, err, out, graphPlanState(t, f), f.logs.String())
		}
	}
	state := graphPlanState(t, f)
	raw, _ := json.Marshal(state)
	due, _ := f.db.ListDueGraphDispatches(t.Context(), time.Now().Add(time.Minute), 20)
	t.Fatalf("parallel graph did not stop: state=%s due=%+v logs=%s", raw, due, f.logs.String())
}

func assertParallelSignalRestart(t *testing.T, f *fixture, base Request, remote *graphPlanHTTP, forge *fakeForge, signal string) {
	t.Helper()
	state := graphPlanState(t, f)
	item := state.Stories[0]
	if item.Parallel == nil || item.Parallel.Sealed || len(item.Approvals) > 0 || len(item.Parallel.Blockers) == 0 {
		t.Fatalf("signal allowed partial approval: %+v", item)
	}
	if signal == "growth" {
		if item.Route == nil || item.Route.Fallback == "" {
			t.Fatalf("growth fallback missing: %+v", item.Route)
		}
	} else {
		remote.mu.Lock()
		patches := remote.patches
		remote.mu.Unlock()
		if patches != 0 {
			t.Fatalf("blocked group published %d stage effects", patches)
		}
	}
	if signal == "link" || signal == "failure" || signal == "retry" {
		assertParallelStopNamesCause(t, forge, signal)
	}
	if signal == "question" {
		visible := false
		for _, post := range forge.all() {
			visible = visible || (strings.Contains(post.body, "Which retention boundary") && strings.Contains(post.body, "`a` Use bounded local retention (recommended)") && strings.Contains(post.body, "`b` Keep every revision"))
		}
		if !visible {
			t.Fatal("retained member question was not visible to the owner")
		}
		member := item.Parallel.Members["technical"]
		retained, err := (&planGraphRuntime{opts: f.opts}).loadParallelMember(t.Context(), member.AttemptID)
		mustProgress(t, err)
		questions := retained.Result.Questions
		if len(questions) != 1 || questions[0].Audience != "technical" || questions[0].RecommendedOption != "a" || len(questions[0].Options) != 2 {
			t.Fatalf("member question lost structured choices: %+v", questions)
		}
	}
	reopened, err := store.Open(t.Context(), f.paths.DBPath())
	mustProgress(t, err)
	retained, _, err := reopened.GetPlanWorkflowState(t.Context(), f.ref().String())
	mustProgress(t, err)
	mustProgress(t, reopened.Close())
	if retained.Stories[0].Parallel.ID != item.Parallel.ID || len(retained.Stories[0].Parallel.Blockers) == 0 || len(retained.Stories[0].Parallel.Members) == 0 {
		t.Fatal("restart lost group or durable signal")
	}
	remote.mu.Lock()
	createsBefore, patchesBefore := remote.creates, remote.patches
	remote.mu.Unlock()
	// A fresh invocation reads retained state; a partial group cannot become an
	// approval merely because its original process and result transport ended.
	req := asStoryTask(t, f, parallelAutomationContinuation(base, "Continue retained planning."), item.Key)
	executeGraphPlan(t, f, req)
	resumed := graphPlanState(t, f).Stories[0]
	if len(resumed.Approvals) > 0 || resumed.Parallel.Sealed {
		t.Fatalf("resume approved blocked work: %+v", resumed)
	}
	remote.mu.Lock()
	createsAfter, patchesAfter := remote.creates, remote.patches
	remote.mu.Unlock()
	if createsAfter != createsBefore || (signal != "growth" && patchesAfter != patchesBefore) {
		t.Fatal("resume duplicated blocked effects")
	}
	if signal == "growth" && (resumed.Route == nil || resumed.Route.Fallback == "") {
		t.Fatal("resume lost sticky growth fallback")
	}
}

// Cancellation must join every admitted member before the coordinator returns;
// the result transports and durable attempts cannot outlive that return.
func TestPlanParallelEndToEnd_CancellationJoinsAllMembers(t *testing.T) {
	type launchedMember struct {
		attempt    int64
		descriptor string
	}
	started := make(chan launchedMember, 3)
	joined := make(chan int64, 3)
	runner := lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		if req.Graph == nil || req.MCPLaunch == nil {
			return nil, fmt.Errorf("member invocation lacks graph or result route")
		}
		started <- launchedMember{req.Graph.AttemptID, req.MCPLaunch.DescriptorPath()}
		<-ctx.Done()
		joined <- req.Graph.AttemptID
		return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "Parent canceled", Model: "observed-canceled-model", FinishedAt: time.Now()}, ctx.Err()
	})
	f, remote, base, _ := setupGraphPlanFixture(t, runner, false)
	revision := saveProposedSet(t, f, []planbrief.PlanStorySetEntry{{ID: "small", Name: "Cancelable plan", Intent: "Join all members", Size: "S", SizeReason: "One bounded local change."}})
	executeGraphPlan(t, f, promoteTrigger(base, 900, 7, promoteCommand(revision)))
	request := asStoryTask(t, f, parallelAutomationContinuation(base, "Continue the selected story."), "story-001")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		out, err := Execute(ctx, f.opts, request)
		if err == nil {
			err = out.Err
		}
		finished <- err
	}()
	members := make([]launchedMember, 0, 3)
	for range 3 {
		select {
		case member := <-started:
			members = append(members, member)
		case <-time.After(20 * time.Second):
			t.Fatal("not every parallel member started")
		}
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(15 * time.Second):
		t.Fatal("coordinator did not join cancellation")
	}
	assertParallelDispatchClassification(t, f, request.SessionID)
	if len(joined) != 3 {
		t.Fatalf("coordinator returned before member join: %d", len(joined))
	}
	metadataPaths := make(map[string]bool)
	for _, member := range members {
		path := assertParallelMemberMetadata(t, f, member.attempt, "observed-canceled-model")
		if metadataPaths[path] {
			t.Fatal("canceled members shared metadata")
		}
		metadataPaths[path] = true
		attempt, err := f.db.GetGraphNodeAttempt(t.Context(), member.attempt)
		mustProgress(t, err)
		if attempt.Status == store.GraphRunning {
			t.Fatalf("member attempt remained running after cancellation: %d", attempt.ID)
		}
		run, err := f.db.GetRun(t.Context(), attempt.RunID)
		mustProgress(t, err)
		if run.Status == store.RunRunning {
			t.Fatalf("member run remained running after cancellation: %d", run.ID)
		}
		probeCtx, probeCancel := context.WithTimeout(t.Context(), time.Second)
		response, err := (submission.Client{DescriptorPath: member.descriptor}).Call(probeCtx, "communication_preflight", []byte(`{"result":{}}`))
		probeCancel()
		if err == nil && response.Error == "" {
			t.Fatalf("member result route survived cancellation: %d", attempt.ID)
		}
	}
	item := graphPlanState(t, f).Stories[0]
	if item.Parallel == nil || item.Parallel.Sealed || len(item.Approvals) > 0 {
		t.Fatalf("canceled group advanced: %+v", item)
	}
	remote.mu.Lock()
	patches := remote.patches
	remote.mu.Unlock()
	if patches != 0 {
		t.Fatalf("canceled group applied %d stage effects", patches)
	}
}

func parallelAutomationContinuation(base Request, body string) Request {
	base.Trigger = Trigger{CommentID: (store.Schedule{ID: nextStoryTrigger()}).TriggerID(), Body: body, Author: "owner", AuthorID: 7}
	base.Command = Command{Kind: KindFreeText, Text: body}
	return base
}

func assertParallelDispatchClassification(t *testing.T, f *fixture, sessionID int64) {
	t.Helper()
	instance, err := f.db.LatestWorkflowInstance(t.Context(), sessionID, "plan")
	mustProgress(t, err)
	runsAgent, err := GraphDispatchRunsAgent(t.Context(), f.db, store.GraphDispatch{InstanceID: instance.ID, Node: "parallel_group"})
	if err != nil || !runsAgent {
		t.Fatalf("retained parallel group was not classified as an agent dispatch: %v %v", runsAgent, err)
	}
}

func assertParallelMemberMetadata(t *testing.T, f *fixture, attemptID int64, model string) string {
	t.Helper()
	attempt, err := f.db.GetGraphNodeAttempt(t.Context(), attemptID)
	mustProgress(t, err)
	run, err := f.db.GetRun(t.Context(), attempt.RunID)
	mustProgress(t, err)
	session, err := f.db.GetSession(t.Context(), run.SessionID)
	mustProgress(t, err)
	segment, err := f.db.GetSegment(t.Context(), run.SegmentID)
	mustProgress(t, err)
	ref, err := naming.ParseRef(session.TaskRef)
	mustProgress(t, err)
	path, err := naming.RunMetaPath(f.opts.Paths.Home(), ref, run.Ordinal)
	mustProgress(t, err)
	raw, err := os.ReadFile(path)
	mustProgress(t, err)
	var meta harness.Meta
	mustProgress(t, json.Unmarshal(raw, &meta))
	if meta.RunOrdinal != run.Ordinal || meta.SessionID != run.SessionID || meta.SegmentOrdinal != segment.Ordinal || meta.SessionRef != segment.SessionRef || meta.LogPath != run.LogPath || meta.Agent != run.Agent || meta.Mode != "plan" || meta.Model != model {
		t.Fatalf("member metadata lost run/provider correlation: attempt=%d run=%+v segment=%+v meta=%+v", attemptID, run, segment, meta)
	}
	return path
}

// assertParallelStopNamesCause requires the owner's stop notice to carry the
// join's real cause, never a member step's success outcome.
func assertParallelStopNamesCause(t *testing.T, forge *fakeForge, signal string) {
	t.Helper()
	want := map[string]string{"link": "criterion AC-002 has no task", "retry": "criterion AC-002 has no task", "failure": "Technical evidence is unavailable."}[signal]
	named := false
	for _, post := range forge.all() {
		if strings.Contains(post.body, "ended success") {
			t.Fatalf("stop notice names a member step's success instead of the cause:\n%s", post.body)
		}
		named = named || strings.Contains(post.body, want)
	}
	if !named {
		t.Fatalf("no owner notice names the parallel stop cause %q", want)
	}
}

// assertOwnerRetryRerunsStoppedGroup reshapes the stopped group into MAR-63's
// stored state (an "engineer" task owner and the join's refusal as blocker),
// then sends the owner's `/plan Continue …`: every member runs again, the join
// seals with an authorized role, and neither a replay of that trigger, a second
// Continue during the run nor one after it runs the members again.
func assertOwnerRetryRerunsStoppedGroup(t *testing.T, f *fixture, base Request, key string, rounds func() map[string]int, onRetry func(func() error)) {
	t.Helper()
	const continueBody = "/plan Continue the unfinished step from the saved Plan state."
	mustProgress(t, f.db.UpdatePlanWorkflowState(t.Context(), f.ref().String(), func(s *planbrief.PlanState) (bool, error) {
		g := parallelStory(s, key).Parallel
		m := g.Members["acceptance"]
		m.Contribution.Draft.Criteria = m.Contribution.Draft.Criteria[:1]
		m.Contribution.Draft.Tasks[0].OwnerRole = "engineer"
		g.Members["acceptance"] = m
		g.Blockers = []string{"task requires ID, description, authorized role and criterion links"}
		return true, nil
	}))
	stopped := graphPlanState(t, f).Stories[0].Parallel
	if !retryStoppedParallel(stopped, 950) || parallelBlockerOutcome(stopped) != "stopped" {
		t.Fatalf("MAR-63-shaped group is not a retryable stop: %+v", stopped)
	}
	before := rounds()
	onRetry(func() error {
		current := graphPlanState(t, f).Stories[0].Parallel
		if current.Trigger != 950 || len(current.Blockers) > 0 {
			return fmt.Errorf("owner retry reused the stopped group: trigger=%d blockers=%v", current.Trigger, current.Blockers)
		}
		if retryStoppedParallel(current, 951) {
			return fmt.Errorf("a second Continue would replace the running group")
		}
		return nil
	})
	executeGraphPlan(t, f, asStoryTask(t, f, promoteTrigger(base, 950, 7, continueBody), key))
	onRetry(nil)
	after := rounds()
	for _, role := range planReviewStages {
		if after[role] != before[role]+1 {
			t.Fatalf("owner retry ran %s %d times, want once: before=%v after=%v", role, after[role]-before[role], before, after)
		}
	}
	item := graphPlanState(t, f).Stories[0]
	g := item.Parallel
	if g.Trigger != 950 || !g.Sealed || len(g.Blockers) > 0 || g.ID == stopped.ID {
		t.Fatalf("owner retry did not seal a fresh group: %+v", g)
	}
	if len(g.Proposal.Tasks) != 1 || g.Proposal.Tasks[0].OwnerRole == "engineer" || !planbrief.TaskOwnerScope().AdmitsRole(g.Proposal.Tasks[0].OwnerRole) {
		t.Fatalf("retried join kept an unauthorized owner: %+v", g.Proposal.Tasks)
	}
	if retryStoppedParallel(g, 950) || retryStoppedParallel(g, 951) {
		t.Fatal("a sealed group would be replaced again")
	}
	executeGraphPlan(t, f, asStoryTask(t, f, promoteTrigger(base, 951, 7, continueBody), key))
	if again := rounds(); !maps.Equal(again, after) {
		t.Fatalf("a second Continue ran members again: %v -> %v", after, again)
	}
	if graphPlanState(t, f).Stories[0].Parallel.ID != g.ID {
		t.Fatal("a second Continue replaced the sealed group")
	}
}

// boundOwnerRole reads the first authorized task owner role from the factory
// binding in a member prompt; a member without the binding cannot know one.
func boundOwnerRole(prompt string) (string, error) {
	const label = "Authorized task owner roles (assign every task to one of these): "
	for _, line := range strings.Split(prompt, "\n") {
		if raw, ok := strings.CutPrefix(line, label); ok {
			if role, _, _ := strings.Cut(strings.TrimSuffix(raw, "."), ", "); role != "" {
				return role, nil
			}
		}
	}
	return "", fmt.Errorf("parallel member prompt lacks the task owner role binding")
}

// assertUnboundRoleRefused preflights the contribution with a role outside the
// scope and requires the submission check to name the task's owner role.
func assertUnboundRoleRefused(ctx context.Context, req agent.Request, contribution planbrief.PlanParallelContribution) error {
	wrong := contribution
	wrong.Draft.Tasks = []planbrief.TaskContent{contribution.Draft.Tasks[0]}
	wrong.Draft.Tasks[0].OwnerRole = "engineer"
	raw, err := json.Marshal(ar.Result{Outcome: "completed", Comment: "Retained role-owned contribution.", Payload: ar.Payload{Plan: &ar.Plan{Contribution: &wrong}}})
	if err != nil {
		return err
	}
	data := append(append([]byte(`{"result":`), raw...), '}')
	client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
	pre, err := client.Call(ctx, "communication_preflight", data)
	if err != nil {
		return fmt.Errorf("preflight unknown role: %w", err)
	}
	if pre.Error != "" {
		return fmt.Errorf("preflight unknown role: %s", pre.Error)
	}
	var diagnostics ar.UpdateDiagnostics
	if err = json.Unmarshal(pre.Result, &diagnostics); err != nil {
		return err
	}
	if diagnostics.Valid || !strings.Contains(diagnosticPaths(diagnostics), "tasks[0].owner_role") {
		return fmt.Errorf("unknown task owner role was not refused at submission: %s", diagnosticPaths(diagnostics))
	}
	return nil
}
