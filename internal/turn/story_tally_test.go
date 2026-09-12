package turn

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// seedManagedStories seeds a managed feature and total stories directly
// through the store, the first done of them "completed" — a legacy-shaped
// done state independent of [store.ManagedStory.EngineeringComplete] — and
// the rest "planning". It exists to drive [storyTally]'s own read-and-count
// logic without the full StoryProgress ingestion pipeline, which
// [TestStoryTallyCountsEngineeringCompleteAsDone] exercises separately for
// the one predicate arm this seeding cannot reach.
func seedManagedStories(t *testing.T, f *fixture, parent string, version, total, done int) {
	t.Helper()
	ctx := t.Context()
	if err := f.db.PutManagedFeature(ctx, store.ManagedFeature{
		ParentTask: parent, Generation: 1, State: "planning", WorkflowVersion: version,
	}); err != nil {
		t.Fatalf("seed managed feature: %v", err)
	}
	for i := range total {
		key := fmt.Sprintf("US-%d", i+1)
		state := "planning"
		switch {
		case i < done:
			state = "completed"
		case i == done:
			// One non-done story sits at "blocked" rather than "planning" so
			// this predicate is exercised too (AC-015): a blocked story must
			// not count, whatever else is true of the feature.
			state = "blocked"
		}
		s := store.ManagedStory{
			ParentTask: parent, Generation: 1, Key: key,
			IssueNumber: 300 + i, IssueID: int64(3000 + i), BodyHash: "hash-" + key, State: state,
		}
		if err := f.db.PutManagedStory(ctx, s); err != nil {
			t.Fatalf("seed managed story %s: %v", key, err)
		}
	}
}

// TestStoryTallyCountsDoneStoriesForImplementRun covers S2-T5's "2 of 5"
// scenario and AC-015's legacy verified/completed arm and its blocked
// exclusion, all at once: of five stories, two are "completed" (done), one is
// "blocked" (not done, even though it is a terminal-looking state) and two
// are "planning" (not done).
func TestStoryTallyCountsDoneStoriesForImplementRun(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	parent := f.ref().String()
	seedManagedStories(t, f, parent, store.WorkflowLegacy, 5, 2)

	result := Result{Ref: f.ref(), Run: store.Run{Mode: string(agent.ModeImplement)}}
	tally := storyTally(t.Context(), result, f.db, f.opts.Logger)
	if tally == nil || tally.Done != 2 || tally.Total != 5 {
		t.Fatalf("storyTally(Implement) = %+v, want &{Done:2 Total:5}", tally)
	}
}

// TestStoryTallyGatedOnImplementMode covers AC-013: the same managed feature,
// read by a Plan-mode run, yields no tally at all — the gate is
// result.Run.Mode, not whether the feature happens to have unfinished
// stories.
func TestStoryTallyGatedOnImplementMode(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	parent := f.ref().String()
	seedManagedStories(t, f, parent, store.WorkflowLegacy, 5, 2)

	result := Result{Ref: f.ref(), Run: store.Run{Mode: string(agent.ModePlan)}}
	if tally := storyTally(t.Context(), result, f.db, f.opts.Logger); tally != nil {
		t.Errorf("storyTally(Plan) = %+v, want nil: a Plan run reports no partial tally (AC-013)", tally)
	}
}

// TestStoryTallyNilWithNoManagedFeature covers AC-016's "the feature is not
// managed" case: no [store.DB.PutManagedFeature] call happened at all for
// this task, so the read is an ordinary miss rather than a failure, and it
// still yields nil.
func TestStoryTallyNilWithNoManagedFeature(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)

	result := Result{Ref: f.ref(), Run: store.Run{Mode: string(agent.ModeImplement)}}
	if tally := storyTally(t.Context(), result, f.db, f.opts.Logger); tally != nil {
		t.Errorf("storyTally with no managed feature = %+v, want nil", tally)
	}
}

// TestStoryTallyNilOnStoreErrorAndLogsIt covers AC-016's store-failure case:
// a read that genuinely fails — simulated here by closing the database out
// from under it — returns nil rather than propagating, and is logged rather
// than silently dropped.
func TestStoryTallyNilOnStoreErrorAndLogsIt(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	parent := f.ref().String()
	seedManagedStories(t, f, parent, store.WorkflowLegacy, 3, 1)

	if err := f.db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	result := Result{Ref: f.ref(), Run: store.Run{Mode: string(agent.ModeImplement)}}
	tally := storyTally(t.Context(), result, f.db, f.opts.Logger)
	if tally != nil {
		t.Errorf("storyTally after the store failed = %+v, want nil (AC-016)", tally)
	}
	if !strings.Contains(f.logs.String(), "story tally") {
		t.Errorf("a failed tally read was not logged (AC-016):\n%s", f.logs.String())
	}
}

// TestStoryTallyCountsEngineeringCompleteAsDone covers AC-015's separated
// workflow arm: a story that went through real engineering-completion
// ingestion lands at EngineeringComplete=true, state="frozen" — not
// "verified" or "completed" — and still has to count as done, or every
// successful separated-workflow Implement run would report 0-of-N.
func TestStoryTallyCountsEngineeringCompleteAsDone(t *testing.T) {
	f, _, runID, root := engineeringRun(t)
	e := engineeringEvent(t, f, runID, root)
	writeProgressEvent(t, root, e)
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))

	stories, err := f.db.ListManagedStories(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	if !stories[0].EngineeringComplete || stories[0].State != "frozen" {
		t.Fatalf("fixture precondition failed: US-1 = %+v, want EngineeringComplete=true, State=frozen", stories[0])
	}

	result := Result{Ref: f.ref(), Run: store.Run{Mode: string(agent.ModeImplement)}}
	tally := storyTally(t.Context(), result, f.db, f.opts.Logger)
	if tally == nil || tally.Done != 1 || tally.Total != 2 {
		t.Fatalf("storyTally after engineering completion = %+v, want &{Done:1 Total:2}: "+
			"engineering_complete=1,state=frozen must count as done (AC-015)", tally)
	}
}

// TestRunInputCarriesTheStoryTally covers the one line [Reporter.runInput]
// added for this story: [report.Run.Stories] on the render input it builds is
// exactly what [storyTally] computes for that result — Implement-mode reaches
// the "2 of 5" tally its managed feature has, and Plan-mode against the same
// feature carries no tally at all (AC-013). [storyTally] itself is exercised
// as a bare function by the tests above; this one exercises the point where
// its result actually reaches a [report.Run] a comment gets rendered from,
// which is otherwise untested.
func TestRunInputCarriesTheStoryTally(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)
	parent := f.ref().String()
	seedManagedStories(t, f, parent, store.WorkflowLegacy, 5, 2)

	implement := Result{Ref: f.ref(), SessionID: f.session.ID, Run: store.Run{Mode: string(agent.ModeImplement), Ordinal: 1}}
	run, err := reporter.runInput(t.Context(), implement)
	if err != nil {
		t.Fatalf("runInput(Implement): %v", err)
	}
	if run.Stories == nil || run.Stories.Done != 2 || run.Stories.Total != 5 {
		t.Errorf("runInput(Implement).Stories = %+v, want &{Done:2 Total:5}", run.Stories)
	}

	plan := Result{Ref: f.ref(), SessionID: f.session.ID, Run: store.Run{Mode: string(agent.ModePlan), Ordinal: 1}}
	run, err = reporter.runInput(t.Context(), plan)
	if err != nil {
		t.Fatalf("runInput(Plan): %v", err)
	}
	if run.Stories != nil {
		t.Errorf("runInput(Plan).Stories = %+v, want nil (AC-013)", run.Stories)
	}
}

// TestStoryDonePredicate is the unit-level complement to the delivery tests
// above: every state [storyTally] can see, checked directly against
// AC-015's predicate.
func TestStoryDonePredicate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		story store.ManagedStory
		want  bool
	}{
		{"engineering complete and frozen (separated workflow)", store.ManagedStory{EngineeringComplete: true, State: "frozen"}, true},
		{"verified (legacy workflow)", store.ManagedStory{State: "verified"}, true},
		{"completed (legacy workflow)", store.ManagedStory{State: "completed"}, true},
		{"blocked does not count", store.ManagedStory{State: "blocked"}, false},
		{"planning does not count", store.ManagedStory{State: "planning"}, false},
		{"frozen without engineering complete does not count", store.ManagedStory{State: "frozen"}, false},
		{"running does not count", store.ManagedStory{State: "running"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := storyDone(tc.story); got != tc.want {
				t.Errorf("storyDone(%+v) = %v, want %v", tc.story, got, tc.want)
			}
		})
	}
}
