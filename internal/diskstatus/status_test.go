package diskstatus

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestObservationPreservesPartialAndCompleteIntervals(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	zero := uint64(0)
	complete := int64(42)
	o := Observation{Version: 1, ObservedAt: at, FreeBytes: &zero, GuardEnabled: false, Main: Cache{Path: "/main", State: "partial", PartialBytes: 7, EntriesExamined: 2, PassStartedAt: at, ProgressAt: at.Add(time.Minute), CompleteBytes: &complete, CompleteStartedAt: at.Add(-time.Hour), CompleteAt: at.Add(-time.Minute), CompleteStale: true}}
	raw, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.FreeBytes == nil || *got.FreeBytes != 0 || got.Main.PartialBytes != 7 || *got.Main.CompleteBytes != 42 || !got.Main.CompleteAt.Equal(o.Main.CompleteAt) {
		t.Fatalf("round trip: %+v", got)
	}
	text := Text(got, at.Add(20*time.Minute))
	for _, want := range []string{"free=0.00 GiB", "disabled (display only)", "observed partial logical bytes=7", "last complete approximate logical bytes=42", "(stale)", "(stale/interrupted)", "automatic_cleanup=unavailable"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
}
func TestUnavailableObservationDoesNotFabricateZero(t *testing.T) {
	t.Parallel()
	for _, raw := range [][]byte{nil, []byte(`{"version":2}`)} {
		got, err := Decode(raw)
		if err != nil || got != nil {
			t.Fatalf("decode=%+v %v", got, err)
		}
	}
	if _, err := Decode([]byte(`{`)); err == nil {
		t.Fatal("malformed JSON accepted")
	}
	text := Text(&Observation{Version: 1}, time.Now())
	if !strings.Contains(text, "free=unavailable") || !strings.Contains(text, "complete size unavailable") || strings.Contains(text, "free=0.00") {
		t.Fatal(text)
	}
}
