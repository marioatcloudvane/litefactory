package store

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"
)

// This fixture characterizes the durable representation before the factory
// ownership cutover. It is a same-schema restart check, not an upgrade rehearsal.
func TestFactoryPendingStateSurvivesRestart(t *testing.T) {
	db, launch := captureFixture(t)
	ctx := t.Context()
	admitted, err := db.AdmitWorkflowLaunch(ctx, launch)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: admitted.Instance.ID, LaunchID: admitted.LaunchID, Node: "work", Limit: 8, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetSession(ctx, launch.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := db.GetRepo(ctx, session.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	code, err := db.SetConnection(ctx, connectionFixture(repo, CodeConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	tickets, err := db.SetConnection(ctx, connectionFixture(repo, TicketConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	// A later connection is deliberately present. Recovery must retain the
	// exact version, rather than silently resolving current configuration.
	next := tickets
	next.CredentialEnv = "REPLACEMENT_TEST_TOKEN"
	if _, err = db.SetConnection(ctx, next, tickets.Revision); err != nil {
		t.Fatal(err)
	}
	owner := connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "pending-client")
	at := connectorTestNow
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO connector_grants(id,client_id,resource,scope,owner_id,generation,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?)`, "pending-grant", "pending-client", connectorTestResource, "standards:management", 42, owner.Generation, FormatTime(at), FormatTime(at.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	proposal := OwnerAction{ID: "pending-proposal", PolicyVersion: 1, Principal: OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "mcp", ClientID: "pending-client", GrantID: "pending-grant"}, RequestKey: "pending-key", RequestDigest: "pending-request", Facts: `{"target_revision":"reviewed","connection_version":1}`, Payload: `{"exact":"protected pending payload"}`, Digest: "pending-digest", Scope: "standards:management", Tier: "conversational", State: "awaiting_confirmation", CreatedAt: at, AvailableAt: at, ConfirmNotBefore: at.Add(5 * time.Second), ExpiresAt: at.Add(15 * time.Minute), ExecutionID: "owner-action:pending-proposal"}
	if err = db.CreateOwnerAction(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	beforeProposal, err := db.ReadOwnerAction(ctx, proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	run := testRun(t, db, session.ID, 55)
	if _, err = db.FinishRun(ctx, run.ID, RunOutcome{Status: RunFailed, Error: "interrupted after result"}); err != nil {
		t.Fatal(err)
	}
	delivery := reportIntent(session, run)
	_, err = db.EnsureReportDelivery(ctx, delivery)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.MarkReportDeliverySent(ctx, delivery.Key); err != nil {
		t.Fatal(err)
	}
	beforeDelivery, err := db.GetReportDelivery(ctx, delivery.Key)
	if err != nil {
		t.Fatal(err)
	}
	var sequence int
	var name, path string
	if err = db.sql.QueryRowContext(ctx, `PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	// captureFixture's cleanup owns this pointer.
	*db = *reopened
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	savedCapture, err := db.WorkflowCaptureForAttempt(ctx, attempt.ID)
	if err != nil || savedCapture.ID != admitted.LaunchID || !bytes.Equal(savedCapture.Files, launch.Files) || !reflect.DeepEqual(savedCapture.Provenance, launch.Provenance) {
		t.Fatalf("pending attempt lost its immutable capture: %+v %v", savedCapture, err)
	}
	for _, original := range []ConnectionVersion{code, tickets} {
		saved, readErr := db.GetConnectionVersion(ctx, original.ID)
		if readErr != nil || !reflect.DeepEqual(saved, original) {
			t.Fatalf("retained connection changed: %+v %v", saved, readErr)
		}
	}
	savedProposal, err := db.ReadOwnerAction(ctx, proposal.ID)
	if err != nil || !reflect.DeepEqual(savedProposal, beforeProposal) {
		t.Fatalf("pending protected proposal changed: %+v %v", savedProposal, err)
	}
	savedDelivery, err := db.GetReportDelivery(ctx, delivery.Key)
	if err != nil || !reflect.DeepEqual(savedDelivery, beforeDelivery) || savedDelivery.Completed {
		t.Fatalf("pending delivery changed or completed without evidence: %+v %v", savedDelivery, err)
	}
	savedOwner, err := db.ConnectorOwner(ctx, 42)
	if err != nil || savedOwner.Generation != owner.Generation {
		t.Fatalf("owner authorization generation changed: %+v %v", savedOwner, err)
	}
	grants, err := db.ListConnectorGrants(ctx, 42, at)
	if err != nil || len(grants) != 1 || grants[0].ID != "pending-grant" {
		t.Fatalf("pending consent grant lost: %+v %v", grants, err)
	}
	// Revocation still fences the saved proposal before admission and cannot
	// create an execution journal.
	if err = db.RevokeConnectorGrant(ctx, 42, "pending-grant", proposal.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if _, err = db.MutateOwnerAction(ctx, proposal.Principal, proposal.ID, "confirm", "", proposal.ConfirmNotBefore); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("restarted proposal ignored grant revocation: %v", err)
	}
	if _, err = db.ReadNaturalExecution(ctx, proposal.ExecutionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("refused restarted proposal acquired an execution: %v", err)
	}
}
