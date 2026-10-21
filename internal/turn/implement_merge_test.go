package turn

import (
	"fmt"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestImplementMergeDeferredDiscoveryReturnsRetryAndChangesNothing(t *testing.T) {
	for name, cause := range map[string]error{
		"discovery deferred": ticket.ErrDiscoveryDeferred,
		"rate limited":       github.ErrRateLimited,
	} {
		t.Run(name, func(t *testing.T) {
			f := newImplementFixture(t, implementStories(1, nil)...)
			f.ready()
			f.drive("story-001", store.ImplementDone, implementClock)
			before := f.row("story-001")
			f.repo.beginErr = fmt.Errorf("github: inspect repository: %w", cause)
			res, out, err := f.merge()
			if err != nil || res.Outcome != "retry" || out.Result != "retry" || out.Reason == "" {
				t.Fatalf("%s %+v %v", res.Outcome, out, err)
			}
			if got := f.row("story-001"); got.State != store.ImplementDone || got.Revision != before.Revision {
				t.Fatalf("story = %s rev %d, must stay done at rev %d", got.State, got.Revision, before.Revision)
			}
			if f.forge.posts != 0 || len(f.repo.merges) != 0 || len(f.repo.pushes) != 0 {
				t.Fatal("a deferred merge must change nothing")
			}
			f.repo.beginErr = nil
			if res, _, err := f.merge(); err != nil || res.Outcome != "merged" {
				t.Fatalf("after deferral: %s %v", res.Outcome, err)
			}
		})
	}
}
