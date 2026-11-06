package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// linearStartFiling adds a labelled Linear filing with no session or alias.
func (fx *bugStartFixture) linearStartFiling(t *testing.T) store.BugFiling {
	t.Helper()
	ctx := t.Context()
	f, _, err := fx.db.AdmitBugFiling(ctx, store.BugFiling{ExecutionID: "owner-action:lin", OwnerActionID: "lin", OwnerID: "owner", OwnerGeneration: 1, RepositoryID: fx.repo.ID, RepositoryRef: "acme/api", Title: "T", Body: "B", Tracker: store.BugTrackerLinear, EffectKey: "effect-1", LabelID: "label-1", Destination: "team/project"}, fx.now)
	if err != nil {
		t.Fatal(err)
	}
	labelled := f
	labelled.State, labelled.IssueNodeID, labelled.IssueNumber, labelled.IssueLink = store.BugFilingLabelled, "uuid-1", 20, "https://linear.app/x/issue/ENG-20"
	if ok, err := fx.db.SaveBugFiling(ctx, labelled, f.State, fx.now); err != nil || !ok {
		t.Fatal(ok, err)
	}
	return labelled
}

func stubLinearSeams(t *testing.T, fx *bugStartFixture, bindErrs *[]error) *int {
	t.Helper()
	admit, bind := bugStartLinearAdmit, bugStartLinearBind
	t.Cleanup(func() { bugStartLinearAdmit, bugStartLinearBind = admit, bind })
	admits := 0
	bugStartLinearAdmit = func(d *Daemon, ctx context.Context, repo store.Repo, f store.BugFiling) (store.Session, ticket.Ref, error) {
		admits++
		s, err := d.db.GetOrCreateSession(ctx, repo, f.IssueNumber)
		return s, ticket.Ref{}, err
	}
	bugStartLinearBind = func(*Daemon, context.Context, int64, ticket.Ref) error {
		if len(*bindErrs) == 0 {
			return nil
		}
		err := (*bindErrs)[0]
		*bindErrs = (*bindErrs)[1:]
		return err
	}
	return &admits
}

func TestBugFilingStartLinearAdmitsOneSessionAndSavesIt(t *testing.T) {
	fx := newBugStartFixture(t)
	f := fx.linearStartFiling(t)
	stubLinearSeams(t, fx, &[]error{})
	if err := fx.d.stepBugFilingStart(t.Context(), f); err != nil {
		t.Fatal(err)
	}
	got, err := fx.db.ReadBugFiling(t.Context(), f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID == 0 || got.State != store.BugFilingLabelled || len(fx.handoff) != 1 {
		t.Fatalf("filing %+v handoffs %v", got, fx.handoff)
	}
	s, err := fx.db.GetSession(t.Context(), got.SessionID)
	if err != nil || s.IssueNumber != 20 {
		t.Fatalf("session %+v %v", s, err)
	}
}

func TestBugFilingStartLinearRecoversSingleSessionAfterInterruptedBind(t *testing.T) {
	fx := newBugStartFixture(t)
	f := fx.linearStartFiling(t)
	boom := errors.New("crash before bind")
	admits := stubLinearSeams(t, fx, &[]error{boom})
	if err := fx.d.stepBugFilingStart(t.Context(), f); !errors.Is(err, boom) {
		t.Fatalf("first pass: %v", err)
	}
	got, _ := fx.db.ReadBugFiling(t.Context(), f.ID)
	if got.SessionID != 0 || len(fx.handoff) != 0 {
		t.Fatalf("interrupted pass saved work: %+v %v", got, fx.handoff)
	}
	if err := fx.d.stepBugFilingStart(t.Context(), got); err != nil {
		t.Fatal(err)
	}
	after, _ := fx.db.ReadBugFiling(t.Context(), f.ID)
	if after.SessionID == 0 || *admits != 2 || len(fx.handoff) != 1 {
		t.Fatalf("filing %+v admits %d handoffs %v", after, *admits, fx.handoff)
	}
	again, err := fx.db.GetOrCreateSession(t.Context(), fx.repo, 20)
	if err != nil || again.ID != after.SessionID {
		t.Fatalf("session %d vs %d: %v", again.ID, after.SessionID, err)
	}
}

func TestBugFilingStartLinearSettlesAdmittedSession(t *testing.T) {
	fx := newBugStartFixture(t)
	f := fx.linearStartFiling(t)
	stubLinearSeams(t, fx, &[]error{})
	step := func() store.BugFiling {
		if err := fx.d.stepBugFilingStart(t.Context(), mustReadFiling(t, fx, f.ID)); err != nil {
			t.Fatal(err)
		}
		return mustReadFiling(t, fx, f.ID)
	}
	step()
	step()
	got := step()
	if len(fx.handoff) != 1 || got.State != store.BugFilingStartAdmitted || got.Reason != bugStartRecorded {
		t.Fatalf("filing %+v handoffs %v", got, fx.handoff)
	}
}

func mustReadFiling(t *testing.T, fx *bugStartFixture, id int64) store.BugFiling {
	t.Helper()
	f, err := fx.db.ReadBugFiling(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
