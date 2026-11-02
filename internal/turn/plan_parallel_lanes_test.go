package turn

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

var parallelLaneRoles = []string{"story", "technical", "acceptance"}

func unitCost(string) int { return 1 }

// laneFixture is a total-4 pool with some slots held elsewhere and the group
// admitted on its one-slot minimum, as the daemon admits it.
func laneFixture(t *testing.T, free int) (*slotStoryPool, *slotStoryParent, func()) {
	t.Helper()
	pool := &slotStoryPool{total: 4}
	var other func()
	if held := pool.total - free; held > 0 {
		var ok bool
		var err error
		if other, ok, err = pool.acquire(held); err != nil || !ok {
			t.Fatal(err)
		}
	}
	parent := &slotStoryParent{pool: pool}
	if ok, err := parent.Replace(t.Context(), 1); err != nil || !ok {
		t.Fatalf("minimum refused with %d free: %v", free, err)
	}
	return pool, parent, func() {
		if other != nil {
			other()
		}
	}
}

type laneProbe struct {
	mu           sync.Mutex
	active, peak int
	ran          []string
}

func (p *laneProbe) enter(role string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active++
	p.peak = max(p.peak, p.active)
	p.ran = append(p.ran, role)
}

func (p *laneProbe) leave() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active--
}

func assertPoolBounded(t *testing.T, pool *slotStoryPool) {
	t.Helper()
	pool.mu.Lock()
	defer pool.mu.Unlock()
	for _, used := range pool.observed {
		if used > pool.total {
			t.Fatalf("pool overbooked: %d > %d", used, pool.total)
		}
	}
}

func TestParallelLanesOneFreeSlotRunsMembersSequentially(t *testing.T) {
	pool, parent, done := laneFixture(t, 1)
	defer done()
	probe := &laneProbe{}
	ctx := WithStepCapacity(t.Context(), parent)
	err := runParallelMembers(ctx, newParallelLanes(ctx, 1, 4), parallelLaneRoles, unitCost, func(_ context.Context, role string) error {
		probe.enter(role)
		defer probe.leave()
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if probe.peak != 1 || !slices.Equal(probe.ran, parallelLaneRoles) {
		t.Fatalf("peak=%d ran=%v", probe.peak, probe.ran)
	}
	if used, _ := pool.Capacity(); used != pool.total {
		t.Fatalf("members kept borrowed slots: used=%d", used)
	}
	assertPoolBounded(t, pool)
	parent.Release()
	if used, _ := pool.Capacity(); used != pool.total-1 {
		t.Fatalf("group release leaked: used=%d", used)
	}
}

func TestParallelLanesTwoFreeSlotsRunAtMostTwoMembers(t *testing.T) {
	pool, parent, done := laneFixture(t, 2)
	defer done()
	defer parent.Release()
	probe := &laneProbe{}
	paired := make(chan struct{})
	var once sync.Once
	ctx := WithStepCapacity(t.Context(), parent)
	err := runParallelMembers(ctx, newParallelLanes(ctx, 1, 4), parallelLaneRoles, unitCost, func(_ context.Context, role string) error {
		probe.enter(role)
		defer probe.leave()
		probe.mu.Lock()
		if probe.active == 2 {
			once.Do(func() { close(paired) })
		}
		probe.mu.Unlock()
		select {
		case <-paired:
		case <-time.After(5 * time.Second):
			return errors.New("second member never ran alongside")
		}
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if probe.peak != 2 || len(probe.ran) != 3 {
		t.Fatalf("peak=%d ran=%v", probe.peak, probe.ran)
	}
	if used, _ := pool.Capacity(); used != pool.total-1 {
		t.Fatalf("borrowed slot leaked or lost: used=%d", used)
	}
	assertPoolBounded(t, pool)
}

func TestParallelLanesNeverBorrowBeyondPoolOrFullCost(t *testing.T) {
	pool, parent, done := laneFixture(t, 4)
	defer done()
	defer parent.Release()
	probe := &laneProbe{}
	ctx := WithStepCapacity(t.Context(), parent)
	// A full cost of 2 caps the group at two members even with the pool idle.
	err := runParallelMembers(ctx, newParallelLanes(ctx, 1, 2), parallelLaneRoles, unitCost, func(_ context.Context, role string) error {
		probe.enter(role)
		defer probe.leave()
		time.Sleep(20 * time.Millisecond)
		return nil
	})
	if err != nil || probe.peak > 2 || len(probe.ran) != 3 {
		t.Fatalf("err=%v peak=%d ran=%v", err, probe.peak, probe.ran)
	}
	assertPoolBounded(t, pool)
	if used, _ := pool.Capacity(); used != 1 {
		t.Fatalf("used=%d after the members joined", used)
	}
}

func TestParallelLanesReleaseSlotsAfterMemberError(t *testing.T) {
	pool, parent, done := laneFixture(t, 3)
	defer done()
	defer parent.Release()
	ctx := WithStepCapacity(t.Context(), parent)
	failure := errors.New("member failed")
	ran := 0
	var mu sync.Mutex
	err := runParallelMembers(ctx, newParallelLanes(ctx, 1, 4), parallelLaneRoles, unitCost, func(_ context.Context, role string) error {
		mu.Lock()
		ran++
		mu.Unlock()
		if role == "technical" {
			return failure
		}
		return nil
	})
	if !errors.Is(err, failure) || ran != 3 {
		t.Fatalf("err=%v ran=%d", err, ran)
	}
	if used, _ := pool.Capacity(); used != 2 {
		t.Fatalf("failed member leaked: used=%d", used)
	}
	assertPoolBounded(t, pool)
}

func TestParallelLanesReleaseSlotsOnCancellation(t *testing.T) {
	pool, parent, done := laneFixture(t, 1)
	defer done()
	defer parent.Release()
	ctx, cancel := context.WithCancel(WithStepCapacity(t.Context(), parent))
	defer cancel()
	ran := 0
	err := runParallelMembers(ctx, newParallelLanes(ctx, 1, 4), parallelLaneRoles, unitCost, func(ctx context.Context, _ string) error {
		ran++
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) || ran != 1 {
		t.Fatalf("err=%v ran=%d", err, ran)
	}
	if used, _ := pool.Capacity(); used != pool.total {
		t.Fatalf("cancelled member leaked or lost slots: used=%d", used)
	}
	assertPoolBounded(t, pool)
}

func TestParallelLanesUnmeteredRunsEveryMemberAtOnce(t *testing.T) {
	probe := &laneProbe{}
	all := make(chan struct{})
	var once sync.Once
	err := runParallelMembers(t.Context(), newParallelLanes(t.Context(), 1, 4), parallelLaneRoles, unitCost, func(_ context.Context, role string) error {
		probe.enter(role)
		defer probe.leave()
		probe.mu.Lock()
		if probe.active == 3 {
			once.Do(func() { close(all) })
		}
		probe.mu.Unlock()
		select {
		case <-all:
			return nil
		case <-time.After(5 * time.Second):
			return errors.New("members did not overlap")
		}
	})
	if err != nil || probe.peak != 3 {
		t.Fatalf("err=%v peak=%d", err, probe.peak)
	}
}
