package daemon_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func recoveryDaemon(t *testing.T, f *fixture, enabled bool) *daemon.Daemon {
	t.Helper()
	d, err := daemon.New(daemon.Options{AutoResumeOnSessionLimit: enabled, DefaultAgent: agent.Kind(store.AgentClaude), ToolProfiles: config.DefaultToolProfiles().Permissions, Agents: turn.RunnerSet{agent.Kind(store.AgentClaude): nil}, Classifiers: map[agent.Kind]agent.Classifier{agent.Kind(store.AgentClaude): fakeModeClassifier{}}, DB: f.db, Forge: f.client, Filter: f.filter, Turn: f.turns, Reporter: f.reporter, InstanceID: "recovery-test", Interval: testInterval, Logger: f.logger, Now: f.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func seedAutomaticAdmission(t *testing.T, f *fixture, delay time.Duration) store.Continuation {
	t.Helper()
	f.plantCursor()
	f.issue("recover")
	f.settle()
	s := f.session()
	if err := f.db.SetSessionWorktree(t.Context(), s.ID, "/saved/worktree"); err != nil {
		t.Fatal(err)
	}
	s = f.session()
	run := f.allocateRunningRun(s, 42)
	segment, err := f.db.GetSegment(t.Context(), run.SegmentID)
	if err != nil {
		t.Fatal(err)
	}
	at := f.clock.Now()
	zero := int64(0)
	_, c, err := f.db.FinishInterruptedRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunFailed, SemanticOutcome: "interrupted", ExitCode: &zero, FinishedAt: at}, &store.Continuation{DueAt: at.Add(delay), ResetBound: at.Add(delay - time.Minute), Precision: "exact", Context: store.ContinuationContext{Task: s.TaskRef, Worktree: s.WorktreePath, Agent: store.AgentClaude, Mode: store.ModeSpike, Request: "original request", ResumeIdentity: segment.SessionRef, SessionRef: segment.SessionRef, SegmentOrdinal: segment.Ordinal, OwnerCommentID: 42}}, "")
	if err != nil {
		t.Fatal(err)
	}
	f.daemon = recoveryDaemon(t, f, true)
	return *c
}

func TestAutomaticAdmissionCancelsFutureIntent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*fixture)
	}{
		{"disabled", func(f *fixture) { f.daemon = recoveryDaemon(t, f, false) }},
		{"closed", func(f *fixture) { f.forge.closeIssue(testIssue) }},
		{"agent changed", func(f *fixture) { f.issueWithLabels("recover", "Coding Agent: Codex") }},
		{"mode changed", func(f *fixture) { f.issueWithLabels("recover", "Mode: Plan") }},
		{"new owner instruction", func(f *fixture) { f.forge.addComment(testIssue, 50, owner(), "/spike newer request") }},
		{"conversation reset", func(f *fixture) {
			if _, err := f.db.GetOrCreateSegment(t.Context(), f.session().ID, 2, "replacement", store.AgentClaude); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			c := seedAutomaticAdmission(t, f, time.Hour)
			tc.change(f)
			f.settle()
			got, err := f.db.GetContinuation(t.Context(), c.ID)
			if err != nil || got.Status != "cancelled" || got.CancellationReason == "" {
				t.Fatalf("stale action=%+v err=%v", got, err)
			}
			f.daemon = recoveryDaemon(t, f, true)
			f.clock.advance(2 * time.Hour)
			f.settle()
			got, err = f.db.GetContinuation(t.Context(), c.ID)
			if err != nil || got.Status != "cancelled" {
				t.Fatalf("cancelled action resurrected=%+v err=%v", got, err)
			}
			for _, call := range f.turns.calls() {
				if call.Continuation != nil {
					t.Fatalf("stale action launched: %+v", call)
				}
			}
		})
	}
}

func TestAutomaticAdmissionDeadlineAndSavedContext(t *testing.T) {
	f := newFixture(t)
	c := seedAutomaticAdmission(t, f, time.Hour)
	f.settle()
	got, err := f.db.GetContinuation(t.Context(), c.ID)
	if err != nil || got.Status != "pending" || len(f.turns.calls()) != 0 {
		t.Fatalf("early admission=%+v err=%v", got, err)
	}
	f.clock.advance(2 * time.Hour)
	f.settle()
	calls := f.turns.calls()
	if len(calls) != 1 {
		t.Fatalf("overdue calls=%d logs=%s", len(calls), f.logs)
	}
	req := calls[0]
	if req.Continuation == nil || req.Continuation.ID != c.ID || req.Trigger.Body != c.Context.Request || req.Mode != agent.ModeSpike || req.Agent != agent.Kind(store.AgentClaude) || req.SessionID != c.SessionID {
		t.Fatalf("saved context lost: %+v", req)
	}
	if f.watermark() != 42 {
		t.Fatalf("automatic admission consumed watermark: %d", f.watermark())
	}
}

func TestAutomaticAdmissionObservationFailureDefers(t *testing.T) {
	f := newFixture(t)
	c := seedAutomaticAdmission(t, f, time.Hour)
	f.clock.advance(2 * time.Hour)
	f.forge.onListComments = func(w http.ResponseWriter, _ int) bool {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "unavailable"})
		return true
	}
	_, _ = f.tickErr()
	if err := f.daemon.Quiesce(t.Context()); err != nil {
		t.Fatal(err)
	}
	got, err := f.db.GetContinuation(t.Context(), c.ID)
	if err != nil || got.Status != "pending" || len(f.turns.calls()) != 0 || f.watermark() != 42 {
		t.Fatalf("failed observation consumed recovery=%+v err=%v calls=%d", got, err, len(f.turns.calls()))
	}
	f.forge.onListComments = nil
	f.settle()
	if len(f.turns.calls()) != 1 {
		t.Fatalf("recovered observation calls=%d logs=%s", len(f.turns.calls()), f.logs)
	}
}

func TestAutomaticAdmissionScheduleControlDoesNotSupersede(t *testing.T) {
	f := newFixture(t)
	c := seedAutomaticAdmission(t, f, time.Hour)
	f.forge.addComment(testIssue, 50, owner(), "/schedule list")
	f.settle()
	got, err := f.db.GetContinuation(t.Context(), c.ID)
	if err != nil || got.Status != "pending" {
		t.Fatalf("schedule control cancelled unrelated recovery: %+v %v", got, err)
	}
}

func TestAutomaticAdmissionInflightExcludesDuplicateLaunch(t *testing.T) {
	f := newFixture(t)
	c := seedAutomaticAdmission(t, f, time.Hour)
	started := make(chan turn.Request, 1)
	release := make(chan struct{})
	f.turns.fn = func(ctx context.Context, req turn.Request) (turn.Result, error) {
		started <- req
		select {
		case <-release:
		case <-ctx.Done():
			return turn.Result{}, ctx.Err()
		}
		return f.ranSuccessfully(ctx, req)
	}
	f.clock.advance(2 * time.Hour)
	f.tick()
	select {
	case req := <-started:
		if req.Continuation == nil || req.Continuation.ID != c.ID {
			t.Fatalf("wrong admission: %+v", req)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("automatic action did not start")
	}
	f.tick()
	got, err := f.db.GetContinuation(t.Context(), c.ID)
	if err != nil || got.Status != "reserved" || len(f.turns.calls()) != 1 {
		t.Fatalf("inflight reservation duplicated: %+v %v calls=%d", got, err, len(f.turns.calls()))
	}
	close(release)
	if err := f.daemon.Quiesce(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.settle()
	if len(f.turns.calls()) != 1 {
		t.Fatalf("subsequent tick duplicated allocation: %d", len(f.turns.calls()))
	}
}

func TestAutomaticCancellationNoticeRetriesUntilConfirmed(t *testing.T) {
	f := newFixture(t)
	c := seedAutomaticAdmission(t, f, time.Hour)
	f.settle() // Confirm the original interruption announcement first.
	before := f.watermark()
	posts, _, _ := f.forge.snapshot()
	baseline := len(posts)
	if _, err := f.db.GetOrCreateSegment(t.Context(), f.session().ID, 2, "replacement", store.AgentClaude); err != nil {
		t.Fatal(err)
	}
	f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "lost notice"})
		return true
	}
	_, _ = f.tickErr()
	if err := f.daemon.Quiesce(t.Context()); err != nil {
		t.Fatal(err)
	}
	saved, err := f.db.GetContinuation(t.Context(), c.ID)
	if err != nil || saved.Status != "cancelled" {
		t.Fatalf("cancellation lost: %+v %v", saved, err)
	}
	pending, err := f.db.ListUnreportedContinuationCancellations(t.Context())
	if err != nil || len(pending) != 1 || pending[0].ID != c.ID {
		t.Fatalf("failed notice lost: %+v %v", pending, err)
	}
	f.forge.onPostComment = nil
	f.settle()
	pending, err = f.db.ListUnreportedContinuationCancellations(t.Context())
	if err != nil || len(pending) != 0 {
		t.Fatalf("confirmed notice remains pending: %+v %v", pending, err)
	}
	posts, _, _ = f.forge.snapshot()
	if len(posts) != baseline+1 || !strings.Contains(strings.ToLower(posts[len(posts)-1]), "manual") {
		t.Fatalf("missing manual-recovery notice: %v", posts)
	}
	f.settle()
	posts, _, _ = f.forge.snapshot()
	if len(posts) != baseline+1 || f.watermark() != before || len(f.turns.calls()) != 0 {
		t.Fatalf("notice duplicated work/progress: posts=%d watermark=%d calls=%d", len(posts), f.watermark(), len(f.turns.calls()))
	}
}
