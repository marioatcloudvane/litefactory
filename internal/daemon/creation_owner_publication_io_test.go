package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// An owner's approval publishes the exact proposal from the factory itself, on a
// graph Research session and outside Research alike, and a replayed approval
// reports the same publication without creating anything twice.
func TestOwnerApprovalPublishesInAnyModeAndPath(t *testing.T) {
	for _, tc := range []struct{ name, label, path string }{
		{"graph research", "Mode: Research", "graph"},
		{"implement", "Mode: Implement", ""},
		{"no mode label", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPublicationIO(t)
			f.createOK = true
			task := rejectionTask(t, f)
			task.issue.Labels = nil
			if tc.label != "" {
				task.issue.Labels = []string{tc.label}
			}
			if tc.path != "" {
				_, err := f.db.AdmitResearchPath(t.Context(), f.session.ID, tc.path)
				publicationMust(t, err)
			}
			prepared, err := f.runtime.PrepareCreationProposal(t.Context(), successfulPreparationControl(t, f, 60, "Graph dispatch never gets bound"))
			publicationMust(t, err)
			c := rejectionSource(f, "/mcp-create "+prepared.Hash+" linked")
			publicationMust(t, f.d.handleComment(t.Context(), task, c))
			rejectionWatermark(t, f, 70)
			if f.creationPosts != 1 || f.creationTitle != "Graph dispatch never gets bound" || f.posts != 1 {
				t.Fatalf("creations=%d title=%q replies=%d", f.creationPosts, f.creationTitle, f.posts)
			}
			reply := f.bodies[0]
			for _, want := range []string{"Published proposal `" + prepared.Hash + "`", "#901 Graph dispatch never gets bound"} {
				if !strings.Contains(reply, want) {
					t.Errorf("missing %q in %s", want, reply)
				}
			}
			intent, err := f.cs.GetOwnerIntent(t.Context(), store.CreationSource{Repository: "owner/api", SourceTask: f.session.TaskRef, CommentID: c.ID, OwnerID: 10})
			publicationMust(t, err)
			if pub, _, err := f.cs.ClaimOwnerPublication(t.Context(), intent, true); err != nil || pub.State != "confirmed" {
				t.Fatalf("publication not retained: %+v %v", pub, err)
			}

			// A crash before consumption replays the same comment: the retained
			// publication is reported, never dispatched again.
			rejectionRestart(t, f)
			publicationMust(t, f.d.handleComment(t.Context(), task, c))
			if f.creationPosts != 1 {
				t.Fatalf("replayed approval published again: %d", f.creationPosts)
			}
		})
	}
}

// A refused publication is stored, so its reason survives replay, and it
// dispatches nothing.
func TestOwnerApprovalRefusals(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"stale", "a newer proposal replaced this one"},
		{"expired", "more than 30 minutes after it was posted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPublicationIO(t)
			f.createOK = true
			task := rejectionTask(t, f)
			prepared, err := f.runtime.PrepareCreationProposal(t.Context(), successfulPreparationControl(t, f, 60, "Original"))
			publicationMust(t, err)
			c := rejectionSource(f, "/mcp-create "+prepared.Hash+" linked")
			switch tc.name {
			case "stale":
				// Admitted while current, then superseded before publishing.
				_, err = f.runtime.AdmitCreationApproval(t.Context(), ownerApproval(f, c.ID, c.Body, c.CreatedAt))
				publicationMust(t, err)
				f.now = f.now.Add(time.Minute)
				_, err = f.runtime.PrepareCreationProposal(t.Context(), successfulPreparationControl(t, f, 61, "Replacement"))
				publicationMust(t, err)
			case "expired":
				_, err = f.runtime.AdmitCreationApproval(t.Context(), ownerApproval(f, c.ID, c.Body, c.CreatedAt))
				publicationMust(t, err)
				f.now = f.now.Add(31 * time.Minute)
				rejectionRestart(t, f)
			}
			task.tickAt = f.now
			publicationMust(t, f.d.handleComment(t.Context(), task, c))
			if f.creationPosts != 0 {
				t.Fatalf("refused approval published %d issue(s)", f.creationPosts)
			}
			reply := f.bodies[len(f.bodies)-1]
			if !strings.Contains(reply, "nothing was published") || !strings.Contains(reply, tc.want) {
				t.Fatalf("wrong refusal: %s", reply)
			}
		})
	}
}

func ownerApproval(f *publicationIO, id int64, body string, at time.Time) turn.OwnerGrantControl {
	return turn.OwnerGrantControl{Repository: "owner/api", SourceTask: f.session.TaskRef, CommentID: id, ActorID: 10, CreatedAt: at, Body: body}
}
