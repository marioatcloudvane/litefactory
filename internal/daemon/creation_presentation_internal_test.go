package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// stubReporter is a Reporter with no presentation capability.
type stubReporter struct{}

func (stubReporter) ReportTurn(context.Context, turn.Result) (turn.Posted, error) {
	return turn.Posted{}, nil
}
func (stubReporter) ReportRun(context.Context, turn.RunReport) (turn.Posted, error) {
	return turn.Posted{}, nil
}
func (stubReporter) ReportReply(context.Context, turn.ReplyRequest) (turn.Posted, error) {
	return turn.Posted{}, nil
}
func (stubReporter) Consume(context.Context, int64, int64) (store.Watermark, error) {
	return store.Watermark{}, nil
}

// presenterStub adds the capability and counts its calls.
type presenterStub struct {
	stubReporter
	runs []int64
	err  error
}

func (p *presenterStub) ReportCreationPresentation(_ context.Context, runID int64) (turn.Posted, error) {
	p.runs = append(p.runs, runID)
	return turn.Posted{CommentID: 7}, p.err
}

type presentationUnit struct {
	d       *Daemon
	t       *task
	db      *store.DB
	session store.Session
	run     store.Run
	cs      *store.CreationStore
	at      time.Time
}

func newPresentationUnit(t *testing.T) *presentationUnit {
	t.Helper()
	db, _, session := recoveryDatabase(t)
	d, _, _ := recoveryUnitFixture()
	d.db = db
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, store.FirstSegmentOrdinal, "unit-conversation", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: store.ModeSpike, Agent: store.AgentClaude, TriggerCommentID: 40, LogPath: func(int) string { return "/unit.jsonl" }})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := store.NewCreationStore(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &presentationUnit{d: d, t: &task{session: session, ref: ref}, db: db, session: session, run: run, cs: cs, at: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}
}

func (u *presentationUnit) retain(t *testing.T) store.CreationProposal {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"version": 1, "repository": "acme/api", "source_task": u.session.TaskRef, "relationship": "linked", "artifacts": []map[string]string{{"title": "Unit", "body": "Exact publishable content"}}})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	p := store.CreationProposal{Hash: hex.EncodeToString(sum[:]), Repository: "acme/api", SourceTask: u.session.TaskRef, Relationship: "linked", Artifacts: 1, Envelope: raw}
	if err := u.cs.RetainProposalForRun(t.Context(), p, store.CreationPresentation{RunID: u.run.ID}); err != nil {
		t.Fatal(err)
	}
	return p
}

func (u *presentationUnit) present(t *testing.T) error {
	t.Helper()
	return u.d.presentCreationProposal(recoveryUnitContext(t, u.at), u.t, u.run.ID, 40)
}

func TestCreationRecoveryKeyShape(t *testing.T) {
	if got := creationRecoveryKey(42, 12); got != "creation:source:42:12" {
		t.Fatalf("key = %q", got)
	}
	if creationRecoveryKey(42, 12) == recoveryKey(42, 12) || creationRecoveryKey(42, 12) == "stopped:"+recoveryKey(42, 12) {
		t.Fatal("presentation debt shares a key with another source")
	}
}

func TestPresentationDeferredPredicate(t *testing.T) {
	for _, tc := range []struct {
		name       string
		unresolved store.ReportDelivery
		found      bool
		own        string
		want       bool
	}{
		{"nothing unresolved", store.ReportDelivery{}, false, "", false},
		{"nothing unresolved and a bound key", store.ReportDelivery{}, false, "mine", false},
		{"a different key while none is bound", store.ReportDelivery{Key: "other"}, true, "", true},
		{"a different key beside a bound one", store.ReportDelivery{Key: "other"}, true, "mine", true},
		{"only its own bound delivery", store.ReportDelivery{Key: "mine"}, true, "mine", false},
	} {
		if got := presentationDeferred(tc.unresolved, tc.found, tc.own); got != tc.want {
			t.Errorf("%s: deferred = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPresentCreationProposalNothingOwed(t *testing.T) {
	t.Run("no run", func(t *testing.T) {
		u := newPresentationUnit(t)
		stub := &presenterStub{}
		u.d.reporter = stub
		if err := u.d.presentCreationProposal(recoveryUnitContext(t, u.at), u.t, 0, 40); err != nil || len(stub.runs) != 0 {
			t.Fatalf("err=%v calls=%v", err, stub.runs)
		}
	})
	t.Run("no row", func(t *testing.T) {
		u := newPresentationUnit(t)
		stub := &presenterStub{}
		u.d.reporter = stub
		if err := u.present(t); err != nil || len(stub.runs) != 0 {
			t.Fatalf("err=%v calls=%v", err, stub.runs)
		}
	})
	t.Run("no row and no capability", func(t *testing.T) {
		u := newPresentationUnit(t)
		u.d.reporter = stubReporter{}
		if err := u.present(t); err != nil {
			t.Fatalf("nothing owed must not require the capability: %v", err)
		}
	})
	t.Run("already presented", func(t *testing.T) {
		u := newPresentationUnit(t)
		u.retain(t)
		if err := u.cs.BindCreationPresentationDelivery(t.Context(), u.run.ID, "delivered"); err != nil {
			t.Fatal(err)
		}
		if err := u.cs.MarkCreationPresented(t.Context(), u.run.ID); err != nil {
			t.Fatal(err)
		}
		stub := &presenterStub{}
		u.d.reporter = stub
		if err := u.present(t); err != nil || len(stub.runs) != 0 {
			t.Fatalf("err=%v calls=%v", err, stub.runs)
		}
	})
}

func TestPresentCreationProposalWithoutCapabilityFailsLoudly(t *testing.T) {
	u := newPresentationUnit(t)
	p := u.retain(t)
	u.d.reporter = stubReporter{}
	err := u.present(t)
	if err == nil || isRecoveryPending(err) || !strings.Contains(err.Error(), "cannot present it") || !strings.Contains(err.Error(), p.Hash) {
		t.Fatalf("err = %v, want a loud non-recovery error naming the proposal", err)
	}
	if _, owed := u.d.recoveries.items[creationRecoveryKey(u.session.ID, 40)]; owed {
		t.Fatal("a missing capability recorded retry debt that can never succeed")
	}
	if _, err := u.cs.PendingCreationPresentation(t.Context(), u.run.ID); err != nil {
		t.Fatalf("the row must stay owed: %v", err)
	}
}

func TestPresentCreationProposalPostsThroughTheCreationKey(t *testing.T) {
	u := newPresentationUnit(t)
	u.retain(t)
	stub := &presenterStub{}
	u.d.reporter = stub
	if err := u.present(t); err != nil {
		t.Fatal(err)
	}
	if len(stub.runs) != 1 || stub.runs[0] != u.run.ID {
		t.Fatalf("presenter calls = %v", stub.runs)
	}

	failing := newPresentationUnit(t)
	failing.retain(t)
	failing.d.reporter = &presenterStub{err: &turn.DeliveryError{Key: "delivery-key", State: "uncertain", Cause: context.DeadlineExceeded}}
	err := failing.present(t)
	if !isRecoveryPending(err) {
		t.Fatalf("a forge failure is not recovery debt: %v", err)
	}
	debt := failing.d.recoveries.items[creationRecoveryKey(failing.session.ID, 40)]
	if debt.State != "uncertain" || debt.DeliveryKey != "delivery-key" || debt.RunID != failing.run.ID || !debt.NextAt.Equal(failing.at.Add(time.Minute)) {
		t.Fatalf("debt = %+v", debt)
	}
	if _, exists := failing.d.recoveries.items[recoveryKey(failing.session.ID, 40)]; exists {
		t.Fatal("presentation debt was recorded under the run's own source key")
	}
}

func TestPresentCreationProposalDefersBesideADifferentUnresolvedDelivery(t *testing.T) {
	u := newPresentationUnit(t)
	p := u.retain(t)
	marker := "<!-- lf:delivery v=1 key=other-delivery -->"
	if _, err := u.db.EnsureReportDelivery(t.Context(), store.ReportDelivery{Key: "other-delivery", Version: 1, SessionID: u.session.ID, TriggerID: 40, Task: u.session.TaskRef, Destination: u.session.TaskRef, TriggerNamespace: "comment", Purpose: "reply", BotUserID: 9, Body: "Other\n\n" + marker, Marker: marker}); err != nil {
		t.Fatal(err)
	}
	stub := &presenterStub{}
	u.d.reporter = stub
	err := u.present(t)
	if !isRecoveryPending(err) || len(stub.runs) != 0 {
		t.Fatalf("err=%v calls=%v, want deferral without an attempt", err, stub.runs)
	}
	debt := u.d.recoveries.items[creationRecoveryKey(u.session.ID, 40)]
	if debt.State == "resolved" || !strings.Contains(debt.Reason, "unresolved") || !debt.NextAt.Equal(u.at.Add(time.Minute)) {
		t.Fatalf("deferral debt = %+v", debt)
	}

	// Not due yet: no second look at the store or the presenter.
	if err := u.d.presentCreationProposal(recoveryUnitContext(t, u.at.Add(time.Second)), u.t, u.run.ID, 40); !isRecoveryPending(err) || len(stub.runs) != 0 {
		t.Fatalf("an ineligible retry attempted: err=%v calls=%v", err, stub.runs)
	}

	// Once the other delivery is complete the same call goes through.
	if _, err := u.db.MarkReportDeliverySent(t.Context(), "other-delivery"); err != nil {
		t.Fatal(err)
	}
	if _, err := u.db.ConfirmReportDelivery(t.Context(), "other-delivery", 5); err != nil {
		t.Fatal(err)
	}
	if _, err := u.db.CompleteReportDelivery(t.Context(), "other-delivery"); err != nil {
		t.Fatal(err)
	}
	if err := u.d.presentCreationProposal(recoveryUnitContext(t, u.at.Add(time.Minute)), u.t, u.run.ID, 40); err != nil || len(stub.runs) != 1 {
		t.Fatalf("err=%v calls=%v", err, stub.runs)
	}
	if got := u.d.recoveries.items[creationRecoveryKey(u.session.ID, 40)].State; got != "resolved" {
		t.Fatalf("debt state = %q for %s", got, p.Hash)
	}
}

func TestCreationDebtSelectsOnlyThisSessionsUnresolvedPresentations(t *testing.T) {
	d, _, _ := recoveryUnitFixture()
	d.initRecoveries()
	rows := []store.ReportRecovery{
		{Key: creationRecoveryKey(1, 40), SessionID: 1, TriggerID: 40, RunID: 9, State: "uncertain"},
		{Key: creationRecoveryKey(1, 41), SessionID: 1, TriggerID: 41, RunID: 10, State: "resolved"},
		{Key: creationRecoveryKey(2, 40), SessionID: 2, TriggerID: 40, RunID: 11, State: "pending"},
		{Key: recoveryKey(1, 42), SessionID: 1, TriggerID: 42, RunID: 12, State: "pending"},
		{Key: "stopped:" + recoveryKey(1, 43), SessionID: 1, TriggerID: 43, RunID: 13, State: "stopped"},
		{Key: creationRecoveryKey(1, 44), SessionID: 1, TriggerID: 44, State: "pending"},
	}
	for _, row := range rows {
		d.recoveries.items[row.Key] = row
	}
	debt := d.creationDebt(1)
	if len(debt) != 1 || debt[0].RunID != 9 {
		t.Fatalf("debt = %+v, want only the unresolved presentation of session 1", debt)
	}
}
