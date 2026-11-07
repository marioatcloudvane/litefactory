package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

type bugStartFixture struct {
	d       *Daemon
	db      *store.DB
	repo    store.Repo
	filing  store.BugFiling
	now     time.Time
	handoff []int64
	// admit makes the fake hand-off admit the Bug under its trigger.
	admit bool
	// authority is what the effect check returns.
	authority error
}

func newBugStartFixture(t *testing.T) *bugStartFixture {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "bugstart.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	f, _, err := db.AdmitBugFiling(ctx, store.BugFiling{ExecutionID: "owner-action:x", OwnerActionID: "x", OwnerID: "owner", OwnerGeneration: 1, RepositoryID: repo.ID, RepositoryRef: "acme/api", Title: "T", Body: "B", Marker: "M"}, now)
	if err != nil {
		t.Fatal(err)
	}
	labelled := f
	labelled.State, labelled.IssueID, labelled.IssueNumber, labelled.IssueLink = store.BugFilingLabelled, 99, 10, "https://github.com/acme/api/issues/10"
	if ok, err := db.SaveBugFiling(ctx, labelled, f.State, now); err != nil || !ok {
		t.Fatal(ok, err)
	}
	fx := &bugStartFixture{db: db, repo: repo, now: now, admit: true}
	fx.d = &Daemon{db: db, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	fx.d.brainstormTasks.clock = func() time.Time { return fx.now }
	handOff, authority := bugStartHandOff, bugStartAuthority
	t.Cleanup(func() { bugStartHandOff, bugStartAuthority = handOff, authority })
	bugStartAuthority = func(*Daemon, context.Context, store.BugFiling) error { return fx.authority }
	bugStartHandOff = func(d *Daemon, ctx context.Context, _ store.Repo, s store.Session, _ store.BugFiling, a store.BugFilingStartAttempt) (bool, error) {
		// What the real hand-off leaves behind when the process stops right
		// after the turn: the reserved attempt, its run and, when the turn
		// got that far, the Bug admission under the private trigger.
		fx.handoff = append(fx.handoff, a.TriggerID())
		if ok, err := d.db.MoveBugFilingStartAttempt(ctx, a.ID, store.BugStartPending, store.BugStartReserved, "", 0, "", fx.now); err != nil || !ok {
			return false, errors.Join(err, errors.New("attempt not reserved"))
		}
		if fx.admit {
			fx.admitBug(t, s.ID, a.TriggerID())
		}
		fx.run(t, s.ID, a.TriggerID(), store.RunFailed, fx.admit)
		return false, nil
	}
	fx.filing = fx.read(t)
	return fx
}

func (fx *bugStartFixture) read(t *testing.T) store.BugFiling {
	t.Helper()
	f, err := fx.db.ReadBugFiling(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (fx *bugStartFixture) step(t *testing.T) store.BugFiling {
	t.Helper()
	if err := fx.d.stepBugFilingStart(t.Context(), fx.read(t)); err != nil {
		t.Fatal(err)
	}
	return fx.read(t)
}

func (fx *bugStartFixture) session(t *testing.T) store.Session {
	t.Helper()
	s, err := fx.db.GetOrCreateSession(t.Context(), fx.repo, 10)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (fx *bugStartFixture) admitBug(t *testing.T, sessionID, trigger int64) store.WorkflowAdmission {
	t.Helper()
	ctx := t.Context()
	if _, err := fx.db.AdmitWorkflow(ctx, store.TaskWorkflowAdmission{SessionID: sessionID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.db.AdmitWorkflowPath(ctx, sessionID, "graph"); err != nil {
		t.Fatal(err)
	}
	def, err := fx.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 2, Path: "bug/bug.dot", Digest: strings.Repeat("c", 64), CreatedAt: fx.now})
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := fx.db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Workflow: "bug", SessionID: sessionID, TriggerID: trigger, DefinitionID: def.ID, Start: "conversation", ContentDigest: def.Digest, TopologyDigest: def.Digest, Files: []byte(`{"graph.dot":"eA=="}`)})
	if err != nil {
		t.Fatal(err)
	}
	return admitted
}

func (fx *bugStartFixture) run(t *testing.T, sessionID, trigger int64, status store.RunStatus, bug bool) store.Run {
	t.Helper()
	ctx := t.Context()
	segment, err := fx.db.GetOrCreateSegment(ctx, sessionID, store.FirstSegmentOrdinal, "conv", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	params := store.AllocateRunParams{SessionID: sessionID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: trigger, Prelaunch: true, LogPath: func(int) string { return "/logs/run.jsonl" }}
	if !bug {
		params.Workflow, params.Mode = "", store.ModeSpike
	}
	run, err := fx.db.AllocateRun(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if status == store.RunPrelaunch {
		return run
	}
	if run, err = fx.db.StartRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if status == store.RunFailed {
		code := int64(1)
		if run, err = fx.db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunFailed, ExitCode: &code, FinishedAt: fx.now}); err != nil {
			t.Fatal(err)
		}
	}
	return run
}

// conversationToAnalyst records the start node's completed attempt and the
// transition to the analyst, as the Bug runtime does before the analyst run.
// The conversation runs inline, so its attempt is bound to no run.
func (fx *bugStartFixture) conversationToAnalyst(t *testing.T, instanceID int64) {
	t.Helper()
	ctx := t.Context()
	a, err := fx.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instanceID, Node: "conversation", StartedAt: fx.now})
	if err != nil {
		t.Fatal(err)
	}
	if err = fx.db.CompleteGraphNodeAttempt(ctx, a.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "analyse", FinishedAt: fx.now, InstanceStatus: store.GraphRunning, Transition: &store.GraphTransition{InstanceID: instanceID, FromNode: "conversation", ToNode: "analyst", OnOutcome: "analyse", At: fx.now}}); err != nil {
		t.Fatal(err)
	}
}

func (fx *bugStartFixture) attempts(t *testing.T) []store.BugFilingStartAttempt {
	t.Helper()
	attempts, err := fx.db.BugFilingStartAttempts(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	return attempts
}

func TestBugFilingStartHandsOffOncePrivatelyAndReplaysAfterRestart(t *testing.T) {
	fx := newBugStartFixture(t)
	f := fx.step(t)
	if len(fx.handoff) != 1 {
		t.Fatalf("hand-offs %v", fx.handoff)
	}
	trigger := fx.handoff[0]
	if !store.IsBugStartTrigger(trigger) || !store.IsPrivateTrigger(trigger) {
		t.Fatalf("trigger %d is not in the private Bug start band", trigger)
	}
	attempts := fx.attempts(t)
	if len(attempts) != 1 || attempts[0].TriggerID() != trigger || attempts[0].State != store.BugStartReserved {
		t.Fatalf("attempts %+v", attempts)
	}
	if f.SessionID == 0 || f.State != store.BugFilingLabelled {
		t.Fatalf("filing %+v", f)
	}
	// Restart: the reserved attempt has no worker and is judged from its run.
	fx.step(t)
	f = fx.step(t)
	if len(fx.handoff) != 1 {
		t.Fatalf("second hand-off after restart: %v", fx.handoff)
	}
	attempts = fx.attempts(t)
	if len(attempts) != 1 || attempts[0].State != store.BugStartSucceeded || attempts[0].Outcome != store.BugStartOutcomeReplayed {
		t.Fatalf("attempts %+v", attempts)
	}
	// The start run is finished with no node attempt: S2r stays partial.
	if f.State != store.BugFilingStartAdmitted || f.AdmissionTrigger != trigger || f.Reason != bugStartRecorded {
		t.Fatalf("filing %+v", f)
	}
	// S2r does not time out and never hands off again.
	fx.now = fx.now.Add(2 * bugStartWait)
	if f = fx.step(t); f.State != store.BugFilingStartAdmitted || f.Reason != bugStartRecorded || len(fx.handoff) != 1 {
		t.Fatalf("S2r moved: %+v %v", f, fx.handoff)
	}
}

func TestBugFilingStartOwnerShapeS2rThenAnalystDispatches(t *testing.T) {
	fx := newBugStartFixture(t)
	s := fx.session(t)
	admitted := fx.admitBug(t, s.ID, 4242)
	fx.run(t, s.ID, 4242, store.RunFailed, true)
	fx.conversationToAnalyst(t, admitted.Instance.ID)
	f := fx.step(t)
	if len(fx.handoff) != 0 || f.State != store.BugFilingStartAdmitted || f.Reason != bugStartRecorded || f.AdmissionTrigger != 4242 {
		t.Fatalf("owner-binding S2r: %+v %v", f, fx.handoff)
	}
	if len(fx.attempts(t)) != 0 {
		t.Fatal("an attempt opened after the Bug admission")
	}
	if _, err := fx.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: admitted.Instance.ID, Node: "analyst", StartedAt: fx.now}); err != nil {
		t.Fatal(err)
	}
	if f = fx.step(t); f.State != store.BugFilingDispatched || f.Reason != "" {
		t.Fatalf("S3 not dispatched: %+v", f)
	}
}

func TestBugFilingStartS1AndS2LeaveToRuntimeThenTimeOut(t *testing.T) {
	for _, shape := range []string{"S1", "S2"} {
		t.Run(shape, func(t *testing.T) {
			fx := newBugStartFixture(t)
			s := fx.session(t)
			admitted := fx.admitBug(t, s.ID, 777)
			if shape == "S2" {
				if _, err := fx.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: admitted.Instance.ID, Node: "conversation", StartedAt: fx.now}); err != nil {
					t.Fatal(err)
				}
			}
			f := fx.step(t)
			if len(fx.handoff) != 0 || len(fx.attempts(t)) != 0 || f.State != store.BugFilingStartAdmitted || f.Reason != bugStartAdmitted {
				t.Fatalf("%s: %+v %v", shape, f, fx.handoff)
			}
			fx.now = fx.now.Add(bugStartWait + time.Minute)
			f = fx.step(t)
			if f.State != store.BugFilingStartFailed || !strings.Contains(f.Reason, f.IssueLink) || len(fx.handoff) != 0 {
				t.Fatalf("%s after wait: %+v", shape, f)
			}
		})
	}
}

func TestBugFilingStartOwnerWaitAndEndedInstanceFail(t *testing.T) {
	fx := newBugStartFixture(t)
	s := fx.session(t)
	admitted := fx.admitBug(t, s.ID, 777)
	f := fx.step(t)
	ev, err := fx.db.BugStartEvidence(t.Context(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	parked := ev
	parked.HasWait, parked.Wait = true, store.GraphOwnerWait{InstanceID: admitted.Instance.ID, State: "waiting"}
	if err = fx.d.settleBugStart(t.Context(), f, parked, fx.now); err != nil {
		t.Fatal(err)
	}
	if got := fx.read(t); got.State != store.BugFilingStartFailed || got.Reason != bugStartParked {
		t.Fatalf("parked wait: %+v", got)
	}
	ended := ev
	ended.Instance.Status = store.GraphFailed
	other := newBugStartFixture(t)
	if err = other.d.settleBugStart(t.Context(), other.read(t), ended, other.now); err != nil {
		t.Fatal(err)
	}
	if got := other.read(t); got.State != store.BugFilingStartFailed || !strings.Contains(got.Reason, got.IssueLink) {
		t.Fatalf("ended instance: %+v", got)
	}
}

func TestBugFilingStartRevocationBeforeHandOff(t *testing.T) {
	fx := newBugStartFixture(t)
	fx.authority = errBugFilingRevoked
	f := fx.step(t)
	if len(fx.handoff) != 0 {
		t.Fatalf("handed off after revocation: %v", fx.handoff)
	}
	if f.State != store.BugFilingStartFailed || !strings.HasPrefix(f.Reason, "authority was revoked; filed and labelled as "+f.IssueLink) {
		t.Fatalf("filing %+v", f)
	}
	// A transient authority read failure sends nothing and keeps the filing.
	tr := newBugStartFixture(t)
	tr.authority = errors.New("database busy")
	if err := tr.d.stepBugFilingStart(t.Context(), tr.read(t)); err == nil {
		t.Fatal("transient authority failure hidden")
	}
	if got := tr.read(t); len(tr.handoff) != 0 || got.State != store.BugFilingLabelled {
		t.Fatalf("transient: %+v %v", got, tr.handoff)
	}
}

func TestBugFilingStartFailureLimit(t *testing.T) {
	fx := newBugStartFixture(t)
	fx.admit = false
	var f store.BugFiling
	for i := 0; i < 2*store.BugStartFailureLimit+2; i++ {
		f = fx.step(t)
	}
	if len(fx.handoff) != store.BugStartFailureLimit {
		t.Fatalf("hand-offs %v", fx.handoff)
	}
	seen := map[int64]bool{}
	for _, id := range fx.handoff {
		if seen[id] {
			t.Fatalf("an identity was handed off twice: %v", fx.handoff)
		}
		seen[id] = true
	}
	want := "filed and labelled as " + f.IssueLink + " but not started; an owner comment on the issue starts it"
	if f.State != store.BugFilingStartFailed || f.Reason != want || f.FailedStartAttempts != store.BugStartFailureLimit {
		t.Fatalf("filing %+v", f)
	}
}

func TestBugFilingStartAdoptsOwnerAdmissionAtAnalyst(t *testing.T) {
	fx := newBugStartFixture(t)
	s := fx.session(t)
	admitted := fx.admitBug(t, s.ID, 5150)
	fx.run(t, s.ID, 5150, store.RunFailed, true)
	fx.conversationToAnalyst(t, admitted.Instance.ID)
	if _, err := fx.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: admitted.Instance.ID, Node: "analyst", StartedAt: fx.now}); err != nil {
		t.Fatal(err)
	}
	f := fx.step(t)
	if len(fx.handoff) != 0 || f.State != store.BugFilingDispatched || f.AdmissionTrigger != 5150 {
		t.Fatalf("owner admission not adopted: %+v %v", f, fx.handoff)
	}
}
