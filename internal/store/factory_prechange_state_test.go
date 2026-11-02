package store

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// These rows were written by the pre-cutover store, not by the implementation
// under test. Regenerate/rehearse with scripts/factory-pending-state.py.
func TestFactoryPrechangePendingStateRecovery(t *testing.T) {
	ctx := t.Context()
	var expected struct {
		Baseline      string
		Session       Session
		Attempt       GraphNodeAttempt
		Capture       WorkflowCapture
		Code, Tickets ConnectionVersion
		Owner         ConnectorOwner
		Grants        []ConnectorGrantSummary
		Proposal      OwnerAction
		Delivery      ReportDelivery
	}
	data, err := os.ReadFile("testdata/factory-pending-state/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	if expected.Baseline == "" {
		t.Fatal("missing baseline provenance")
	}
	compressed, err := os.Open("testdata/factory-pending-state/pending.db.gz")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := compressed.Close(); err != nil {
			t.Error(err)
		}
	})
	reader, err := gzip.NewReader(compressed)
	if err != nil {
		t.Fatal(err)
	}
	data, err = io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pending.db")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	same := func(name string, got, want any, err error) {
		t.Helper()
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("pre-change %s changed: got %+v want %+v: %v", name, got, want, err)
		}
	}
	session, err := db.GetSession(ctx, expected.Session.ID)
	same("session", session, expected.Session, err)
	attempt, err := db.GetGraphNodeAttempt(ctx, expected.Attempt.ID)
	same("attempt", attempt, expected.Attempt, err)
	capture, err := db.WorkflowCaptureForAttempt(ctx, attempt.ID)
	same("capture", capture, expected.Capture, err)
	for _, retained := range []ConnectionVersion{expected.Code, expected.Tickets} {
		actual, err := db.GetConnectionVersion(ctx, retained.ID)
		same("retained route", actual, retained, err)
	}
	proposal, err := db.ReadOwnerAction(ctx, expected.Proposal.ID)
	same("protected proposal", proposal, expected.Proposal, err)
	delivery, err := db.GetReportDelivery(ctx, expected.Delivery.Key)
	same("uncertain delivery", delivery, expected.Delivery, err)
	owner, err := db.ConnectorOwner(ctx, 42)
	same("owner generation", owner, expected.Owner, err)
	grants, err := db.ListConnectorGrants(ctx, 42, proposal.CreatedAt)
	same("consent grants", grants, expected.Grants, err)

	currentTickets, err := db.CurrentConnection(ctx, session.RepoID, TicketConnection)
	if err != nil || currentTickets.ID == expected.Tickets.ID || currentTickets.Revision <= expected.Tickets.Revision {
		t.Fatalf("fixture lost its superseding current route: %+v: %v", currentTickets, err)
	}

	// Existing approval remains pending: a restart supplies no confirmation.
	// Revocation still prevents the saved request acquiring execution authority.
	if err := db.RevokeConnectorGrant(ctx, 42, "pending-grant", proposal.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MutateOwnerAction(ctx, proposal.Principal, proposal.ID, "confirm", "", proposal.ConfirmNotBefore); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("pre-change action ignored revocation: %v", err)
	}
	if _, err := db.ReadNaturalExecution(ctx, proposal.ExecutionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("refused action acquired execution: %v", err)
	}

	// A retained possibly-sent intent cannot complete until provider evidence is
	// supplied. Confirmation then resumes the existing report without a new run.
	watermarkBefore, foundBefore, err := db.GetWatermark(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteReportDelivery(ctx, delivery.Key); err == nil {
		t.Fatal("uncertain pre-change delivery completed without evidence")
	}
	watermarkRefused, foundRefused, err := db.GetWatermark(ctx, session.ID)
	if err != nil || foundRefused != foundBefore || watermarkRefused != watermarkBefore {
		t.Fatalf("refused recovery changed the source watermark: %+v: %v", watermarkRefused, err)
	}
	confirmed, err := db.ConfirmReportDelivery(ctx, delivery.Key, 9001, "https://github.com/acme/api/issues/9516#issuecomment-9001")
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Body != delivery.Body || confirmed.Marker != delivery.Marker || confirmed.Destination != delivery.Destination {
		t.Fatal("confirmation replaced retained publication intent")
	}
	if _, err := db.CompleteReportDelivery(ctx, delivery.Key); err != nil {
		t.Fatal(err)
	}
	runs, err := db.ListRuns(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != delivery.RunID || runs[0].Status != RunReported {
		t.Fatalf("recovery replaced the pre-change run: %+v", runs)
	}
	watermark, found, err := db.GetWatermark(ctx, session.ID)
	if err != nil || !found || watermark.LastCommentID != delivery.TriggerID {
		t.Fatalf("confirmed recovery did not consume its original source: %+v: %v", watermark, err)
	}
	completed, err := db.GetReportDelivery(ctx, delivery.Key)
	if err != nil || !completed.Completed {
		t.Fatalf("recovery left publication pending: %+v: %v", completed, err)
	}
	replayed, err := db.CompleteReportDelivery(ctx, delivery.Key)
	same("replayed delivery", replayed, completed, err)
	// The fixture deliberately contains a newer current ticket connection. It
	// must not replace the old connection retained above while recovery proceeds.
	original, err := db.GetConnectionVersion(ctx, expected.Tickets.ID)
	same("post-recovery retained route", original, expected.Tickets, err)
	if proposal.ExpiresAt.Sub(proposal.CreatedAt) != 15*time.Minute {
		t.Fatal("protected action deadline changed")
	}
}
