package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestInboxNavigationMasksRevokedContentWithoutShiftingSavedPositions(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)
	ghRepo := testRepo(t, db, "owner", "github")
	linearRepo := testRepo(t, db, "owner", "linear")
	gh, err := db.SetConnection(ctx, ConnectionVersion{RepoID: ghRepo.ID, Role: TicketConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: "owner/github", Enabled: true, Provenance: "explicit"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	linear, err := db.SetConnection(ctx, ConnectionVersion{RepoID: linearRepo.ID, Role: TicketConnection, Provider: ticket.Linear, Origin: "https://api.linear.app", Destination: "team/project", Enabled: true, Provenance: "explicit", Linear: LinearConfiguration{WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan"}`, LifecycleStatesJSON: `{"completed":"done","retired":"cancelled"}`}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	sharedRepo := testRepo(t, db, "owner", "shared")
	shared, err := db.SetConnection(ctx, ConnectionVersion{RepoID: sharedRepo.ID, Role: TicketConnection, Provider: ticket.GitHub, Origin: gh.Origin, Destination: gh.Destination, Enabled: true, Provenance: "explicit"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	scope := InboxScope{Owner: "owner", Transport: "test", Conversation: "room"}
	snapshot := InboxSnapshot{ID: "navigation", Scope: scope, CapturedAt: now, ExpiresAt: now.Add(24 * time.Hour), Total: 4, Coverage: []InboxCoverage{{RepoID: ghRepo.ID, ConnectionID: gh.ID, Source: "github", Project: gh.Destination}, {RepoID: linearRepo.ID, ConnectionID: linear.ID, Source: "linear", Project: linear.Destination}, {RepoID: sharedRepo.ID, ConnectionID: shared.ID, Source: "github", Project: shared.Destination}}}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO owner_inbox_snapshots VALUES(?,?,?,?,?,?,?)`, snapshot.ID, scope.Owner, scope.Transport, scope.Conversation, FormatTime(now), FormatTime(snapshot.ExpiresAt), string(raw)); err != nil {
		t.Fatal(err)
	}
	ghEntry := InboxDecision{Key: "gh-a", RepoID: ghRepo.ID, ConnectionID: gh.ID, Ordinal: 0, Source: "github", Summary: "GitHub recorded wait A", Locator: ticket.Locator{Provider: ticket.GitHub, Origin: gh.Origin, Container: gh.Destination, Key: "7"}}
	linearEntry := InboxDecision{Key: "linear-secret-key", RepoID: linearRepo.ID, ConnectionID: linear.ID, Ordinal: 1, Source: "linear", Summary: "Linear private waiting reason", Locator: ticket.Locator{Provider: ticket.Linear, Origin: linear.Origin, Container: linear.Destination, Key: "private-id"}}
	secondGH := ghEntry
	secondGH.Key = "gh-b"
	secondGH.Ordinal = 2
	secondGH.Summary = "GitHub recorded wait B"
	mixed := ghEntry
	mixed.Key = "mixed-private-key"
	mixed.Ordinal = 3
	mixed.RepoID = sharedRepo.ID
	mixed.ConnectionID = shared.ID
	mixed.Summary = "Mixed private waiting reason"
	for _, d := range []InboxDecision{ghEntry, linearEntry, secondGH, mixed} {
		raw, err = json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.sql.ExecContext(ctx, `INSERT INTO owner_inbox_entries VALUES(?,?,?,?)`, snapshot.ID, d.Ordinal, d.Key, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	organization := InboxOrganization{Version: 1, Layout: "action", Mode: "organized", Tickets: []InboxOrganizedTicket{{Key: InboxTicketIdentity(linearEntry), DecisionKeys: []string{linearEntry.Key}, Summary: "Linear private agent summary", PromotionReason: "private promotion", EvidenceIDs: []string{"private-evidence"}}, {Key: InboxTicketIdentity(ghEntry), DecisionKeys: []string{secondGH.Key, mixed.Key, ghEntry.Key}, Summary: "Agent summary references Linear private content", PromotionReason: "private cross-source promotion", EvidenceIDs: []string{"private-evidence"}}}}
	if _, err = db.FinalizeInboxOrganization(ctx, scope, snapshot.ID, organization, now); err != nil {
		t.Fatal(err)
	}
	before, err := db.ReadInboxOrganization(ctx, scope, snapshot.ID, 1, now)
	if err != nil || before.Mode != "organized" {
		t.Fatal(before, err)
	}
	var originalView string
	if err = db.sql.QueryRowContext(ctx, `SELECT content FROM owner_inbox_organizations WHERE snapshot_id=? AND version=1`, snapshot.ID).Scan(&originalView); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DisableConnection(ctx, linearRepo.ID, TicketConnection, linear.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DisableConnection(ctx, sharedRepo.ID, TicketConnection, shared.Revision); err != nil {
		t.Fatal(err)
	}
	page, view, err := db.ReadInboxNavigation(ctx, scope, snapshot.ID, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 2 || page.Entries[0].Ordinal != 0 || page.Entries[1].Ordinal != 2 || page.Snapshot.Total != 4 {
		t.Fatalf("snapshot positions shifted: %+v", page)
	}
	if len(view.Tickets) != 2 || view.Tickets[0].Key != "" || len(view.Tickets[0].DecisionKeys) != 0 || view.Tickets[1].DecisionKeys[0] != "gh-b" || len(view.Tickets[1].DecisionKeys) != 3 || view.Tickets[1].DecisionKeys[1] != "" || view.Tickets[1].DecisionKeys[2] != "gh-a" {
		t.Fatalf("view positions shifted: %+v", view)
	}
	if view.Mode != "fallback" || view.FallbackReason == "" || view.Tickets[1].Summary != "GitHub recorded wait B\nGitHub recorded wait A" {
		t.Fatalf("unsafe fallback: %+v", view)
	}
	raw, err = json.Marshal(struct {
		Page InboxPage
		View InboxOrganization
	}{page, view})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"mixed-private-key", "Mixed private", "linear-secret-key", "private-id", "private waiting", "private agent", "private promotion", "private cross-source", "private-evidence", "Agent summary"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("revoked content exposed: %s", secret)
		}
	}
	var retainedView string
	if err = db.sql.QueryRowContext(ctx, `SELECT content FROM owner_inbox_organizations WHERE snapshot_id=? AND version=1`, snapshot.ID).Scan(&retainedView); err != nil || retainedView != originalView {
		t.Fatal("immutable view changed", err)
	}
	if page.Snapshot.Coverage[1].Status != "access unavailable or configuration changed" {
		t.Fatal("missing coverage gap")
	}
	if _, err = db.ReadCompleteInboxSnapshot(ctx, scope, snapshot.ID, now); !errors.Is(err, ErrInboxExpired) {
		t.Fatal("strict reader changed", err)
	}
	if _, err = db.ReadInboxOrganization(ctx, scope, snapshot.ID, 1, now); !errors.Is(err, ErrInboxExpired) {
		t.Fatal("strict organization reader changed", err)
	}
	absent, _, err := db.ReadInboxNavigation(ctx, scope, snapshot.ID, 2, now)
	if !errors.Is(err, ErrNotFound) || len(absent.Entries) != 0 {
		t.Fatal("absent organization blocks authorized browse", err)
	}
	foreign := scope
	foreign.Conversation = "other"
	if p, v, e := db.ReadInboxNavigation(ctx, foreign, snapshot.ID, 1, now); !errors.Is(e, ErrInboxExpired) || len(p.Entries) > 0 || len(v.Tickets) > 0 {
		t.Fatal("foreign navigation exposed content", e)
	}
	if p, v, e := db.ReadInboxNavigation(ctx, scope, snapshot.ID, 1, now.Add(24*time.Hour)); !errors.Is(e, ErrInboxExpired) || len(p.Entries) > 0 || len(v.Tickets) > 0 {
		t.Fatal("expired navigation exposed content", e)
	}
}
