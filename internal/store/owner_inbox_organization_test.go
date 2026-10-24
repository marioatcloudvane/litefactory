package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestInboxOrganizationImmutableCompleteScopedAndExpiring(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	repo := testRepo(t, db, "owner", "organized")
	connection, err := db.SetConnection(ctx, ConnectionVersion{RepoID: repo.ID, Role: TicketConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: "owner/organized", Enabled: true, Provenance: "explicit"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	waiting := now.Add(-time.Hour)
	_, err = db.sql.ExecContext(ctx, `INSERT INTO implement_stories(parent,generation,story_key,position,branch,state,reason,created_at,updated_at,state_at,worker_session) VALUES(?,1,'story',0,'lf/story','parked','Urgency: Owner requests immediate recovery.',?,?,?,?)`, session.TaskRef, FormatTime(waiting), FormatTime(now), FormatTime(waiting), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	scope := InboxScope{Owner: "owner", Transport: "test", Conversation: "room"}
	snapshot, err := db.CreateProtectedInboxSnapshot(ctx, scope, InboxFilter{}, now, func(text string) (string, error) { return strings.ReplaceAll(text, "immediate", "[redacted]"), nil })
	if err != nil {
		t.Fatal(err)
	}
	page, err := db.ReadCompleteInboxSnapshot(ctx, scope, snapshot.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || !page.Entries[0].WaitingAt.Equal(waiting) || len(page.Entries[0].Evidence) != 1 || page.Entries[0].Evidence[0].DecisionKey != page.Entries[0].Key {
		t.Fatalf("facts %+v", page.Entries)
	}
	if strings.Contains(page.Entries[0].Summary, "immediate") || strings.Contains(page.Entries[0].Evidence[0].Excerpt, "immediate") || !strings.Contains(page.Entries[0].Evidence[0].Excerpt, "[redacted]") {
		t.Fatal("unprotected display evidence persisted")
	}
	in := InboxOrganization{Version: 1, Mode: "organized", Layout: "action", Tickets: []InboxOrganizedTicket{{Key: InboxTicketIdentity(page.Entries[0]), DecisionKeys: []string{page.Entries[0].Key}, Summary: "Recovery"}}}
	missing := in
	missing.Tickets = nil
	if _, err = db.FinalizeInboxOrganization(ctx, scope, snapshot.ID, missing, now); err == nil {
		t.Fatal("missing membership accepted")
	}
	saved, err := db.FinalizeInboxOrganization(ctx, scope, snapshot.ID, in, now)
	if err != nil {
		t.Fatal(err)
	}
	replacement := in
	replacement.Mode = "fallback"
	replacement.FallbackReason = "later failure"
	winner, err := db.FinalizeInboxOrganization(ctx, scope, snapshot.ID, replacement, now)
	if err != nil || winner.Mode != saved.Mode {
		t.Fatal("immutable winner", winner, err)
	}
	foreign := scope
	foreign.Owner = "other"
	if _, err = db.ReadInboxOrganization(ctx, foreign, snapshot.ID, 1, now); !errors.Is(err, ErrInboxExpired) {
		t.Fatal("foreign owner", err)
	}
	if _, err = db.ReadInboxOrganization(ctx, scope, snapshot.ID, 1, now.Add(24*time.Hour)); !errors.Is(err, ErrInboxExpired) {
		t.Fatal("expired", err)
	}
	if _, err = db.DisableConnection(ctx, repo.ID, TicketConnection, connection.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReadInboxOrganization(ctx, scope, snapshot.ID, 1, now); !errors.Is(err, ErrInboxExpired) {
		t.Fatal("revoked saved view", err)
	}
	if _, err = db.ReadInboxOrganization(ctx, scope, snapshot.ID, 2, now); !errors.Is(err, ErrNotFound) {
		t.Fatal("absent view blocks independent factual browsing", err)
	}
	if _, err = db.ReadCompleteInboxSnapshot(ctx, scope, snapshot.ID, now); !errors.Is(err, ErrInboxExpired) {
		t.Fatal("revoked full snapshot", err)
	}
	if err = db.PurgeInboxSnapshots(ctx, now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.sql.QueryRowContext(ctx, `SELECT count(*) FROM owner_inbox_organizations`).Scan(&count); err != nil || count != 0 {
		t.Fatal("organization retained after snapshot expiry", count, err)
	}
}

func TestInboxCompleteSnapshotRefreshesCoverageOnlyAccess(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	repo := testRepo(t, db, "owner", "coverage")
	connection, err := db.SetConnection(ctx, ConnectionVersion{RepoID: repo.ID, Role: TicketConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: "owner/coverage", Enabled: true, Provenance: "explicit"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	scope := InboxScope{Owner: "owner", Transport: "test", Conversation: "room"}
	snapshot, err := db.CreateInboxSnapshot(ctx, scope, InboxFilter{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DisableConnection(ctx, repo.ID, TicketConnection, connection.Revision); err != nil {
		t.Fatal(err)
	}
	page, err := db.ReadCompleteInboxSnapshot(ctx, scope, snapshot.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 0 || len(page.Snapshot.Coverage) != 1 || page.Snapshot.Coverage[0].Status != "access unavailable or configuration changed" {
		t.Fatalf("stale coverage: %+v", page)
	}
}

func TestInboxStopWithoutSourceAttemptRetainsCreationTime(t *testing.T) {
	db, _, instance, session := persistentBugStopFixture(t)
	ctx := t.Context()
	at := graphTestTime()
	if _, err := db.SetConnection(ctx, ConnectionVersion{RepoID: session.RepoID, Role: TicketConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: "acme/api", Enabled: true, Provenance: "explicit"}, 0); err != nil {
		t.Fatal(err)
	}
	stop := GraphStop{Key: "inbox-stop", InstanceID: instance.ID, Origin: "repair", Cause: "exhausted", Reason: "Owner guidance required", Candidate: []byte(`{"questions":[{"text":"Which recovery?"}]}`), At: at, AllowRunless: true}
	if err := db.ParkGraphNode(ctx, stop); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureReportDelivery(ctx, ReportDelivery{Key: stop.Key, SessionID: session.ID, Task: session.TaskRef, Destination: "acme/api#457", TriggerNamespace: "issue-comment", Purpose: "bug-question", BotUserID: 99, Body: "Reply with guidance", Marker: stop.Key}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MarkReportDeliverySent(ctx, stop.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ConfirmReportDelivery(ctx, stop.Key, 101); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CompleteReportDelivery(ctx, stop.Key); err != nil {
		t.Fatal(err)
	}
	scope := InboxScope{Owner: "owner", Transport: "test", Conversation: "room"}
	snapshot, err := db.CreateInboxSnapshot(ctx, scope, InboxFilter{}, at)
	if err != nil {
		t.Fatal(err)
	}
	page, err := db.ReadCompleteInboxSnapshot(ctx, scope, snapshot.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || !page.Entries[0].WaitingAt.Equal(at) || page.Entries[0].WaitingProvenance != "graph stop created_at" {
		t.Fatalf("source-less stop lost membership or time: %+v", page.Entries)
	}
}
