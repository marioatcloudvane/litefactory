package logevidence

import (
	"time"

	"github.com/cloudvane/litefactory/internal/retainedevidence"
)

type pausedPreparation struct {
	owner               runRepository
	root                string
	run                 int64
	fingerprint, policy string
	snapshot            *sourceSnapshot
	records             map[string]*recordPreparation
}

func (p pausedPreparation) close() {
	if p.snapshot != nil {
		p.snapshot.close()
	}
	for _, record := range p.records {
		record.close()
	}
}

var pausedViews = retainedevidence.NewPreparationCache[pausedPreparation](16, 15*time.Minute, func(p pausedPreparation) { p.close() })

// pause transfers private prepared state only when its invocation is terminal.
// The token in a locator confers no authority and can be consumed just once.
func (s *Service) pause(r *Result) {
	if r.Remaining == nil {
		return
	}
	locator := r.Remaining
	key := digestJSON([]any{locator.RunID, locator.Fingerprint})
	snapshot := s.budget.snapshots[key]
	if snapshot == nil {
		return
	}
	state := pausedPreparation{owner: s.db, root: s.root, run: locator.RunID, fingerprint: locator.Fingerprint, policy: locator.Policy, snapshot: snapshot, records: map[string]*recordPreparation{}}
	delete(s.budget.snapshots, key)
	for key, record := range s.budget.preparations {
		if record.fingerprint == locator.Fingerprint && record.sourceRevision == snapshot.digest {
			state.records[key] = record
			delete(s.budget.preparations, key)
		}
	}
	locator.CacheID = snapshot.cacheID
	pausedViews.Put(locator.CacheID, state)
}

func (s *Service) resume(locator *ViewLocator) bool {
	if locator.CacheID == "" {
		return true
	}
	state, ok := pausedViews.Take(locator.CacheID)
	if !ok {
		return true
	} // A cache can expire: rebuild and verify the retained source.
	if state.owner != s.db || state.root != s.root || state.run != locator.RunID || state.fingerprint != locator.Fingerprint || state.policy != locator.Policy {
		state.close()
		return false
	}
	key := digestJSON([]any{locator.RunID, locator.Fingerprint})
	if previous := s.budget.snapshots[key]; previous != nil {
		previous.close()
	}
	s.budget.snapshots[key] = state.snapshot
	for key, record := range state.records {
		if previous := s.budget.preparations[key]; previous != nil {
			previous.close()
		}
		s.budget.preparations[key] = record
	}
	return true
}

// Pause is called only by trusted orchestration after an outer invocation
// budget ends. Previously issued locators retain their opaque preparation ID.
// All old handles are revoked and their shared inner budget remains terminal.
func (s *Service) Pause() error {
	b := s.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	b.terminal = true
	for _, snapshot := range b.snapshots {
		r := Result{Remaining: &ViewLocator{RunID: snapshot.run, Fingerprint: snapshot.fingerprint, Policy: snapshot.policy}}
		s.pause(&r)
	}
	s.cancel()
	return nil
}
