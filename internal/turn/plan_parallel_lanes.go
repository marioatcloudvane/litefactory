package turn

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// parallelLanes meters how many Plan parallel members run at once. The group
// is admitted on its minimum (see graphexec.AdmissionSlotCost), which hosts one
// member at a time; every further concurrent member borrows its own slots from
// the shared pool without waiting, up to the group's full declared cost. The
// degree of concurrency is therefore exactly what the pool could give, and a
// member's slots return the moment it finishes, whatever way it finishes.
type parallelLanes struct {
	mu sync.Mutex
	// unbounded is the factory-less case (no step reservation in the context),
	// where admission is not metered either.
	unbounded bool
	// free is the part of the admitted grant no member currently uses.
	free int
	// headroom is how many more slots the group may still borrow.
	headroom int
	grower   StepCapacityGrower
}

func newParallelLanes(ctx context.Context, admission, full int) *parallelLanes {
	capacity := stepCapacity(ctx)
	if capacity == nil {
		return &parallelLanes{unbounded: true}
	}
	grower, _ := capacity.(StepCapacityGrower)
	return &parallelLanes{free: admission, headroom: max(full-admission, 0), grower: grower}
}

// take hands out cost slots for one member without waiting. ok is false when
// neither the grant nor the pool has them free right now.
func (l *parallelLanes) take(ctx context.Context, cost int) (func(), bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.unbounded {
		return func() {}, true, nil
	}
	if cost <= l.free {
		l.free -= cost
		return sync.OnceFunc(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.free += cost
		}), true, nil
	}
	if l.grower == nil || cost > l.headroom {
		return nil, false, nil
	}
	release, ok, err := l.grower.TryGrow(ctx, cost)
	if err != nil || !ok {
		return nil, false, err
	}
	l.headroom -= cost
	return sync.OnceFunc(func() {
		release()
		l.mu.Lock()
		defer l.mu.Unlock()
		l.headroom += cost
	}), true, nil
}

// runParallelMembers runs every role exactly once, as many at a time as lanes
// allows, in role order. Each member's slots are released before the
// scheduler hears it finished, on success, error and cancellation alike. A
// role that cannot start (cancellation, or a member larger than every lane)
// reports an error instead of running, so the join sees every role.
func runParallelMembers(ctx context.Context, lanes *parallelLanes, roles []string, cost func(string) int, run func(context.Context, string) error) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(roles))
	finished := make(chan struct{}, len(roles))
	running := 0
	for i, role := range roles {
		var refused error
		for {
			if refused = ctx.Err(); refused != nil {
				break
			}
			release, ok, err := lanes.take(ctx, cost(role))
			if err != nil {
				refused = err
				break
			}
			if ok {
				running++
				wg.Add(1)
				go func(role string) {
					defer wg.Done()
					defer func() { finished <- struct{}{} }()
					defer release()
					errs <- run(ctx, role)
				}(role)
				break
			}
			if running == 0 {
				refused = fmt.Errorf("needs %d slot(s) the group cannot hold", cost(role))
				break
			}
			select {
			case <-finished:
				running--
			case <-ctx.Done():
			}
		}
		if refused != nil {
			for _, rest := range roles[i:] {
				errs <- fmt.Errorf("parallel member %q not started: %w", rest, refused)
			}
			break
		}
	}
	wg.Wait()
	close(errs)
	var combined error
	for e := range errs {
		combined = errors.Join(combined, e)
	}
	return combined
}
