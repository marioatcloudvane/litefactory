package turn

import (
	"encoding/json"
	"errors"
	"github.com/cloudvane/litefactory/internal/store"
	"reflect"
	"strings"
	"testing"
)

func TestFamilyReceiptsDurableDispositionAndHistoricalReader(t *testing.T) {
	db, session, o := agentResultFixture(t, "research")
	app, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewReceiptService(db, app.Binding(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte(`{"message":"fixture-secret-permit-content"}`)
	for _, state := range []string{"pending", "rejected", "uncertain", "confirmed"} {
		if _, first, e := s.Begin(t.Context(), state, "feedback_preflight", secret); e != nil || !first {
			t.Fatalf("begin %s: %v", state, e)
		}
		if state != "pending" {
			if e := s.Finish(t.Context(), state, state, store.FamilyReference{}); e != nil {
				t.Fatal(e)
			}
		}
	}
	reopened, err := store.Open(t.Context(), db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	recovered, err := NewReceiptService(reopened, app.Binding(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"missing", "pending", "rejected", "uncertain", "confirmed"} {
		before, e := recovered.Get(t.Context(), state)
		if e != nil || before.Disposition != state {
			t.Fatalf("%s: %+v %v", state, before, e)
		}
		for range 3 {
			data, e := recovered.HandleGet(t.Context(), state, []byte(`{"request_id":"`+state+`"}`))
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(string(data), "fixture-secret") {
				t.Fatal("authored secret in receipt")
			}
		}
		after, e := recovered.Get(t.Context(), state)
		if e != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("lookup mutated outcome", e)
		}
	}
	if _, first, e := s.Begin(t.Context(), "missing", "feedback_preflight", secret); e != nil || !first {
		t.Fatal("missing lookup allocated recursive receipt", e)
	}
	old := s.scope
	o.Attempt = agentResultRun(t, db, session, "research", "current-reader", 2)
	current, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(t.Context(), "confirmed"); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatalf("revoked old reader: %v", err)
	}
	restricted, err := NewReceiptService(db, current.Binding(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, e := restricted.Get(t.Context(), "confirmed"); e != nil || got.Disposition != "missing" {
		t.Fatalf("ungranted old run leaked: %+v %v", got, e)
	}
	historical, err := NewReceiptService(db, current.Binding(), []store.FamilyScope{old}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := historical.Get(t.Context(), "confirmed")
	if err != nil || got.Disposition != "confirmed" {
		t.Fatalf("authorized history: %+v %v", got, err)
	}
	bytes, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bytes), "fixture-secret") {
		t.Fatal("historical secret leaked")
	}
	if _, _, err = s.Begin(t.Context(), "resurrected", "activity_update", secret); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatal("historical read restored old writer", err)
	}
	for _, mutate := range []func(*store.ResultBinding){func(b *store.ResultBinding) { b.RunID = old.RunID }, func(b *store.ResultBinding) { b.Attempt = old.Attempt }, func(b *store.ResultBinding) { b.Task = "github:foreign/repo#1" }} {
		wrong := current.Binding()
		mutate(&wrong)
		reader, e := NewReceiptService(db, wrong, nil, nil)
		if e != nil {
			continue
		}
		if _, e = reader.Get(t.Context(), "confirmed"); !errors.Is(e, store.ErrResultAuthority) {
			t.Fatal("mismatched current reader accepted", e)
		}
	}
}

func TestFamilyReceiptsUnderlyingResultSurvivesLostLedgerLink(t *testing.T) {
	db, _, o := agentResultFixture(t, "research")
	app, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewReceiptService(db, app.Binding(), nil, map[string]FamilyProjection{"tool:feedback_preflight": ResultReceiptProjection(db, app.Binding())})
	if err != nil {
		t.Fatal(err)
	}
	const id = "lost-ledger-link"
	if _, _, err = s.Begin(t.Context(), id, "feedback_preflight", resultCreate("research")); err != nil {
		t.Fatal(err)
	}
	submitted, err := app.Submit(t.Context(), id, resultCreate("research"))
	if err != nil || submitted.Receipt == nil {
		t.Fatal("underlying receipt missing", err)
	}
	// Simulate process failure after the authoritative owner committed and before
	// the generic ledger linked its result. Lookup must observe, never resubmit.
	before, err := db.ResultRevisionNumbers(t.Context(), app.Binding())
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		got, e := s.Get(t.Context(), id)
		if e != nil {
			t.Fatal(e)
		}
		if got.Disposition != "pending" || got.States.Input != "accepted" || got.States.Engineering != "pending" || got.States.Acceptance != "pending" || got.States.Effects != "pending" {
			t.Fatalf("distinct authoritative states lost: %+v", got)
		}
	}
	after, err := db.ResultRevisionNumbers(t.Context(), app.Binding())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("receipt lookup dispatched another application", err)
	}
}
