package store

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestProjectApproverDoctorAdmissionAndEffectBlockReplacement(t *testing.T) {
	db, conn, a := approverFixture(t)
	a, err := db.SetApprover(t.Context(), a, conn.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	conn.Destination = "tickets/project"
	conn, err = db.SetConnection(t.Context(), conn, conn.Revision)
	if err != nil {
		t.Fatal(err)
	}
	command := DoctorCommand{RepoID: a.RepoID, TaskRef: "github:tickets/project#7", IssueID: 700, IssueNumber: 7, CommentID: 123, ActorID: 42, CreatedAt: a.ActivatedAt.Add(time.Second), Operation: "diagnose", Instructions: "explain the saved work"}
	command.Source = &DoctorCommandSource{ConnectionVersionID: conn.ID, PublisherID: 99, Ticket: ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: conn.Origin, RemoteID: strconv.FormatInt(command.IssueID, 10)}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: conn.Origin, Container: conn.Destination, Key: "7"}}}
	_, err = db.AdmitDoctorCommand(t.Context(), command)
	if err != nil {
		t.Fatal(err)
	}
	next := a
	next.Actor.RemoteID = "43"
	if _, err = db.SetApprover(t.Context(), next, conn.Revision, a.Revision); !errors.Is(err, ErrApproverBusy) {
		t.Fatalf("queued doctor instruction allowed replacement: %v", err)
	}
	command, err = db.ClaimDoctorCommand(t.Context(), "", false, "doctor-worker")
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := db.BeginDoctorAttempt(t.Context(), command.TaskRef, DoctorAttempt{ID: "doctor-attempt", SourceID: command.SourceID(), OwnerText: command.Instructions, PromptVersion: "p", GuideVersion: "g"})
	if err != nil {
		t.Fatal(err)
	}
	blockers, err := db.ApproverBlockers(t.Context(), a.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, b := range blockers {
		found[b.Kind] = true
	}
	if !found["doctor_command"] || !found["doctor_attempt"] {
		t.Fatalf("doctor work owners absent: %+v", blockers)
	}
	if err = db.FailDoctorAttempt(t.Context(), "doctor-attempt", "failed", "provider unavailable"); err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteDoctorCommand(t.Context(), command.ID, "retained doctor response", conversation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetApprover(t.Context(), next, conn.Revision, a.Revision); !errors.Is(err, ErrApproverBusy) {
		t.Fatalf("unprepared doctor response allowed replacement: %v", err)
	}
	pending, err := db.DoctorCommandsNeedingReports(t.Context())
	if err != nil || len(pending) != 1 {
		t.Fatalf("doctor report debt missing: %+v %v", pending, err)
	}
	if err = db.EnsureDoctorDeliveries(t.Context(), pending[0], []string{"wrong publisher"}, 100); !errors.Is(err, ErrTicketIdentityConflict) {
		t.Fatalf("doctor source publishing actor changed: %v", err)
	}
	if err = db.EnsureDoctorDeliveries(t.Context(), pending[0], []string{"retained doctor response"}, 99); err != nil {
		t.Fatal(err)
	}
	deliveries, err := db.PendingDoctorDeliveries(t.Context())
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("doctor publication missing: %+v %v", deliveries, err)
	}
	source, err := db.GetDoctorDeliverySource(t.Context(), deliveries[0].Key)
	if err != nil || source.ConnectionVersionID != conn.ID || source.PublisherID != 99 || source.Ticket.Identity.RemoteID != "700" {
		t.Fatalf("doctor publication source lost: %+v %v", source, err)
	}
	if _, err = db.MarkReportDeliverySent(t.Context(), deliveries[0].Key); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetApprover(t.Context(), next, conn.Revision, a.Revision); !errors.Is(err, ErrApproverBusy) {
		t.Fatalf("uncertain doctor publication allowed replacement: %v", err)
	}
	if _, err = db.ConfirmReportDelivery(t.Context(), deliveries[0].Key, 999); err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteDoctorDelivery(t.Context(), deliveries[0].Key); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetApprover(t.Context(), next, conn.Revision, a.Revision); err != nil {
		t.Fatal(err)
	}
}
