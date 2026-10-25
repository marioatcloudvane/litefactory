package turn

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestManagedNoScopePreparationAssessmentRemainsReadOnly(t *testing.T) {
	var f *fixture
	var before retainedSnapshot
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		surface, ok := req.Permissions.Payload.(harness.ToolSurface)
		if !ok || req.Mode != string(agent.ModeImplement) || req.Permissions.Profile != "validate" || !reflect.DeepEqual(surface.Tools, []string{"Read", "Glob", "Grep"}) || slices.Contains(surface.AllowedTools, "Write") {
			t.Fatalf("managed assessment gained write authority or changed mode: %+v", req)
		}
		if !strings.Contains(req.Prompt, "limited to preparation and reading") || strings.Contains(req.Prompt, "UNAPPROVED SCOPE TEXT") {
			t.Fatalf("assessment lost its authority boundary or admitted refinement scope: %s", req.Prompt)
		}
		t.Logf("managed reading invocation ref=%s path=%s profile=%s tools=%v", req.Ref, req.Path, req.Permissions.Profile, surface.Tools)
		assertRetained(t, before, captureRetained(t, f, req.WorktreePath))
		r := succeeded(req, f.paths.Home())
		r.ResumeToken = "assessment-conversation"
		return r, nil
	})
	f = newFixture(t, runner)
	mustProgress(t, f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: f.session.TaskRef, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	pending := store.StoryRefinement{SessionID: f.session.ID, CommentID: 8080, ChildNumber: 200, ChildID: 2000, Generation: 1, ActorID: 42, Body: "UNAPPROVED SCOPE TEXT"}
	mustProgress(t, f.db.AdmitStoryRefinement(t.Context(), pending))
	dir := seedTaskWorktree(t, f)
	conflictBaseAndBranch(t, f.repo, dir)
	dirtyRetained(t, f, dir)
	before = captureRetained(t, f, dir)
	f.opts.AutoSyncBase = true
	req := f.request()
	for turn := 0; turn < 2; turn++ {
		result := f.execute(req)
		if result.Harness == nil || result.Committed || result.Pushed || result.Run.ID == 0 {
			t.Fatalf("assessment did not run or saved changes: %+v logs=%s", result, f.logs)
		}
		assertRetained(t, before, captureRetained(t, f, dir))
		feature, err := f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
		mustProgress(t, err)
		if feature.State != "planning" || feature.Ready {
			t.Fatalf("assessment froze unsettled scope: %+v", feature)
		}
		remaining, err := f.db.ListPendingStoryRefinements(t.Context(), f.session.ID)
		mustProgress(t, err)
		if !reflect.DeepEqual(remaining, []store.StoryRefinement{pending}) {
			t.Fatalf("assessment consumed pending refinement: %+v", remaining)
		}
		req.Trigger.CommentID++
	}
	if runner.callCount() != 2 {
		t.Fatal("cold and resumed assessments did not each reach reading")
	}
}

// Preparation and reporting are not receiving steps. Existing assignments and
// an exact target with no eligible receiver survive repeated observation.
func TestPreparationObservationPreservesOwnerInputBindings(t *testing.T) {
	f := newFixture(t, nil)
	at := time.Now().UTC()
	definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "core", Mode: "implement", ProfileVersion: 1, Path: "implement/implement.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: "worker", StartedAt: at})
	mustProgress(t, err)
	for _, source := range []struct {
		id         int64
		node, body string
	}{{31, "worker", "Exact assigned instruction bytes"}, {32, "reviewer", "Exact pending target bytes"}} {
		_, err := f.db.RecordComment(t.Context(), store.Comment{SessionID: f.session.ID, ForgeCommentID: source.id, AuthorUserID: 42, Body: source.body, CreatedAt: at, Classification: store.ClassificationInstruction})
		mustProgress(t, err)
		_, err = f.db.RetainOwnerInstruction(t.Context(), f.session.ID, source.id, 42, source.body, store.OwnerInputTarget{InstanceID: instance.ID, Node: source.node})
		mustProgress(t, err)
	}
	segment, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, store.FirstSegmentOrdinal, "saved-input", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: testComment, LogPath: func(int) string { return "/logs/owner-input" }})
	mustProgress(t, err)
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "worker", RunID: run.ID, AcceptOwnerInput: true, StartedAt: at})
	mustProgress(t, err)
	assigned, err := f.db.GetOwnerInstruction(t.Context(), f.session.ID, 31)
	mustProgress(t, err)
	pending, err := f.db.GetOwnerInstruction(t.Context(), f.session.ID, 32)
	mustProgress(t, err)
	t.Logf("original assigned input=%+v; original undelivered input=%+v", assigned, pending)
	if assigned.AttemptID != attempt.ID || pending.AttemptID != 0 {
		t.Fatal("fixture did not establish saved and pending input")
	}
	dir := seedTaskWorktree(t, f)
	initial := f.repo.GitIn(dir, "rev-parse", "HEAD")
	remote := taskOperator(t, f.repo, testBranch, initial, "remote-task.txt", "remote task work")
	taskOperator(t, f.repo, testBranch, remote, "remote-task-two.txt", "more remote task work")
	advanceSyncBase(t, f.repo, 2)
	f.opts.AutoSyncBase, f.opts.AutoSyncMaxBehind = true, 1
	clone, err := f.opts.Git.Open(t.Context(), f.repo.Dir)
	mustProgress(t, err)
	for range 2 {
		_, _, _, sync, _, err := prepareClone(t.Context(), f.opts, f.request(), clone, f.reloadSession(), f.storeRepo, f.opts.Author, discardLogger(), nil)
		mustProgress(t, err)
		if sync.Task == nil || sync.Task.Outcome != syncOutcomeSkipped || sync.Outcome != syncOutcomeSkipped {
			t.Fatalf("fixture did not defer both phases: %+v", sync)
		}
		_, err = f.db.RecordRunSyncObservations(t.Context(), run.ID, []store.SyncObservation{observation(*sync.Task), observation(sync)})
		mustProgress(t, err)
		passage, err := graphPreparationCaveat(t.Context(), f.db, instance.ID)
		mustProgress(t, err)
		t.Logf("preparation/report projection without receiving input: %s", passage)
		if strings.Count(passage, "Preparation caveat") != 1 {
			t.Fatalf("report projection lost combined preparation: %s", passage)
		}
		for _, want := range []store.OwnerInstruction{assigned, pending} {
			got, err := f.db.GetOwnerInstruction(t.Context(), f.session.ID, want.CommentID)
			mustProgress(t, err)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("observation or reporting changed saved binding: got=%+v want=%+v", got, want)
			}
		}
	}
}

// Only a recorded successful synchronization settles earlier freshness. A
// retry or actual stop must retain the earlier preparation caveat.
func TestGraphPreparationCaveatSettlesOnlyObservedFreshness(t *testing.T) {
	for _, outcome := range []string{"current", "synced", "retry", "conflict"} {
		t.Run(outcome, func(t *testing.T) {
			f := newFixture(t, nil)
			at := time.Now().UTC()
			_, err := f.db.FreezeSessionSlug(t.Context(), f.session.ID, testSlug, testBranch)
			mustProgress(t, err)
			def, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "core", Mode: "implement", ProfileVersion: 1, Path: "implement/implement.dot", Digest: strings.Repeat("b", 64), CreatedAt: at})
			mustProgress(t, err)
			instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: def.ID, CurrentNode: "sync", StartedAt: at})
			mustProgress(t, err)
			segment, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, store.FirstSegmentOrdinal, "caveat-settlement", store.AgentClaude)
			mustProgress(t, err)
			run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: testComment, LogPath: func(int) string { return "/logs/settlement" }})
			mustProgress(t, err)
			_, err = f.db.RecordRunSyncObservations(t.Context(), run.ID, []store.SyncObservation{
				{Phase: "task", Ref: "origin/" + testBranch, LocalSHA: "local", RemoteSHA: "task", Outcome: "blocked", Behind: 1, Reason: "task remains unresolved"},
				{Phase: "base", Ref: "origin/main", LocalSHA: "local", RemoteSHA: "base", Outcome: "blocked", Behind: 1, Reason: "base remains unresolved"},
			})
			mustProgress(t, err)
			source, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "sync", RunID: run.ID, StartedAt: at, Limit: 8})
			mustProgress(t, err)
			mustProgress(t, f.db.FinishGraphNodeAttempt(t.Context(), source.ID, store.GraphSucceeded, "done", at))
			final, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "sync", RunID: run.ID, StartedAt: at, Limit: 8})
			mustProgress(t, err)
			raw, err := json.Marshal(syncBaseOutput{Working: testBranch, Base: "main", Result: outcome, Reason: "final observation"})
			mustProgress(t, err)
			_, err = f.db.RecordGraphOutput(t.Context(), store.GraphOutput{NodeAttemptID: final.ID, Kind: store.GraphOutputJSON, Payload: raw, CreatedAt: at})
			mustProgress(t, err)
			mustProgress(t, f.db.FinishGraphNodeAttempt(t.Context(), final.ID, store.GraphSucceeded, outcome, at))
			reportAttempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "sync", RunID: run.ID, StartedAt: at, Limit: 8})
			mustProgress(t, err)
			mustProgress(t, f.db.FinishGraphNodeAttempt(t.Context(), reportAttempt.ID, store.GraphSucceeded, "published", at))
			caveat, err := graphPreparationCaveat(t.Context(), f.db, instance.ID)
			mustProgress(t, err)
			if !strings.Contains(caveat, "task remains unresolved") {
				t.Fatal("base observation settled unrelated task freshness")
			}
			wantBase := outcome == "retry" || outcome == "conflict"
			if strings.Contains(caveat, "base remains unresolved") != wantBase {
				t.Fatalf("incorrect settlement for %s: %s", outcome, caveat)
			}
		})
	}
}
