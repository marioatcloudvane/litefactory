package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestBugParentPRPublicationRecovery(t *testing.T) {
	db, _, _, forge, h, in := prJourneySetup(t)
	publications := 0
	h.Publish = func(_ context.Context, r store.BugDeliveryReview) (store.BugDeliveryReview, error) {
		publications++
		if publications == 1 {
			return r, errors.New("controlled publication interruption before effect")
		}
		prJourneyPublish(t, db, r)
		return db.ConfirmBugDeliveryReview(t.Context(), r.SessionID, r.Digest)
	}
	first, e := h.Prepare(t.Context(), db, in)
	if e == nil {
		t.Fatalf("publication failure counted as review-ready %+v", first)
	}
	next, e := h.Prepare(t.Context(), db, in)
	if e != nil || next.Outcome != "review-ready" || next.Review == nil || next.Review.CommentID == 0 {
		t.Fatalf("publication recovery %+v %v", next, e)
	}
	if publications != 2 || forge.creates != 1 {
		t.Fatalf("recovery duplicated PR or publication %d %d", publications, forge.creates)
	}
	again, e := h.Prepare(t.Context(), db, in)
	if e != nil || again.Outcome != "review-ready" || publications != 2 || forge.creates != 1 {
		t.Fatalf("confirmed replay %+v %v", again, e)
	}
}
