package turn

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/harness"
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
	attachTicketSnapshot(t, db, repo, session.ID, v, ref)
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

// attachTicketSnapshot pins a session to tickets the way admission does, so
// its turns are authorized against the retained ticket source.
func attachTicketSnapshot(t *testing.T, db *store.DB, repo store.Repo, sessionID int64, tickets store.ConnectionVersion, ref ticket.Ref) {
	t.Helper()
	code, err := db.CurrentConnection(t.Context(), repo.ID, store.CodeConnection)
	if errors.Is(err, store.ErrNotFound) {
		code, err = db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: repo.Slug(), CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
	}
	mustProgress(t, err)
	binding, err := db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
	mustProgress(t, err)
	mustProgress(t, db.AttachExecutionSnapshot(t.Context(), store.ExecutionSnapshot{SessionID: sessionID, RepoID: repo.ID, BindingID: binding.ID, CodeVersionID: code.ID, TicketVersionID: tickets.ID, ActorID: 99, ActorLogin: "publisher"}))
}

// A started session keeps its ticket source after the project's ticket
// connection is disabled or replaced; only new work sees the change (#712).
func TestSavedTurnsRetainTicketSourceAfterConnectionChanges(t *testing.T) {
	for _, provider := range []ticket.Provider{ticket.GitHub, ticket.Linear} {
		t.Run(string(provider), func(t *testing.T) {
			f := newFixture(t, newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
				t.Fatal("the turn must stop at clone verification in this fixture")
				return nil, nil
			}))
			ctx := t.Context()
			tickets := store.ConnectionVersion{RepoID: f.storeRepo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: f.storeRepo.Slug(), CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit"}
			if provider == ticket.Linear {
				tickets.Provider, tickets.Origin, tickets.Destination = ticket.Linear, "https://api.linear.app", "team/project"
				tickets.Linear = store.LinearConfiguration{WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan","implement":"label-implement"}`, LifecycleStatesJSON: `{"completed":"done","retired":"cancelled"}`}
			}
			tickets, err := f.db.SetConnection(ctx, tickets, 0)
			mustProgress(t, err)
			actor := ticket.Identity{Provider: tickets.Provider, Origin: tickets.Origin, RemoteID: "42"}
			authorID := int64(42)
			if provider == ticket.Linear {
				actor.RemoteID = "opaque-owner"
				authorID, err = f.db.LocalTicketSlot(ctx, "actor", actor)
				mustProgress(t, err)
			}
			a, err := f.db.SetApprover(ctx, store.ApproverAppointment{RepoID: f.storeRepo.ID, Actor: actor, Handle: "approver", VerifiedAt: time.Now(), Provenance: "explicit"}, tickets.Revision, 0)
			mustProgress(t, err)
			ref := ticket.Ref{Identity: ticket.Identity{Provider: tickets.Provider, Origin: tickets.Origin, RemoteID: "ticket-700"}, Locator: ticket.Locator{Provider: tickets.Provider, Origin: tickets.Origin, Container: tickets.Destination, Key: strconv.Itoa(testIssue)}}
			attachTicketSnapshot(t, f.db, f.storeRepo, f.session.ID, tickets, ref)
			disabled, err := f.db.DisableConnection(ctx, f.storeRepo.ID, store.TicketConnection, tickets.Revision)
			mustProgress(t, err)

			// C1: an ordinary owner turn on the saved session passes ticket
			// authorization. The fixture's local origin is not a forge remote,
			// so the retained code target stops the turn later, at clone
			// verification, which is outside ticket authority.
			f.opts.ProjectApprovers = true
			req := f.request()
			req.Trigger.AuthorID = authorID
			req.Trigger.Actor = actor
			req.Trigger.Source = ticket.Identity{Provider: tickets.Provider, Origin: tickets.Origin, RemoteID: "reply-1"}
			req.Trigger.CreatedAt = a.ActivatedAt.Add(time.Second)
			_, err = Execute(ctx, f.opts, req)
			if errors.Is(err, store.ErrConnectionDisabled) || errors.Is(err, store.ErrApproverUnauthorized) || errors.Is(err, store.ErrApproverMissing) || err == nil || !strings.Contains(err.Error(), "original execution target is unavailable") {
				t.Fatalf("saved turn did not pass ticket authority after disable: %v", err)
			}
			opts, err := bindProjectApprover(ctx, f.opts, f.session.ID)
			if err != nil || opts.TicketApprover == nil || opts.TicketApprover.ID != a.ID {
				t.Fatalf("saved binding lost approver after disable: %+v %v", opts.TicketApprover, err)
			}

			// C2: replacing the connection with another provider or origin
			// leaves the saved session on its original source.
			replacement := store.ConnectionVersion{RepoID: f.storeRepo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: "https://replacement.example/api/v3", Destination: "other/tasks", CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit"}
			_, err = f.db.SetConnection(ctx, replacement, disabled.Revision)
			mustProgress(t, err)
			opts, err = bindProjectApprover(ctx, f.opts, f.session.ID)
			if err != nil || opts.TicketApprover == nil {
				t.Fatalf("saved binding refused after replacement: %v", err)
			}
			req.Trigger.CommentID++
			req.Trigger.Source.RemoteID = "reply-2"
			claim, err := claimTurnAuthority(ctx, opts, req)
			if err != nil {
				t.Fatalf("saved claim refused after replacement: %v", err)
			}
			mustProgress(t, f.db.CompleteProjectAuthority(ctx, claim.ID))

			// New work is still checked against the current connection.
			_, err = f.db.ClaimProjectAuthority(ctx, store.AuthorityClaim{RepoID: a.RepoID, AppointmentID: a.ID, Revision: a.Revision, Actor: actor, Source: req.Trigger.Source, CreatedAt: req.Trigger.CreatedAt, Kind: "turn", Key: "new-work"})
			if !errors.Is(err, store.ErrApproverUnauthorized) {
				t.Fatalf("new work authorized against replaced connection: %v", err)
			}
		})
	}
}
