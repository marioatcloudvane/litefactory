package turn

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

// A graph stage runs under an identity the factory minted, not a comment. That
// identity is larger than any id a forge will ever issue, so consuming one as a
// comment moves the watermark past everything the owner will ever say — the
// task goes permanently deaf, and the symptom is a reply that is simply never
// read. Three live sessions were lost to this before it had a test.
func TestConsumeRefusesFactoryMintedIdentities(t *testing.T) {
	t.Parallel()
	for name, id := range map[string]int64{
		"graph stage":        store.GraphDispatch{ID: 1}.TriggerID(),
		"automatic recovery": store.Continuation{ID: 1}.TriggerID(),
		"manual schedule":    store.Schedule{ID: 1}.TriggerID(),
	} {
		if !store.IsPrivateTrigger(id) {
			t.Errorf("%s identity %d is not recognised as factory-minted", name, id)
		}
	}
	for _, comment := range []int64{1, 9001, 5743291088} {
		if store.IsPrivateTrigger(comment) {
			t.Errorf("comment id %d must remain consumable", comment)
		}
	}
}
