package webhook

import (
	"container/list"
	"sync"
	"time"
)

// Delivery-id memory bounds. The set is in memory only: a restart forgets it,
// and a redelivery after a restart merely costs one extra issue read.
const (
	// MaxDeliveryIDs is how many delivery ids are remembered at once.
	MaxDeliveryIDs = 10000
	// DeliveryIDTTL is how long a delivery id is remembered.
	DeliveryIDTTL = time.Hour
)

type deliveryEntry struct {
	id string
	at time.Time
}

// deliveries remembers recent delivery ids, evicting the oldest first.
type deliveries struct {
	max int
	ttl time.Duration

	mu    sync.Mutex
	order *list.List
	ids   map[string]*list.Element
}

func newDeliveries(max int, ttl time.Duration) *deliveries {
	return &deliveries{max: max, ttl: ttl, order: list.New(), ids: make(map[string]*list.Element)}
}

// seen reports whether id was recorded and has not expired.
func (d *deliveries) seen(id string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.expire(now)
	_, ok := d.ids[id]
	return ok
}

// record remembers id at now, evicting the oldest ids beyond the bound.
func (d *deliveries) record(id string, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.expire(now)
	if _, ok := d.ids[id]; ok {
		return
	}
	for d.order.Len() >= d.max {
		d.evict(d.order.Front())
	}
	d.ids[id] = d.order.PushBack(deliveryEntry{id: id, at: now})
}

func (d *deliveries) len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.order.Len()
}

func (d *deliveries) expire(now time.Time) {
	for e := d.order.Front(); e != nil; e = d.order.Front() {
		if now.Sub(e.Value.(deliveryEntry).at) < d.ttl {
			return
		}
		d.evict(e)
	}
}

func (d *deliveries) evict(e *list.Element) {
	delete(d.ids, e.Value.(deliveryEntry).id)
	d.order.Remove(e)
}
