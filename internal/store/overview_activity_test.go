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
	entries, more, _, err := db.ListOverviewActivity(ctx, now)
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
	entries, more, _, err = db.ListOverviewActivity(ctx, now)
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

func TestOverviewActivityListsInFlightSlotsWithoutExecutionEvidence(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	repo := testRepo(t, db, "owner", "private")
	now := time.Date(2026, 11, 5, 13, 4, 34, 0, time.UTC)
	written := now.Add(-time.Minute)
	sessions := make([]Session, 5)
	for i := range sessions {
		var err error
		sessions[i], err = db.GetOrCreateSession(ctx, repo, i+1)
		if err != nil {
			t.Fatal(err)
		}
	}
	// A has a running run; B holds a slot before any run, graph or lock;
	// C is a lock-only worker; D has a running graph; E is an input check.
	runA := testRun(t, db, sessions[0].ID, 1)
	if _, err := db.sql.ExecContext(ctx, `UPDATE runs SET started_at=? WHERE id=?`, FormatTime(now.Add(-time.Hour)), runA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE sessions SET lock_owner='worker',lock_acquired_at=? WHERE id=?`, FormatTime(now.Add(-time.Hour)), sessions[2].ID); err != nil {
		t.Fatal(err)
	}
	graphD := overviewGraphWait(t, db, sessions[3], "d-graph", "plan", "set_ask", "claimed", now.Add(-time.Hour))
	ids := []int64{sessions[0].ID, sessions[1].ID, sessions[2].ID, sessions[3].ID, sessions[4].ID}
	labels := map[int64]string{sessions[1].ID: "due_schedule"}
	write := func(at time.Time, inFlight []int64) {
		t.Helper()
		if err := db.ReplaceHealthSnapshot(ctx, ListenerStatus{WrittenAt: at, LastHeartbeatStartedAt: at, RunsInFlight: len(inFlight), InFlightSessions: inFlight, InFlightLabels: labels, SlotsUsed: len(inFlight), SlotsTotal: 5}, nil); err != nil {
			t.Fatal(err)
		}
	}
	write(written, ids)
	entries, more, warnings, err := db.ListOverviewActivity(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if more || len(warnings) != 0 || len(entries) != 5 {
		t.Fatalf("in-flight membership: %+v more=%v warnings=%v", entries, more, warnings)
	}
	bySession := map[int64][]OverviewActivity{}
	for _, a := range entries {
		bySession[a.SessionID] = append(bySession[a.SessionID], a)
	}
	for i, want := range []string{fmt.Sprintf("run:%d", runA.ID), fmt.Sprintf("inflight:%d", sessions[1].ID), fmt.Sprintf("worker:%d", sessions[2].ID), fmt.Sprintf("graph:%d", graphD), fmt.Sprintf("inflight:%d", sessions[4].ID)} {
		got := bySession[sessions[i].ID]
		if len(got) != 1 || got[0].ID != want {
			t.Fatalf("session %d entries %+v, want exactly %s", i, got, want)
		}
	}
	b := bySession[sessions[1].ID][0]
	if b.Kind != "in_flight" || b.TaskRef != sessions[1].TaskRef || b.Repository != "owner/private" || b.Stage != "due_schedule" || b.State != "working" || !b.StartedAt.Equal(written) || b.RunID != nil {
		t.Fatalf("in-flight slot identity: %+v", b)
	}
	if e := bySession[sessions[4].ID][0]; e.Stage != "checking_for_input" {
		t.Fatalf("unlabelled in-flight stage: %+v", e)
	}

	// A stale or future snapshot adds no phantom running work.
	for _, at := range []time.Time{now.Add(-6 * time.Minute), now.Add(time.Minute)} {
		write(at, ids)
		entries, _, warnings, err = db.ListOverviewActivity(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 3 || len(warnings) != 0 {
			t.Fatalf("snapshot at %v added in-flight entries: %+v %v", at, entries, warnings)
		}
	}

	// An in-flight ID with no session row is a disclosed coverage gap.
	write(written, []int64{sessions[1].ID, 999999})
	entries, _, warnings, err = db.ListOverviewActivity(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 || len(warnings) != 1 || warnings[0] != "unidentified_in_flight_work" {
		t.Fatalf("unidentified in-flight work: %+v %v", entries, warnings)
	}
	for _, a := range entries {
		if a.SessionID == 999999 {
			t.Fatal("unresolvable in-flight session entered activity")
		}
	}

	// The combined list keeps its sentinel cap.
	var many []int64
	for i := 0; i < 60; i++ {
		s, e := db.GetOrCreateSession(ctx, repo, 100+i)
		if e != nil {
			t.Fatal(e)
		}
		many = append(many, s.ID)
	}
	write(written, many)
	entries, more, _, err = db.ListOverviewActivity(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 50 || !more {
		t.Fatalf("combined cap: %d more=%v", len(entries), more)
	}
}
