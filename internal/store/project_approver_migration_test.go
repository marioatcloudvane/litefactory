package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestProjectApproverPopulatedMigrationFreezesAuthenticatedHistory(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	})
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "project_approvers"))
	_, err = db.UpsertIdentity(t.Context(), Identity{Forge: ForgeGitHub, Role: RoleOwner, Handle: "original-owner", UserID: 42, Source: SourceGH})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := db.GetIdentity(t.Context(), ForgeGitHub, RoleOwner)
	if err != nil {
		t.Fatal(err)
	}
	repo := testRepo(t, db, "code-owner", "repo")
	connection, err := db.SetConnection(t.Context(), connectionFixture(repo, TicketConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	comment, err := db.RecordComment(t.Context(), Comment{SessionID: session.ID, ForgeCommentID: 123, AuthorHandle: owner.Handle, AuthorUserID: owner.UserID, Body: "original approved instruction\nbytes", CreatedAt: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), Classification: ClassificationInstruction})
	if err != nil {
		t.Fatal(err)
	}
	watermark, _, err := db.BootstrapWatermark(t.Context(), session.ID, 123)
	if err != nil {
		t.Fatal(err)
	}
	state := planbrief.PlanState{SharedRevision: "retained-shared", Stories: []planbrief.PlanStoryState{{Key: "story-003", Stage: "acceptance", Candidate: "exact-candidate", Approvals: map[string]string{"story": "approved-original"}, Wait: &planbrief.PlanWait{ID: "exact-wait", Stage: "acceptance", Candidate: "exact-candidate", IssueID: 7}}}, ConsumedReplies: []int64{123}}
	revision, err := db.SavePlanWorkflowState(t.Context(), session.TaskRef, 0, state)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(t.Context(), `INSERT INTO owner_exceptions(parent_task,generation,comment_id,actor_id,request_json,expansion_json,report_comment_id) VALUES(?,1,123,42,'{"candidate":"exact-candidate"}','[{"snapshot":"retained-snapshot"}]',124)`, session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := db.EnsureReportDelivery(t.Context(), ReportDelivery{Key: "retained-report", SessionID: session.ID, TriggerID: 123, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "comment", Purpose: "reply", BotUserID: 99, Body: "original prepared\npublication bytes", Marker: "retained-marker"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.MarkReportDeliverySent(t.Context(), delivery.Key); err != nil {
		t.Fatal(err)
	}
	delivery, err = db.GetReportDelivery(t.Context(), delivery.Key)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpsertIdentity(t.Context(), Identity{Forge: ForgeGitHub, Role: RoleOwner, Handle: "replacement-global", UserID: 43, Source: SourceGH}); err != nil {
		t.Fatal(err)
	}
	frozen, err := db.LegacyApproverIdentity(t.Context(), repo.ID)
	if err != nil || !reflect.DeepEqual(owner, frozen) {
		t.Fatalf("historical owner overwritten: %+v %v", frozen, err)
	}
	a := ApproverAppointment{RepoID: repo.ID, Actor: ticket.Identity{Provider: ticket.GitHub, Origin: connection.Origin, RemoteID: "42"}, Handle: owner.Handle, VerifiedAt: time.Now(), Provenance: "legacy"}
	a, err = db.ImportLegacyApprover(t.Context(), a, connection.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = db.ReconcileLegacyApproverEvidence(t.Context(), a); err != nil {
			t.Fatal(err)
		}
		if err = db.Migrate(t.Context()); err != nil {
			t.Fatal(err)
		}
		again, e := db.ImportLegacyApprover(t.Context(), a, connection.Revision)
		if e != nil || again.ID != a.ID || again.RequireFresh {
			t.Fatalf("repeat migration changed authority: %+v %v", again, e)
		}
	}
	gotComment, err := db.GetComment(t.Context(), session.ID, 123)
	if err != nil || !reflect.DeepEqual(comment, gotComment) {
		t.Fatalf("source changed: %+v %v", gotComment, err)
	}
	gotWatermark, _, err := db.GetWatermark(t.Context(), session.ID)
	if err != nil || !reflect.DeepEqual(watermark, gotWatermark) {
		t.Fatalf("consumption changed: %+v %v", gotWatermark, err)
	}
	gotState, gotRevision, err := db.GetPlanWorkflowState(t.Context(), session.TaskRef)
	if err != nil || gotRevision != revision || !reflect.DeepEqual(state, gotState) {
		t.Fatalf("saved approval/wait changed: %+v %v", gotState, err)
	}
	gotDelivery, err := db.GetReportDelivery(t.Context(), delivery.Key)
	if err != nil || !reflect.DeepEqual(delivery, gotDelivery) {
		t.Fatalf("prepared uncertain effect changed: %+v %v", gotDelivery, err)
	}
	exception, err := db.GetOwnerException(t.Context(), session.TaskRef, 123)
	if err != nil || exception.ActorID != 42 || exception.RequestJSON != "{\"candidate\":\"exact-candidate\"}" {
		t.Fatalf("historical approval rewritten: %+v %v", exception, err)
	}
	newRepo := testRepo(t, db, "new-owner", "new-repo")
	eligible, err := db.LegacyApproverEligible(t.Context(), newRepo.ID)
	if err != nil || eligible {
		t.Fatalf("new project inherited legacy authority: %t %v", eligible, err)
	}
	var count int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM project_approver_appointments WHERE repo_id=?`, repo.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("repeat migration duplicated appointment: %d %v", count, err)
	}
}

func TestProjectApproverLegacyBodyRunCannotInventAuthor(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "unattributed.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	})
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "project_approvers"))
	if _, err = db.UpsertIdentity(t.Context(), Identity{Forge: ForgeGitHub, Role: RoleOwner, Handle: "original-owner", UserID: 42, Source: SourceGH}); err != nil {
		t.Fatal(err)
	}
	repo := testRepo(t, db, "code-owner", "repo")
	connection, err := db.SetConnection(t.Context(), connectionFixture(repo, TicketConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, 1, "legacy-body", "claude")
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(t.Context(), AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "spike", Agent: "claude", TriggerCommentID: 0, LogPath: func(int) string { return "/retained/body.jsonl" }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunSucceeded}); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	a := ApproverAppointment{RepoID: repo.ID, Actor: ticket.Identity{Provider: ticket.GitHub, Origin: connection.Origin, RemoteID: "42"}, Handle: "original-owner", VerifiedAt: time.Now(), Provenance: "legacy"}
	if _, err = db.ImportLegacyApprover(t.Context(), a, connection.Revision); !errors.Is(err, ErrApproverUnauthorized) {
		t.Fatalf("unfinished body invented approver authorship: %v", err)
	}
	var count int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM project_approver_appointments WHERE repo_id=?`, repo.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed attribution changed appointment: %d %v", count, err)
	}
	// A completed unattributed body stays historical data; it never acquires a
	// synthetic source actor just because future project authority is imported.
	if _, err = db.CompleteRunReport(t.Context(), run.ID, 999); err != nil {
		t.Fatal(err)
	}
	a, err = db.ImportLegacyApprover(t.Context(), a, connection.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.ReconcileLegacyApproverEvidence(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if err = db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM project_legacy_authority_evidence WHERE repo_id=? AND record_kind='run' AND record_key=?`, repo.ID, run.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("completed body acquired invented actor: %d %v", count, err)
	}
}

func TestProjectApproverLegacyRunlessReportRequiresExactSource(t *testing.T) {
	for _, trigger := range []int64{0, 456} {
		t.Run(strconv.FormatInt(trigger, 10), func(t *testing.T) {
			db, err := Open(t.Context(), filepath.Join(t.TempDir(), "legacy.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			applyMigrationsThrough(t, db, migrationVersionBefore(t, "project_approvers"))
			if _, err = db.UpsertIdentity(t.Context(), Identity{Forge: ForgeGitHub, Role: RoleOwner, Handle: "owner", UserID: 42, Source: SourceGH}); err != nil {
				t.Fatal(err)
			}
			repo := testRepo(t, db, "code-owner", "repo")
			connection, err := db.SetConnection(t.Context(), connectionFixture(repo, TicketConnection), 0)
			if err != nil {
				t.Fatal(err)
			}
			session, err := db.GetOrCreateSession(t.Context(), repo, 7)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.RecordComment(t.Context(), Comment{SessionID: session.ID, ForgeCommentID: 123, AuthorUserID: 42, AuthorHandle: "owner", Body: "unrelated instruction", CreatedAt: time.Now(), Classification: ClassificationInstruction}); err != nil {
				t.Fatal(err)
			}
			if _, err = db.EnsureReportDelivery(t.Context(), ReportDelivery{Key: "missing-source", SessionID: session.ID, TriggerID: trigger, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "forge", Purpose: "reply", BotUserID: 99, Body: "original bytes", Marker: "retained"}); err != nil {
				t.Fatal(err)
			}
			if err = db.Migrate(t.Context()); err != nil {
				t.Fatal(err)
			}
			a := ApproverAppointment{RepoID: repo.ID, Actor: ticket.Identity{Provider: ticket.GitHub, Origin: connection.Origin, RemoteID: "42"}, Handle: "owner", VerifiedAt: time.Now(), Provenance: "legacy"}
			if _, err = db.ImportLegacyApprover(t.Context(), a, connection.Revision); !errors.Is(err, ErrApproverUnauthorized) {
				t.Fatalf("unrelated comment authorized report: %v", err)
			}
			var count int
			if err = db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM project_approver_appointments WHERE repo_id=?`, repo.ID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("failed import changed authority: %d %v", count, err)
			}
			delivery, err := db.GetReportDelivery(t.Context(), "missing-source")
			if err != nil || delivery.Body != "original bytes" || delivery.Completed {
				t.Fatalf("pending effect changed: %+v %v", delivery, err)
			}
		})
	}
}
