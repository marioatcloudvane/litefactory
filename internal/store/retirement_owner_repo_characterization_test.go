package store

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// GetRetirementOwner(...).Repo comes from SessionRepository: a pinned session
// keeps its retained snapshot target (main) while an unpinned one follows the
// live row (develop).
func TestRetirementOwnerRepoFollowsSessionRepository(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")
	code, err := db.SetConnection(t.Context(), connectionFixture(repo, CodeConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	tickets, err := db.SetConnection(t.Context(), connectionFixture(repo, TicketConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := db.GetOrCreateSession(t.Context(), repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	unpinned, err := db.GetOrCreateSession(t.Context(), repo, 2)
	if err != nil {
		t.Fatal(err)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: tickets.Provider, Origin: tickets.Origin, RemoteID: "101"}, Locator: ticket.Locator{Provider: tickets.Provider, Origin: tickets.Origin, Container: tickets.Destination, Key: "1"}}
	binding, err := db.AddTicketBinding(t.Context(), TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AttachExecutionSnapshot(t.Context(), ExecutionSnapshot{SessionID: pinned.ID, RepoID: repo.ID, BindingID: binding.ID, CodeVersionID: code.ID, TicketVersionID: tickets.ID, ActorID: 42}); err != nil {
		t.Fatal(err)
	}
	live := repo
	live.BaseBranch = "develop"
	if _, err = db.UpsertRepo(t.Context(), live); err != nil {
		t.Fatal(err)
	}
	snap, err := db.GetExecutionSnapshot(t.Context(), pinned.ID)
	if err != nil || snap.CodeTarget.BaseBranch != "main" {
		t.Fatalf("snapshot target=%q err=%v, want retained main", snap.CodeTarget.BaseBranch, err)
	}

	for name, want := range map[string]struct {
		id   int64
		base string
	}{"pinned": {pinned.ID, "main"}, "unpinned": {unpinned.ID, "develop"}} {
		owner, err := db.GetRetirementOwner(t.Context(), want.id)
		if err != nil || owner.Repo.BaseBranch != want.base {
			t.Fatalf("%s owner repo base=%q err=%v, want %s", name, owner.Repo.BaseBranch, err, want.base)
		}
	}
}

func TestRetirementOwnerRepoKeepsLegacyBasePin(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")
	if _, err := db.SetConnection(t.Context(), connectionFixture(repo, CodeConnection), 0); err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ChangeRepoBase(t.Context(), repo.ID, "develop"); err != nil {
		t.Fatal(err)
	}
	owner, err := db.GetRetirementOwner(t.Context(), session.ID)
	if err != nil || owner.Repo.BaseBranch != "main" {
		t.Fatalf("owner repo base=%q err=%v, want legacy pin main", owner.Repo.BaseBranch, err)
	}
}
