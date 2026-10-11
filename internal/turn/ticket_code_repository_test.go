package turn

import (
	"strings"
	"testing"
)

func TestRetainedDeliveryRejectsChangedParentBeforeCodeEffects(t *testing.T) {
	f, forge := featureDeliveryFixture(t, true)
	q := prepareTestDelivery(t, f)
	beforeCreates := forge.creates
	forge.parent.ID++
	err := f.opts.StoryProgress.deliverDelivery(t.Context(), &q)
	if err == nil || !strings.Contains(err.Error(), "delivery parent identity changed") {
		t.Fatalf("changed parent was accepted: %v", err)
	}
	if forge.creates != beforeCreates || forge.merges != 0 || forge.closes != 0 {
		t.Fatal("changed parent dispatched a code or closure effect")
	}
	if q.MergeAttempted || q.MergeConfirmed || q.Closed || q.Reported {
		t.Fatalf("changed parent advanced retained delivery: %+v", q)
	}
	err = f.opts.StoryProgress.ApplyDelivery(t.Context(), mergeControl(q))
	if err == nil || !strings.Contains(err.Error(), "delivery parent identity changed") {
		t.Fatalf("changed parent consumed new approval: %v", err)
	}
	retained, err := f.db.GetDelivery(t.Context(), q.Key)
	mustProgress(t, err)
	if retained.ApprovalComment != 0 || retained.MergeAttempted {
		t.Fatalf("changed parent consumed merge authority: %+v", retained)
	}
}

func TestTicketCannotRetargetRetainedCodeDelivery(t *testing.T) {
	for _, retained := range []bool{false, true} {
		t.Run(map[bool]string{false: "before proposal", true: "saved proposal"}[retained], func(t *testing.T) {
			f, forge := featureDeliveryFixture(t, true)
			control := deliveryControl(f.session.TaskRef, "/deliver 1")
			if retained {
				control = mergeControl(prepareTestDelivery(t, f))
			}
			beforeCreates := forge.creates
			registered, err := f.db.GetRepo(t.Context(), f.session.RepoID)
			mustProgress(t, err)
			registered.Owner = "unrelated"
			_, err = f.db.UpsertRepo(t.Context(), registered)
			mustProgress(t, err)
			err = f.opts.StoryProgress.ApplyDelivery(t.Context(), control)
			if err == nil || !strings.Contains(err.Error(), "registered code repository conflicts") {
				t.Fatalf("contradictory registered clone was accepted: %v", err)
			}
			if forge.creates != beforeCreates || forge.merges != 0 || forge.closes != 0 {
				t.Fatal("repository contradiction dispatched a code or closure effect")
			}
			if retained {
				proposals, err := f.db.ListDeliveries(t.Context(), f.session.TaskRef)
				mustProgress(t, err)
				if len(proposals) != 1 || proposals[0].ApprovalComment != 0 || proposals[0].MergeAttempted {
					t.Fatalf("contradictory registration consumed merge authority: %+v", proposals)
				}
			}
		})
	}
}
