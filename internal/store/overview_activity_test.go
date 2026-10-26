package store

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestOverviewActivityUsesCurrentExecutionEvidenceAndIndependentSentinel(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	repo := testRepo(t, db, "owner", "private")
	now := time.Date(2026, 10, 26, 6, 0, 0, 0, time.UTC)
	sessions := make([]Session, 5)
	for i := range sessions {
		var err error
		sessions[i], err = db.GetOrCreateSession(ctx, repo, i+1)
		if err != nil {
			t.Fatal(err)
		}
	}
	// Session bookkeeping alone can remain running after execution finishes.
	if _, err := db.sql.ExecContext(ctx, `UPDATE sessions SET state='running' WHERE id=?`, sessions[0].ID); err != nil {
		t.Fatal(err)
	}
	running := testRun(t, db, sessions[1].ID, 1)
	prelaunch := testRun(t, db, sessions[2].ID, 1)
	if _, err := db.sql.ExecContext(ctx, `UPDATE runs SET started_at=?,workflow='bug' WHERE id=?`, FormatTime(now), running.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE runs SET started_at=?,status='prelaunch' WHERE id=?`, FormatTime(now), prelaunch.ID); err != nil {
		t.Fatal(err)
	}
	// A current graph on an agent's session must not duplicate its agent row.
	overviewGraphWait(t, db, sessions[1], "agent-graph", "implement", "ask", "waiting", now.Add(-time.Hour))
	overviewGraphWait(t, db, sessions[3], "old-graph", "plan", "set_ask", "waiting", now.Add(-2*time.Hour))
	currentGraph := overviewGraphWait(t, db, sessions[3], "current-graph", "plan", "set_ask", "waiting", now.Add(-time.Hour))
	if _, err := db.sql.ExecContext(ctx, `UPDATE sessions SET state='idle',lock_owner='worker',lock_acquired_at=? WHERE id=?`, FormatTime(now.Add(-30*time.Minute)), sessions[4].ID); err != nil {
		t.Fatal(err)
	}
	entries, more, err := db.ListOverviewActivity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 || more {
		t.Fatalf("execution membership: %+v more=%v", entries, more)
	}
	byID := map[string]OverviewActivity{}
	for _, a := range entries {
		byID[a.ID] = a
		if a.SessionID == sessions[0].ID {
			t.Fatal("stale session state entered activity")
		}
	}
	bug := byID[fmt.Sprintf("run:%d", running.ID)]
	if bug.Workflow != "bug" || bug.Kind != "agent" || bug.RunID == nil || *bug.RunID != running.ID {
		t.Fatalf("standalone Bug identity: %+v", bug)
	}
	if byID[fmt.Sprintf("run:%d", prelaunch.ID)].State != "prelaunch" {
		t.Fatal("prelaunch missing")
	}
	graph := byID[fmt.Sprintf("graph:%d", currentGraph)]
	if graph.RunID != nil || graph.Workflow != "plan" || graph.Stage != "set_ask" || graph.State != "waiting_for_owner" {
		t.Fatalf("runless graph: %+v", graph)
	}
	if byID[fmt.Sprintf("worker:%d", sessions[4].ID)].Kind != "worker" {
		t.Fatal("worker-only lock missing")
	}
	// A busy independent list has an exact sentinel and stable ID tie breaks.
	var expected []string
	for i := 0; i < 51; i++ {
		s, e := db.GetOrCreateSession(ctx, repo, 100+i)
		if e != nil {
			t.Fatal(e)
		}
		_, e = db.sql.ExecContext(ctx, `UPDATE sessions SET lock_owner='worker',lock_acquired_at=? WHERE id=?`, FormatTime(now.Add(time.Hour)), s.ID)
		if e != nil {
			t.Fatal(e)
		}
		expected = append(expected, fmt.Sprintf("worker:%d", s.ID))
	}
	sort.Strings(expected)
	entries, more, err = db.ListOverviewActivity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 50 || !more {
		t.Fatalf("activity cap: %d more=%v", len(entries), more)
	}
	for i, a := range entries {
		if a.ID != expected[i] {
			t.Fatalf("tie order %d: %s want %s", i, a.ID, expected[i])
		}
	}
}

func TestOverviewRegistryAndHealthRetainUnknownAndLaterRegistrations(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	health, err := db.OverviewHealth(ctx)
	if err != nil || health.Found {
		t.Fatalf("missing health became known: %+v %v", health, err)
	}
	first := testRepo(t, db, "owner", "private")
	second := testRepo(t, db, "owner", "disabled")
	_, err = db.SetConnection(ctx, ConnectionVersion{RepoID: second.ID, Role: TicketConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: "owner/disabled", Enabled: false, Provenance: "explicit"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	entries, more, err := db.OverviewRepositories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || more || entries[0].ID != first.ID || entries[0].ConnectionStatus != "missing_connection" || entries[1].ConnectionStatus != "disabled_connection" {
		t.Fatalf("registry hidden: %+v more=%v", entries, more)
	}
	later := testRepo(t, db, "later", "registration")
	entries, more, err = db.OverviewRepositories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || more || entries[2].ID != later.ID {
		t.Fatalf("later registration hidden: %+v", entries)
	}
	now := time.Date(2026, 10, 26, 6, 0, 0, 0, time.UTC)
	listener := ListenerStatus{WrittenAt: now, LastHeartbeatStartedAt: now.Add(-time.Minute), DiscoveryPaused: true, PauseReason: "provider rate limit", ExpectedResumeAt: now.Add(time.Minute), RunsInFlight: 2, SlotsUsed: 2, SlotsTotal: 4, RunsHeld: "disk reserve"}
	if err = db.ReplaceHealthSnapshot(ctx, listener, nil); err != nil {
		t.Fatal(err)
	}
	health, err = db.OverviewHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := health.Listener
	if !health.Found || !got.WrittenAt.Equal(now) || !got.LastHeartbeatStartedAt.Equal(listener.LastHeartbeatStartedAt) || !got.DiscoveryPaused || got.PauseReason != listener.PauseReason || !got.ExpectedResumeAt.Equal(listener.ExpectedResumeAt) || got.RunsInFlight != 2 || got.SlotsUsed != 2 || got.SlotsTotal != 4 || got.RunsHeld != "disk reserve" {
		t.Fatalf("health facts lost: %+v", health)
	}
	for i := 0; i < 48; i++ {
		testRepo(t, db, "owner", fmt.Sprintf("registered-%02d", i))
	}
	entries, more, err = db.OverviewRepositories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 50 || !more || entries[0].ID != first.ID {
		t.Fatalf("registry cap: %d more=%v", len(entries), more)
	}
}
