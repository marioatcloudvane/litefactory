package turn

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func parallelBasisFixture() (*planGraphRuntime, planbrief.PlanState) {
	r := planGraphRuntime{selected: "story-001", definition: &teamsource.WorkflowDefinition{Digest: "capture"}, scope: planbrief.ContentScope{Roles: []string{"engineer"}, IssueRefs: []string{"github:acme/api#3"}, Guidance: []string{"go-stack"}}}
	s := planbrief.PlanState{SharedRevision: "shared", StorySet: &planbrief.PlanStorySetState{Promoted: true, PromotedRevision: "promoted", Keys: map[string]string{"s1": "story-001", "s2": "story-002"}, Entries: []planbrief.PlanStorySetEntry{{ID: "s1", Size: "S", SizeReason: "Small surface"}, {ID: "s2", Size: "L", SizeReason: "Large surface"}}}, Stories: []planbrief.PlanStoryState{
		{Key: "story-001", Candidate: "candidate", Draft: planbrief.PlanDraft{Key: "story-001", Name: "Selected", Dependencies: planbrief.DependencyContent{StoryKeys: []string{"dependency"}}}},
		{Key: "dependency", Candidate: "dep", Draft: planbrief.PlanDraft{Key: "dependency", Name: "Dependency", Dependencies: planbrief.DependencyContent{StoryKeys: []string{"transitive"}}}},
		{Key: "transitive", Candidate: "deep", Draft: planbrief.PlanDraft{Key: "transitive", Name: "Transitive"}},
		{Key: "story-002", Candidate: "sibling", Draft: planbrief.PlanDraft{Key: "story-002", Name: "Independent"}},
	}}
	return &r, s
}

func TestPlanParallelRawFacts(t *testing.T) {
	for _, tc := range []struct{ name, size, reason, valid string }{
		{"small", "S", "Small surface", "true"}, {"medium", "M", "Several parts", "true"}, {"large", "L", "Many parts", "true"},
		{"missing", "", "", "false"}, {"unknown size", "XS", "Small surface", "false"}, {"blank reason", "S", "  ", "false"},
		{"multiline reason", "S", "one\ntwo", "false"}, {"long reason", "S", strings.Repeat("x", 281), "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, s := parallelBasisFixture()
			s.StorySet.Entries[0].Size = tc.size
			s.StorySet.Entries[0].SizeReason = tc.reason
			facts := planStoryFacts(s, "story-001")
			if facts["story.size"] != tc.size || facts["story.sizing_valid"] != tc.valid || facts["story.fallback"] != "false" {
				t.Fatalf("facts=%v", facts)
			}
			s.Stories[0].Route = &planbrief.PlanRouteState{Fallback: "growth reason"}
			if got := planStoryFacts(s, "story-001")["story.fallback"]; got != "true" {
				t.Fatalf("growth fact=%s", got)
			}
		})
	}
	_, unknown := parallelBasisFixture()
	unknown.Stories[0].Route = &planbrief.PlanRouteState{Version: 2}
	if facts := planStoryFacts(unknown, "story-001"); facts["story.sizing_valid"] != "false" {
		t.Fatalf("unknown route version allowed automatic progress: %v", facts)
	}
	for _, s := range []planbrief.PlanState{{}, {StorySet: &planbrief.PlanStorySetState{Keys: map[string]string{}}}} {
		if facts := planStoryFacts(s, "missing"); facts["story.sizing_valid"] != "false" || facts["story.fallback"] != "false" {
			t.Fatalf("legacy facts=%v", facts)
		}
	}
}

func TestPlanParallelBasisInvalidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*planGraphRuntime, *planbrief.PlanState)
	}{
		{"selected intent", func(_ *planGraphRuntime, s *planbrief.PlanState) { s.Stories[0].Draft.Name = "changed" }},
		{"shared decisions", func(_ *planGraphRuntime, s *planbrief.PlanState) { s.SharedRevision = "changed" }},
		{"dependency", func(_ *planGraphRuntime, s *planbrief.PlanState) { s.Stories[1].Draft.Name = "changed" }},
		{"transitive dependency", func(_ *planGraphRuntime, s *planbrief.PlanState) { s.Stories[2].Draft.Name = "changed" }},
		{"dependency approval", func(_ *planGraphRuntime, s *planbrief.PlanState) {
			s.Stories[1].Approvals = map[string]string{"story": "approved"}
		}},
		{"missing dependency", func(_ *planGraphRuntime, s *planbrief.PlanState) { s.Stories = append(s.Stories[:1], s.Stories[2:]...) }},
		{"dependency hold", func(_ *planGraphRuntime, s *planbrief.PlanState) { s.Stories[1].Hold = "owner review" }},
		{"dependency retirement", func(_ *planGraphRuntime, s *planbrief.PlanState) { s.Stories[1].Retired = true }},
		{"scope", func(r *planGraphRuntime, _ *planbrief.PlanState) { r.scope.IssueRefs = []string{"github:acme/api#4"} }},
		{"capture", func(r *planGraphRuntime, _ *planbrief.PlanState) { r.definition.Digest = "changed" }},
		{"sizing evidence", func(_ *planGraphRuntime, s *planbrief.PlanState) { s.StorySet.Entries[0].SizeReason = "changed" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, s := parallelBasisFixture()
			before := r.parallelBasis(s, nil)
			tc.mutate(r, &s)
			if after := r.parallelBasis(s, nil); after == before {
				t.Fatal("changed basis retained")
			}
		})
	}
}

func TestPlanParallelBasisIndependentSibling(t *testing.T) {
	r, s := parallelBasisFixture()
	before := r.parallelBasis(s, nil)
	s.Stories[3].Draft.Name = "Independent completed"
	s.Stories[3].Approvals = map[string]string{"acceptance": "approved"}
	s.Stories[3].Hold = "sibling hold"
	s.StorySet.Entries[1].SizeReason = "independent sizing changed"
	if got := r.parallelBasis(s, nil); got != before {
		t.Fatal("independent sibling invalidated selected story")
	}
}

func TestPlanParallelBasisPreservesAppliedProjection(t *testing.T) {
	r, s := parallelBasisFixture()
	s.Stories[0].Candidate = ""
	basis := r.parallelBasis(s, nil)
	g, err := planbrief.NewPlanParallelGroup("group", basis, 3, s.Stories[0].Draft)
	if err != nil {
		t.Fatal(err)
	}
	s.Stories[0].Draft.Name = "Applied projection"
	s.Stories[0].Candidate = "applied"
	g.ExpectedDraftRevision = s.Stories[0].Draft.Revision()
	if got := r.parallelBasis(s, g); got != basis {
		t.Fatal("own applied projection invalidated sealed basis")
	}
}

func TestPlanParallelContributionWireRoundTrip(t *testing.T) {
	c := planbrief.PlanParallelContribution{Draft: planbrief.PlanDraft{Key: "story-001", Name: "Intent", UserStory: planbrief.UserStoryContent{Actor: "Owner", Want: "A plan", Benefit: "Clarity"}}, Agreement: "proposal"}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var got planbrief.PlanParallelContribution
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Draft.Name != c.Draft.Name || got.Agreement != c.Agreement {
		t.Fatalf("round trip changed contribution: %+v", got)
	}
}

// The group is admitted on its minimum lane, never its full bundle, so it
// starts as soon as one member fits instead of waiting for an idle pool.
func TestPlanParallelCapacityAdmitsOnMinimum(t *testing.T) {
	for _, tc := range []struct {
		name                string
		weight, total, cost int
	}{
		{"default", 1, 4, 1}, {"weighted", 3, 6, 3}, {"impossible", 3, 2, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, session, _ := agentResultFixture(t, "plan")
			raw, err := os.ReadFile("../../teams/source/base-team/plan/plan.dot")
			if err != nil {
				t.Fatal(err)
			}
			if tc.weight != 1 {
				raw = []byte(strings.Replace(string(raw), `technical_agent    [lf_action="lf_agent",`, fmt.Sprintf(`technical_agent    [lf_slot_budget="%d", lf_action="lf_agent",`, tc.weight), 1))
			}
			profile, err := graphexec.ParseProfile(raw)
			if err != nil {
				t.Fatal(err)
			}
			definition, err := db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "parallel-fixture", Mode: "plan", ProfileVersion: 1, Path: "plan/plan.dot", Digest: profile.Digest})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.AdmitPlanWorkflowPath(t.Context(), session.ID, "graph"); err != nil {
				t.Fatal(err)
			}
			files, err := json.Marshal(map[string][]byte{"plan.dot": raw})
			if err != nil {
				t.Fatal(err)
			}
			admission, err := db.AdmitWorkflowLaunch(t.Context(), store.WorkflowLaunch{Mode: "plan", SessionID: session.ID, DefinitionID: definition.ID, Start: profile.Start, ContentDigest: profile.Digest, TopologyDigest: profile.Digest, Files: files})
			if err != nil {
				t.Fatal(err)
			}
			pool := &slotStoryPool{total: tc.total}
			parent := &slotStoryParent{pool: pool}
			defer parent.Release()
			// Leave one slot fewer than the minimum free.
			held := 1
			if tc.cost <= tc.total {
				held = tc.total - tc.cost + 1
			}
			competitor, ok, err := pool.acquire(held)
			if err != nil || !ok {
				t.Fatal(err)
			}
			ctx := WithStepCapacity(t.Context(), parent)
			admitted, diagnostic, err := admitGraphCapacity(ctx, Options{DB: db}, admission.Instance.ID, admission.LaunchID, "parallel_group", "plan")
			if err != nil {
				t.Fatal(err)
			}
			if admitted || !strings.Contains(diagnostic, fmt.Sprintf("requested=%d", tc.cost)) {
				t.Fatalf("admitted=%v diagnostic=%s", admitted, diagnostic)
			}
			if used, _ := pool.Capacity(); used != held {
				t.Fatalf("refused admission leaked %d reservations", used-held)
			}
			competitor()
			if tc.cost < tc.total {
				// With only the minimum free the group starts.
				rest, ok, err := pool.acquire(tc.total - tc.cost)
				if err != nil || !ok {
					t.Fatal(err)
				}
				defer rest()
			}
			admitted, diagnostic, err = admitGraphCapacity(ctx, Options{DB: db}, admission.Instance.ID, admission.LaunchID, "parallel_group", "plan")
			if err != nil {
				t.Fatal(err)
			}
			if tc.cost > tc.total {
				if admitted || !strings.Contains(diagnostic, "impossible capacity fit") {
					t.Fatalf("impossible admitted=%v diagnostic=%s", admitted, diagnostic)
				}
				if used, _ := pool.Capacity(); used != 0 {
					t.Fatalf("impossible bundle leaked %d", used)
				}
				return
			}
			if !admitted {
				t.Fatalf("bundle refused: %s", diagnostic)
			}
			if used, _ := pool.Capacity(); used != tc.total {
				t.Fatalf("used=%d want=%d", used, tc.total)
			}
			competing := &slotStoryParent{pool: pool}
			defer competing.Release()
			if ok, _, err := admitGraphCapacity(WithStepCapacity(t.Context(), competing), Options{DB: db}, admission.Instance.ID, admission.LaunchID, "parallel_group", "plan"); err != nil || ok {
				t.Fatalf("competing group over-admitted: %v %v", ok, err)
			}
			if used, _ := pool.Capacity(); used != tc.total {
				t.Fatalf("competing group leaked %d", used-tc.total)
			}
			parent.Release()
			parent.Release()
			if used, _ := pool.Capacity(); used != tc.total-tc.cost {
				t.Fatalf("group release leaked %d", used-(tc.total-tc.cost))
			}
		})
	}
}

func TestPlanParallelCachedProjectionRechecksGuards(t *testing.T) {
	for _, name := range []string{"group version", "route version", "owner hold", "lifecycle hold", "owner wait"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			r, state := parallelBasisFixture()
			r.opts, r.parentRef = f.opts, f.ref()
			item := &state.Stories[0]
			g, err := planbrief.NewPlanParallelGroup("group", "basis", 4, item.Draft)
			mustProgress(t, err)
			item.Parallel = g
			item.Route = &planbrief.PlanRouteState{Version: planbrief.PlanParallelVersion}
			g.Basis = r.parallelBasis(state, g)
			g.Sealed = true
			g.Projections["story"] = json.RawMessage(`{"request_id":"cached"}`)
			switch name {
			case "group version":
				g.Version++
			case "route version":
				item.Route.Version++
			case "owner hold":
				item.Hold = "Owner review required"
			case "lifecycle hold":
				item.LifecycleHold = "Remote lifecycle unresolved"
			case "owner wait":
				item.Wait = &planbrief.PlanWait{ID: "owner"}
			}
			_, err = f.db.SavePlanWorkflowState(t.Context(), f.ref().String(), 0, state)
			mustProgress(t, err)
			definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "test", Mode: "plan", ProfileVersion: 1, Path: "plan.dot", Digest: strings.Repeat("a", 64)})
			mustProgress(t, err)
			instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: "parallel_group", StartedAt: time.Now()})
			mustProgress(t, err)
			attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "parallel_group", StartedAt: time.Now()})
			mustProgress(t, err)
			mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "ready", FinishedAt: time.Now(), InstanceStatus: store.GraphRunning}))
			_, _, err = r.parallelProjection(t.Context(), ActivityRequest{InstanceID: instance.ID, Attrs: map[string]string{"lf_source": "parallel_group", "lf_stage": "story"}})
			if err == nil {
				t.Fatal("cached request bypassed current guard")
			}
		})
	}
}

func TestPlanParallelGrowthReselectsInvalidatedStage(t *testing.T) {
	f := newFixture(t, nil)
	r, state := parallelBasisFixture()
	r.opts, r.parentRef, r.stage = f.opts, f.ref(), "acceptance"
	item := &state.Stories[0]
	item.Stage = "story"
	item.Route = &planbrief.PlanRouteState{Version: planbrief.PlanParallelVersion, Fallback: "Changed intent"}
	item.Parallel = &planbrief.PlanParallelGroup{Version: planbrief.PlanParallelVersion, Blockers: []string{"Changed intent"}}
	_, err := f.db.SavePlanWorkflowState(t.Context(), f.ref().String(), 0, state)
	mustProgress(t, err)
	outcome := r.parallelOutcome(t.Context(), "fallback")
	if outcome.Outcome != "fallback" || r.stage != "story" || !strings.Contains(r.rewindReason, "Changed intent") {
		t.Fatalf("growth did not reselect required review: %+v stage=%s reason=%s", outcome, r.stage, r.rewindReason)
	}
}
