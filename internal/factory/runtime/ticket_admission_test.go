package runtime

import (
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/access"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// An allowed grantee's comment is proven by its access decision row alone: it
// is eligible, yet the approver's claims ledger gains nothing. The owner's own
// comment still writes its child-instruction claim.
func TestEligibleCommentWritesNoApproverClaimForGrantee(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "admission.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	counter, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := counter.Close(); err != nil {
			t.Error(err)
		}
	})
	claims := func() int {
		t.Helper()
		var n int
		if err := counter.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_authority_claims`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	repo, err := db.UpsertRepo(ctx, store.Repo{Path: "/admission", Forge: store.ForgeGitHub, Owner: "acme", Name: "tickets", RemoteURL: "git@github.com:acme/tickets.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: "/admission-trees"})
	if err != nil {
		t.Fatal(err)
	}
	origin := ticket.Origin("https://api.github.com")
	tickets, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: origin, Destination: repo.Slug(), CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	code, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: origin, Destination: repo.Slug(), CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
	if err != nil {
		t.Fatal(err)
	}
	approver := ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "42"}
	appointment, err := db.SetApprover(ctx, store.ApproverAppointment{RepoID: repo.ID, Actor: approver, Handle: "approver", VerifiedAt: time.Now(), Provenance: "explicit"}, tickets.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, repo, 9)
	if err != nil {
		t.Fatal(err)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "909"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: repo.Slug(), Key: "9"}}
	if _, err = db.BindSessionTicket(ctx, session.ID, ref); err != nil {
		t.Fatal(err)
	}
	binding, err := db.AddTicketBinding(ctx, store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AttachExecutionSnapshot(ctx, store.ExecutionSnapshot{SessionID: session.ID, RepoID: repo.ID, BindingID: binding.ID, CodeVersionID: code.ID, TicketVersionID: tickets.ID, ActorID: 99, ActorLogin: "publisher"}); err != nil {
		t.Fatal(err)
	}

	admission := NewTicketAdmission(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	created := appointment.ActivatedAt.Add(time.Minute)
	granteeActor := ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "77"}
	allow := func(remoteID, body string) {
		t.Helper()
		row, began, err := db.BeginAccessDecision(ctx, store.AccessDecision{RepoID: repo.ID, SessionID: session.ID,
			Source: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: remoteID}, Actor: granteeActor, Handle: "helper",
			TargetJSON: `{}`, RequestDigest: access.Digest(body)})
		if err != nil || !began {
			t.Fatalf("begin %s: %v %v", remoteID, began, err)
		}
		if _, err = db.FinishAccessDecision(ctx, row.ID, store.AccessDecisionOutcome{Decision: store.AccessAllow, Reason: "checked"}); err != nil {
			t.Fatal(err)
		}
	}
	comment := func(id, author int64, body string) github.Comment {
		return github.Comment{ID: id, Body: body, Author: github.User{ID: author, Login: "someone"}, CreatedAt: created}
	}

	// Control: the owner's comment writes its claim.
	before := claims()
	ok, err := admission.EligibleComment(ctx, session.ID, comment(100, 42, "owner says go"))
	if err != nil || !ok {
		t.Fatalf("owner comment: ok=%v err=%v", ok, err)
	}
	if after := claims(); after != before+1 {
		t.Fatalf("owner comment claims: %d -> %d, want one new claim", before, after)
	}

	// Allowed grantee comment and managed-story input: eligible, no claim.
	allow("101", "please add a health endpoint")
	allow("102", "managed child input from the grantee")
	for _, tc := range []struct {
		name string
		c    github.Comment
	}{
		{"comment", comment(101, 77, "please add a health endpoint")},
		{"managed story input", comment(102, 77, "managed child input from the grantee")},
	} {
		before = claims()
		ok, err = admission.EligibleComment(ctx, session.ID, tc.c)
		if err != nil || !ok {
			t.Fatalf("%s: ok=%v err=%v", tc.name, ok, err)
		}
		if after := claims(); after != before {
			t.Fatalf("%s: claims changed %d -> %d", tc.name, before, after)
		}
	}

	// A grantee comment without an allow decision stays ineligible, no claim.
	before = claims()
	ok, err = admission.EligibleComment(ctx, session.ID, comment(103, 77, "no decision"))
	if err != nil || ok || claims() != before {
		t.Fatalf("undecided grantee: ok=%v err=%v claims=%d->%d", ok, err, before, claims())
	}
}
