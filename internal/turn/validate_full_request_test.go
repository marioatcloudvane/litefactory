package turn

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestValidateFullRequestPersistsPolicyBeforeExecution(t *testing.T) {
	v := newValidateAdmissionFixture(t)
	v.allMerged(t)
	for i, body := range []string{"/validate full", "/validate full"} {
		comment := int64(910 + i)
		_, handled, err := v.admit(t, v.request(comment, validateOwnerID, body))
		if !handled || !errors.Is(err, errValidateStop) {
			t.Fatalf("admit handled=%v err=%v", handled, err)
		}
		entries := v.entries(t)
		if len(entries) != 1 || entries[0].Note != "full" {
			t.Fatalf("entries %+v", entries)
		}
		entry := entries[0]
		source := fmt.Sprintf("entry/%d/comment/%d/commit/%s", entry.ID, comment, entry.Commit)
		request, err := v.f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: fmt.Sprintf("%s/session/%d", v.f.session.TaskRef, v.f.session.ID), SourceKey: source, Policy: "full", Commit: entry.Commit})
		if err != nil || request.Generation != i+1 {
			t.Fatalf("request %+v err=%v", request, err)
		}
		replay, err := v.f.db.EnsureValidationRequest(t.Context(), request)
		if err != nil || replay.ID != request.ID {
			t.Fatalf("replay %+v err=%v", replay, err)
		}
		if err := v.f.db.SetGraphInstanceStatus(t.Context(), entry.InstanceID, store.GraphCancelled, time.Time{}, implementClock); err != nil {
			t.Fatal(err)
		}
	}
	before := v.binds
	currentInstance := v.entries(t)[0].InstanceID
	_, handled, err := v.admit(t, v.request(910, validateOwnerID, "/validate full"))
	if !handled || err != nil || v.binds != before {
		t.Fatalf("old request replay changed later run: handled=%v binds=%d err=%v", handled, v.binds, err)
	}
	if entry := v.entries(t)[0]; entry.InstanceID != currentInstance {
		t.Fatalf("retired request replaced current instance: got %d want %d", entry.InstanceID, currentInstance)
	}

}

func TestValidatePlainRequestUsesAutomaticPolicy(t *testing.T) {
	v := newValidateAdmissionFixture(t)
	v.allMerged(t)
	_, handled, err := v.admit(t, v.request(930, validateOwnerID, "/validate"))
	if !handled || !errors.Is(err, errValidateStop) {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	entry := v.entries(t)[0]
	_, err = v.f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: fmt.Sprintf("%s/session/%d", v.f.session.TaskRef, v.f.session.ID), SourceKey: fmt.Sprintf("entry/%d/comment/%d/commit/%s", entry.ID, entry.CommentID, entry.Commit), Policy: "full", Commit: entry.Commit})
	if err == nil {
		t.Fatal("plain request was not durably bound to automatic policy")
	}
}
