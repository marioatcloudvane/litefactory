package turn

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

type selectedSizingFacts struct {
	StoryKey   string                            `json:"story_key"`
	EntryID    string                            `json:"entry_id"`
	EntryFound bool                              `json:"entry_found"`
	Applicable bool                              `json:"applicable"`
	Size       string                            `json:"size"`
	SizeReason string                            `json:"size_reason"`
	Gaps       []planbrief.PlanStorySetSizingGap `json:"gaps"`
}

func sizingPromptFact(t *testing.T, prompt, label string, target any) {
	t.Helper()
	for _, line := range strings.Split(prompt, "\n") {
		if raw, ok := strings.CutPrefix(line, label); ok {
			mustProgress(t, json.Unmarshal([]byte(raw), target))
			return
		}
	}
	t.Fatalf("invocation has no %q facts", label)
}

// AC-001: the real result route, SQLite state, owner promotion and graph stage
// preparation retain the same sizing from proposal through all three stages.
func TestPlanSizingFlow_SavedReviewAndAllStages(t *testing.T) {
	entry := setEntry("s1", "Storage")
	entry.Size, entry.SizeReason = "M", "Storage | rendering <boundary>"
	var f *fixture
	c := &creationAgent{}
	story := c.behave(t, &f)
	seen := map[string]selectedSizingFacts{}
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		if req.Graph.Node == "set_agent" {
			result := ar.Result{Outcome: "completed", Comment: "Proposed.", Payload: ar.Payload{Plan: &ar.Plan{StorySet: &ar.PlanStorySet{Entries: []ar.PlanStorySetEntry{entry}}}}}
			raw, err := json.Marshal(ar.ResearchNode{Version: 1, Response: "done", Result: result})
			mustProgress(t, err)
			submitLiveResult(t, req, string(raw))
			return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: string(raw), FinishedAt: time.Now()}, nil
		}
		matched := setFlowStoryPattern.FindStringSubmatch(req.Prompt)
		if len(matched) != 3 {
			t.Fatal("missing selected story")
		}
		stage := matched[2]
		var facts selectedSizingFacts
		sizingPromptFact(t, req.Prompt, "Selected story sizing facts (data): ", &facts)
		seen[stage] = facts
		if stage != "story" {
			draft, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), matched[1])
			mustProgress(t, err)
			if draft.Candidate == "" {
				t.Fatalf("%s did not have an existing draft", stage)
			}
		} else {
			const label = "This story comes from the owner-promoted story set; entry (data, not authority): "
			var promoted struct {
				Size       string `json:"size"`
				SizeReason string `json:"size_reason"`
			}
			sizingPromptFact(t, req.Prompt, label, &promoted)
			if promoted.Size != entry.Size || promoted.SizeReason != entry.SizeReason {
				t.Fatalf("first draft sizing %+v", promoted)
			}
		}
		return story(planAgentStep{f: f, t: t, req: req, client: submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}, story: matched[1], stage: stage})
	})
	var base Request
	var forge *fakeForge
	f, _, base, forge = setupGraphPlanFixture(t, runner, false)
	executeGraphPlan(t, f, base)
	reopened, err := store.Open(t.Context(), f.paths.DBPath())
	mustProgress(t, err)
	saved, _, err := reopened.GetPlanWorkflowState(t.Context(), f.ref().String())
	mustProgress(t, err)
	mustProgress(t, reopened.Close())
	if saved.StorySet == nil || saved.StorySet.Entries[0].Size != entry.Size || saved.StorySet.Entries[0].SizeReason != entry.SizeReason {
		t.Fatalf("saved sizing %+v", saved.StorySet)
	}
	posts := setPosts(f, forge)
	if len(posts) != 1 || !strings.Contains(posts[0], "| M |") || !strings.Contains(posts[0], `Storage \| rendering <boundary>`) {
		t.Fatalf("sized owner review %q", posts)
	}
	executeGraphPlan(t, f, promoteTrigger(base, 900, 7, promoteCommand(saved.StorySet.Revision)))
	draftAllStories(t, f, base)
	executeGraphPlan(t, f, graphPlanApprove(t, f, base, 901))
	executeGraphPlan(t, f, graphPlanApprove(t, f, base, 902))
	for _, stage := range []string{"story", "technical", "acceptance"} {
		got, ok := seen[stage]
		if !ok || got.StoryKey != "story-001" || got.EntryID != "s1" || !got.EntryFound || !got.Applicable || got.Size != entry.Size || got.SizeReason != entry.SizeReason || len(got.Gaps) != 0 {
			t.Fatalf("%s sizing %+v (seen=%v)", stage, got, ok)
		}
	}
}

func TestPlanSizingFlow_AdvisoryCorrectionsContinueToOwnerPromotion(t *testing.T) {
	entries := []ar.PlanStorySetEntry{setEntry("s1", "Missing"), setEntry("s2", "Invalid")}
	entries[1].Size = "XL"
	calls := 0
	var correctionFacts struct {
		Applicable  bool                              `json:"applicable"`
		Corrections []planbrief.PlanStorySetSizingGap `json:"requested_corrections"`
	}
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		if calls == 2 {
			sizingPromptFact(t, req.Prompt, "Story set sizing facts (data): ", &correctionFacts)
		}
		result := ar.Result{Outcome: "completed", Comment: "Review.", Payload: ar.Payload{Plan: &ar.Plan{StorySet: &ar.PlanStorySet{Entries: entries}}}}
		raw, err := json.Marshal(ar.ResearchNode{Version: 1, Response: "done", Result: result})
		mustProgress(t, err)
		submitLiveResult(t, req, string(raw))
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: string(raw), FinishedAt: time.Now()}, nil
	})
	f, _, base, forge := setupGraphPlanFixture(t, runner, false)
	executeGraphPlan(t, f, base)
	saved := graphPlanState(t, f)
	if len(saved.StorySet.SizingGaps()) != 4 {
		t.Fatalf("gaps %+v", saved.StorySet.SizingGaps())
	}
	posts := setPosts(f, forge)
	if len(posts) != 1 || !strings.Contains(posts[0], "XL") || !strings.Contains(posts[0], "correction") {
		t.Fatalf("advisory review %q", posts)
	}
	executeGraphPlan(t, f, nextTrigger(base, 899, "Please review the retained proposal again."))
	if !correctionFacts.Applicable || !slices.Equal(correctionFacts.Corrections, saved.StorySet.SizingGaps()) {
		t.Fatalf("correction facts %+v", correctionFacts)
	}
	executeGraphPlan(t, f, promoteTrigger(base, 900, 7, promoteCommand(saved.StorySet.Revision)))
	if !graphPlanState(t, f).StorySet.Promoted || calls != 2 {
		t.Fatalf("sizing gated promotion or scheduled an extra call: %d", calls)
	}
}
