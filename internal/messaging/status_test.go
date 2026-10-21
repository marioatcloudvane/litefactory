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
	if err != nil || !strings.Contains(text, "no running tasks or owner waits") {
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
	for _, want := range []string{"acme/litefactory#649: running", "#650: idle", "Waiting on: owner — implement review", "#651: idle", "Waiting on: owner — implement question", "#652: idle", "Waiting on: owner — parked", "Choose deployment target"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q: %s", want, text)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if text := s.taskSummary(cancelled); !strings.Contains(text, "could not be read") || strings.Contains(text, "no running tasks") {
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
	text = s.taskSummary(ctx)
	if !strings.Contains(text, "#614: running") || !strings.Contains(text, "Scope:") || strings.Contains(text, "left out") {
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
	for n := 1; n <= 105; n++ {
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
	text := s.taskSummary(ctx)
	if !strings.Contains(text, "additional entries left out") || !strings.Contains(text, "Scope:") {
		t.Fatalf("undisclosed truncation: %s", text)
	}
	selected, err := s.summary(ctx, Message{Arguments: map[string]string{"task": "105"}}, at)
	if err != nil || !strings.Contains(selected, "#105: running") {
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
