package store

import (
	"errors"
	"github.com/cloudvane/litefactory/internal/ticket"
	"testing"
)

func TestTicketNoticeDispatchPreservesPreparedBytesAndReceipt(t *testing.T) {
	db := newTestDB(t)
	ref := resolvedSessionTicket("owner/repo", "700")
	actor := ticket.Identity{Provider: ticket.GitHub, Origin: ref.Identity.Origin, RemoteID: "77"}
	if _, err := db.BindTicketEffect(t.Context(), "notice:original", ref.Locator, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResolveTicketEffect(t.Context(), "notice:original", ref); err != nil {
		t.Fatal(err)
	}
	body := "prepared legacy bytes\n<!-- marker -->"
	prepared, err := db.PrepareTicketNotice(t.Context(), "notice:original", body)
	if err != nil || prepared.Body != body || prepared.Attempted {
		t.Fatalf("preparation consumed dispatch: %+v %v", prepared, err)
	}
	n, claimed, err := db.ClaimTicketNotice(t.Context(), "notice:original", body)
	if err != nil || !claimed || n.Body != body || !n.Attempted {
		t.Fatalf("claim: %+v %v %v", n, claimed, err)
	}
	n, claimed, err = db.ClaimTicketNotice(t.Context(), "notice:original", body)
	if err != nil || claimed || n.Body != body {
		t.Fatalf("repeat: %+v %v %v", n, claimed, err)
	}
	if _, _, err = db.ClaimTicketNotice(t.Context(), "notice:original", "changed"); !errors.Is(err, ErrTicketIdentityConflict) {
		t.Fatalf("changed intent accepted: %v", err)
	}
	if err = db.ConfirmTicketNotice(t.Context(), "notice:original", "5001"); err != nil {
		t.Fatal(err)
	}
	if err = db.ConfirmTicketNotice(t.Context(), "notice:original", "5002"); !errors.Is(err, ErrTicketIdentityConflict) {
		t.Fatalf("receipt changed: %v", err)
	}
	n, claimed, err = db.ClaimTicketNotice(t.Context(), "notice:original", body)
	if err != nil || claimed || n.ReplyID != "5001" {
		t.Fatalf("receipt: %+v %v %v", n, claimed, err)
	}
}
