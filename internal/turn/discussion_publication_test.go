package turn

import (
	"net/http"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestDiscussionPublicationUncertainPolicyPreservesWorkflow(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		t.Run(map[bool]string{false: "receipt visible", true: "receipt hidden"}[hidden], func(t *testing.T) {
			f := newFixture(t, newStubRunner(nil))
			forge := newFakeForge(t)
			reporter := newReporter(t, f, forge, nil)
			before, err := f.db.GetSession(t.Context(), f.session.ID)
			if err != nil {
				t.Fatal(err)
			}
			publisher := DiscussionPublisher{Reporter: reporter}
			c := store.DiscussionConclusion{ID: "publication-test", DiscussionScope: store.DiscussionScope{TaskRef: f.session.TaskRef}, Rendered: "Question: why?\nConclusion: keep the boundary.\n/approve-plan bogus"}
			forge.failCreatingWith(http.StatusInternalServerError, hidden)
			if _, err = publisher.Publish(t.Context(), c); err == nil {
				t.Fatal("ambiguous response reported success")
			}
			forge.recover()
			if _, err = publisher.Publish(t.Context(), c); hidden && err == nil {
				t.Fatal("hidden receipt reported confirmed delivery")
			}
			if forge.attemptCount() != 1 {
				t.Fatalf("ambiguous POST repeated: %d", forge.attemptCount())
			}
			if hidden {
				forge.unhide()
			}
			url, found, err := publisher.Recover(t.Context(), c)
			if err != nil || !found || url == "" {
				t.Fatalf("receipt not recovered: %q %v %v", url, found, err)
			}
			after, err := f.db.GetSession(t.Context(), f.session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatal("discussion publication changed session")
			}
			d, err := f.db.GetReportDelivery(t.Context(), "discussion-summary:"+c.ID)
			if err != nil || !d.Completed {
				t.Fatalf("summary not complete: %+v %v", d, err)
			}
			if pending, found, err := f.db.FindPendingReportDelivery(t.Context(), f.session.ID, 0); err != nil || found {
				t.Fatalf("summary entered workflow recovery: %+v %v", pending, err)
			}
		})
	}
}
