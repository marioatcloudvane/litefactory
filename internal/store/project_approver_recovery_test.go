package store

import (
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestProjectApproverRemoteRetryReacquiresImmutableClaim(t *testing.T) {
	db, conn, a := approverFixture(t)
	a, err := db.SetApprover(t.Context(), a, conn.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	c := AuthorityClaim{RepoID: a.RepoID, AppointmentID: a.ID, Revision: a.Revision, Actor: a.Actor, Source: ticket.Identity{Provider: ticket.GitHub, Origin: conn.Origin, RemoteID: "123"}, CreatedAt: a.ActivatedAt.Add(time.Second), Kind: "creation-approval", Key: "original-source"}
	c, err = db.ClaimProjectAuthority(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteProjectAuthority(t.Context(), c.ID); err != nil {
		t.Fatal(err)
	}
	drift := c
	drift.CreatedAt = c.CreatedAt.Add(time.Second)
	if _, err = db.ClaimProjectAuthority(t.Context(), drift); !errors.Is(err, ErrApproverConflict) {
		t.Fatalf("immutable source time drift admitted: %v", err)
	}
	retried, err := db.ClaimProjectAuthority(t.Context(), c)
	if err != nil || retried.ID != c.ID || retried.Completed {
		t.Fatalf("retry failed to reacquire guard: %+v %v", retried, err)
	}
	next := a
	next.Actor.RemoteID = "43"
	if _, err = db.SetApprover(t.Context(), next, conn.Revision, a.Revision); !errors.Is(err, ErrApproverBusy) {
		t.Fatalf("retry did not block replacement: %v", err)
	}
}

func TestProjectApproverRestartRecoversConsumedSourceKeepsUncertainEffect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo := testRepo(t, db, "code-owner", "repo")
	conn, err := db.SetConnection(t.Context(), connectionFixture(repo, TicketConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.SetApprover(t.Context(), ApproverAppointment{RepoID: repo.ID, Actor: ticket.Identity{Provider: ticket.GitHub, Origin: conn.Origin, RemoteID: "42"}, Handle: "ticket-owner", VerifiedAt: time.Now(), Provenance: "explicit"}, conn.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.BindLegacySessionOrigin(t.Context(), session.ID, conn.Origin); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetConnection(t.Context(), connectionFixture(repo, CodeConnection), 0); err != nil {
		t.Fatal(err)
	}
	created := a.ActivatedAt.Add(time.Second)
	if _, err = db.RecordComment(t.Context(), Comment{SessionID: session.ID, ForgeCommentID: 123, AuthorHandle: a.Handle, AuthorUserID: 42, Body: "fresh original instruction", CreatedAt: created, Classification: ClassificationInstruction}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.BootstrapWatermark(t.Context(), session.ID, 0); err != nil {
		t.Fatal(err)
	}
	claim, err := db.ClaimProjectAuthority(t.Context(), AuthorityClaim{RepoID: repo.ID, AppointmentID: a.ID, Revision: a.Revision, Actor: a.Actor, Source: ticket.Identity{Provider: ticket.GitHub, Origin: conn.Origin, RemoteID: "123"}, CreatedAt: created, Kind: "turn", Key: strconv.FormatInt(session.ID, 10) + ":123"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.RecordComment(t.Context(), Comment{SessionID: session.ID, ForgeCommentID: 125, AuthorHandle: "code-owner", AuthorUserID: 7, Body: "/merge exact-target", CreatedAt: created, Classification: ClassificationThirdParty}); err != nil {
		t.Fatal(err)
	}
	codeClaim, err := db.ClaimProjectCodeAuthority(t.Context(), AuthorityClaim{RepoID: repo.ID, AppointmentID: a.ID, Revision: a.Revision, Actor: ticket.Identity{Provider: ticket.GitHub, Origin: conn.Origin, RemoteID: "7"}, Source: ticket.Identity{Provider: ticket.GitHub, Origin: conn.Origin, RemoteID: "125"}, CreatedAt: created, Kind: "code-control", Key: strconv.FormatInt(session.ID, 10) + ":125"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.EnsureReportDelivery(t.Context(), ReportDelivery{Key: "uncertain", SessionID: session.ID, TriggerID: 123, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "comment", Purpose: "reply", BotUserID: 99, Body: "original prepared bytes", Marker: "original-marker"}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.MarkReportDeliverySent(t.Context(), "uncertain"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	})
	if err = db.RecoverConsumedProjectAuthorityClaims(t.Context(), repo.ID); err != nil {
		t.Fatal(err)
	}
	var completed bool
	if err = db.sql.QueryRowContext(t.Context(), `SELECT completed FROM project_authority_claims WHERE id=?`, claim.ID).Scan(&completed); err != nil || completed {
		t.Fatalf("unconsumed source guard cleared: %t %v", completed, err)
	}
	if _, err = db.AdvanceWatermark(t.Context(), session.ID, 125); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE sessions SET lock_owner='active-worker' WHERE id=?`, session.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.RecoverConsumedProjectAuthorityClaims(t.Context(), repo.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.sql.QueryRowContext(t.Context(), `SELECT completed FROM project_authority_claims WHERE id=?`, claim.ID).Scan(&completed); err != nil || completed {
		t.Fatalf("live guard cleared after source consumption: %t %v", completed, err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE sessions SET lock_owner=NULL WHERE id=?`, session.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.RecoverConsumedProjectAuthorityClaims(t.Context(), repo.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.sql.QueryRowContext(t.Context(), `SELECT completed FROM project_authority_claims WHERE id=?`, claim.ID).Scan(&completed); err != nil || !completed {
		t.Fatalf("consumed qualified source not recovered: %t %v", completed, err)
	}
	if err = db.sql.QueryRowContext(t.Context(), `SELECT completed FROM project_authority_claims WHERE id=?`, codeClaim.ID).Scan(&completed); err != nil || !completed {
		t.Fatalf("numeric-session code guard not recovered: %t %v", completed, err)
	}
	next := a
	next.Actor.RemoteID = "43"
	if _, err = db.SetApprover(t.Context(), next, conn.Revision, a.Revision); !errors.Is(err, ErrApproverBusy) {
		t.Fatalf("uncertain publication stopped blocking: %v", err)
	}
	effect, err := db.GetReportDelivery(t.Context(), "uncertain")
	if err != nil || effect.Body != "original prepared bytes" || !effect.PossiblySent || effect.Completed {
		t.Fatalf("uncertain effect changed: %+v %v", effect, err)
	}
	if _, err = db.ConfirmReportDelivery(t.Context(), effect.Key, 999); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CompleteReportDelivery(t.Context(), effect.Key); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetApprover(t.Context(), next, conn.Revision, a.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestProjectApproverDoctorGuardRecoversOnlyExactDurableIntake(t *testing.T) {
	db, conn, a := approverFixture(t)
	a, err := db.SetApprover(t.Context(), a, conn.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	makeClaim := func(id string) AuthorityClaim {
		return AuthorityClaim{RepoID: a.RepoID, AppointmentID: a.ID, Revision: a.Revision, Actor: a.Actor, Source: ticket.Identity{Provider: ticket.GitHub, Origin: conn.Origin, RemoteID: id}, CreatedAt: a.ActivatedAt.Add(time.Second), Kind: "doctor-instruction", Key: id}
	}
	admitted, err := db.ClaimProjectAuthority(t.Context(), makeClaim("123"))
	if err != nil {
		t.Fatal(err)
	}
	absent, err := db.ClaimProjectAuthority(t.Context(), makeClaim("125"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AdmitDoctorCommand(t.Context(), DoctorCommand{RepoID: a.RepoID, TaskRef: "github:code-owner/repo#7", IssueID: 700, IssueNumber: 7, CommentID: 123, ActorID: 42, CreatedAt: admitted.CreatedAt, Operation: "diagnose", Instructions: "original doctor instruction"}); err != nil {
		t.Fatal(err)
	}
	if err = db.RecoverConsumedProjectAuthorityClaims(t.Context(), a.RepoID); err != nil {
		t.Fatal(err)
	}
	var completed bool
	if err = db.sql.QueryRowContext(t.Context(), `SELECT completed FROM project_authority_claims WHERE id=?`, admitted.ID).Scan(&completed); err != nil || !completed {
		t.Fatalf("durable intake guard not recovered: %t %v", completed, err)
	}
	if err = db.sql.QueryRowContext(t.Context(), `SELECT completed FROM project_authority_claims WHERE id=?`, absent.ID).Scan(&completed); err != nil || completed {
		t.Fatalf("absent doctor source guard cleared: %t %v", completed, err)
	}
	next := a
	next.Actor.RemoteID = "43"
	if _, err = db.SetApprover(t.Context(), next, conn.Revision, a.Revision); !errors.Is(err, ErrApproverBusy) {
		t.Fatalf("queued doctor instruction ceased blocking: %v", err)
	}
}
