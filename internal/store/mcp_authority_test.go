package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func mcpLeaseFixture(t *testing.T) (*DB, MCPLease) {
	t.Helper()
	db := newTestDB(t)
	s := testSession(t, db, 901)
	b, err := db.BindResultAttempt(t.Context(), resultRun(t, db, s, "research", "cold", 1))
	if err != nil {
		t.Fatal(err)
	}
	l := MCPLease{Binding: b, Instance: strings.Repeat("a", 64), PermissionDigest: strings.Repeat("b", 64), ExpiresAt: time.Now().Add(time.Hour)}
	if err = db.StartMCPInstance(t.Context(), l.Instance); err != nil {
		t.Fatal(err)
	}
	if err = db.BindMCPLease(t.Context(), l); err != nil {
		t.Fatal(err)
	}
	return db, l
}

func TestMCPLease_AllResultOperationsRefuseDeadAuthority(t *testing.T) {
	for _, state := range []string{"active", "replaced", "cancelled", "closed", "expired", "restart", "ended"} {
		t.Run(state, func(t *testing.T) {
			db, l := mcpLeaseFixture(t)
			b := l.Binding
			record := resultRecord(b, "original", 0)
			if _, err := db.RecordResult(WithMCPLease(t.Context(), l), record); err != nil {
				t.Fatal(err)
			}
			switch state {
			case "replaced", "cancelled", "closed":
				if err := db.RevokeMCPLease(t.Context(), l, state); err != nil {
					t.Fatal(err)
				}
			case "expired":
				l.ExpiresAt = time.Now().Add(-time.Second)
				if _, err := db.sql.ExecContext(t.Context(), `UPDATE mcp_attempt_leases SET expires_ns=?`, l.ExpiresAt.UnixNano()); err != nil {
					t.Fatal(err)
				}
			case "restart":
				if err := db.StartMCPInstance(t.Context(), strings.Repeat("c", 64)); err != nil {
					t.Fatal(err)
				}
			case "ended":
				if err := db.EndResultAttempt(t.Context(), b); err != nil {
					t.Fatal(err)
				}
			}
			before := resultCounts(t, db)
			operations := map[string]func(context.Context) error{
				"discovery_guard":   func(ctx context.Context) error { return db.CheckMCPLease(ctx, l) },
				"preflight":         func(ctx context.Context) error { return db.CheckResultWriter(ctx, b) },
				"historical_reader": func(ctx context.Context) error { return db.CheckResultReader(ctx, b) },
				"receipt": func(ctx context.Context) error {
					_, err := db.GetResultReceipt(ctx, b, b.RunID, "original")
					return err
				},
				"revision": func(ctx context.Context) error { _, err := db.GetResultRevision(ctx, b, 1); return err },
				"status":   func(ctx context.Context) error { _, err := db.ResultRevisionNumbers(ctx, b); return err },
				"replay": func(ctx context.Context) error {
					_, err := db.ReplayResultRequest(ctx, b, "original", record.Payload)
					return err
				},
				"duplicate_submit": func(ctx context.Context) error { _, err := db.RecordResult(ctx, record); return err },
			}
			for name, operation := range operations {
				t.Run(name, func(t *testing.T) {
					err := operation(WithMCPLease(t.Context(), l))
					if state == "active" {
						if err != nil {
							t.Fatal(err)
						}
					} else if !errors.Is(err, ErrResultAuthority) {
						t.Fatalf("dead authority: %v", err)
					}
				})
			}
			if state != "active" {
				if _, err := db.RecordResult(WithMCPLease(t.Context(), l), resultRecord(b, "new", 1)); !errors.Is(err, ErrResultAuthority) {
					t.Fatalf("mutation admitted: %v", err)
				}
			}
			if resultCounts(t, db) != before {
				t.Fatal("refusal/replay changed durable history")
			}
			if _, err := db.GetResultReceipt(t.Context(), b, b.RunID, "original"); err != nil {
				t.Fatalf("internal history revoked: %v", err)
			}
		})
	}
}

func TestMCPLease_ExactExpiryAndImmutableSelectors(t *testing.T) {
	db, l := mcpLeaseFixture(t)
	for _, delta := range []time.Duration{-time.Nanosecond, 0, time.Nanosecond} {
		err := db.withTx(t.Context(), func(tx *sql.Tx) error { return checkMCPLease(t.Context(), tx, l, l.ExpiresAt.Add(delta)) })
		if delta < 0 && err != nil {
			t.Fatal(err)
		}
		if delta >= 0 && !errors.Is(err, ErrResultAuthority) {
			t.Fatalf("expiry delta %s: %v", delta, err)
		}
	}
	for _, field := range []string{"task", "mode", "scope", "run", "attempt", "result", "permission", "instance", "expiry"} {
		t.Run(field, func(t *testing.T) {
			bad := l
			switch field {
			case "task":
				bad.Binding.Task = "github:other/repo#1"
			case "mode":
				bad.Binding.Mode = "implement"
			case "scope":
				bad.Binding.ContextHash = strings.Repeat("d", 64)
			case "run":
				bad.Binding.RunID++
			case "attempt":
				bad.Binding.Attempt = "foreign"
			case "result":
				bad.Binding.ResultID++
			case "permission":
				bad.PermissionDigest = strings.Repeat("e", 64)
			case "instance":
				bad.Instance = strings.Repeat("f", 64)
			case "expiry":
				bad.ExpiresAt = bad.ExpiresAt.Add(time.Second)
			}
			if err := db.CheckMCPLease(t.Context(), bad); !errors.Is(err, ErrResultAuthority) {
				t.Fatalf("forged selector admitted: %v", err)
			}
			if err := db.CheckResultWriter(WithMCPLease(t.Context(), l), bad.Binding); bad.Binding != l.Binding && !errors.Is(err, ErrResultAuthority) {
				t.Fatalf("context scope overridden: %v", err)
			}
		})
	}
}
