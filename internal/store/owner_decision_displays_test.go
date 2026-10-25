package store

import (
	"testing"
	"time"
)

func TestOwnerDecisionDisplaysRejectReplacementAndRetainBinding(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	now := time.Now().UTC()
	a := OwnerDecisionDisplay{TaskRef: "github:o/r#1", Action: "/validate-ship", Target: "validate:1:H:D", Revision: "0123456789ab", InstanceID: 1, PublicationID: 100, TestedCommit: "H", DeliveryCommit: "D", CreatedAt: now}
	if err := db.PutOwnerDecisionDisplay(ctx, a); err != nil {
		t.Fatal(err)
	}
	d, err := db.ResolveOwnerDecisionDisplay(ctx, a.TaskRef, a.Action, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	binding := OwnerDecisionBinding{TaskRef: a.TaskRef, CommentID: 101, AuthorID: 7, Body: "/validate-ship -- reason", Normalized: "/validate-ship 0123456789ab -- reason", Display: d, CreatedAt: now}
	if err = db.PutOwnerDecisionBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	b := a
	b.Target = "validate:1:H:E"
	b.DeliveryCommit = "E"
	b.PublicationID = 102
	if err = db.PutOwnerDecisionDisplay(ctx, b); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ResolveOwnerDecisionDisplay(ctx, a.TaskRef, a.Action, "", 0); err == nil {
		t.Fatal("bare shorthand adopted replacement")
	}
	if _, err = db.ResolveOwnerDecisionDisplay(ctx, a.TaskRef, a.Action, d.Reference, 0); err == nil {
		t.Fatal("delayed reference adopted replacement")
	}
	if _, err = db.ExplicitOwnerDecisionDisplay(ctx, a.TaskRef, a.Action, a.Revision); err == nil {
		t.Fatal("revision silently adopted new delivery head")
	}
	fresh, err := db.ResolveOwnerDecisionDisplay(ctx, b.TaskRef, b.Action, OwnerDecisionReference(b.TaskRef, b.Action, b.Target, b.Revision), 0)
	if err != nil || fresh.DeliveryCommit != "E" {
		t.Fatalf("fresh reference: %+v %v", fresh, err)
	}
	retained, err := db.GetOwnerDecisionBinding(ctx, a.TaskRef, 101)
	if err != nil || retained.Display.DeliveryCommit != "D" || retained.Body != binding.Body {
		t.Fatalf("replay: %+v %v", retained, err)
	}
	if err = db.ResolveOwnerDecisionInstance(ctx, 1); err != nil {
		t.Fatal(err)
	}
	next := b
	next.Target = "validate:2:H:F"
	next.InstanceID = 2
	next.PublicationID = 103
	if err = db.PutOwnerDecisionDisplay(ctx, next); err != nil {
		t.Fatal(err)
	}
	if d, err := db.ResolveOwnerDecisionDisplay(ctx, next.TaskRef, next.Action, "", 0); err != nil || d.Target != next.Target {
		t.Fatalf("completed displays interfered with new target: %+v %v", d, err)
	}
	binding.Body = "edited"
	if err = db.PutOwnerDecisionBinding(ctx, binding); err == nil {
		t.Fatal("edited replay changed admitted bytes")
	}
}

func TestOwnerDecisionDisplayExplicitPriorityAndChildScope(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	now := time.Now().UTC()
	a := OwnerDecisionDisplay{TaskRef: "github:o/r#1", Action: "/approve-plan", Target: "/approve-plan story-001 story candidate-a", Revision: "revision-a", PublicationID: 100, CreatedAt: now}
	b := a
	b.Target = "/approve-plan story-002 story candidate-b"
	b.Revision = "revision-b"
	b.PublicationID = 101
	for _, d := range []OwnerDecisionDisplay{a, b} {
		if err := db.PutOwnerDecisionDisplay(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ResolveOwnerDecisionDisplay(ctx, a.TaskRef, a.Action, "", 0); err == nil {
		t.Fatal("parent bare shorthand picked a story")
	}
	d, err := db.ResolveOwnerStoryDecisionDisplay(ctx, a.TaskRef, a.Action, "", 0, "story-001")
	if err != nil || d.Target != a.Target {
		t.Fatalf("child shorthand: %+v %v", d, err)
	}
	ref := OwnerDecisionReference(b.TaskRef, b.Action, b.Target, b.Revision)
	d, err = db.ResolveOwnerDecisionDisplay(ctx, b.TaskRef, b.Action, ref, a.PublicationID)
	if err != nil || d.Target != b.Target {
		t.Fatalf("reference must win over reply-to: %+v %v", d, err)
	}
	b.PublicationID = 102
	if err = db.PutOwnerDecisionDisplay(ctx, b); err != nil {
		t.Fatal(err)
	}
	d, err = db.ResolveOwnerDecisionDisplay(ctx, b.TaskRef, b.Action, "", 102)
	if err != nil || d.Target != b.Target {
		t.Fatalf("repeated display reply-to: %+v %v", d, err)
	}
}
