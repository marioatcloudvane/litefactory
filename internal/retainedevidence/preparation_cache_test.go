package retainedevidence

import (
	"testing"
	"time"
)

func TestPreparationCacheMovesAndReleasesOwnership(t *testing.T) {
	t.Parallel()
	disposed := make(chan int, 8)
	cache := NewPreparationCache(1, time.Hour, func(value int) { disposed <- value })
	cache.Put("first", 1)
	cache.Put("second", 2)
	if got := <-disposed; got != 1 {
		t.Fatalf("evicted %d, want oldest entry", got)
	}
	if _, ok := cache.Take("first"); ok {
		t.Fatal("evicted entry remained available")
	}
	if value, ok := cache.Take("second"); !ok || value != 2 {
		t.Fatal("live entry failed to transfer")
	}
	if _, ok := cache.Take("second"); ok {
		t.Fatal("private preparation transferred twice")
	}
	select {
	case <-disposed:
		t.Fatal("transferred entry was disposed by its former owner")
	default:
	}

	expiring := NewPreparationCache(1, 10*time.Millisecond, func(value int) { disposed <- value })
	expiring.Put("expiring", 3)
	select {
	case got := <-disposed:
		if got != 3 {
			t.Fatalf("expired %d, want 3", got)
		}
	case <-time.After(time.Second):
		t.Fatal("abandoned preparation resources did not expire")
	}
	if _, ok := expiring.Take("expiring"); ok {
		t.Fatal("expired entry remained available")
	}
}
