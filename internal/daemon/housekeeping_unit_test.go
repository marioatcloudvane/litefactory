package daemon

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func hkLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func TestHousekeeping_Run_Scheduling(t *testing.T) {
	t.Parallel()
	log, _ := hkLogger()
	h := NewHousekeeping(log)
	var runs []time.Time
	h.Register(HousekeepingStep{Name: "s", Run: func(_ context.Context, now time.Time) error {
		runs = append(runs, now)
		return nil
	}})
	t0 := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	ctx := context.Background()

	h.Run(ctx, t0)
	h.Run(ctx, t0.Add(time.Minute))
	h.Run(ctx, t0.Add(HousekeepingInterval-time.Nanosecond))
	if len(runs) != 1 {
		t.Fatalf("runs before 24h = %d, want 1 (first tick only)", len(runs))
	}
	h.Run(ctx, t0.Add(HousekeepingInterval))
	if len(runs) != 2 {
		t.Fatalf("runs at 24h = %d, want 2", len(runs))
	}
	h.Run(ctx, t0.Add(HousekeepingInterval+time.Hour))
	if len(runs) != 2 {
		t.Fatalf("next due must be 24h after the second run, runs=%d", len(runs))
	}
}

func TestHousekeeping_Run_FirstTickAfterRestartRuns(t *testing.T) {
	t.Parallel()
	log, _ := hkLogger()
	n := 0
	mk := func() *Housekeeping {
		h := NewHousekeeping(log)
		h.Register(HousekeepingStep{Name: "s", Run: func(context.Context, time.Time) error { n++; return nil }})
		return h
	}
	now := time.Now()
	mk().Run(context.Background(), now)
	mk().Run(context.Background(), now)
	if n != 2 {
		t.Fatalf("n = %d, want 2 (in-memory due time)", n)
	}
}

func TestHousekeeping_Run_StepFailureNeverBlocksLaterSteps(t *testing.T) {
	t.Parallel()
	log, buf := hkLogger()
	h := NewHousekeeping(log)
	var order []string
	h.Register(HousekeepingStep{Name: "one", Run: func(context.Context, time.Time) error { order = append(order, "one"); return nil }})
	h.Register(HousekeepingStep{Name: "two", Run: func(context.Context, time.Time) error { order = append(order, "two"); return errors.New("boom") }})
	h.Register(HousekeepingStep{Name: "three", Run: func(context.Context, time.Time) error { order = append(order, "three"); return nil }})

	h.Run(context.Background(), time.Now())

	if got := strings.Join(order, ","); got != "one,two,three" {
		t.Fatalf("order = %s", got)
	}
	out := buf.String()
	if !strings.Contains(out, "housekeeping step failed") || !strings.Contains(out, "step=two") || !strings.Contains(out, "boom") {
		t.Errorf("failure not logged: %s", out)
	}
}

func TestHousekeeping_Run_NoStepsOrCancelledCtxDoesNothing(t *testing.T) {
	t.Parallel()
	log, _ := hkLogger()
	h := NewHousekeeping(log)
	h.Run(context.Background(), time.Now()) // no steps: must not panic

	ran := false
	h.Register(HousekeepingStep{Name: "s", Run: func(context.Context, time.Time) error { ran = true; return nil }})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.Run(ctx, time.Now())
	if ran {
		t.Fatal("step ran with cancelled context")
	}
	// A cancelled tick must not consume the due time.
	h.Run(context.Background(), time.Now())
	if !ran {
		t.Fatal("step should run on next live tick")
	}
}

func TestNewHousekeeping_DefaultStepsAndOrder(t *testing.T) {
	t.Parallel()
	log, _ := hkLogger()
	names := func(h *Housekeeping) []string {
		var out []string
		for _, s := range h.steps {
			out = append(out, s.Name)
		}
		return out
	}
	tests := []struct {
		name string
		opts HousekeepingOptions
		want string
	}{
		{"all set/fixed order", HousekeepingOptions{DBPath: "/x/db", DeployBackupKeep: 5, LogsDir: "/x/logs", LogRetainDays: 30},
			"prune deploy backups,rotate global logs,delete old rotated logs"},
		{"no db path/skips backups", HousekeepingOptions{DeployBackupKeep: 5, LogsDir: "/x/logs", LogRetainDays: 30},
			"rotate global logs,delete old rotated logs"},
		{"zero keep/skips backups", HousekeepingOptions{DBPath: "/x/db", LogsDir: "/x/logs", LogRetainDays: 30},
			"rotate global logs,delete old rotated logs"},
		{"zero retain/skips logs", HousekeepingOptions{DBPath: "/x/db", DeployBackupKeep: 5, LogsDir: "/x/logs"},
			"prune deploy backups"},
		{"nothing set/no steps", HousekeepingOptions{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.Join(names(newHousekeeping(tt.opts, log)), ","); got != tt.want {
				t.Errorf("steps = %q, want %q", got, tt.want)
			}
		})
	}
}
