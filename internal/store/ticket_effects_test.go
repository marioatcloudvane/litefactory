package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestTicketEffectBindingRetainsDestinationAndActorAcrossConflicts(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	original := resolvedSessionTicket("owner/repo", "700")
	actor := ticket.Identity{Provider: ticket.GitHub, Origin: original.Identity.Origin, RemoteID: "99"}
	effect, err := db.BindTicketEffect(t.Context(), "report:existing-key", original.Locator, actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = effect.Ref(); err == nil {
		t.Fatal("unresolved locator permits mutation")
	}
	effect, err = db.ResolveTicketEffect(t.Context(), effect.Key, original)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*ticket.Ref){
		func(r *ticket.Ref) { r.Identity.RemoteID = "701" },
		func(r *ticket.Ref) {
			r.Identity.Origin = "https://other.example/api"
			r.Locator.Origin = r.Identity.Origin
		},
		func(r *ticket.Ref) { r.Locator.Key = "8" },
	} {
		changed := original
		change(&changed)
		if _, err = db.ResolveTicketEffect(t.Context(), effect.Key, changed); !errors.Is(err, ErrTicketIdentityConflict) {
			t.Fatalf("contradiction accepted: %v", err)
		}
	}
	otherActor := actor
	otherActor.RemoteID = "100"
	if _, err = db.BindTicketEffect(t.Context(), effect.Key, original.Locator, otherActor); !errors.Is(err, ErrTicketIdentityConflict) {
		t.Fatalf("bot rebinding: %v", err)
	}
	if _, err = db.BindTicketEffect(t.Context(), effect.Key, original.Locator, ticket.Identity{}); !errors.Is(err, ErrTicketIdentityConflict) {
		t.Fatalf("actor qualification dropped: %v", err)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	retained, err := db.GetTicketEffect(t.Context(), effect.Key)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(effect, retained) {
		t.Fatalf("retained identity changed: %+v", retained)
	}
}

func TestTicketEffectOriginIsFrozenBeforeResolution(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	locator := resolvedSessionTicket("owner/repo", "700").Locator
	original, err := db.BindTicketEffect(t.Context(), "plan:legacy-key", locator, ticket.Identity{})
	if err != nil {
		t.Fatal(err)
	}
	foreign := locator
	foreign.Origin = "https://enterprise.example/api/v3"
	if _, err = db.BindTicketEffect(t.Context(), original.Key, foreign, ticket.Identity{}); !errors.Is(err, ErrTicketIdentityConflict) {
		t.Fatalf("unresolved legacy origin rebound: %v", err)
	}
	actor := ticket.Identity{Provider: ticket.GitHub, Origin: foreign.Origin, RemoteID: "99"}
	if _, err = db.BindTicketEffect(t.Context(), "bad-actor", locator, actor); !errors.Is(err, ErrTicketIdentityConflict) {
		t.Fatalf("foreign actor accepted: %v", err)
	}
	if _, err = db.GetTicketEffect(t.Context(), "bad-actor"); !errors.Is(err, ErrNotFound) {
		t.Fatal("invalid actor left partial metadata")
	}
}
