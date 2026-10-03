package store

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestGraphEffectPersistentCompetingClaimsAndImmutableHistory(t *testing.T) {
	db, path, instance, _ := persistentBugStopFixture(t)
	ctx := t.Context()
	at := graphTestTime()
	attempt, e := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: "repair", Limit: 3, StartedAt: at})
	if e != nil {
		t.Fatal(e)
	}
	intent := GraphEffect{Operation: "delivery:merge", InstanceID: instance.ID, SourceAttemptID: attempt.ID, Kind: "merge", Binding: json.RawMessage(`{"head":"immutable"}`), CreatedAt: at}
	if _, e = db.RetainGraphEffect(ctx, intent); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, e := db.ClaimGraphEffect(ctx, intent.Operation, attempt.ID, at); results <- e })
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("claims=%d", success)
	}
	if e = db.ObserveGraphEffect(ctx, intent.Operation, attempt.ID, "uncertain", json.RawMessage(`{"reason":"lost acknowledgment"}`), at); e != nil {
		t.Fatal(e)
	}
	db = reopenBugStopDB(t, db, path)
	retained, e := db.GetGraphEffect(ctx, intent.Operation)
	if e != nil {
		t.Fatal(e)
	}
	if retained.State != "uncertain" || retained.ClaimAttemptID != attempt.ID {
		t.Fatalf("retained=%+v", retained)
	}
	if _, e = db.ClaimGraphEffect(ctx, intent.Operation, attempt.ID, at); e == nil {
		t.Fatal("uncertain effect redispatched")
	}
	intent.Binding = json.RawMessage(`{"head":"different"}`)
	if _, e = db.RetainGraphEffect(ctx, intent); e == nil {
		t.Fatal("binding overwritten")
	}
	if e = db.ObserveGraphEffect(ctx, intent.Operation, attempt.ID, "confirmed", json.RawMessage(`{"merge":"observed"}`), at); e != nil {
		t.Fatal(e)
	}
	if e = db.ObserveGraphEffect(ctx, intent.Operation, attempt.ID, "intent", json.RawMessage(`{"missing":true}`), at); e == nil {
		t.Fatal("confirmed effect reset")
	}
	history, e := db.ListGraphOutputs(ctx, attempt.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(history) != 2 {
		t.Fatalf("history=%d", len(history))
	}
}
