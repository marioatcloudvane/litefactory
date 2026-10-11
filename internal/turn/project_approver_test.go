package turn

import (
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestTurnApproverBindingAndQualifiedClaims(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/bound", Forge: "github", Owner: "code-owner", Name: "tickets", RemoteURL: "git@github.com:code-owner/tickets.git"})
	if err != nil {
		t.Fatal(err)
	}
	origin := ticket.Origin("https://api.github.com")
	v, err := db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: origin, Destination: repo.Slug(), CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	actor := ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "42"}
	a, err := db.SetApprover(t.Context(), store.ApproverAppointment{RepoID: repo.ID, Actor: actor, Handle: "approver", VerifiedAt: time.Now(), Provenance: "explicit"}, v.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 9)
	if err != nil {
		t.Fatal(err)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "909"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: repo.Slug(), Key: "9"}}
	if _, err = db.BindSessionTicket(t.Context(), session.ID, ref); err != nil {
		t.Fatal(err)
	}
	opts, err := bindProjectApprover(t.Context(), Options{DB: db, ProjectApprovers: true}, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := ticketOwnerID(t.Context(), opts)
	if err != nil || owner != 42 {
		t.Fatalf("owner=%d err=%v", owner, err)
	}
	for _, tc := range []struct {
		name    string
		actor   ticket.Identity
		created time.Time
		author  int64
		want    error
	}{
		{"fresh", actor, a.ActivatedAt.Add(time.Second), 42, nil},
		{"before", actor, a.ActivatedAt.Add(-time.Second), 42, store.ErrApproverReplay},
		{"equal", actor, a.ActivatedAt, 42, store.ErrApproverReplay},
		{"missing-time", actor, time.Time{}, 42, store.ErrApproverReplay},
		{"origin-collision", ticket.Identity{Provider: ticket.GitHub, Origin: "https://enterprise.example/api/v3", RemoteID: "42"}, a.ActivatedAt.Add(time.Second), 42, store.ErrApproverUnauthorized},
		{"wrong-actor", ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "43"}, a.ActivatedAt.Add(time.Second), 43, store.ErrApproverUnauthorized},
		{"author-mismatch", actor, a.ActivatedAt.Add(time.Second), 43, store.ErrApproverUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trigger := Trigger{CommentID: 101, AuthorID: tc.author, Actor: tc.actor, Source: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: strconv.Itoa(101)}, CreatedAt: tc.created}
			claim, e := claimTurnAuthority(t.Context(), opts, Request{SessionID: session.ID, Trigger: trigger})
			if tc.want != nil {
				if !errors.Is(e, tc.want) {
					t.Fatalf("want %v got %v", tc.want, e)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = db.CompleteProjectAuthority(t.Context(), claim.ID); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestValidateShipSeparatesUnfinishedTicketDecisions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		finding ValidateFinding
		want    bool
	}{
		{"open acceptance", ValidateFinding{State: findingAwaitingCheck}, true},
		{"optional choice", ValidateFinding{Optional: true, State: findingOpen}, true},
		{"choice already recorded", ValidateFinding{Optional: true, State: findingOpen, Choice: &store.ValidateChoice{Choice: store.ChoiceDefer}}, false},
		{"settled", ValidateFinding{State: "passed"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateShipSettlesTicketDecisions(ValidateState{Findings: []ValidateFinding{tc.finding}}); got != tc.want {
				t.Fatalf("settles=%t want=%t", got, tc.want)
			}
		})
	}
}
