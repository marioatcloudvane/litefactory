package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// s2rKill is the panic value that stands in for the process dying.
type s2rKill struct{}

// s2rFixture is the real Bug runtime over a real store; only the agent is
// faked. The private start trigger is minted from a real start attempt row.
type s2rFixture struct {
	f       *fixture
	runtime *BugRuntime
	req     Request
	attempt store.BugFilingStartAttempt
	calls   *atomic.Int32
	kill    *atomic.Bool
	// during runs inside the analyst agent, while its run is live.
	during func(context.Context)
}

func newS2rFixture(t *testing.T) *s2rFixture {
	t.Helper()
	calls := &atomic.Int32{}
	s := &s2rFixture{calls: calls}
	runner := lifecycleMCPHost(func(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
		calls.Add(1)
		if s.during != nil {
			s.during(ctx)
		}
		mustProgress(t, req.ValidateBugAnalyst())
		raw, err := json.Marshal(map[string]any{"version": 1, "response": "blocked", "result": map[string]any{"outcome": "blocked", "reason": "Controlled evidence needed", "comment": "Diagnosis retained", "payload": map[string]any{"bug": map[string]any{"response": "blocked", "invocation": fmt.Sprintf("analysis:%d:%d", req.Graph.InstanceID, req.Graph.AttemptID), "base_revision": 0, "evidence": []string{"Controlled report"}}}}})
		mustProgress(t, err)
		submitLiveResult(t, req, string(raw))
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, FinishedAt: time.Now()}, nil
	})
	f, runtime, req := bugBootstrapFixture(t, runner)
	ctx := t.Context()
	filing, _, err := f.db.AdmitBugFiling(ctx, store.BugFiling{ExecutionID: "exec-s2r", OwnerActionID: "action-s2r", OwnerID: "owner-42", RepositoryID: f.session.RepoID, RepositoryRef: "acme/acme-api", Title: "Crash on save", Body: "Saving crashes.", Marker: "<!-- lf:bug-filing v=1 marker=0123456789abcdef0123456789abcdef -->"}, time.Now())
	mustProgress(t, err)
	attempt, _, err := f.db.OpenBugFilingStartAttempt(ctx, filing.ID, f.session.ID, time.Now())
	mustProgress(t, err)
	reserved, err := f.db.MoveBugFilingStartAttempt(ctx, attempt.ID, store.BugStartPending, store.BugStartReserved, "", 0, "", time.Now())
	mustProgress(t, err)
	if !reserved {
		t.Fatal("start attempt not reserved")
	}
	req.Trigger.CommentID = attempt.TriggerID()
	req.Trigger.Body = "Investigate the bootstrap failure"
	req.Command = ParseCommand(req.Trigger.Body)
	kill := &atomic.Bool{}
	now := f.opts.MCP.Now
	sessionID := f.session.ID
	db := f.db
	// The first clock read after the analyst run is marked running and before
	// its node attempt exists is where the process dies.
	f.opts.MCP.Now = func() time.Time {
		if kill.Load() {
			runs, err := db.ListRuns(context.Background(), sessionID)
			if err == nil {
				for _, run := range runs {
					if run.Status == store.RunRunning {
						kill.Store(false)
						panic(s2rKill{})
					}
				}
			}
		}
		return now()
	}
	runtime.opts.MCP = f.opts.MCP
	s.f, s.runtime, s.req, s.attempt, s.kill = f, runtime, req, attempt, kill
	return s
}

// kill hands off the private start trigger and kills the process after the
// conversation moved to the analyst and its run started, before the analyst
// attempt is saved. The conversation history stays.
func (s *s2rFixture) killStart(t *testing.T) {
	t.Helper()
	s.kill.Store(true)
	killed := func() (killed bool) {
		defer func() {
			if v := recover(); v != nil {
				if _, ok := v.(s2rKill); !ok {
					panic(v)
				}
				killed = true
			}
		}()
		_, err := Execute(t.Context(), s.f.opts, s.req)
		t.Logf("start returned without the kill: %v", err)
		return false
	}()
	if !killed {
		t.Fatal("the process was not killed before the analyst attempt")
	}
}

// restart is the next process: orphan cleanup, then the Bug Reconcile.
func (s *s2rFixture) restart(t *testing.T) store.GraphInstance {
	t.Helper()
	ctx := t.Context()
	if _, err := s.f.db.ReconcileOrphans(ctx, "restarted-daemon"); err != nil {
		t.Fatal(err)
	}
	mustProgress(t, s.runtime.Reconcile(ctx))
	instance, err := s.f.db.LatestStandaloneBugInstance(ctx, s.f.session.ID)
	mustProgress(t, err)
	return instance
}

// assertS2r checks the exact retained run-before-analyst state.
func (s *s2rFixture) assertS2r(t *testing.T, instance store.GraphInstance) {
	t.Helper()
	ctx := t.Context()
	if instance.Status != store.GraphRunning || instance.CurrentNode != "analyst" || !instance.NextDueAt.IsZero() {
		t.Fatalf("instance=%+v", instance)
	}
	runs, err := s.f.db.ListRuns(ctx, s.f.session.ID)
	mustProgress(t, err)
	if len(runs) == 0 || runs[0].Status != store.RunFailed || runs[0].TriggerCommentID != s.attempt.TriggerID() || !store.IsBugStartTrigger(runs[0].TriggerCommentID) {
		t.Fatalf("runs=%+v", runs)
	}
	launched, found, err := s.f.db.WorkflowLaunchInstance(ctx, s.f.session.ID, s.attempt.TriggerID())
	mustProgress(t, err)
	if !found || launched != instance.ID {
		t.Fatal("the private start trigger is not the instance's launch")
	}
	if _, err = s.f.db.GetGraphOwnerWait(ctx, instance.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("owner wait parked: %v", err)
	}
	if _, err = s.f.db.LatestGraphDispatch(ctx, instance.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("graph dispatch recorded: %v", err)
	}
	attempts, err := s.f.db.ListGraphNodeAttempts(ctx, instance.ID)
	mustProgress(t, err)
	if len(attempts) != 1 || attempts[0].Node != "conversation" || attempts[0].Status != store.GraphSucceeded || attempts[0].Outcome != "analyse" || attempts[0].RunID != 0 {
		t.Fatalf("attempts=%+v", attempts)
	}
	transitions, err := s.f.db.ListGraphTransitions(ctx, instance.ID)
	mustProgress(t, err)
	if len(transitions) != 1 || transitions[0].FromNode != "conversation" || transitions[0].ToNode != "analyst" || transitions[0].OnOutcome != "analyse" {
		t.Fatalf("transitions=%+v", transitions)
	}
	comments, err := s.f.db.ListGraphComments(ctx, instance.ID, "", 1)
	mustProgress(t, err)
	if len(comments) != 0 {
		t.Fatalf("graph comments=%+v", comments)
	}
	if s.calls.Load() != 0 {
		t.Fatalf("analyst invoked %d times", s.calls.Load())
	}
}

// ownerReply is a fresh real comment by the authenticated owner.
func (s *s2rFixture) ownerReply(id int64) Request {
	req := s.req
	req.Trigger.CommentID = id
	req.Trigger.AuthorID = 42
	req.Trigger.Body = "run it again"
	req.Command = ParseCommand(req.Trigger.Body)
	return req
}

// refused asserts that a request is answered as not applied and changes nothing.
func (s *s2rFixture) refused(t *testing.T, req Request) {
	t.Helper()
	ctx := t.Context()
	before, err := s.f.db.LatestStandaloneBugInstance(ctx, s.f.session.ID)
	mustProgress(t, err)
	attempts, err := s.f.db.ListGraphNodeAttempts(ctx, before.ID)
	mustProgress(t, err)
	_, err = Execute(ctx, s.f.opts, req)
	if !errors.Is(err, ErrBugReplyNotApplied) {
		t.Fatalf("trigger %d was not refused: %v", req.Trigger.CommentID, err)
	}
	after, err := s.f.db.LatestStandaloneBugInstance(ctx, s.f.session.ID)
	mustProgress(t, err)
	now, err := s.f.db.ListGraphNodeAttempts(ctx, after.ID)
	mustProgress(t, err)
	if after.ID != before.ID || after.CurrentNode != before.CurrentNode || len(now) != len(attempts) || s.calls.Load() != 0 {
		t.Fatalf("refused trigger changed the Bug: before=%+v after=%+v calls=%d", before, after, s.calls.Load())
	}
	if found, err := s.f.db.HasWorkflowLaunch(ctx, s.f.session.ID, req.Trigger.CommentID); err != nil || (found && req.Trigger.CommentID != s.attempt.TriggerID()) {
		t.Fatalf("refused trigger recorded a launch: %v", err)
	}
}

// TestBugStartS2rCharacterization pins the run-before-analyst interruption on
// the real Bug runtime: the start run failed, no owner wait, the completed
// conversation attempt and its transition kept, the current node analyst and
// no analyst attempt. The owner's fresh reply continues it.
func TestBugStartS2rCharacterization(t *testing.T) {
	s := newS2rFixture(t)
	s.killStart(t)
	instance := s.restart(t)
	s.assertS2r(t, instance)
	if _, err := Execute(t.Context(), s.f.opts, s.ownerReply(9001)); err != nil {
		t.Fatalf("owner reply was refused: %v", err)
	}
}

func TestBugStartS2rOwnerReplyResumesSameBug(t *testing.T) {
	s := newS2rFixture(t)
	ctx := t.Context()
	s.killStart(t)
	instance := s.restart(t)
	s.assertS2r(t, instance)
	var second error
	s.during = func(context.Context) {
		s.during = nil
		// A second owner reply while the restart's run is live.
		_, second = s.runtime.graphAdmission(ctx, s.ownerReply(9002))
	}
	if _, err := Execute(ctx, s.f.opts, s.ownerReply(9001)); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(second, ErrBugReplyNotApplied) {
		t.Fatalf("second reply during the live restart was not refused: %v", second)
	}
	instances, err := s.f.db.ListStandaloneBugInstances(ctx)
	mustProgress(t, err)
	if len(instances) != 1 || instances[0].ID != instance.ID {
		t.Fatalf("instances=%+v", instances)
	}
	if previous, _, err := s.f.db.WorkflowPredecessor(ctx, instance.ID); err != nil || previous != 0 {
		t.Fatalf("successor recorded: %d %v", previous, err)
	}
	launched, found, err := s.f.db.WorkflowLaunchInstance(ctx, s.f.session.ID, 9001)
	mustProgress(t, err)
	if !found || launched != instance.ID {
		t.Fatal("the reply's launch is not on the same instance")
	}
	if found, err := s.f.db.HasWorkflowLaunch(ctx, s.f.session.ID, 9002); err != nil || found {
		t.Fatalf("second reply recorded a launch: %t %v", found, err)
	}
	analyst := func() int {
		attempts, err := s.f.db.ListGraphNodeAttempts(ctx, instance.ID)
		mustProgress(t, err)
		n := 0
		for _, a := range attempts {
			if a.Node == "analyst" {
				n++
			}
		}
		return n
	}
	if analyst() != 1 || s.calls.Load() != 1 {
		t.Fatalf("analyst attempts=%d calls=%d", analyst(), s.calls.Load())
	}
	wait, err := s.f.db.GetGraphOwnerWait(ctx, instance.ID)
	mustProgress(t, err)
	if wait.OriginNode != "analyst" {
		t.Fatalf("wait=%+v", wait)
	}
	// A replay of the same reply never opens a second analysis.
	_, err = Execute(ctx, s.f.opts, s.ownerReply(9001))
	t.Logf("replay: %v", err)
	if analyst() != 1 || s.calls.Load() != 1 {
		t.Fatalf("replay: analyst attempts=%d calls=%d", analyst(), s.calls.Load())
	}
}

func TestBugStartS2rRestartGuards(t *testing.T) {
	t.Run("live run", func(t *testing.T) {
		s := newS2rFixture(t)
		s.killStart(t)
		s.refused(t, s.ownerReply(9001))
	})
	t.Run("private start trigger", func(t *testing.T) {
		s := newS2rFixture(t)
		ctx := t.Context()
		s.killStart(t)
		s.assertS2r(t, s.restart(t))
		s.refused(t, s.req)
		moved, err := s.f.db.MoveBugFilingStartAttempt(ctx, s.attempt.ID, store.BugStartReserved, store.BugStartFailed, store.BugStartOutcomeFailed, 0, "interrupted", time.Now())
		mustProgress(t, err)
		if !moved {
			t.Fatal("start attempt not closed")
		}
		next, created, err := s.f.db.OpenBugFilingStartAttempt(ctx, s.attempt.FilingID, s.f.session.ID, time.Now())
		mustProgress(t, err)
		if !created || !store.IsPrivateTrigger(next.TriggerID()) {
			t.Fatalf("next start attempt=%+v", next)
		}
		fresh := s.req
		fresh.Trigger.CommentID = next.TriggerID()
		s.refused(t, fresh)
		instance, err := s.f.db.LatestStandaloneBugInstance(ctx, s.f.session.ID)
		mustProgress(t, err)
		s.assertS2r(t, instance)
	})
	t.Run("graph trigger", func(t *testing.T) {
		s := newS2rFixture(t)
		ctx := t.Context()
		s.killStart(t)
		instance := s.restart(t)
		// A refused admission loads the pinned definition without admitting.
		if _, err := s.runtime.graphAdmission(ctx, s.req); !errors.Is(err, ErrBugReplyNotApplied) {
			t.Fatal(err)
		}
		for trigger, want := range map[int64]bool{9001: true, store.GraphDispatch{ID: 1}.TriggerID(): false, s.attempt.TriggerID(): false, store.BugStartTrigger(s.attempt.ID + 1): false} {
			got, err := s.runtime.untouchedGraphLaunch(ctx, instance)
			mustProgress(t, err)
			if !got && !store.IsPrivateTrigger(trigger) {
				got, err = s.runtime.stoppedGraphLaunch(ctx, instance)
				mustProgress(t, err)
			}
			if got != want {
				t.Fatalf("trigger %d restart=%t want %t", trigger, got, want)
			}
		}
		s.assertS2r(t, instance)
	})
	t.Run("run from another trigger", func(t *testing.T) {
		s := newS2rFixture(t)
		ctx := t.Context()
		s.killStart(t)
		instance := s.restart(t)
		runs, err := s.f.db.ListRuns(ctx, s.f.session.ID)
		mustProgress(t, err)
		other, err := s.f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: s.f.session.ID, SegmentID: runs[0].SegmentID, Mode: runs[0].Mode, Workflow: runs[0].Workflow, Agent: runs[0].Agent, TriggerCommentID: 500, LogPath: func(n int) string { return fmt.Sprintf("%s/other-%04d.jsonl", t.TempDir(), n) }})
		mustProgress(t, err)
		_, err = s.f.db.FinishRun(ctx, other.ID, store.RunOutcome{Status: store.RunFailed, Error: "earlier work"})
		mustProgress(t, err)
		s.refused(t, s.ownerReply(9001))
		s.assertS2r(t, instance)
	})
	t.Run("not the owner", func(t *testing.T) {
		s := newS2rFixture(t)
		s.killStart(t)
		instance := s.restart(t)
		req := s.ownerReply(9001)
		req.Trigger.AuthorID = 77
		_, err := Execute(t.Context(), s.f.opts, req)
		if err == nil {
			t.Fatal("a reply by someone else restarted the Bug")
		}
		s.assertS2r(t, instance)
	})
}

func TestBugStartS2rPrivateTriggerIdentity(t *testing.T) {
	if !store.IsPrivateTrigger(store.BugStartTrigger(1)) || !store.IsBugStartTrigger(store.BugStartTrigger(1)) {
		t.Fatal("the minted start trigger is not private")
	}
	if store.IsPrivateTrigger(6664969403) {
		t.Fatal("a real forge comment id is private")
	}
}
