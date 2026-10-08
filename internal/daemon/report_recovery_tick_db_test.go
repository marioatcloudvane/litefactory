package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type observedPendingReporter struct {
	daemon.Reporter
	inner *turn.Reporter
	calls atomic.Int64
}

func (r *observedPendingReporter) ReportPending(ctx context.Context, sessionID, triggerID int64) (turn.Posted, bool, error) {
	if triggerID == 40 {
		r.calls.Add(1)
	}
	return r.inner.ReportPending(ctx, sessionID, triggerID)
}

func TestReportRecoverySQLiteTickPreservesOrderingAndOtherTaskCapacity(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(fmt.Sprintf("confirmed_%t", confirmed), func(t *testing.T) {
			f := newFixture(t)
			f.daemon = f.newDaemon(daemon.Options{MaxConcurrentRuns: 1})
			f.plantCursor()
			f.issue("Blocked report")
			f.forge.addIssue(fakeIssue{Number: 124, Title: "Unrelated work", Body: "No instruction", User: stranger()})
			f.settle()
			f.settle()
			session := f.session()
			f.forge.addComment(testIssue, 40, owner(), "/spike old request")
			f.forge.addComment(testIssue, 41, owner(), "/spike following request")
			marker := "<!-- lf:delivery v=1 key=durable-test -->"
			delivery, err := f.db.EnsureReportDelivery(t.Context(), store.ReportDelivery{Key: "durable-test", Version: 1, SessionID: session.ID, TriggerID: 40, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "comment", Purpose: "reply", BotUserID: botID, Body: "Retained explanation\n\n" + marker, Marker: marker})
			if err != nil {
				t.Fatal(err)
			}
			if claimed, err := f.db.MarkReportDeliverySent(t.Context(), delivery.Key); err != nil || !claimed {
				t.Fatalf("dispatch boundary: %v %v", claimed, err)
			}
			state := "uncertain"
			if confirmed {
				if _, err := f.db.ConfirmReportDelivery(t.Context(), delivery.Key, 900); err != nil {
					t.Fatal(err)
				}
				state = "confirmed"
			}
			tickAt := f.clock.Now()
			due := tickAt.Add(testInterval)
			key := fmt.Sprintf("source:%d:40", session.ID)
			if err := f.db.PutReportRecovery(t.Context(), store.ReportRecovery{Key: key, SessionID: session.ID, TriggerID: 40, DeliveryKey: delivery.Key, Task: session.TaskRef, NextAt: due, State: state, Reason: "prior delivery incomplete"}); err != nil {
				t.Fatal(err)
			}
			// Reconstruct daemon to prove it reads eligibility before replaying a source.
			observed := &observedPendingReporter{Reporter: f.reporter, inner: f.reporter}
			f.daemon = f.newDaemon(daemon.Options{MaxConcurrentRuns: 1, Reporter: observed, Now: func() time.Time { return tickAt }})
			_, beforeLists, beforePosts, _ := f.forge.counts()
			for i := 0; i < 3; i++ {
				f.settle()
			}
			_, afterLists, afterPosts, _ := f.forge.counts()
			if observed.calls.Load() != 0 || afterPosts != beforePosts || f.watermark() >= 40 || len(f.turns.calls()) != 0 {
				t.Fatalf("ineligible effects: list=%d/%d post=%d/%d watermark=%d calls=%d", beforeLists, afterLists, beforePosts, afterPosts, f.watermark(), len(f.turns.calls()))
			}
			f.forge.addComment(124, 50, owner(), "/spike unrelated")
			f.settle()
			f.settle()
			if !slices.Equal(f.turns.triggers(), []int64{50}) || f.watermark() >= 40 {
				t.Fatalf("blocked source monopolized capacity: triggers=%v watermark=%d", f.turns.triggers(), f.watermark())
			}
			tickAt = due
			_, beforeLists, beforePosts, _ = f.forge.counts()
			f.settle()
			_, afterLists, afterPosts, _ = f.forge.counts()
			if confirmed {
				// Saved receipt is trusted even when the comment is absent remotely.
				if f.watermark() < 40 || observed.calls.Load() != 1 {
					t.Fatalf("confirmed receipt used lookup or stayed blocked: lists=%d/%d watermark=%d", beforeLists, afterLists, f.watermark())
				}
			} else {
				if afterLists <= beforeLists || afterPosts != beforePosts || f.watermark() >= 40 || slices.Contains(f.turns.triggers(), int64(41)) {
					t.Fatal("empty lookup resent, consumed, or advanced the ordered queue")
				}
				row, err := f.db.GetReportRecovery(t.Context(), key)
				if err != nil || row.Attempts != 1 || !row.NextAt.Equal(due.Add(2*testInterval)) {
					t.Fatalf("eligible recovery did not persist cadence: %+v %v", row, err)
				}
				f.forge.addComment(testIssue, 900, bot(), delivery.Body)
				for i := 0; i < 2; i++ {
					f.settle()
				}
				if f.watermark() >= 40 {
					t.Fatal("trusted evidence bypassed due time")
				}
				tickAt = row.NextAt
				f.settle()
			}
			f.settle()
			f.settle()
			row, err := f.db.GetReportRecovery(t.Context(), key)
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("resolved recovery retained: %+v %v", row, err)
			}
			saved, err := f.db.GetReportDelivery(t.Context(), delivery.Key)
			if err != nil || !saved.Completed || saved.CommentID != 900 {
				t.Fatalf("delivery not completed from receipt: %+v %v", saved, err)
			}
			if slices.Contains(f.turns.triggers(), int64(40)) || !slices.Contains(f.turns.triggers(), int64(41)) {
				t.Fatalf("original relaunched or following source stayed blocked: %v", f.turns.triggers())
			}
		})
	}
}
