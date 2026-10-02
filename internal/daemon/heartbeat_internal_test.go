package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// A comment's fate is news once per change: a deferral repeats nothing, and a
// comment that moves on from a deferral to a dispatch is announced again.
func TestCommentFatesAnnounceOncePerChange(t *testing.T) {
	var f commentFates
	steps := []struct {
		fate     string
		announce bool
		first    bool
	}{
		{fateDeferred, true, true},
		{fateDeferred, false, false},
		{fateQueued, true, false},
		{fateQueued, false, false},
		{fateDeferred, false, false},
		{fateDispatched, true, false},
		{fateDispatched, false, false},
	}
	for i, s := range steps {
		announce, first := f.note(1, 42, s.fate)
		if announce != s.announce || first != s.first {
			t.Fatalf("step %d (%s): announce=%v first=%v, want %v %v", i, s.fate, announce, first, s.announce, s.first)
		}
	}
	if announce, first := f.note(2, 42, fateDispatched); !announce || !first {
		t.Fatal("the same comment id in another session is a different comment")
	}
}

func TestCommentFatesAreBounded(t *testing.T) {
	var f commentFates
	for i := int64(0); i < commentFateMemory+10; i++ {
		f.note(1, i, fateIgnored)
	}
	if len(f.last) > commentFateMemory || len(f.order) > commentFateMemory {
		t.Fatalf("memory grew to %d/%d", len(f.last), len(f.order))
	}
}

// The budget's read-only view names why discovery is deferred.
func TestDiscoveryViewDeferralReasons(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	reset := now.Add(30 * time.Minute)
	stale := discoveryView{Limit: 5000, Remaining: 4000, Reserve: 2500, Share: 2500, Reset: reset, now: now}
	if reason, at := stale.deferral(); reason != pauseReasonStale || !at.IsZero() {
		t.Fatalf("stale view=%q %v", reason, at)
	}
	spent := discoveryView{Limit: 5000, Remaining: 2500, Reserve: 2500, Share: 2500, Reset: reset, now: now}
	if reason, at := spent.deferral(); reason != pauseReasonSpent || !at.Equal(reset) {
		t.Fatalf("spent view=%q %v", reason, at)
	}
}

func TestPauseStateLogsTransitionsOnly(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	var p pauseState
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	p.leave(ctx, log, now)
	p.enter(ctx, log, now, pauseReasonStale, time.Time{})
	p.enter(ctx, log, now.Add(time.Minute), pauseReasonSpent, now.Add(time.Hour))
	if got := strings.Count(buf.String(), "discovery paused"); got != 1 {
		t.Fatalf("pause lines=%d\n%s", got, &buf)
	}
	if !strings.Contains(buf.String(), "when fresh rate-limit data arrives") || !strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("pause line=%s", &buf)
	}
	if snap := p.snapshot(); snap.Reason != pauseReasonSpent || !snap.Since.Equal(now) {
		t.Fatalf("snapshot=%+v", snap)
	}
	p.leave(ctx, log, now.Add(5*time.Minute))
	p.leave(ctx, log, now.Add(6*time.Minute))
	if got := strings.Count(buf.String(), "discovery resumed after 5m0s"); got != 1 || !strings.Contains(buf.String(), "paused_for_seconds=300") {
		t.Fatalf("resume lines=%d\n%s", got, &buf)
	}
}
