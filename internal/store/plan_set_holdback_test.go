package store

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// Story-003 T-022/T-023 gap tests: the story set survives real SQLite, and the
// hold-back that keeps new sessions on the legacy Plan route is unchanged (AC-045,
// AC-012). CONC-24: graph Plan stays held back until the owner-run tryout and a
// separately approved lift.

// A story set proposed, promoted and saved is what a later turn, or a restart,
// reads back: revisions, reserved keys, the promoted revision and the stories.
func TestPlanWorkflowStore_StorySetSurvivesSaveAndRestartReads(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := newTestDB(t)
	session := testSession(t, db, 377)
	entries := []planbrief.PlanStorySetEntry{
		{ID: "s1", Name: "Storage", Intent: "Keep the data", DependsOn: []string{"s2"}},
		{ID: "s2", Name: "Schema", Intent: "Define the tables"},
	}
	state := planbrief.PlanState{SharedRevision: "initial"}
	revision, _, err := state.ProposeStorySet(entries)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := db.SavePlanWorkflowState(ctx, session.TaskRef, 0, state)
	if err != nil {
		t.Fatal(err)
	}
	proposed, rev, err := db.GetPlanWorkflowState(ctx, session.TaskRef)
	if err != nil || rev != saved || proposed.StorySet == nil || proposed.StorySet.Revision != revision || proposed.StorySet.Promoted || len(proposed.Stories) != 0 {
		t.Fatalf("proposed set read back as %+v rev %d err %v", proposed.StorySet, rev, err)
	}
	if _, err = proposed.PromoteStorySet(revision); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SavePlanWorkflowState(ctx, session.TaskRef, rev, proposed); err != nil {
		t.Fatal(err)
	}
	again, _, err := db.GetPlanWorkflowState(ctx, session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	wantKeys := map[string]string{"s1": "story-001", "s2": "story-002"}
	if !again.StorySet.Promoted || again.StorySet.PromotedRevision != revision || !reflect.DeepEqual(again.StorySet.Keys, wantKeys) || len(again.Stories) != 2 {
		t.Fatalf("promoted set read back as %+v with %d stories", again.StorySet, len(again.Stories))
	}
	if !reflect.DeepEqual(again.Stories[0].Draft.Dependencies.StoryKeys, []string{"story-002"}) {
		t.Fatalf("translated dependencies were lost: %+v", again.Stories[0].Draft.Dependencies)
	}
	// A read state repeats the promotion for the same revision as a no-op.
	if changed, err := again.PromoteStorySet(revision); err != nil || changed {
		t.Fatalf("repeat after a read: %v %v", changed, err)
	}
	// A stale writer cannot overwrite the promoted state.
	if _, err = db.SavePlanWorkflowState(ctx, session.TaskRef, rev, proposed); err == nil {
		t.Fatal("a save at a stale revision succeeded")
	}
}

// New sessions select the legacy Plan route through the factory's admission seam;
// the choice is frozen on first admission and neither a repeat nor a later graph
// selection moves it (AC-045 hold-back retained).
func TestAdmitPlanPath_NewSessionsStayLegacyAndRoutesAreFrozen(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := newTestDB(t)

	fresh := testSession(t, db, 501)
	if _, err := db.PlanExecutionPath(ctx, fresh.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a session had a route before admission: %v", err)
	}
	for i := range 2 {
		if route, err := db.AdmitPlanPath(ctx, fresh.ID); err != nil || route != "legacy" {
			t.Fatalf("admission %d selected %q, %v; new sessions must stay on the legacy route", i, route, err)
		}
	}
	if route, err := db.AdmitPlanWorkflowPath(ctx, fresh.ID, "graph"); err != nil || route != "legacy" {
		t.Fatalf("a later graph selection moved a legacy session to %q, %v", route, err)
	}

	existing := testSession(t, db, 502)
	if route, err := db.AdmitPlanWorkflowPath(ctx, existing.ID, "graph"); err != nil || route != "graph" {
		t.Fatalf("setup: %q %v", route, err)
	}
	if route, err := db.AdmitPlanPath(ctx, existing.ID); err != nil || route != "graph" {
		t.Fatalf("a session already on graph Plan was moved to %q, %v", route, err)
	}
	if route, err := db.PlanExecutionPath(ctx, existing.ID); err != nil || route != "graph" {
		t.Fatalf("persisted route %q, %v", route, err)
	}

	for _, bad := range []string{"", "Graph", "set", "legacy graph"} {
		if _, err := db.AdmitPlanWorkflowPath(ctx, fresh.ID, bad); err == nil {
			t.Errorf("route %q was admitted", bad)
		}
	}
	if _, err := db.AdmitPlanPath(ctx, 0); err == nil {
		t.Error("a missing session was admitted")
	}
}

// AC-045: the parent's rollout obligations stay recorded and unsatisfied in the
// plan brief. Completing the three stories does not satisfy the real-agent tryout
// or the separately approved hold-back lift, and this guard fails if the record is
// dropped or rewritten as done. It skips where the session artifacts are absent.
func TestPlanBrief_ParentRolloutObligationsRemainRecordedAndPending(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", ".aiteam", "lf-377-graph-plan-bypasses-the-shared-mcp", "2026-09-24", "graph-plan-bypasses-the-shared-mcp", "plan_brief.md")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("plan brief %s is not present in this checkout", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.ReplaceAll(string(raw), "\r\n", "\n")
	_, rest, ok := strings.Cut(doc, "\n## Parent rollout obligations\n")
	if !ok {
		t.Fatal("the plan brief has no 'Parent rollout obligations' section")
	}
	section, _, _ := strings.Cut(rest, "\n## ")
	rows := map[string]string{}
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 4 {
			continue
		}
		rows[strings.TrimSpace(cells[0])] = strings.TrimSpace(cells[len(cells)-1])
	}
	for _, obligation := range []string{"Real graph Plan tryout", "Hold-back lift and verification"} {
		status, ok := rows[obligation]
		if !ok {
			t.Errorf("no %q row in the rollout obligations", obligation)
			continue
		}
		if !strings.Contains(status, "Pending") {
			t.Errorf("%q is no longer pending: %q", obligation, status)
		}
		for _, done := range []string{"Satisfied", "Completed", "Done"} {
			if strings.HasPrefix(status, done) {
				t.Errorf("%q is recorded as %q without evidence", obligation, done)
			}
		}
	}
	if !strings.Contains(section, "unsatisfied") {
		t.Error("the section no longer says the obligations are unsatisfied")
	}
}
