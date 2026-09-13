package agentresult_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
)

func progressBinding() ar.ProgressBinding {
	return ar.ProgressBinding{RunID: 1, Attempt: "attempt-1", Active: true}
}
func progressAt() time.Time { return time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC) }
func TestProgress_OrderingReplayAndCoalescing(t *testing.T) {
	b := progressBinding()
	s, d := ar.ReduceProgress(ar.ProgressState{}, b, progressAt(), ar.AdvisoryProgress{Sequence: 5, Message: "started"})
	if d.Kind != "accepted" || d.RunID != b.RunID || d.Attempt != b.Attempt || d.Sequence != 5 {
		t.Fatal("accepted attribution missing")
	}
	cases := []struct {
		name       string
		update     ar.AdvisoryProgress
		kind, code string
		highest    int64
		count      int
	}{
		{"exact replay", ar.AdvisoryProgress{Sequence: 5, Message: "started"}, "replayed", "", 5, 1},
		{"changed replay", ar.AdvisoryProgress{Sequence: 5, Message: "changed"}, "rejected", "sequence_conflict", 5, 1},
		{"older unseen", ar.AdvisoryProgress{Sequence: 3, Message: "late"}, "stale", "", 5, 1},
		{"gap accepted", ar.AdvisoryProgress{Sequence: 100, Message: "latest"}, "accepted", "", 100, 2},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			next, disposition := ar.ReduceProgress(s, b, progressAt(), tt.update)
			view := next.View()
			if disposition.Kind != tt.kind || disposition.Code != tt.code || view.HighestSequence != tt.highest || view.AcceptedUpdates != tt.count {
				t.Fatalf("unexpected disposition/view: %+v %+v", disposition, view)
			}
			if s.View().Latest.Message != "started" {
				t.Fatal("reducer mutated baseline")
			}
		})
	}
}
func TestProgress_UTF8AndExactMessageBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name, message string
		valid         bool
	}{
		{"ASCII limit", strings.Repeat("a", 2048), true}, {"ASCII overflow", strings.Repeat("a", 2049), false},
		{"multibyte limit", strings.Repeat("é", 1024), true}, {"multibyte overflow", strings.Repeat("é", 1024) + "a", false},
		{"invalid UTF8", string([]byte{0xff}), false}, {"blank", " \t\n", false}, {"empty", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			state, d := ar.ReduceProgress(ar.ProgressState{}, progressBinding(), progressAt(), ar.AdvisoryProgress{Sequence: 1, Message: tt.message})
			if (d.Kind == "accepted") != tt.valid {
				t.Fatalf("boundary disposition %+v", d)
			}
			if !tt.valid && state.View().AcceptedUpdates != 0 {
				t.Fatal("rejected text consumed budget")
			}
		})
	}
	for _, seq := range []int64{0, -1} {
		_, d := ar.ReduceProgress(ar.ProgressState{}, progressBinding(), progressAt(), ar.AdvisoryProgress{Sequence: seq, Message: "text"})
		if d.Kind != "rejected" {
			t.Fatal("nonpositive sequence accepted")
		}
	}
}
func TestProgress_DedupEvictionAndTotalLimit(t *testing.T) {
	b := progressBinding()
	var state ar.ProgressState
	for i := 1; i <= ar.MaxProgressUpdates; i++ {
		var d ar.ProgressDisposition
		state, d = ar.ReduceProgress(state, b, progressAt(), ar.AdvisoryProgress{Sequence: int64(i), Message: fmt.Sprintf("status-%d", i)})
		if d.Kind != "accepted" {
			t.Fatalf("update %d rejected: %+v", i, d)
		}
		if state.View().RetainedIdentities > 32 {
			t.Fatal("unbounded identities")
		}
		if i == 32 && state.View().RetainedIdentities != 32 {
			t.Fatal("last allowed identity missing")
		}
		if i == 33 {
			unchanged, d := ar.ReduceProgress(state, b, progressAt(), ar.AdvisoryProgress{Sequence: 1, Message: "different evicted message"})
			if d.Kind != "stale" || unchanged.View().AcceptedUpdates != 33 {
				t.Fatal("evicted identity was retained or counted")
			}
			_, d = ar.ReduceProgress(state, b, progressAt(), ar.AdvisoryProgress{Sequence: 2, Message: "different retained message"})
			if d.Code != "sequence_conflict" {
				t.Fatal("first retained identity lost")
			}
		}
	}
	before := state.View()
	after, d := ar.ReduceProgress(state, b, progressAt(), ar.AdvisoryProgress{Sequence: 1025, Message: "over budget"})
	if d.Code != "update_limit" || !reflect.DeepEqual(before, after.View()) {
		t.Fatal("first excessive update changed state")
	}
	replay, d := ar.ReduceProgress(state, b, progressAt(), ar.AdvisoryProgress{Sequence: 1024, Message: "status-1024"})
	if d.Kind != "replayed" || replay.View().AcceptedUpdates != 1024 {
		t.Fatal("exact replay consumed total budget")
	}
	stale, d := ar.ReduceProgress(state, b, progressAt(), ar.AdvisoryProgress{Sequence: 1, Message: "status-1"})
	if d.Kind != "stale" || stale.View().AcceptedUpdates != 1024 {
		t.Fatal("evicted replay consumed budget")
	}
	if state.View().Latest.Message != "status-1024" || state.View().RetainedIdentities != 32 {
		t.Fatal("latest coalescing did not remain bounded")
	}
}
func TestProgress_RevocationFreshAttemptAndCopyIsolation(t *testing.T) {
	b := progressBinding()
	state, _ := ar.ReduceProgress(ar.ProgressState{}, b, progressAt(), ar.AdvisoryProgress{Sequence: 99, Message: "prior"})
	before := state.View()
	revoked := b
	revoked.Active = false
	next, d := ar.ReduceProgress(state, revoked, progressAt(), ar.AdvisoryProgress{Sequence: 100, Message: "forbidden"})
	if d.Code != "authority" || !reflect.DeepEqual(before, next.View()) {
		t.Fatal("revoked writer changed progress")
	}
	fresh := b
	fresh.RunID = 2
	fresh.Attempt = "attempt-2"
	reset, d := ar.ReduceProgress(state, fresh, progressAt(), ar.AdvisoryProgress{Sequence: 1, Message: "fresh"})
	if d.Kind != "accepted" || reset.View().AcceptedUpdates != 1 || reset.View().HighestSequence != 1 || reset.View().RunID != 2 {
		t.Fatal("fresh attempt did not reset ordering")
	}
	view := reset.View()
	view.Latest.Message = "mutated"
	if reset.View().Latest.Message != "fresh" || state.View().Latest.Message != "prior" {
		t.Fatal("view/state aliasing")
	}
	// Advisory processing has no result argument or writer. Existing result values
	// remain independently owned and cannot acquire authoritative state from text.
	snapshot, c := updateSnapshot("research", "incomplete")
	original := authored(t, snapshot.Result)
	_, d = ar.ReduceProgress(reset, fresh, progressAt(), ar.AdvisoryProgress{Sequence: 2, Message: "completed accepted published"})
	if d.Kind != "accepted" || snapshot.Meta.ResultRevision != 1 || !reflect.DeepEqual(original, authored(t, snapshot.Result)) || !ar.ValidateResult(snapshot.Result, c).Valid {
		t.Fatal("advisory text changed result authority")
	}
}
func TestProgress_TerminalExpiryAndExplicitCleanup(t *testing.T) {
	b := progressBinding()
	at := progressAt()
	state, _ := ar.ReduceProgress(ar.ProgressState{}, b, at, ar.AdvisoryProgress{Sequence: 1, Message: "last"})
	terminal := ar.MarkProgressTerminal(state, at)
	if state.View().TerminalAt != nil || terminal.View().TerminalAt == nil {
		t.Fatal("termination alias or absent marker")
	}
	again := ar.MarkProgressTerminal(terminal, at.Add(time.Hour))
	if !again.View().TerminalAt.Equal(at) {
		t.Fatal("repeated marking extended retention")
	}
	for _, tt := range []struct {
		name  string
		now   time.Time
		clear bool
		empty bool
	}{{"before", at.Add(24*time.Hour - time.Nanosecond), false, false}, {"exact", at.Add(24 * time.Hour), false, true}, {"after", at.Add(25 * time.Hour), false, true}, {"explicit", at.Add(time.Minute), true, true}} {
		t.Run(tt.name, func(t *testing.T) {
			got := ar.CleanTerminalProgress(terminal, tt.now, tt.clear)
			if (got.View().Latest == nil) != tt.empty {
				t.Fatal("terminal retention boundary failed")
			}
		})
	}
	if got := ar.CleanTerminalProgress(state, at.Add(48*time.Hour), false); got.View().Latest == nil {
		t.Fatal("active progress expired as terminal")
	}
	if got := ar.CleanTerminalProgress(state, at, true); got.View().Latest != nil {
		t.Fatal("explicit cleanup failed")
	}
	_, d := ar.ReduceProgress(terminal, b, at.Add(time.Hour), ar.AdvisoryProgress{Sequence: 2, Message: "after termination"})
	if d.Code != "terminal" {
		t.Fatal("terminal attempt accepted new content")
	}
	expired, d := ar.ReduceProgress(terminal, b, at.Add(24*time.Hour), ar.AdvisoryProgress{Sequence: 2, Message: "after expiry"})
	if d.Code != "terminal" || expired.View().Latest != nil {
		t.Fatal("reducer extended expired retention")
	}
	view := terminal.View()
	*view.TerminalAt = at.Add(time.Hour)
	if !terminal.View().TerminalAt.Equal(at) {
		t.Fatal("terminal time aliases view")
	}
}
