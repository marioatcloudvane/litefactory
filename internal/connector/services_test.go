package connector

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/owneraction"
)

func TestServiceSlotPreservesUnavailableTypedNil(t *testing.T) {
	var slot serviceSlot[OwnerActions]
	if slot.Load() != nil {
		t.Fatal("unconfigured owner operations advertised as available")
	}
	var unavailable *owneraction.Service
	slot.Store(unavailable)
	if slot.Load() != nil {
		t.Fatal("typed nil owner operations advertised as available")
	}
	ready := &owneraction.Service{}
	slot.Store(ready)
	if slot.Load() != ready {
		t.Fatal("ready owner operations not published")
	}
	slot.Store(unavailable)
	if slot.Load() != nil {
		t.Fatal("withdrawn owner operations remain available")
	}
}
