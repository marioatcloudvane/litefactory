package retainedevidence

import (
	"sync"
	"time"
)

// PreparationCache holds private unfinished read preparations between explicitly
// budget-paused invocations. Keys must be opaque service-generated identities;
// they confer no access. Callers recheck their current grant and source/view
// identities before taking an entry. Taking moves ownership, never a budget.
// A cache belongs to the trusted service lifetime, not to an agent invocation.
type PreparationCache[T any] struct {
	mu       sync.Mutex
	entries  map[string]*preparationEntry[T]
	capacity int
	ttl      time.Duration
	dispose  func(T)
}

type preparationEntry[T any] struct {
	value   T
	expires time.Time
	timer   *time.Timer
}

// NewPreparationCache bounds retained work by entry count and lifetime. Dispose
// must release each entry's private files/processes and must not call the cache.
// Invalid bounds panic because only trusted static service policy supplies them.
func NewPreparationCache[T any](capacity int, ttl time.Duration, dispose func(T)) *PreparationCache[T] {
	if capacity < 1 || ttl <= 0 || dispose == nil {
		panic("invalid private preparation cache policy")
	}
	return &PreparationCache[T]{entries: make(map[string]*preparationEntry[T]), capacity: capacity, ttl: ttl, dispose: dispose}
}

// Put transfers private preparation ownership into the bounded cache. Replaced
// or oldest entries are released. It must only be used for budget-paused work;
// cancellation and ordinary reader shutdown must dispose unfinished work.
func (c *PreparationCache[T]) Put(key string, value T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if old := c.entries[key]; old != nil {
		c.removeLocked(key, old, true)
	}
	if len(c.entries) >= c.capacity {
		var oldestKey string
		var oldest *preparationEntry[T]
		for candidateKey, candidate := range c.entries {
			if oldest == nil || candidate.expires.Before(oldest.expires) {
				oldestKey, oldest = candidateKey, candidate
			}
		}
		c.removeLocked(oldestKey, oldest, true)
	}
	entry := &preparationEntry[T]{value: value, expires: time.Now().Add(c.ttl)}
	c.entries[key] = entry
	// At most capacity timers are live. Each callback runs once at expiry;
	// transfer/eviction stops it, and the entry identity check handles a
	// callback already waiting on the mutex. The service need not retain a
	// permanent sweeper goroutine after all private work has been released.
	entry.timer = time.AfterFunc(c.ttl, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.entries[key] == entry {
			c.removeLocked(key, entry, true)
		}
	})
}

// Take removes a live entry and transfers its private resources to the caller.
// An expired or unknown key returns false; callers must report unavailability
// or rebuild the same pinned source, never silently select a different source.
func (c *PreparationCache[T]) Take(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entries[key]
	if entry == nil {
		var zero T
		return zero, false
	}
	if !time.Now().Before(entry.expires) {
		c.removeLocked(key, entry, true)
		var zero T
		return zero, false
	}
	c.removeLocked(key, entry, false)
	return entry.value, true
}

func (c *PreparationCache[T]) removeLocked(key string, entry *preparationEntry[T], dispose bool) {
	delete(c.entries, key)
	if entry.timer != nil {
		entry.timer.Stop()
	}
	if dispose {
		c.dispose(entry.value)
	}
}
