package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestQualifiedReplyPreservesLegacyReplayAndRejectsIdentityCollisions(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	session, err := db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	ref := resolvedSessionTicket("owner/repo", "700")
	if _, err := db.BindSessionTicket(t.Context(), session.ID, ref); err != nil {
		t.Fatal(err)
	}
	old, err := db.RecordComment(t.Context(), Comment{SessionID: session.ID, ForgeCommentID: 10, AuthorUserID: 42, AuthorHandle: "old handle", Body: "original bytes", Classification: ClassificationThirdParty})
	if err != nil {
		t.Fatal(err)
	}
	reply := ticket.Reply{Ticket: ref, Identity: ticket.Identity{Provider: ticket.GitHub, Origin: ref.Identity.Origin, RemoteID: "10"}, Author: ticket.Actor{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: ref.Identity.Origin, RemoteID: "42"}, Handle: "new handle"}, Body: "edited reply"}
	got, err := db.RecordTicketReply(t.Context(), session.ID, reply, ClassificationInstruction)
	if err != nil || !reflect.DeepEqual(got, old) {
		t.Fatalf("qualified replay altered legacy row: %+v, %v", got, err)
	}
	for _, tc := range []struct {
		name   string
		change func(*ticket.Reply)
	}{
		{"reply origin", func(r *ticket.Reply) { r.Identity.Origin = "https://other.example" }},
		{"actor origin", func(r *ticket.Reply) { r.Author.Identity.Origin = "https://other.example" }},
		{"ticket identity", func(r *ticket.Reply) { r.Ticket.Identity.RemoteID = "701" }},
		{"retained author", func(r *ticket.Reply) { r.Author.Identity.RemoteID = "99" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := reply
			tc.change(&changed)
			if _, err := db.RecordTicketReply(t.Context(), session.ID, changed, ClassificationInstruction); !errors.Is(err, ErrTicketIdentityConflict) {
				t.Fatalf("identity collision admitted: %v", err)
			}
		})
	}
	reply.Identity.RemoteID = "11"
	reply.Author.Identity.RemoteID = ""
	got, err = db.RecordTicketReply(t.Context(), session.ID, reply, ClassificationThirdParty)
	if err != nil || got.AuthorUserID != 0 || got.Tag != "c2" {
		t.Fatalf("deleted actor or tag continuity lost: %+v, %v", got, err)
	}
	if _, present, err := db.GetWatermark(t.Context(), session.ID); err != nil || present {
		t.Fatalf("reply recording consumed a source: %v", err)
	}
}

func TestQualifiedReplyRequiresResolvedSessionBinding(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	session, err := db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.BindLegacySessionOrigin(t.Context(), session.ID, "https://api.github.com"); err != nil {
		t.Fatal(err)
	}
	ref := resolvedSessionTicket("owner/repo", "700")
	reply := ticket.Reply{Ticket: ref, Identity: ticket.Identity{Provider: ticket.GitHub, Origin: ref.Identity.Origin, RemoteID: "10"}, Author: ticket.Actor{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: ref.Identity.Origin, RemoteID: "42"}}}
	if _, err := db.RecordTicketReply(t.Context(), session.ID, reply, ClassificationInstruction); err == nil {
		t.Fatal("unresolved session admitted reply")
	}
	rows, err := db.ListComments(t.Context(), session.ID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("refusal left partial comment: %+v, %v", rows, err)
	}
}
