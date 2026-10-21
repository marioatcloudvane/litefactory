package turn

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// standardsCoordinatorFixture admits the parent's real coordinator graph at
// its ready activity. That activity starts stories without launching an agent,
// so there is deliberately no parent agent run to supply an implicit binding.
func standardsCoordinatorFixture(t *testing.T) (*implementFixture, store.Repo, store.Session, store.WorkflowAdmission, ActivityRequest) {
	t.Helper()
	f := newImplementFixture(t, implementStories(2, nil)...)
	root := t.TempDir()
	repo, err := f.db.UpsertRepo(t.Context(), store.Repo{Path: root, Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(root, "worktrees")})
	mustProgress(t, err)
	parent, err := f.db.GetOrCreateSession(t.Context(), repo, implementParent.Issue)
	mustProgress(t, err)
	_, err = f.db.AdmitWorkflowPath(t.Context(), parent.ID, "graph")
	mustProgress(t, err)
	definition := mustCaptureImplement(t)
	pin, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: definition.Profile.Team, Mode: "implement", ProfileVersion: definition.Profile.ProfileVersion, Path: workflowGraphPath(definition), Digest: definition.Profile.Digest, TeamSnapshotDigest: definition.Digest, CreatedAt: implementClock})
	mustProgress(t, err)
	files, err := json.Marshal(definition.Files)
	mustProgress(t, err)
	topology, err := graphexec.SemanticDigest(definition.Profile)
	mustProgress(t, err)
	admission, err := f.db.AdmitWorkflowLaunch(t.Context(), store.WorkflowLaunch{SessionID: parent.ID, TriggerID: 100, Mode: "implement", DefinitionID: pin.ID, Start: "ready", ContentDigest: definition.Digest, TopologyDigest: topology, Files: files, Provenance: store.WorkflowProvenance(definition.Origin)})
	mustProgress(t, err)
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: admission.Instance.ID, LaunchID: admission.LaunchID, Node: "ready", StartedAt: implementClock})
	mustProgress(t, err)
	activity := ActivityRequest{Mode: agent.ModeImplement, Node: "ready", InstanceID: admission.Instance.ID, NodeAttemptID: attempt.ID}
	return f, repo, parent, admission, activity
}

func standardsWorkerScheduledRun(t *testing.T, db *store.DB, session store.Session, trigger int64) store.Run {
	t.Helper()
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, 1, "standards-worker", store.AgentClaude)
	mustProgress(t, err)
	run, err := db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: trigger, Prelaunch: true, LogPath: func(n int) string { return fmt.Sprintf("standards-worker-%d.log", n) }})
	mustProgress(t, err)
	return run
}

func TestStandardsCoordinatorHandoffRetainsBookAndSeparatesWorkers(t *testing.T) {
	f, repo, parent, coordinator, activity := standardsCoordinatorFixture(t)
	ctx := t.Context()
	first := publishRunStandards(t, f.db, "worker-first-book", 0, []standards.Operation{
		{Kind: "category.add", Category: &standards.Category{ID: "go", Name: "Go"}},
		{Kind: "category.add", Category: &standards.Category{ID: "quality", Name: "Quality"}},
		{Kind: "standard.add", Standard: &standards.Standard{ID: "go-rule", Title: "Go rule", Body: "Original Go guidance", CategoryIDs: []string{"go"}}},
		{Kind: "standard.add", Standard: &standards.Standard{ID: "quality-rule", Title: "Quality rule", Body: "Original quality guidance", CategoryIDs: []string{"quality"}}},
	})
	captureImplementStandards(ctx, Options{DB: f.db}, Request{SessionID: parent.ID, Mode: agent.ModeImplement, Trigger: Trigger{CommentID: 100}}, coordinator.LaunchID, true)
	root, err := f.db.GetStandardsGraphInvocation(ctx, coordinator.LaunchID)
	mustProgress(t, err)
	if root >= 0 {
		t.Fatalf("coordinator without an agent run has nonnegative root %d", root)
	}
	result, err := (ImplementReadyActivity{f.bindings}).Execute(ctx, Options{DB: f.db}, activity)
	mustProgress(t, err)
	if result.Outcome != "started" {
		t.Fatalf("ready did not start workers: %+v", result)
	}
	newer := publishRunStandards(t, f.db, "worker-newer-book", first.ID, []standards.Operation{{Kind: "standard.replace", Standard: &standards.Standard{ID: "go-rule", Title: "New Go rule", Body: "New guidance must wait for another owner request", CategoryIDs: []string{"go"}}}})
	consumers := []string{}
	var firstWorker store.Session
	var firstSelection *store.StandardsRunSelection
	for i, key := range []string{"story-001", "story-002"} {
		row, err := f.db.GetImplementStory(ctx, implementParent.String(), 1, key)
		mustProgress(t, err)
		inherited, err := f.db.GetStandardsStoryInvocation(ctx, row.Parent, row.Generation, row.Key, row.StateAt)
		mustProgress(t, err)
		if inherited != root {
			t.Fatalf("story %s inherited %d, want %d", key, inherited, root)
		}
		worker, err := f.db.GetOrCreateSession(ctx, repo, 11+i)
		mustProgress(t, err)
		mustProgress(t, f.db.SetImplementWorkerSession(ctx, row.Parent, row.Generation, row.Key, worker.ID, implementClock))
		schedule, _, err := f.db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{SessionID: worker.ID, SourceCommentID: int64(1000 + i), OwnerHandle: "owner", Payload: "Continue the Implement work of this issue.", DueAt: implementClock})
		mustProgress(t, err)
		launch := standardsWorkerGraphLaunch(t, f.db, worker, schedule.TriggerID())
		captureImplementStandards(ctx, Options{DB: f.db}, Request{SessionID: worker.ID, Mode: agent.ModeImplement, Trigger: Trigger{CommentID: schedule.TriggerID()}, StandardsHandoff: true, StandardsRootRunID: inherited}, launch.LaunchID, true)
		run := standardsWorkerScheduledRun(t, f.db, worker, schedule.TriggerID())
		boundRoot, consumer, _ := prepareStandards(ctx, f.db, &MCPRuntime{}, run, "worker_agent", "implement")
		if boundRoot != fmt.Sprint(root) {
			t.Fatalf("worker %s bound %s, want coordinator %d", key, boundRoot, root)
		}
		runtime := &standards.Runtime{Store: f.db, RootRunID: boundRoot, ConsumerKey: consumer, AttemptID: "worker-" + key}
		category := "go"
		if i == 1 {
			category = "quality"
		}
		selected, err := runtime.Select(ctx, 0, []string{category}, "Relevant to this worker")
		mustProgress(t, err)
		if selected.Binding.RevisionID != first.ID || !reflect.DeepEqual(selected.Selection.CategoryIDs, []string{category}) {
			t.Fatalf("worker book or choice changed: %+v", selected)
		}
		consumers = append(consumers, consumer)
		if i == 0 {
			firstWorker = worker
			firstSelection = selected.Selection
		}
	}
	if consumers[0] == consumers[1] {
		t.Fatal("sibling workers collided on one consumer key")
	}
	// Another private schedule is an automatic relaunch of this same story work.
	// Its factory handoff must preserve the book and the consumer's selection.
	retry, _, err := f.db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{SessionID: firstWorker.ID, SourceCommentID: 2000, OwnerHandle: "owner", Payload: "Continue the Implement work of this issue.", DueAt: implementClock.Add(time.Minute)})
	mustProgress(t, err)
	retryLaunch := standardsWorkerGraphLaunch(t, f.db, firstWorker, retry.TriggerID())
	captureImplementStandards(ctx, Options{DB: f.db}, Request{SessionID: firstWorker.ID, Mode: agent.ModeImplement, Trigger: Trigger{CommentID: retry.TriggerID()}, StandardsHandoff: true, StandardsRootRunID: root}, retryLaunch.LaunchID, true)
	retryRun := standardsWorkerScheduledRun(t, f.db, firstWorker, retry.TriggerID())
	retryRoot, retryConsumer, _ := prepareStandards(ctx, f.db, &MCPRuntime{}, retryRun, "worker_agent", "implement")
	resumed := &standards.Runtime{Store: f.db, RootRunID: retryRoot, ConsumerKey: retryConsumer}
	restored, err := resumed.Status(ctx)
	mustProgress(t, err)
	if retryRoot != fmt.Sprint(root) || retryConsumer != consumers[0] || !reflect.DeepEqual(restored.Selection, firstSelection) || restored.Binding.RevisionID != first.ID {
		t.Fatalf("scheduled retry replaced retained worker context: %+v", restored)
	}
	// A direct owner request on the same worker session has no handoff authority.
	// It must bind current rather than inherit the earlier coordinator context.
	direct := standardsWorkerScheduledRun(t, f.db, firstWorker, 3000)
	directRoot, _, _ := prepareStandards(ctx, f.db, &MCPRuntime{}, direct, "worker_agent", "implement")
	binding, err := f.db.GetStandardsRunBinding(ctx, directRoot)
	mustProgress(t, err)
	if directRoot == fmt.Sprint(root) || binding.RevisionID != newer.ID {
		t.Fatalf("new owner request did not bind current: %+v", binding)
	}
	// If this owner launch fails, its private infrastructure retry belongs to
	// the new invocation, rather than returning to the story-start handoff.
	directLaunch := standardsWorkerGraphLaunch(t, f.db, firstWorker, 3000)
	captureImplementStandards(ctx, Options{DB: f.db}, Request{SessionID: firstWorker.ID, Mode: agent.ModeImplement, Trigger: Trigger{CommentID: 3000}}, directLaunch.LaunchID, true)
	directRuntime := &standards.Runtime{Store: f.db, RootRunID: directRoot, ConsumerKey: consumers[0], AttemptID: "new-owner-attempt"}
	directSelection, err := directRuntime.Select(ctx, 0, []string{"go"}, "Guidance for the new owner request")
	mustProgress(t, err)
	mustProgress(t, f.db.SetGraphInstanceStatus(ctx, directLaunch.Instance.ID, store.GraphFailed, time.Time{}, implementClock.Add(2*time.Minute)))
	failedRoot, err := f.db.GetStandardsGraphInstanceInvocation(ctx, directLaunch.Instance.ID)
	mustProgress(t, err)
	if fmt.Sprint(failedRoot) != directRoot || failedRoot == root {
		t.Fatalf("failed owner instance selected original story root: got %d, owner %s, story %d", failedRoot, directRoot, root)
	}
	publishRunStandards(t, f.db, "worker-after-failure-book", newer.ID, []standards.Operation{{Kind: "standard.replace", Standard: &standards.Standard{ID: "go-rule", Title: "Even newer Go rule", Body: "This publication must not enter an infrastructure retry", CategoryIDs: []string{"go"}}}})
	privateRetry, _, err := f.db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{SessionID: firstWorker.ID, SourceCommentID: 5000, OwnerHandle: "owner", Payload: "Continue the Implement work of this issue.", DueAt: implementClock.Add(3 * time.Minute)})
	mustProgress(t, err)
	privateLaunch := standardsWorkerGraphLaunch(t, f.db, firstWorker, privateRetry.TriggerID())
	captureImplementStandards(ctx, Options{DB: f.db}, Request{SessionID: firstWorker.ID, Mode: agent.ModeImplement, Trigger: Trigger{CommentID: privateRetry.TriggerID()}, StandardsHandoff: true, StandardsRootRunID: failedRoot}, privateLaunch.LaunchID, true)
	privateRun := standardsWorkerScheduledRun(t, f.db, firstWorker, privateRetry.TriggerID())
	privateRoot, privateConsumer, _ := prepareStandards(ctx, f.db, &MCPRuntime{}, privateRun, "worker_agent", "implement")
	privateRuntime := &standards.Runtime{Store: f.db, RootRunID: privateRoot, ConsumerKey: privateConsumer}
	privateStatus, err := privateRuntime.Status(ctx)
	mustProgress(t, err)
	if privateRoot != directRoot || privateConsumer != consumers[0] || privateStatus.Binding.RevisionID != newer.ID || !reflect.DeepEqual(privateStatus.Selection, directSelection.Selection) {
		t.Fatalf("private retry replaced newer owner invocation with story-start or current guidance: %+v", privateStatus)
	}
}

func standardsWorkerGraphLaunch(t *testing.T, db *store.DB, session store.Session, trigger int64) store.WorkflowAdmission {
	t.Helper()
	_, err := db.AdmitWorkflowPath(t.Context(), session.ID, "graph")
	mustProgress(t, err)
	definition := mustCaptureImplement(t)
	pin, err := db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: definition.Profile.Team, Mode: "implement", ProfileVersion: definition.Profile.ProfileVersion, Path: workflowGraphPath(definition), Digest: definition.Profile.Digest, TeamSnapshotDigest: definition.Digest, CreatedAt: implementClock})
	mustProgress(t, err)
	files, err := json.Marshal(definition.Files)
	mustProgress(t, err)
	topology, err := graphexec.SemanticDigest(definition.Profile)
	mustProgress(t, err)
	launch, err := db.AdmitWorkflowLaunch(t.Context(), store.WorkflowLaunch{SessionID: session.ID, TriggerID: trigger, Mode: "implement", DefinitionID: pin.ID, Start: "worker_agent", ContentDigest: definition.Digest, TopologyDigest: topology, Files: files, Provenance: store.WorkflowProvenance(definition.Origin)})
	mustProgress(t, err)
	return launch
}

func TestStandardsMissingFactoryHandoffAndHistoricalLaunchStayUnavailable(t *testing.T) {
	for _, test := range []struct {
		name               string
		handoff, newLaunch bool
	}{
		{name: "factory worker missing handoff", handoff: true, newLaunch: true},
		{name: "historical coordinator missing binding", handoff: false, newLaunch: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, repo, parent, admission, _ := standardsCoordinatorFixture(t)
			ctx := t.Context()
			published := publishRunStandards(t, f.db, "late-published-book", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "go", Name: "Go"}}})
			session := parent
			trigger := int64(100)
			if test.handoff {
				var err error
				session, err = f.db.GetOrCreateSession(ctx, repo, 11)
				mustProgress(t, err)
				trigger = 4000
				admission = standardsWorkerGraphLaunch(t, f.db, session, trigger)
			}
			// A known factory handoff with no retained source is a failure state, even
			// while a valid newer book is available. Historical admission is likewise
			// not permission to bind current after work may already have started.
			captureImplementStandards(ctx, Options{DB: f.db}, Request{SessionID: session.ID, Mode: agent.ModeImplement, Trigger: Trigger{CommentID: trigger}, StandardsHandoff: test.handoff}, admission.LaunchID, test.newLaunch)
			root, err := f.db.GetStandardsGraphInvocation(ctx, admission.LaunchID)
			mustProgress(t, err)
			runtime := &standards.Runtime{Store: f.db, RootRunID: fmt.Sprint(root), ConsumerKey: standardsSessionConsumerKey(session.ID, "implement", "worker_agent")}
			status, err := runtime.Status(ctx)
			mustProgress(t, err)
			if status.Integrity != "unavailable" || status.Binding.RevisionID == published.ID || status.Binding.RevisionID != 0 || status.Reason == "" {
				t.Fatalf("missing source silently captured current: %+v", status)
			}
			categories, err := runtime.Categories(ctx, 0, "")
			mustProgress(t, err)
			if len(categories.Categories) != 0 || categories.Status.Integrity != "unavailable" {
				t.Fatalf("missing source exposed current categories: %+v", categories)
			}
		})
	}
}
