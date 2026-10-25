package messaging

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestRelativeBound(t *testing.T) {
	zone := time.FixedZone("CEST", 2*60*60)
	now := time.Date(2026, 10, 14, 15, 30, 0, 0, zone) // Wednesday
	day := func(month time.Month, d int) string {
		return time.Date(2026, month, d, 0, 0, 0, 0, zone).Format(time.RFC3339)
	}
	cases := []struct {
		phrase, from, to string
		ok               bool
	}{
		{"today", day(10, 14), "", true},
		{"yesterday", day(10, 13), day(10, 14), true},
		{"since yesterday", day(10, 13), "", true},
		{"Since Yesterday", day(10, 13), "", true},
		{"this week", day(10, 12), "", true},
		{"last week", day(10, 5), day(10, 12), true},
		{"since last week", day(10, 5), "", true},
		{"this month", day(10, 1), "", true},
		{"last month", day(9, 1), day(10, 1), true},
		{"since monday", day(10, 12), "", true},
		{"since wednesday", day(10, 14), "", true},
		{"since thursday", day(10, 8), "", true},
		{"sunday", day(10, 11), "", true},
		{"three days ago", "", "", false},
		{"2026-10-01", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		from, to, ok := relativeBound(tc.phrase, now)
		if from != tc.from || to != tc.to || ok != tc.ok {
			t.Errorf("%q: got %q %q %t want %q %q %t", tc.phrase, from, to, ok, tc.from, tc.to, tc.ok)
		}
	}
}

func TestStatusListsRunningAndOwnerDependentTasks(t *testing.T) {
	db := naturalExecutionDB(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory", BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, "discord", "7", []string{"3"}, nil, FactoryStatus{DB: db})
	text, err := s.summary(ctx, Message{}, at)
	if err != nil || !strings.Contains(text, "Running now: 0.\n") || !strings.Contains(text, "Queued: 0.\n") || !strings.Contains(text, "Waiting on you:\n- none.") {
		t.Fatalf("empty: %s %v", text, err)
	}
	for _, n := range []int{649, 650, 651, 652} {
		session, err := db.GetOrCreateSession(ctx, repo, n)
		if err != nil {
			t.Fatal(err)
		}
		switch n {
		case 649:
			seg, err := db.GetOrCreateSegment(ctx, session.ID, 1, "session", "codex")
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: seg.ID, Mode: "implement", Agent: "codex", TriggerCommentID: 1, StartedAt: at, LogPath: func(int) string { return "/tmp/test-run" }})
			if err != nil {
				t.Fatal(err)
			}
		case 650, 651:
			kind := "review"
			if n == 651 {
				kind = "question"
			}
			def, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "chat", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
			if err != nil {
				t.Fatal(err)
			}
			instance, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: "build", StartedAt: at})
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "build", StartedAt: at})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.FinishGraphNodeAttempt(ctx, attempt.ID, store.GraphSucceeded, kind, at); err != nil {
				t.Fatal(err)
			}
			key := "wait-" + kind
			if err := db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: key, InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "build", Kind: kind, Candidate: []byte(`{"questions":[{"text":"Choose release?"}]}`)}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: key, SessionID: session.ID, Task: session.TaskRef, Destination: "acme/litefactory#" + strconv.Itoa(n), TriggerNamespace: "issue-comment", Purpose: "implement-" + kind, BotUserID: 99, Body: kind, Marker: "marker-" + key}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.MarkReportDeliverySent(ctx, key); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ConfirmReportDelivery(ctx, key, int64(n)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.CompleteReportDelivery(ctx, key); err != nil {
				t.Fatal(err)
			}
		case 652:
			parent := "github:acme/litefactory#100"
			if err := db.EnsureImplementStories(ctx, parent, 1, []store.ImplementStorySeed{{Key: "a", Position: 1, Branch: "lf/a"}}, at); err != nil {
				t.Fatal(err)
			}
			row, err := db.TransitionImplementStory(ctx, store.ImplementTransition{Parent: parent, Generation: 1, Key: "a", From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: 1, At: at})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.SetImplementWorkerSession(ctx, parent, 1, "a", session.ID, at); err != nil {
				t.Fatal(err)
			}
			if _, err := db.TransitionImplementStory(ctx, store.ImplementTransition{Parent: parent, Generation: 1, Key: "a", From: store.ImplementRunning, To: store.ImplementParked, ExpectedRevision: row.Revision, Reason: "Choose deployment target", At: at}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Relevant owner waits must survive the old newest-100 activity cutoff.
	for n := 1000; n < 1105; n++ {
		if _, err := db.GetOrCreateSession(ctx, repo, n); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []int{650, 651, 652} {
		text, err := s.summary(ctx, Message{Arguments: map[string]string{"project": "acme/litefactory", "issue": strconv.Itoa(n)}}, at)
		if err != nil || !strings.Contains(text, "#"+strconv.Itoa(n)+":") || !strings.Contains(text, "Waiting on: owner") {
			t.Fatalf("selected wait: %s %v", text, err)
		}
	}
	text, err = s.summary(ctx, Message{}, at)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Running now: 1.\n- github:acme/litefactory#649 · implement · since 12:00", "#650: idle", "Waiting on: owner — implement review", "#651: idle", "Waiting on: owner — implement question", "#652: idle", "Waiting on: owner — parked since 12:00 (story a): Choose deployment target. To resume: reply on github:acme/litefactory#100 to restart the stopped stories."} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q: %s", want, text)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if text := s.taskSummary(cancelled, Status{}, at); !strings.Contains(text, "could not be read") || strings.Contains(text, "- none.") {
		t.Fatalf("failed read: %s", text)
	}
}

func TestStatusSelectedTaskBeyondRecentWindow(t *testing.T) {
	db := naturalExecutionDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, repo, 614)
	if err != nil {
		t.Fatal(err)
	}
	for n := 1000; n < 1105; n++ {
		if _, err := db.GetOrCreateSession(ctx, repo, n); err != nil {
			t.Fatal(err)
		}
	}
	s := New(db, "discord", "7", nil, nil, FactoryStatus{DB: db})
	text, err := s.summary(ctx, Message{Arguments: map[string]string{"task": "614"}}, at)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"acme/litefactory#614: idle", "Mode: unavailable", "Stage: unavailable", "Waiting on: none recorded", "Recent runs on this task (newest first, limit 3): unavailable"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "status", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "bug", Agent: "codex", TriggerCommentID: 1, StartedAt: at, LogPath: func(int) string { return "/tmp/status-fixture" }}); err != nil {
		t.Fatal(err)
	}
	text, err = s.summary(ctx, Message{Arguments: map[string]string{"project": "acme/litefactory", "issue": "614"}}, at)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#614: running", "Mode: bug", "1: running (bug)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	text = s.taskSummary(ctx, Status{}, at)
	if !strings.Contains(text, "acme/litefactory#614 · bug · since") || !strings.Contains(text, "Scope:") || strings.Contains(text, "left out") {
		t.Fatalf("old task omitted: %s", text)
	}
}

func TestStatusDisclosesOutputOmissions(t *testing.T) {
	db := naturalExecutionDB(t)
	ctx := t.Context()
	at := time.Now().UTC()
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory"})
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 330; n++ {
		session, err := db.GetOrCreateSession(ctx, repo, n)
		if err != nil {
			t.Fatal(err)
		}
		segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "status", "codex")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: "codex", TriggerCommentID: 1, StartedAt: at, LogPath: func(int) string { return "/tmp/status-fixture" }}); err != nil {
			t.Fatal(err)
		}
	}
	s := New(db, "discord", "7", nil, nil, FactoryStatus{DB: db})
	text := s.taskSummary(ctx, Status{}, at)
	if !strings.Contains(text, "additional entries left out") || !strings.Contains(text, "Scope:") {
		t.Fatalf("undisclosed truncation: %s", text)
	}
	selected, err := s.summary(ctx, Message{Arguments: map[string]string{"task": "330"}}, at)
	if err != nil || !strings.Contains(selected, "#330: running") {
		t.Fatalf("selected lookup truncated: %s %v", selected, err)
	}
}

// A feature in Implement reports its real progress from the local ledger, not
// the stale failed flag of an earlier attempt or its plan stages.
func TestStatusReportsImplementProgress(t *testing.T) {
	db := naturalExecutionDB(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 21, 16, 0, 0, 0, time.UTC)
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory", BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := db.GetOrCreateSession(ctx, repo, 728)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSessionState(ctx, parent.ID, store.SessionFailed); err != nil {
		t.Fatal(err)
	}
	def, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "chat", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: parent.ID, DefinitionID: def.ID, CurrentNode: "await", StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ParkGraphInstance(ctx, instance.ID, "await", at.Add(time.Hour), at); err != nil {
		t.Fatal(err)
	}
	ref := parent.TaskRef
	seeds := []store.ImplementStorySeed{{Key: "story-001", Position: 0, Branch: "lf/1"}, {Key: "story-002", Position: 1, Branch: "lf/2"}, {Key: "story-003", Position: 2, Branch: "lf/3"}}
	if err := db.EnsureImplementStories(ctx, ref, 1, seeds, at); err != nil {
		t.Fatal(err)
	}
	for i, key := range []string{"story-001", "story-002"} {
		if _, err := db.TransitionImplementStory(ctx, store.ImplementTransition{Parent: ref, Generation: 1, Key: key, From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: 1, At: at}); err != nil {
			t.Fatal(err)
		}
		worker, err := db.GetOrCreateSession(ctx, repo, 990+i)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SetImplementWorkerSession(ctx, ref, 1, key, worker.ID, at); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			seg, err := db.GetOrCreateSegment(ctx, worker.ID, 1, "session", "codex")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: worker.ID, SegmentID: seg.ID, Mode: "implement", Agent: "codex", TriggerCommentID: 1, StartedAt: at, LogPath: func(int) string { return "/tmp/story-run" }}); err != nil {
				t.Fatal(err)
			}
		} else if _, err := db.RecordImplementFailure(ctx, ref, 1, key, "agent invocation exceeded its wall clock", at); err != nil {
			t.Fatal(err)
		}
	}
	s := New(db, "discord", "7", []string{"3"}, nil, FactoryStatus{DB: db})
	text, err := s.summary(ctx, Message{Arguments: map[string]string{"project": "acme/litefactory", "issue": "728"}}, at)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#728: in progress", "Mode: implement", "await (running), next step due 2026-10-21T17:00:00Z", "2 running, 1 waiting", "story-001 (#990) running", "story-002 (#991) running", "1 failed attempts, last failure: agent invocation exceeded its wall clock", "story-003 waiting", "Active story runs: #990 run 1 (implement) running since 2026-10-21T16:00:00Z", "Waiting on: none recorded"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "#728: failed") {
		t.Errorf("stale failed state: %s", text)
	}
}

// The status lookup lists every task or chat job that works now, so the count
// matches the lines; a slot that cannot be resolved to a task is counted, and
// work owed a slot or a retry is queued with its reason.
func TestStatusListsRunningQueuedAndUnidentified(t *testing.T) {
	db := naturalExecutionDB(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 24, 11, 30, 0, 0, time.UTC)
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory", BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	session := func(n int) store.Session {
		s, err := db.GetOrCreateSession(ctx, repo, n)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	instance := func(s store.Session, mode, node string) store.GraphInstance {
		def, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "chat", Mode: mode, ProfileVersion: 1, Path: mode + ".dot", Digest: strings.Repeat("b", 64), CreatedAt: at})
		if err != nil {
			t.Fatal(err)
		}
		i, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: s.ID, DefinitionID: def.ID, CurrentNode: node, StartedAt: at})
		if err != nil {
			t.Fatal(err)
		}
		return i
	}
	// A Validate suite step runs with no run row.
	validate := session(1004)
	if _, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance(validate, "validate", "suite").ID, Node: "suite", StartedAt: at.Add(-31 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	// An ordinary run.
	bug := session(1005)
	seg, err := db.GetOrCreateSegment(ctx, bug.ID, 1, "session", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: bug.ID, SegmentID: seg.ID, Mode: "bug", Agent: "codex", TriggerCommentID: 1, StartedAt: at.Add(-5 * time.Minute), LogPath: func(int) string { return "/tmp/status-fixture" }}); err != nil {
		t.Fatal(err)
	}
	// A standards author draft in progress.
	if _, err := db.SaveStandardsAuthoring(ctx, store.StandardsAuthoringSession{ID: "author-1", Mode: "create", TargetID: "go-errors", Status: "draft", Data: []byte(`{}`)}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.BeginStandardsAuthoringAttempt(ctx, store.StandardsAuthoringAttempt{ID: "attempt-1", SessionID: "author-1", InputVersion: 1, Token: "token-1", LeaseUntil: time.Now().Add(10 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	// A task owed a look while every slot is busy.
	if err := db.SetSessionNeedsPoll(ctx, session(1049).ID); err != nil {
		t.Fatal(err)
	}
	// A graph step whose start failed and waits for its retry.
	retry := instance(session(957), "implement", "build")
	dispatch, err := db.ParkGraphInstance(ctx, retry.ID, "build", at.Add(-time.Minute), at.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.ReserveGraphDispatch(ctx, dispatch.ID, at); err != nil || !ok {
		t.Fatalf("reserve: %t %v", ok, err)
	}
	if _, err := db.RequeueGraphDispatch(ctx, dispatch.ID, time.Date(2026, 10, 24, 15, 20, 0, 0, time.UTC), "no slot"); err != nil {
		t.Fatal(err)
	}
	// A step parked for later that never failed is scheduled, not queued.
	if _, err := db.ParkGraphInstance(ctx, instance(session(958), "implement", "await").ID, "await", at.Add(time.Hour), at); err != nil {
		t.Fatal(err)
	}

	s := New(db, "discord", "7", nil, nil, FactoryStatus{DB: db})
	health := store.ListenerStatus{DaemonStartedAt: at.Add(-time.Hour), WrittenAt: at, LastHeartbeatStartedAt: at, RunsInFlight: 3, InFlightSessions: []int64{validate.ID, bug.ID, 99999}, SlotsUsed: 4, SlotsTotal: 4}
	if err := db.ReplaceHealthSnapshot(ctx, health, nil); err != nil {
		t.Fatal(err)
	}
	text, err := s.summary(ctx, Message{}, at)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Running now: 3 (+1 unidentified).\n",
		"- github:acme/litefactory#1004 · validate · step suite · since 10:59\n",
		"- github:acme/litefactory#1005 · bug · since 11:25\n",
		"- standards draft · create go-errors · since ",
		"Queued: 2.\n",
		"- github:acme/litefactory#1049 · waiting for a free slot (4/4 busy)",
		"- github:acme/litefactory#957 · step build · retry at 15:20",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "#958") {
		t.Errorf("scheduled step listed as queued: %s", text)
	}

	// A slot that resolves to no task is only counted.
	other := naturalExecutionDB(t)
	if err := other.ReplaceHealthSnapshot(ctx, store.ListenerStatus{DaemonStartedAt: at.Add(-time.Hour), WrittenAt: at, LastHeartbeatStartedAt: at, RunsInFlight: 1, InFlightSessions: []int64{7}, SlotsUsed: 1, SlotsTotal: 4}, nil); err != nil {
		t.Fatal(err)
	}
	text, err = New(other, "discord", "7", nil, nil, FactoryStatus{DB: other}).summary(ctx, Message{}, at)
	if err != nil || !strings.Contains(text, "Running now: 0 (+1 unidentified).\n") || strings.Contains(text, "housekeeping") {
		t.Fatalf("unidentified: %s %v", text, err)
	}
}

// C8 (#1138): an in-flight slot with no run or graph step is named by its task
// and the dispatch source that started its worker, never as housekeeping, and
// the task is not also queued for a slot.
func TestStatusNamesInFlightSlotsBySource(t *testing.T) {
	db := naturalExecutionDB(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 24, 15, 25, 40, 0, time.UTC)
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "litefactory", BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := db.GetOrCreateSession(ctx, repo, 1050)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSessionNeedsPoll(ctx, waiting.ID); err != nil {
		t.Fatal(err)
	}
	scheduled, err := db.GetOrCreateSession(ctx, repo, 1051)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := db.GetOrCreateSession(ctx, repo, 1052)
	if err != nil {
		t.Fatal(err)
	}
	health := store.ListenerStatus{DaemonStartedAt: at.Add(-time.Hour), WrittenAt: at, LastHeartbeatStartedAt: at, RunsInFlight: 4,
		InFlightSessions: []int64{waiting.ID, scheduled.ID, plain.ID, 99999},
		InFlightLabels:   map[int64]string{waiting.ID: "backlog+graph_owner_wait", scheduled.ID: "due_schedule"}, SlotsUsed: 4, SlotsTotal: 4}
	if err := db.ReplaceHealthSnapshot(ctx, health, nil); err != nil {
		t.Fatal(err)
	}
	text, err := New(db, "discord", "7", nil, nil, FactoryStatus{DB: db}).summary(ctx, Message{}, at)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Running now: 3 (+1 unidentified).\n",
		"- github:acme/litefactory#1050 · checking for input (backlog, graph_owner_wait)\n",
		"- github:acme/litefactory#1051 · due_schedule\n",
		"- github:acme/litefactory#1052 · checking for input\n",
		"Queued: 0.\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "housekeeping") {
		t.Errorf("status says housekeeping: %s", text)
	}
}
