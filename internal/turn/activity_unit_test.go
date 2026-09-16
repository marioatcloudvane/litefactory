package turn

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

// Seed through the real reducer to exercise the process-local lifecycle facade
// without a receipt database. Bound update/get execution has separate I/O tests.
func activityUnitService(t *testing.T) (*ActivityService, time.Time) {
	t.Helper()
	at := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	state, disposition := agentresult.ReduceProgress(agentresult.ProgressState{}, agentresult.ProgressBinding{
		RunID: 1, Attempt: "activity-unit", Active: true,
	}, at, agentresult.AdvisoryProgress{Sequence: 7, Message: "checking recovery"})
	if disposition.Kind != "accepted" {
		t.Fatalf("seed disposition = %+v", disposition)
	}
	return &ActivityService{state: state, now: at}, at
}

func TestActivityService_LifecycleRetention(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name            string
		age             time.Duration
		terminal, clear bool
		status          string
	}{
		{"active beyond retention/available", 48 * time.Hour, false, false, "available"},
		{"before deadline/available", 24*time.Hour - time.Nanosecond, true, false, "available"},
		{"exact deadline/cleared", 24 * time.Hour, true, false, "cleared"},
		{"after deadline/cleared", 25 * time.Hour, true, false, "cleared"},
		{"explicit cleanup/cleared", time.Minute, false, true, "cleared"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, at := activityUnitService(t)
			if tt.terminal {
				if _, err := svc.Lifecycle(ActivityLifecycle{Now: at, Terminal: true}); err != nil {
					t.Fatal(err)
				}
			}
			got, err := svc.Lifecycle(ActivityLifecycle{Now: at.Add(tt.age), Clear: tt.clear})
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tt.status {
				t.Fatalf("status = %q, want %q", got.Status, tt.status)
			}
			if tt.status == "available" {
				if got.Latest == nil || got.Latest.Message != "checking recovery" || got.HighestSequence != 7 || got.AcceptedUpdates != 1 || got.RetainedIdentities != 1 {
					t.Fatalf("lost advisory state: %+v", got)
				}
			} else if got.Latest != nil {
				t.Fatalf("cleared view retains message: %+v", got)
			}
		})
	}
}

func TestActivityService_LifecycleDoesNotRenewOrReopen(t *testing.T) {
	t.Parallel()
	svc, at := activityUnitService(t)
	if _, err := svc.Lifecycle(ActivityLifecycle{Now: at, Terminal: true}); err != nil {
		t.Fatal(err)
	}
	for _, age := range []time.Duration{time.Hour, 23 * time.Hour} {
		got, err := svc.Lifecycle(ActivityLifecycle{Now: at.Add(age), Terminal: true})
		if err != nil {
			t.Fatal(err)
		}
		if got.TerminalAt == nil || got.TerminalAt.After(at) {
			t.Fatalf("terminal deadline renewed: %+v", got)
		}
	}
	got, err := svc.Lifecycle(ActivityLifecycle{Now: at.Add(24 * time.Hour)})
	if err != nil || got.Status != "cleared" {
		t.Fatalf("original deadline: %+v, %v", got, err)
	}
	for _, input := range []ActivityLifecycle{{Now: at}, {Now: at.Add(25 * time.Hour)}, {Now: at.Add(26 * time.Hour), Terminal: true}} {
		got, err = svc.Lifecycle(input)
		if err != nil || got.Status != "cleared" || got.Latest != nil || !svc.terminal {
			t.Fatalf("cleanup reopened attempt: %+v, %v", got, err)
		}
	}
}

func TestActivityService_LifecycleClonesViews(t *testing.T) {
	t.Parallel()
	svc, at := activityUnitService(t)
	got, err := svc.Lifecycle(ActivityLifecycle{Now: at, Terminal: true})
	if err != nil {
		t.Fatal(err)
	}
	got.Latest.Message = "completed and approved"
	got.Latest.Sequence = 999
	*got.TerminalAt = at.Add(72 * time.Hour)
	again, err := svc.Lifecycle(ActivityLifecycle{Now: at.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if again.Latest.Message != "checking recovery" || again.Latest.Sequence != 7 || !again.TerminalAt.Equal(at) {
		t.Fatalf("returned pointers mutated retained state: %+v", again)
	}
}

func TestActivityService_LifecycleRequiresTime(t *testing.T) {
	t.Parallel()
	svc, at := activityUnitService(t)
	before, err := svc.Lifecycle(ActivityLifecycle{Now: at})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Lifecycle(ActivityLifecycle{Terminal: true, Clear: true}); err == nil {
		t.Fatal("accepted missing factory time")
	}
	after, err := svc.Lifecycle(ActivityLifecycle{Now: at})
	if err != nil || !reflect.DeepEqual(before, after) || svc.terminal {
		t.Fatalf("invalid input changed lifecycle: %+v, %v", after, err)
	}
}

func TestActivityService_LifecycleConcurrentCopies(t *testing.T) {
	t.Parallel()
	svc, at := activityUnitService(t)
	if _, err := svc.Lifecycle(ActivityLifecycle{Now: at, Terminal: true}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			got, err := svc.Lifecycle(ActivityLifecycle{Now: at.Add(time.Duration(i) * time.Minute), Terminal: true})
			if err != nil {
				t.Error(err)
				return
			}
			if got.Latest == nil || got.Latest.Message != "checking recovery" || got.TerminalAt == nil || !got.TerminalAt.Equal(at) {
				t.Errorf("concurrent view = %+v", got)
				return
			}
			got.Latest.Message = "caller mutation"
			*got.TerminalAt = at.Add(48 * time.Hour)
		})
	}
	wg.Wait()
	got, err := svc.Lifecycle(ActivityLifecycle{Now: at.Add(24 * time.Hour)})
	if err != nil || got.Status != "cleared" {
		t.Fatalf("concurrent calls extended retention: %+v, %v", got, err)
	}
}

func TestActivityService_EmptyLifecycle(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	svc := &ActivityService{now: at}
	got, err := svc.Lifecycle(ActivityLifecycle{Now: at})
	if err != nil || got.Status != "unavailable" || got.Latest != nil {
		t.Fatalf("fresh process view = %+v, %v", got, err)
	}
	got, err = svc.Lifecycle(ActivityLifecycle{Now: at, Clear: true})
	if err != nil || got.Status != "cleared" || got.Latest != nil {
		t.Fatalf("explicit clear view = %+v, %v", got, err)
	}
}
