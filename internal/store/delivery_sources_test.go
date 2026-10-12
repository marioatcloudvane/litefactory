package store

import (
	"errors"
	"testing"
	"time"
)

func TestTypedDeliveryApprovalKeepsOriginalSource(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "code")
	session, err := db.GetOrCreateSession(t.Context(), repo, 42)
	if err != nil {
		t.Fatal(err)
	}
	seedDeliveryTestAuthority(t, db, repo, session)
	q := DeliveryProposal{Key: "proposal", Parent: session.TaskRef, Generation: 1, ParentID: 123, Branch: "feature", Head: "head", Target: "main", Readiness: "ready", Title: "delivery", Body: "proposal"}
	if err = db.RetainDelivery(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	q.PRID, q.PRNumber, q.Proposed = 456, 3, true
	if err = db.SaveDeliveryEffects(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"first", "second"} {
		_, err = db.sql.ExecContext(t.Context(), `INSERT INTO delivery_control_sources(`+deliverySourceColumns+`) VALUES(?,?,?,?,?,?,?,?,?,0)`, id, session.ID, "github-pr-comment", q.Key, q.Generation, 789, 100+i, "/merge exact", FormatTime(time.Now()))
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = db.ApproveDeliverySource(t.Context(), q.Key, "first"); err != nil {
		t.Fatal(err)
	}
	if err = db.ApproveDeliverySource(t.Context(), q.Key, "first"); err != nil {
		t.Fatalf("repeat original approval: %v", err)
	}
	if err = db.ApproveDeliverySource(t.Context(), q.Key, "second"); !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("approval source replaced: %v", err)
	}
	retained, err := db.GetDelivery(t.Context(), q.Key)
	if err != nil || retained.ApprovalComment != 100 || retained.ActorID != 789 {
		t.Fatalf("original approval lost: %+v %v", retained, err)
	}
	var source string
	if err = db.sql.QueryRowContext(t.Context(), `SELECT approval_source_id FROM delivery_proposals WHERE proposal_key=?`, q.Key).Scan(&source); err != nil || source != "first" {
		t.Fatalf("typed attribution lost: %q %v", source, err)
	}
}

func TestDeliveryPREnrollmentFollowsConfirmedPublication(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "enrollment")
	session, err := db.GetOrCreateSession(t.Context(), repo, 42)
	if err != nil {
		t.Fatal(err)
	}
	seedDeliveryTestAuthority(t, db, repo, session)
	q := DeliveryProposal{Key: "proposal", Parent: session.TaskRef, Generation: 1, ParentID: 123, Branch: "feature", Head: "head", Target: "main", Readiness: "ready", Title: "delivery", Body: "proposal"}
	if err = db.RetainDelivery(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	initial := time.Now().UTC().Add(-time.Minute)
	published := initial.Add(10 * time.Second)
	_, err = db.sql.ExecContext(t.Context(), `INSERT INTO delivery_pr_bindings(proposal_key,session_id,pr_id,pr_number,enrolled_at) VALUES(?,?,?,?,?)`, "proposal", session.ID, 123, 3, FormatTime(initial))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.EnsureReportDelivery(t.Context(), ReportDelivery{Key: "notice", SessionID: session.ID, Task: session.TaskRef, Destination: "owner/enrollment#3", TriggerNamespace: "code-pr-proposal", Purpose: "exact-code-approval", BotUserID: 42, Body: "approval", Marker: "marker"})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.EnrollDeliveryPRNotice(t.Context(), "proposal", "notice", published); err != nil {
		t.Fatal(err)
	}
	binding, err := db.GetDeliveryPRBinding(t.Context(), "proposal")
	if err != nil || FormatTime(binding.EnrolledAt) != FormatTime(published) {
		t.Fatalf("publication boundary lost: %+v %v", binding, err)
	}
	if _, err = db.ConfirmReportDelivery(t.Context(), "notice", 99); err != nil {
		t.Fatal(err)
	}
	if err = db.EnrollDeliveryPRNotice(t.Context(), "proposal", "notice", published); err != nil {
		t.Fatalf("recovery changed boundary: %v", err)
	}
	if err = db.EnrollDeliveryPRNotice(t.Context(), "proposal", "notice", published.Add(time.Second)); !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("confirmed boundary changed: %v", err)
	}
}

func seedDeliveryTestAuthority(t *testing.T, db *DB, repo Repo, session Session) {
	t.Helper()
	version, err := db.SetConnection(t.Context(), connectionFixture(repo, CodeConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(t.Context(), `INSERT INTO delivery_code_authorities(`+codeAuthorityColumns+`) VALUES(?,?,?,?,?,?,?)`, session.ID, version.ID, "https://api.github.com", repo.Slug(), "repository-id", 789, 42)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeliverySourceRetainsTimestampAtStoredPrecision(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "precision")
	session, err := db.GetOrCreateSession(t.Context(), repo, 42)
	if err != nil {
		t.Fatal(err)
	}
	seedDeliveryTestAuthority(t, db, repo, session)
	at := time.Date(2026, 10, 11, 12, 0, 0, 123456789, time.FixedZone("local", 3600))
	source := DeliveryControlSource{ID: "github-cli:precision", SessionID: session.ID, Kind: "github-cli-request", Generation: 1, ActorID: 789, Body: "/deliver 1", CreatedAt: at}
	first, err := db.RetainDeliveryControlSource(t.Context(), source)
	if err != nil {
		t.Fatalf("fresh precise timestamp conflicts: %v", err)
	}
	again, err := db.RetainDeliveryControlSource(t.Context(), source)
	if err != nil || first != again {
		t.Fatalf("source changed on replay: %+v %+v %v", first, again, err)
	}
	if !first.CreatedAt.Equal(at.UTC().Truncate(time.Millisecond)) {
		t.Fatalf("stored timestamp changed: %v", first.CreatedAt)
	}
}
