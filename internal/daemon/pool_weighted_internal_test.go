package daemon

import (
	"context"
	"sync"
	"testing"
)

func TestWeightedPoolAtomicAcquisitionAndIdempotentRelease(t *testing.T) {
	p := newPool(7)
	slots := &runSlots{}
	slots.bind(p)
	first, ok, err := slots.TryAcquireWeighted(t.Context(), 3)
	if err != nil || !ok {
		t.Fatalf("reserve three: %v %v", ok, err)
	}
	if _, ok, err := slots.TryAcquireWeighted(t.Context(), 5); err != nil || ok {
		t.Fatalf("partial reservation must not succeed: %v %v", ok, err)
	}
	if used, total := slots.Capacity(); used != 3 || total != 7 {
		t.Fatalf("capacity=%d/%d", used, total)
	}
	first()
	first()
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				release, ok, err := slots.TryAcquireWeighted(t.Context(), 3)
				if err != nil {
					t.Error(err)
					return
				}
				if !ok {
					continue
				}
				if used, total := slots.Capacity(); used > total {
					t.Errorf("capacity exceeded: %d/%d", used, total)
				}
				release()
				release()
			}
		}()
	}
	wg.Wait()
	p.wait()
	if used, _ := p.capacity(); used != 0 {
		t.Fatalf("leaked %d slots", used)
	}
}

func TestStepReservationReplacesCostWithoutChangingWorkerJoin(t *testing.T) {
	p := newPool(3)
	if !p.try() {
		t.Fatal("legacy admission")
	}
	r := &stepReservation{pool: p, cost: 1}
	if ok, err := r.Replace(t.Context(), 3); err != nil || !ok {
		t.Fatalf("1 to 3: %v %v", ok, err)
	}
	if used, _ := p.capacity(); used != 3 {
		t.Fatalf("used=%d", used)
	}
	r.Release()
	if ok, err := r.Replace(t.Context(), 1); err != nil || !ok {
		t.Fatalf("3 to 1: %v %v", ok, err)
	}
	if used, _ := p.capacity(); used != 1 {
		t.Fatalf("used=%d", used)
	}
	r.Release()
	r.Release()
	p.wg.Done()
	p.wait()
}

func TestStepReservationAbandonedLaunchAndCancellation(t *testing.T) {
	p := newPool(3)
	if !p.acquire(2) {
		t.Fatal("reserve abandoned launch")
	}
	p.wg.Add(1)
	r := &stepReservation{pool: p, cost: 2}
	r.abandon()
	p.wait()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if ok, err := r.Replace(ctx, 2); ok || err == nil {
		t.Fatalf("cancelled admission: %v %v", ok, err)
	}
	if used, _ := p.capacity(); used != 0 {
		t.Fatalf("abandoned launch leaked %d", used)
	}
}

func TestCoordinatorReservationReplacementFallsBackToSharedPool(t *testing.T) {
	reserved, shared := newPool(1), newPool(3)
	if !reserved.try() {
		t.Fatal("reserved worker")
	}
	reservation := &stepReservation{pool: reserved, fallback: shared, cost: 1}
	if ok, err := reservation.Replace(t.Context(), 2); err != nil || !ok {
		t.Fatalf("replace at full shared cost: %v %v", ok, err)
	}
	if used, _ := reserved.capacity(); used != 0 {
		t.Fatalf("old reserved cost leaked=%d", used)
	}
	if used, _ := shared.capacity(); used != 2 {
		t.Fatalf("shared cost=%d", used)
	}
	reservation.Release()
	reserved.wg.Done()
	reserved.wait()
}

// Borrowed member slots leave the held grant alone, return idempotently, and
// come back with the whole reservation; a late member release after that is
// not a second free.
func TestStepReservationGrowBorrowsAndReleasesWithoutLeak(t *testing.T) {
	p := newPool(4)
	if !p.acquire(2) {
		t.Fatal("seed other work")
	}
	r := &stepReservation{pool: p}
	if ok, err := r.Replace(t.Context(), 1); err != nil || !ok {
		t.Fatalf("minimum: %v %v", ok, err)
	}
	extra, ok, err := r.TryGrow(t.Context(), 1)
	if err != nil || !ok {
		t.Fatalf("grow into the last free slot: %v %v", ok, err)
	}
	if _, ok, err := r.TryGrow(t.Context(), 1); err != nil || ok {
		t.Fatalf("grow past the pool total: %v %v", ok, err)
	}
	if used, _ := p.capacity(); used != 4 {
		t.Fatalf("used=%d after a refused grow", used)
	}
	extra()
	extra()
	if used, _ := p.capacity(); used != 3 {
		t.Fatalf("borrowed release: used=%d", used)
	}
	late, ok, err := r.TryGrow(t.Context(), 1)
	if err != nil || !ok {
		t.Fatalf("regrow: %v %v", ok, err)
	}
	r.Release()
	if used, _ := p.capacity(); used != 2 {
		t.Fatalf("whole release kept borrowed slots: used=%d", used)
	}
	late()
	r.Release()
	if used, _ := p.capacity(); used != 2 {
		t.Fatalf("late member release freed another worker's slot: used=%d", used)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, ok, err := r.TryGrow(ctx, 1); err == nil || ok {
		t.Fatalf("cancelled grow: %v %v", ok, err)
	}
}
