package store

import (
	"errors"
	"testing"
	"time"
)

func TestSpikeRecordsAreKeyedByInstanceNodeAndKind(t *testing.T) {
	db := newTestDB(t)
	first := graphTestInstance(t, db)
	at := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	if _, err := db.GetSpikeRecord(t.Context(), first.ID, "gate", SpikeRecordBaseline); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absent record: %v", err)
	}
	put := func(instance int64, node, kind, payload string, when time.Time) {
		t.Helper()
		if err := db.PutSpikeRecord(t.Context(), SpikeRecord{InstanceID: instance, Node: node, Kind: kind, Payload: []byte(payload), UpdatedAt: when}); err != nil {
			t.Fatal(err)
		}
	}
	put(first.ID, "gate", SpikeRecordBaseline, `{"commit":"a"}`, at)
	put(first.ID, "check", SpikeRecordCheck, `{"outcome":"passed"}`, at)
	put(first.ID, "check", SpikeRecordCheck, `{"outcome":"failed"}`, at.Add(time.Minute))
	got, err := db.GetSpikeRecord(t.Context(), first.ID, "check", SpikeRecordCheck)
	if err != nil || string(got.Payload) != `{"outcome":"failed"}` {
		t.Fatalf("replay must replace its own record: %+v %v", got, err)
	}
	put(first.ID, "other", SpikeRecordCheck, `{"outcome":"too_big"}`, at.Add(2*time.Minute))
	latest, err := db.LatestSpikeRecord(t.Context(), first.ID, SpikeRecordCheck)
	if err != nil || latest.Node != "other" {
		t.Fatalf("latest = %+v %v", latest, err)
	}
	if baseline, err := db.LatestSpikeRecord(t.Context(), first.ID, SpikeRecordBaseline); err != nil || baseline.Node != "gate" {
		t.Fatalf("kinds are isolated: %+v %v", baseline, err)
	}
	second, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: first.SessionID, DefinitionID: first.DefinitionID, CurrentNode: "work", StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.LatestSpikeRecord(t.Context(), second.ID, SpikeRecordCheck); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another instance saw the record: %v", err)
	}
	for name, in := range map[string]SpikeRecord{
		"no instance": {Node: "n", Kind: SpikeRecordCheck, Payload: []byte("x"), UpdatedAt: at},
		"no node":     {InstanceID: first.ID, Kind: SpikeRecordCheck, Payload: []byte("x"), UpdatedAt: at},
		"bad kind":    {InstanceID: first.ID, Node: "n", Kind: "other", Payload: []byte("x"), UpdatedAt: at},
		"no payload":  {InstanceID: first.ID, Node: "n", Kind: SpikeRecordCheck, UpdatedAt: at},
		"no time":     {InstanceID: first.ID, Node: "n", Kind: SpikeRecordCheck, Payload: []byte("x")},
		"too large":   {InstanceID: first.ID, Node: "n", Kind: SpikeRecordCheck, Payload: make([]byte, MaxSpikeRecordBytes+1), UpdatedAt: at},
	} {
		if err := db.PutSpikeRecord(t.Context(), in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
