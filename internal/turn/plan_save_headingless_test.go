package turn

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// TestPlanSaveAppendsAStoryIndexToAHeadinglessPlanBrief is #429 end to
// end. A Plan role agent overwrote the session's plan_brief.md with its prose
// result comment, which has no "## Story Index" heading. Every
// `/approve-plan-set` save then failed with "plan brief Story Index missing"
// and the Plan never became ready. The save step now appends the Story Index,
// keeps the agent's text, commits the file and marks the Plan ready.
func TestPlanSaveAppendsAStoryIndexToAHeadinglessPlanBrief(t *testing.T) {
	f, _, base, _ := graphPlanFixture(t, "agent")
	executeGraphPlan(t, f, base)
	for i := range 3 {
		executeGraphPlan(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+int64(i+1)))
	}
	runParent(t, f, base)
	state := graphPlanState(t, f)
	if state.AssembledWait == "" {
		t.Fatal("final Plan set wait missing")
	}

	// Seed what run 31 of #406 left behind: a headingless plan_brief.md in the
	// task's session directory.
	worktree := f.worktreeDir()
	if _, err := os.Stat(filepath.Join(worktree, ".git")); err != nil {
		t.Fatalf("the task worktree is not prepared before the save: %v", err)
	}
	session := f.reloadSession()
	slug, err := naming.SlugFromBranch(session.Branch, f.ref().Issue)
	mustProgress(t, err)
	dir, err := naming.AITeamSessionDir(session.Branch, time.Now(), slug)
	mustProgress(t, err)
	rel := dir + "/" + planbrief.FileName
	plan := filepath.Join(worktree, filepath.FromSlash(rel))
	const prose = "Plan accepted for story-001. The acceptance criteria and tasks are complete."
	mustProgress(t, os.MkdirAll(filepath.Dir(plan), 0o755))
	mustProgress(t, os.WriteFile(plan, []byte(prose), 0o600))

	final := base
	final.Trigger = Trigger{CommentID: base.Trigger.CommentID + 10, Body: "/approve-plan-set " + state.AssembledWait, AuthorID: 7}
	final.Command = Command{Kind: KindFreeText, Text: final.Trigger.Body}
	executeGraphPlan(t, f, final)

	feature, err := f.db.GetManagedFeature(t.Context(), f.ref().String())
	mustProgress(t, err)
	saved := graphPlanState(t, f)
	if !feature.Ready || !saved.PlanReady(planbrief.ContentScope{Roles: []string{"go-implementation-engineer"}}) {
		t.Fatalf("Plan not ready after saving over a headingless plan_brief.md: %+v", feature)
	}

	data, err := os.ReadFile(plan)
	mustProgress(t, err)
	text := string(data)
	binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), "story-001")
	mustProgress(t, err)
	if !strings.HasPrefix(text, prose+"\n\n## Story Index\n") {
		t.Fatalf("agent text not kept ahead of an appended Story Index:\n%s", text)
	}
	if got := strings.Count(text, "## Story Index"); got != 1 {
		t.Fatalf("plan_brief.md has %d Story Index sections, want 1:\n%s", got, text)
	}
	if !strings.Contains(text, "| story-001 |") || !strings.Contains(text, fmt.Sprintf("[#%d]", binding.IssueNumber)) {
		t.Fatalf("Story Index does not list the active story:\n%s", text)
	}

	// The save committed the repaired file.
	if status := f.repo.GitIn(worktree, "status", "--porcelain", "--", rel); strings.TrimSpace(status) != "" {
		t.Fatalf("repaired plan_brief.md left uncommitted: %s", status)
	}
	if committed := f.repo.GitIn(worktree, "show", "HEAD:"+rel); strings.TrimSpace(committed) != strings.TrimSpace(text) {
		t.Fatalf("committed plan_brief.md differs from the worktree:\n%s", committed)
	}
}
