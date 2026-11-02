package alerts

import (
	"context"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

type projectionWait struct{ until time.Time }

func (w projectionWait) CITransport(context.Context, string) (store.CITransportState, error) {
	return store.CITransportState{NotBefore: w.until}, nil
}
func TestCIProjectionSeparateReadinessAndTransport(t *testing.T) {
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	l := store.AlertLedger{CI: store.CIObservation{Repository: "private/factory", Branch: "main", Workflow: ".github/workflows/check.yml", Conclusion: "failure", Ready: true, Origin: "https://api.github.com", LastSuccess: at, NextDue: at.Add(5 * time.Minute)}, Observers: map[string]store.AlertObserver{"main_red": {Ready: true, LastSuccess: at}, "ci_monitoring_unavailable": {Ready: true, LastSuccess: at}}}
	wait := projectionWait{at.Add(3 * time.Hour)}
	hidden := ProjectLocal(t.Context(), l, at, func(string) bool { return false }, wait)
	if hidden.CI != nil {
		t.Fatal("target leaked outside repository grant")
	}
	p := ProjectLocal(t.Context(), l, at, func(task string) bool { return task == "github:private/factory#0" }, wait)
	if p.CI == nil || p.CI.Ready || p.CI.Conclusion != "failure" || p.CI.NextRetry == nil || !p.CI.NextRetry.Equal(wait.until) {
		t.Fatalf("CI projection=%+v", p.CI)
	}
	for _, rule := range p.Rules {
		if (rule.Rule == "main_red" || rule.Rule == "ci_monitoring_unavailable") && (rule.Ready || rule.Reason != "rate_limited") {
			t.Fatalf("wait lost from rule: %+v", rule)
		}
	}
	if !l.CI.Ready || l.CI.Conclusion != "failure" {
		t.Fatal("status modified accepted evidence")
	}
	p = Project(l, at.Add(8*time.Minute), func(string) bool { return true })
	if p.CI == nil || p.CI.Ready || p.CI.Reason != "observation_stale" {
		t.Fatalf("stale CI=%+v", p.CI)
	}
}
