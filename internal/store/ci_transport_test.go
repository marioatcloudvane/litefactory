package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCITransportSerializesIndependentConnections(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	file := filepath.Join(t.TempDir(), "transport.db")
	first, err := Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := first.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = first.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Error(err)
		}
	})
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	_, ok, err := first.ClaimCITransport(ctx, "origin", "one", now, now.Add(3*time.Minute))
	if err != nil || !ok {
		t.Fatalf("first claim=%v err=%v", ok, err)
	}
	state, ok, err := second.ClaimCITransport(ctx, "origin", "two", now, now.Add(3*time.Minute))
	if err != nil || ok || state.Lease != "one" {
		t.Fatalf("concurrent claim=%+v %v err=%v", state, ok, err)
	}
	state, ok, err = second.ClaimCITransport(ctx, "origin", "two", now.Add(4*time.Minute), now.Add(7*time.Minute))
	if err != nil || ok || state.Lease != "one" {
		t.Fatalf("expired orphan admitted: state=%+v ok=%v err=%v", state, ok, err)
	}
	if _, err = second.FinishCITransport(ctx, "origin", "two", now, "", false); err == nil {
		t.Fatal("nonholder wrote transport state")
	}
	bound := now.Add(4 * time.Hour)
	if _, err = first.FinishCITransport(ctx, "origin", "one", bound, "retry_after", true); err != nil {
		t.Fatal(err)
	}
	state, ok, err = second.ClaimCITransport(ctx, "origin", "two", now.Add(2*time.Hour), now.Add(2*time.Hour+3*time.Minute))
	if err != nil || ok || !state.NotBefore.Equal(bound) {
		t.Fatalf("durable wait=%+v %v err=%v", state, ok, err)
	}
	_, ok, err = second.ClaimCITransport(ctx, "origin", "two", bound, bound.Add(3*time.Minute))
	if err != nil || !ok {
		t.Fatalf("expired wait=%v err=%v", ok, err)
	}
	state, err = second.FinishCITransport(ctx, "origin", "two", now, "older", false)
	if err != nil || !state.NotBefore.Equal(bound) {
		t.Fatalf("wait shortened=%+v err=%v", state, err)
	}
	ledger, err := first.ReadAlerts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ledger.Revision != 0 {
		t.Fatalf("transport changed alert ledger revision=%d", ledger.Revision)
	}
}
