package store

import (
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func approverFixture(t *testing.T) (*DB, ConnectionVersion, ApproverAppointment) {
	t.Helper()
	db := newTestDB(t)
	repo := testRepo(t, db, "code-owner", "repo")
	conn, err := db.SetConnection(t.Context(), connectionFixture(repo, TicketConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	a := ApproverAppointment{RepoID: repo.ID, Actor: ticket.Identity{Provider: ticket.GitHub, Origin: conn.Origin, RemoteID: "42"}, Handle: "ticket-owner", VerifiedAt: time.Now(), Provenance: "explicit"}
	return db, conn, a
}
func TestProjectApproverCASRefreshAndClaims(t *testing.T) {
	db, conn, a := approverFixture(t)
	a, err := db.SetApprover(t.Context(), a, conn.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	refreshed := a
	refreshed.Handle = "renamed"
	refreshed.VerifiedAt = a.VerifiedAt.Add(time.Hour)
	got, err := db.SetApprover(t.Context(), refreshed, conn.Revision, a.Revision)
	if err != nil || got.ID != a.ID || got.Revision != a.Revision || !got.ActivatedAt.Equal(a.ActivatedAt) {
		t.Fatalf("refresh: %+v %v", got, err)
	}
	claim := AuthorityClaim{RepoID: a.RepoID, AppointmentID: a.ID, Revision: a.Revision, Actor: a.Actor, Source: ticket.Identity{Provider: ticket.GitHub, Origin: conn.Origin, RemoteID: "comment:1"}, CreatedAt: a.ActivatedAt.Add(time.Second), Kind: "instruction", Key: "1"}
	before := claim
	before.CreatedAt = a.ActivatedAt
	if _, err = db.ClaimProjectAuthority(t.Context(), before); !errors.Is(err, ErrApproverReplay) {
		t.Fatalf("equal boundary admitted: %v", err)
	}
	other := claim
	other.Actor.Origin = "https://enterprise.example/api/v3"
	if _, err = db.ClaimProjectAuthority(t.Context(), other); !errors.Is(err, ErrApproverUnauthorized) {
		t.Fatalf("origin collision admitted: %v", err)
	}
	claim, err = db.ClaimProjectAuthority(t.Context(), claim)
	if err != nil {
		t.Fatal(err)
	}
	replacement := a
	replacement.Actor.RemoteID = "43"
	if _, err = db.SetApprover(t.Context(), replacement, conn.Revision, a.Revision); !errors.Is(err, ErrApproverBusy) {
		t.Fatalf("claimed replacement: %v", err)
	}
	if err = db.CompleteProjectAuthority(t.Context(), claim.ID); err != nil {
		t.Fatal(err)
	}
	_, err = db.SetApprover(t.Context(), replacement, conn.Revision, a.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimProjectAuthority(t.Context(), claim); !errors.Is(err, ErrApproverConflict) {
		t.Fatalf("old revision admitted: %v", err)
	}
	original, err := db.GetApproverAppointment(t.Context(), a.ID)
	if err != nil || original.Actor != a.Actor {
		t.Fatalf("history lost: %+v %v", original, err)
	}
}
func TestProjectApproverAdmissionReplacementRace(t *testing.T) {
	db, conn, a := approverFixture(t)
	a, err := db.SetApprover(t.Context(), a, conn.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, e := db.ClaimProjectAuthority(t.Context(), AuthorityClaim{RepoID: a.RepoID, AppointmentID: a.ID, Revision: a.Revision, Actor: a.Actor, Kind: "worker", Key: "task-scan"})
		results <- e
	}()
	go func() {
		<-start
		next := a
		next.Actor.RemoteID = "43"
		_, e := db.SetApprover(t.Context(), next, conn.Revision, a.Revision)
		results <- e
	}()
	close(start)
	one, two := <-results, <-results
	if (one == nil) == (two == nil) {
		t.Fatalf("exactly one operation must win: %v / %v", one, two)
	}
	failed := one
	if failed == nil {
		failed = two
	}
	if !errors.Is(failed, ErrApproverConflict) && !errors.Is(failed, ErrApproverBusy) {
		t.Fatalf("unsafe failure: %v", failed)
	}
}
func TestProjectApproverPendingReportAndConnectionCAS(t *testing.T) {
	db, conn, a := approverFixture(t)
	a, err := db.SetApprover(t.Context(), a, conn.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	next := a
	next.Actor.RemoteID = "43"
	if _, err = db.SetApprover(t.Context(), next, conn.Revision+1, a.Revision); !errors.Is(err, ErrConnectionConflict) {
		t.Fatalf("stale connection: %v", err)
	}
	repo, err := db.GetRepo(t.Context(), a.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(t.Context(), `INSERT INTO report_deliveries(delivery_key,identity_version,session_id,trigger_id,task,destination,trigger_namespace,purpose,bot_user_id,body,marker,possibly_sent) VALUES('pending',1,?,1,?,'code-owner/repo','comment','report',99,'prepared bytes','marker',1)`, session.ID, session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetApprover(t.Context(), next, conn.Revision, a.Revision); !errors.Is(err, ErrApproverBusy) {
		t.Fatalf("uncertain report did not block: %v", err)
	}
	var body string
	if err = db.sql.QueryRowContext(t.Context(), `SELECT body FROM report_deliveries WHERE delivery_key='pending'`).Scan(&body); err != nil || body != "prepared bytes" {
		t.Fatalf("effect mutated: %q %v", body, err)
	}
}

func TestProjectApproverLegacyContinuityAndCorrection(t *testing.T) {
	db, conn, a := approverFixture(t)
	if _, err := db.ImportLegacyApprover(t.Context(), a, conn.Revision); !errors.Is(err, ErrApproverMissing) {
		t.Fatalf("new project inherited legacy authority: %v", err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO project_legacy_approver_eligibility(repo_id) VALUES(?)`, a.RepoID); err != nil {
		t.Fatal(err)
	}
	a, err := db.ImportLegacyApprover(t.Context(), a, conn.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if a.RequireFresh {
		t.Fatal("legacy import reset pending reply boundary")
	}
	repo, err := db.GetRepo(t.Context(), a.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.RecordComment(t.Context(), Comment{SessionID: session.ID, ForgeCommentID: 5, AuthorHandle: a.Handle, AuthorUserID: 42, Body: "original instruction", CreatedAt: a.VerifiedAt, Classification: ClassificationInstruction}); err != nil {
		t.Fatal(err)
	}
	if err = db.ReconcileLegacyApproverEvidence(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if err = db.ReconcileLegacyApproverEvidence(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM project_legacy_authority_evidence WHERE repo_id=? AND record_kind='session' AND record_key=?`, a.RepoID, session.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("repeat evidence: %d %v", count, err)
	}
	repeated, err := db.ImportLegacyApprover(t.Context(), a, conn.Revision)
	if err != nil || repeated.ID != a.ID || repeated.RequireFresh {
		t.Fatalf("repeat initialization: %+v %v", repeated, err)
	}
	if err = db.SetApproverImportConflict(t.Context(), a.RepoID, "verify historical identity"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CurrentApprover(t.Context(), a.RepoID); !errors.Is(err, ErrApproverUnauthorized) {
		t.Fatalf("conflict did not stop admission: %v", err)
	}
	retained, err := db.RawCurrentApprover(t.Context(), a.RepoID)
	if err != nil || retained.ID != a.ID {
		t.Fatalf("conflict lost retained appointment: %+v %v", retained, err)
	}
	next := a
	next.Actor.RemoteID = "43"
	next, err = db.SetApprover(t.Context(), next, conn.Revision, a.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.CurrentApprover(t.Context(), a.RepoID); err != nil {
		t.Fatal(err)
	}
	old, err := db.GetApproverAppointment(t.Context(), a.ID)
	if err != nil || old.Actor != a.Actor || next.ID == a.ID {
		t.Fatalf("correction changed history: %+v %v", old, err)
	}
}

func TestProjectApproverCodeClaimAndWorkerReactivation(t *testing.T) {
	db, conn, a := approverFixture(t)
	repo, err := db.GetRepo(t.Context(), a.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetConnection(t.Context(), connectionFixture(repo, CodeConnection), 0); err != nil {
		t.Fatal(err)
	}
	code := AuthorityClaim{RepoID: a.RepoID, Actor: ticket.Identity{Provider: ticket.GitHub, Origin: conn.Origin, RemoteID: "7"}, Source: ticket.Identity{Provider: ticket.GitHub, Origin: conn.Origin, RemoteID: "comment:1"}, CreatedAt: time.Now(), Kind: "code-control", Key: "1"}
	code, err = db.ClaimProjectCodeAuthority(t.Context(), code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetApprover(t.Context(), a, conn.Revision, 0); !errors.Is(err, ErrApproverBusy) {
		t.Fatalf("code claim did not guard initial set: %v", err)
	}
	if err = db.CompleteProjectAuthority(t.Context(), code.ID); err != nil {
		t.Fatal(err)
	}
	a, err = db.SetApprover(t.Context(), a, conn.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	worker := AuthorityClaim{RepoID: a.RepoID, AppointmentID: a.ID, Revision: a.Revision, Actor: a.Actor, Kind: "worker", Key: "scan"}
	worker, err = db.ClaimProjectAuthority(t.Context(), worker)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteProjectAuthority(t.Context(), worker.ID); err != nil {
		t.Fatal(err)
	}
	repeated, err := db.ClaimProjectAuthority(t.Context(), worker)
	if err != nil || repeated.Completed || repeated.ID != worker.ID {
		t.Fatalf("worker did not reacquire barrier: %+v %v", repeated, err)
	}
	next := a
	next.Actor.RemoteID = "43"
	if _, err = db.SetApprover(t.Context(), next, conn.Revision, a.Revision); !errors.Is(err, ErrApproverBusy) {
		t.Fatalf("reactivated worker not blocking: %v", err)
	}
	if err = db.RecoverProjectWorkerClaims(t.Context(), a.RepoID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetApprover(t.Context(), next, conn.Revision, a.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestProjectApproverUnpublishedDecisionDebt(t *testing.T) {
	db, conn, a := approverFixture(t)
	a, err := db.SetApprover(t.Context(), a, conn.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := db.GetRepo(t.Context(), a.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO owner_exceptions(parent_task,generation,comment_id,actor_id,request_json,expansion_json) VALUES(?,1,11,42,'{}','[]')`, session.TaskRef); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO plan_story_notices(parent,story_key,material_json) VALUES(?,'story-003','{}')`, session.TaskRef); err != nil {
		t.Fatal(err)
	}
	next := a
	next.Actor.RemoteID = "43"
	if _, err = db.SetApprover(t.Context(), next, conn.Revision, a.Revision); !errors.Is(err, ErrApproverBusy) {
		t.Fatalf("unpublished decision allowed replacement: %v", err)
	}
	blockers, err := db.ApproverBlockers(t.Context(), a.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, b := range blockers {
		found[b.Kind] = true
	}
	if !found["owner_exception_report"] || !found["plan_notice"] {
		t.Fatalf("decision owners missing: %+v", blockers)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE owner_exceptions SET report_comment_id=99 WHERE parent_task=?`, session.TaskRef); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE plan_story_notices SET delivered=1 WHERE parent=?`, session.TaskRef); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetApprover(t.Context(), next, conn.Revision, a.Revision); err != nil {
		t.Fatal(err)
	}
	var actor int64
	if err = db.sql.QueryRowContext(t.Context(), `SELECT actor_id FROM owner_exceptions WHERE parent_task=?`, session.TaskRef).Scan(&actor); err != nil || actor != 42 {
		t.Fatalf("historical exception changed actor: %d %v", actor, err)
	}
}

func TestProjectApproverCodeClaimRetainsSessionConnections(t *testing.T) {
	db, tickets, a := approverFixture(t)
	repo, err := db.GetRepo(t.Context(), a.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	code, err := db.SetConnection(t.Context(), connectionFixture(repo, CodeConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	a, err = db.SetApprover(t.Context(), a, tickets.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	ref := resolvedSessionTicket(repo.Slug(), "700")
	if _, err = db.AddTicketBinding(t.Context(), TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	session, err := db.AdmitSession(t.Context(), repo, ref, code.Revision, tickets.Revision, 99, "publisher", TaskRef("github", repo.Owner, repo.Name, 7))
	if err != nil {
		t.Fatal(err)
	}
	changed := code
	changed.Origin = "https://code.example/api/v3"
	changed.Enabled = false
	if _, err = db.SetConnection(t.Context(), changed, code.Revision); err != nil {
		t.Fatal(err)
	}
	changed = tickets
	changed.Origin = "https://tickets.example/api/v3"
	changed.Enabled = false
	if _, err = db.SetConnection(t.Context(), changed, tickets.Revision); err != nil {
		t.Fatal(err)
	}
	claim := AuthorityClaim{RepoID: repo.ID, SessionID: session.ID, AppointmentID: a.ID, Revision: a.Revision, Actor: ticket.Identity{Provider: ticket.GitHub, Origin: code.Origin, RemoteID: "41"}, Source: ticket.Identity{Provider: ticket.GitHub, Origin: tickets.Origin, RemoteID: "123"}, CreatedAt: time.Now(), Kind: "code-control", Key: "retained-session"}
	if _, err = db.ClaimProjectCodeAuthority(t.Context(), claim); err != nil {
		t.Fatalf("saved controls lost retained authority: %v", err)
	}
	wrong := claim
	wrong.Key = "wrong-new-code-origin"
	wrong.Actor.Origin = "https://code.example/api/v3"
	if _, err = db.ClaimProjectCodeAuthority(t.Context(), wrong); !errors.Is(err, ErrApproverUnauthorized) {
		t.Fatalf("current code origin authorized saved control: %v", err)
	}
	wrong = claim
	wrong.Key = "wrong-new-ticket-origin"
	wrong.Source.Origin = "https://tickets.example/api/v3"
	if _, err = db.ClaimProjectCodeAuthority(t.Context(), wrong); !errors.Is(err, ErrApproverUnauthorized) {
		t.Fatalf("current ticket origin authorized saved control: %v", err)
	}
	wrong = claim
	wrong.Key = "stale-authority"
	wrong.Revision++
	if _, err = db.ClaimProjectCodeAuthority(t.Context(), wrong); !errors.Is(err, ErrApproverConflict) {
		t.Fatalf("retained source bypassed authority revision: %v", err)
	}
}

func TestProjectApproverRetainedCodeWorkerIsReplacementBarrier(t *testing.T) {
	db, tickets, a := approverFixture(t)
	repo, err := db.GetRepo(t.Context(), a.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	code, err := db.SetConnection(t.Context(), connectionFixture(repo, CodeConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	a, err = db.SetApprover(t.Context(), a, tickets.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	ref := resolvedSessionTicket(repo.Slug(), "700")
	if _, err = db.AddTicketBinding(t.Context(), TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	session, err := db.AdmitSession(t.Context(), repo, ref, code.Revision, tickets.Revision, 99, "publisher", TaskRef("github", repo.Owner, repo.Name, 7))
	if err != nil {
		t.Fatal(err)
	}
	changed := tickets
	changed.Enabled = false
	changed.Origin = "https://tickets.example/api/v3"
	changed, err = db.SetConnection(t.Context(), changed, tickets.Revision)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := db.ClaimProjectCodeAuthority(t.Context(), AuthorityClaim{RepoID: repo.ID, SessionID: session.ID, AppointmentID: a.ID, Revision: a.Revision, Actor: ticket.Identity{Provider: ticket.GitHub, Origin: code.Origin, RemoteID: "41"}, Source: ref.Identity, Kind: "worker", Key: "saved-code-poll"})
	if err != nil {
		t.Fatalf("retained polling barrier rejected: %v", err)
	}
	restored, err := db.SetConnection(t.Context(), tickets, changed.Revision)
	if err != nil {
		t.Fatal(err)
	}
	next := a
	next.Actor.RemoteID = "43"
	if _, err = db.SetApprover(t.Context(), next, restored.Revision, a.Revision); !errors.Is(err, ErrApproverBusy) {
		t.Fatalf("polling worker allowed replacement: %v", err)
	}
	if err = db.CompleteProjectAuthority(t.Context(), worker.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetApprover(t.Context(), next, restored.Revision, a.Revision); err != nil {
		t.Fatalf("completed polling barrier stayed outstanding: %v", err)
	}
}
