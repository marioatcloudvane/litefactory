package daemon

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

type launchFixture struct {
	d       *Daemon
	db      *store.DB
	session store.Session
	item    store.BrainstormTaskItem
	at      time.Time
}

func newLaunchFixture(t *testing.T) *launchFixture {
	t.Helper()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "launch.db")
	db, err := store.Open(ctx, path)
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
	session, err := db.GetOrCreateSession(ctx, repo, 10)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	ledger := `{"decisions":[]}`
	sum := sha256.Sum256([]byte(ledger))
	raw, err := sql.Open("sqlite", store.DSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	stamp := store.FormatTime(at)
	if _, err = raw.ExecContext(ctx, `INSERT INTO brainstorm_task_batches(id,owner_action_id,execution_id,owner_id,owner_generation,repository_id,repository_ref,brainstorm_id,draft_set_id,state,created_at) VALUES('b1','oa','ex','owner',1,?,'acme/api','bs','ds','running',?)`, repo.ID, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err = raw.ExecContext(ctx, `INSERT INTO brainstorm_task_items(id,batch_id,draft_id,ordinal,title,body,marker,ledger,ledger_digest,state,issue_id,issue_number,session_id,updated_at) VALUES('i1','b1','d1',1,'T','B','M',?,?,'plan_starting',77,10,?,?)`, ledger, hex.EncodeToString(sum[:]), session.ID, stamp); err != nil {
		t.Fatal(err)
	}
	item, err := db.ReadBrainstormTaskItem(ctx, "i1")
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{db: db, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return &launchFixture{d: d, db: db, session: session, item: item, at: at}
}

func (f *launchFixture) reserved(t *testing.T) store.BrainstormLaunchAttempt {
	t.Helper()
	a, _, err := f.db.OpenBrainstormLaunchAttempt(t.Context(), f.item.ID, f.session.ID, f.at)
	if err != nil {
		t.Fatal(err)
	}
	if a.State == store.BrainstormLaunchPending {
		if ok, err := f.db.MoveBrainstormLaunchAttempt(t.Context(), a.ID, store.BrainstormLaunchPending, store.BrainstormLaunchReserved, "", 0, "", f.at); err != nil || !ok {
			t.Fatal(ok, err)
		}
		a.State = store.BrainstormLaunchReserved
	}
	return a
}

func (f *launchFixture) run(t *testing.T, trigger int64, status store.RunStatus) store.Run {
	t.Helper()
	segment, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, store.FirstSegmentOrdinal, "conv", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Mode: "plan", Agent: store.AgentClaude, TriggerCommentID: trigger, Prelaunch: true, LogPath: func(int) string { return "/logs/run.jsonl" }})
	if err != nil {
		t.Fatal(err)
	}
	if status == store.RunPrelaunch {
		return run
	}
	if run, err = f.db.StartRun(t.Context(), run.ID); err != nil {
		t.Fatal(err)
	}
	if status == store.RunFailed {
		code := int64(1)
		if run, err = f.db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunFailed, ExitCode: &code, FinishedAt: f.at}); err != nil {
			t.Fatal(err)
		}
	}
	return run
}

func TestBrainstormLaunchLockHeldAndPrelaunchKeepIdentity(t *testing.T) {
	t.Parallel()
	f := newLaunchFixture(t)
	a := f.reserved(t)
	var handoff error
	outcome, err := f.d.classifyBrainstormAttempt(t.Context(), f.item, a, &handoff, f.at)
	if err != nil || outcome != store.BrainstormLaunchOutcomeDeferred {
		t.Fatalf("lock held: %q %v", outcome, err)
	}
	again := f.reserved(t)
	if again.ID != a.ID {
		t.Fatal("deferral changed the attempt identity")
	}
	f.run(t, a.TriggerID(), store.RunPrelaunch)
	if outcome, err = f.d.classifyBrainstormAttempt(t.Context(), f.item, again, &handoff, f.at); err != nil || outcome != store.BrainstormLaunchOutcomeDeferred {
		t.Fatalf("prelaunch: %q %v", outcome, err)
	}
	attempts, _ := f.db.BrainstormLaunchAttempts(t.Context(), f.item.ID)
	item, _ := f.db.ReadBrainstormTaskItem(t.Context(), f.item.ID)
	if len(attempts) != 1 || attempts[0].State != store.BrainstormLaunchPending || item.FailedLaunchAttempts != 0 {
		t.Fatalf("deferral counted as a retry: %+v %+v", attempts, item)
	}
}

func TestBrainstormLaunchFailureAfterRunRowRetriesUnderNewTrigger(t *testing.T) {
	t.Parallel()
	f := newLaunchFixture(t)
	a := f.reserved(t)
	f.run(t, a.TriggerID(), store.RunFailed)
	var handoff error
	outcome, err := f.d.classifyBrainstormAttempt(t.Context(), f.item, a, &handoff, f.at)
	if err != nil || outcome != store.BrainstormLaunchOutcomeFailed {
		t.Fatalf("failure: %q %v", outcome, err)
	}
	item, _ := f.db.ReadBrainstormTaskItem(t.Context(), f.item.ID)
	if item.FailedLaunchAttempts != 1 {
		t.Fatalf("failure not counted: %+v", item)
	}
	next := f.reserved(t)
	if next.ID == a.ID || next.TriggerID() == a.TriggerID() {
		t.Fatal("retry reused the failed identity")
	}
}

func TestBrainstormLaunchThreeFailuresEndStartFailed(t *testing.T) {
	t.Parallel()
	f := newLaunchFixture(t)
	for range store.BrainstormLaunchFailureLimit {
		a := f.reserved(t)
		var handoff error = errTestHandoff
		if _, err := f.d.classifyBrainstormAttempt(t.Context(), f.item, a, &handoff, f.at); err != nil {
			t.Fatal(err)
		}
	}
	item, _ := f.db.ReadBrainstormTaskItem(t.Context(), f.item.ID)
	if err := f.d.stepBrainstormLaunch(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	item, _ = f.db.ReadBrainstormTaskItem(t.Context(), f.item.ID)
	if item.State != store.BrainstormItemPlanStartFailed || item.IssueNumber != 10 || !strings.Contains(item.Reason, "#10") {
		t.Fatalf("after three failures: %+v", item)
	}
	attempts, _ := f.db.BrainstormLaunchAttempts(t.Context(), f.item.ID)
	if len(attempts) != store.BrainstormLaunchFailureLimit {
		t.Fatalf("attempts: %d", len(attempts))
	}
}

var errTestHandoff = &testHandoffError{}

type testHandoffError struct{}

func (*testHandoffError) Error() string { return "malformed launch" }

func (f *launchFixture) admit(t *testing.T, trigger int64) (store.WorkflowAdmission, store.GraphDefinition) {
	t.Helper()
	ctx := t.Context()
	plan, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: "plan", ProfileVersion: 1, Path: "plan/plan.dot", Digest: strings.Repeat("b", 64), CreatedAt: f.at})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.AdmitPlanPath(ctx, f.session.ID); err != nil {
		t.Fatal(err)
	}
	admitted, err := f.db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: "plan", SessionID: f.session.ID, TriggerID: trigger, DefinitionID: plan.ID, Start: "set_agent", ContentDigest: plan.Digest, TopologyDigest: plan.Digest, Files: []byte(`{"graph.dot":"eA=="}`)})
	if err != nil {
		t.Fatal(err)
	}
	return admitted, plan
}

func TestBrainstormLaunchStartedOnlyAfterSeedAndFirstAgent(t *testing.T) {
	t.Parallel()
	f := newLaunchFixture(t)
	a := f.reserved(t)
	admitted, _ := f.admit(t, a.TriggerID())
	// The run failed after admission (e.g. the brief could not be prepared).
	f.run(t, a.TriggerID(), store.RunFailed)
	outcome, err := f.d.classifyBrainstormAttempt(t.Context(), f.item, a, nil, f.at)
	if err != nil || outcome != store.BrainstormLaunchOutcomeReplayed {
		t.Fatalf("classify: %q %v", outcome, err)
	}
	item, _ := f.db.ReadBrainstormTaskItem(t.Context(), f.item.ID)
	if item.State != store.BrainstormItemPlanStarting || item.Reason == "" {
		t.Fatalf("admitted without a brief reported as started: %+v", item)
	}
	// A later pass opens no second launch for the same item.
	if err = f.d.stepBrainstormLaunch(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	attempts, _ := f.db.BrainstormLaunchAttempts(t.Context(), f.item.ID)
	if len(attempts) != 1 || attempts[0].State != store.BrainstormLaunchSucceeded {
		t.Fatalf("second launch opened: %+v", attempts)
	}
	if err = f.db.FinishBrainstormSeed(t.Context(), admitted.LaunchID, store.BrainstormSeedComplete, "", f.at); err != nil {
		t.Fatal(err)
	}
	item, _ = f.db.ReadBrainstormTaskItem(t.Context(), f.item.ID)
	if err = f.d.stepBrainstormLaunch(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	if item, _ = f.db.ReadBrainstormTaskItem(t.Context(), f.item.ID); item.State != store.BrainstormItemPlanStarting {
		t.Fatalf("started before the first agent: %+v", item)
	}
	if _, err = f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: admitted.Instance.ID, Node: "set_agent", Limit: 3, StartedAt: f.at}); err != nil {
		t.Fatal(err)
	}
	if err = f.d.stepBrainstormLaunch(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	if item, _ = f.db.ReadBrainstormTaskItem(t.Context(), f.item.ID); item.State != store.BrainstormItemPlanStarted {
		t.Fatalf("not started after seed and first agent: %+v", item)
	}
}

func TestBrainstormLaunchRestartClassifiesFromRunRow(t *testing.T) {
	t.Parallel()
	f := newLaunchFixture(t)
	a := f.reserved(t)
	if ok, err := f.db.MoveBrainstormLaunchAttempt(t.Context(), a.ID, store.BrainstormLaunchReserved, store.BrainstormLaunchRunning, "", 0, "", f.at); err != nil || !ok {
		t.Fatal(ok, err)
	}
	a.State = store.BrainstormLaunchRunning
	f.run(t, a.TriggerID(), store.RunRunning)
	// Restart: the run is still owned by its own recovery; nothing changes.
	if outcome, err := f.d.classifyBrainstormAttempt(t.Context(), f.item, a, nil, f.at); err != nil || outcome != "" {
		t.Fatalf("running run: %q %v", outcome, err)
	}
	attempts, _ := f.db.BrainstormLaunchAttempts(t.Context(), f.item.ID)
	if len(attempts) != 1 || attempts[0].State != store.BrainstormLaunchRunning {
		t.Fatalf("restart re-handed a running attempt: %+v", attempts)
	}
}

func TestBrainstormLaunchRunningPlanMeansStarted(t *testing.T) {
	t.Parallel()
	f := newLaunchFixture(t)
	plan, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "plan", ProfileVersion: 1, Path: "plan/plan.dot", Digest: strings.Repeat("c", 64), CreatedAt: f.at})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: plan.ID, CurrentNode: "set_agent", StartedAt: f.at}); err != nil {
		t.Fatal(err)
	}
	if err = f.d.stepBrainstormLaunch(t.Context(), f.item); err != nil {
		t.Fatal(err)
	}
	item, _ := f.db.ReadBrainstormTaskItem(t.Context(), f.item.ID)
	attempts, _ := f.db.BrainstormLaunchAttempts(t.Context(), f.item.ID)
	if item.State != store.BrainstormItemPlanStarted || len(attempts) != 0 {
		t.Fatalf("active Plan got a second launch: %+v %+v", item, attempts)
	}
}
