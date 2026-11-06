package turn

import (
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

// seedNoAttempt leaves the stopped start with no node attempt: the private
// start trigger admitted the launch at the start node, and the one session run
// it launched finished before the conversation attempt was saved. Only rows
// are seeded; the runtime, store and admission are the real ones.
func (s *s2rFixture) seedNoAttempt(t *testing.T) store.GraphInstance {
	t.Helper()
	ctx := t.Context()
	admitted, err := s.runtime.graphAdmission(ctx, s.req)
	mustProgress(t, err)
	segment, err := s.f.db.GetOrCreateSegment(ctx, s.f.session.ID, store.FirstSegmentOrdinal, "s2r-noattempt", store.AgentClaude)
	mustProgress(t, err)
	run, err := s.f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: s.f.session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: s.attempt.TriggerID(), LogPath: func(int) string { return t.TempDir() + "/start.jsonl" }})
	mustProgress(t, err)
	_, err = s.f.db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunFailed, Error: "stopped before the conversation attempt"})
	mustProgress(t, err)
	instance, err := s.f.db.LatestStandaloneBugInstance(ctx, s.f.session.ID)
	mustProgress(t, err)
	if instance.ID != admitted.Instance.ID {
		t.Fatalf("seeded instance %d, latest %d", admitted.Instance.ID, instance.ID)
	}
	return instance
}

// assertNoAttempt checks the exact no-attempt stopped start.
func (s *s2rFixture) assertNoAttempt(t *testing.T, instance store.GraphInstance) {
	t.Helper()
	ctx := t.Context()
	if instance.Status != store.GraphRunning || instance.CurrentNode != "conversation" || !instance.NextDueAt.IsZero() {
		t.Fatalf("instance=%+v", instance)
	}
	runs, err := s.f.db.ListRuns(ctx, s.f.session.ID)
	mustProgress(t, err)
	if len(runs) != 1 || runs[0].Status != store.RunFailed || runs[0].TriggerCommentID != s.attempt.TriggerID() {
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
	transitions, err := s.f.db.ListGraphTransitions(ctx, instance.ID)
	mustProgress(t, err)
	comments, err := s.f.db.ListGraphComments(ctx, instance.ID, "", 1)
	mustProgress(t, err)
	if len(attempts) != 0 || len(transitions) != 0 || len(comments) != 0 || s.calls.Load() != 0 {
		t.Fatalf("attempts=%+v transitions=%+v comments=%+v calls=%d", attempts, transitions, comments, s.calls.Load())
	}
	stopped, err := s.f.db.StoppedBugLaunch(ctx, instance, store.BugStartAnalystNode)
	mustProgress(t, err)
	if !stopped {
		t.Fatal("the shared rule does not see the stopped start")
	}
}

func TestBugStartS2rNoAttemptOwnerReplyResumesSameBug(t *testing.T) {
	s := newS2rFixture(t)
	ctx := t.Context()
	instance := s.seedNoAttempt(t)
	s.assertNoAttempt(t, instance)
	if _, err := Execute(ctx, s.f.opts, s.ownerReply(9001)); err != nil {
		t.Fatalf("owner reply was refused: %v", err)
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
	attempts, err := s.f.db.ListGraphNodeAttempts(ctx, instance.ID)
	mustProgress(t, err)
	if len(attempts) != 2 || attempts[0].Node != "conversation" || attempts[0].Status != store.GraphSucceeded || attempts[1].Node != "analyst" || s.calls.Load() != 1 {
		t.Fatalf("attempts=%+v calls=%d", attempts, s.calls.Load())
	}
}

func TestBugStartS2rNoAttemptRestartGuards(t *testing.T) {
	t.Run("private start trigger", func(t *testing.T) {
		s := newS2rFixture(t)
		instance := s.seedNoAttempt(t)
		s.refused(t, s.req)
		after, err := s.f.db.LatestStandaloneBugInstance(t.Context(), s.f.session.ID)
		mustProgress(t, err)
		if after.ID != instance.ID {
			t.Fatalf("instance replaced: %d -> %d", instance.ID, after.ID)
		}
		s.assertNoAttempt(t, after)
	})
	t.Run("graph trigger", func(t *testing.T) {
		s := newS2rFixture(t)
		ctx := t.Context()
		instance := s.seedNoAttempt(t)
		graph := s.ownerReply(store.GraphDispatch{ID: 1}.TriggerID())
		// A graph trigger never takes the owner-reply restart: with no
		// dispatch behind it the retained admission is refused outright.
		if _, err := s.runtime.graphAdmission(ctx, graph); err == nil {
			t.Fatal("graph trigger admitted")
		}
		if _, err := Execute(ctx, s.f.opts, graph); err == nil {
			t.Fatal("graph trigger executed")
		}
		if found, err := s.f.db.HasWorkflowLaunch(ctx, s.f.session.ID, graph.Trigger.CommentID); err != nil || found {
			t.Fatalf("graph trigger recorded a launch: %t %v", found, err)
		}
		after, err := s.f.db.LatestStandaloneBugInstance(ctx, s.f.session.ID)
		mustProgress(t, err)
		if after.ID != instance.ID {
			t.Fatalf("instance replaced: %d -> %d", instance.ID, after.ID)
		}
		s.assertNoAttempt(t, after)
	})
}
