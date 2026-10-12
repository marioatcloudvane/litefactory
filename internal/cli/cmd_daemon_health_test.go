package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/launchd"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestDaemonRelative(t *testing.T) {
	t.Parallel()
	for in, want := range map[time.Duration]string{
		42 * time.Minute:                "in 42m",
		42*time.Minute + 20*time.Second: "in 42m",
		time.Hour:                       "in 1h",
		65 * time.Minute:                "in 1h5m",
		10 * time.Second:                "now",
		-5 * time.Minute:                "5m ago",
	} {
		if got := daemonRelative(in); got != want {
			t.Errorf("daemonRelative(%s) = %q, want %q", in, got, want)
		}
	}
}

func healthFixture() (store.HealthSnapshot, time.Time) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	return store.HealthSnapshot{
		Found: true,
		Listener: store.ListenerStatus{
			ListenAddress: "127.0.0.1:8787", Listening: true,
			DaemonStartedAt: now.Add(-time.Hour), WrittenAt: now.Add(-time.Minute),
		},
		Repos: []store.RepoDiscoveryStatus{
			{Owner: "acme", Name: "api", WebhookMode: "webhook", SecretPresent: true,
				LastDeliveryAt: now.Add(-3 * time.Minute), LastPollFinishedAt: now.Add(-18 * time.Minute), NextPollDueAt: now.Add(42 * time.Minute)},
			{Owner: "acme", Name: "web", WebhookMode: "polling only"},
		},
	}, now
}

func TestPrintDaemonHealthRunning(t *testing.T) {
	t.Parallel()
	snap, now := healthFixture()
	var out bytes.Buffer
	if err := printDaemonHealth(&out, snap, true, now); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"127.0.0.1:8787/litefactory/webhook (bound)",
		"2026-09-01T11:59:00Z",
		"acme/api", "webhook",
		"2026-09-01T11:57:00Z",
		"2026-09-01T11:42:00Z",
		"2026-09-01T12:42:00Z (in 42m)",
		"ok",
		"acme/web", "polling only",
		daemonNoDelivery,
		"MISSING; repair with `litefactory repo webhook acme/web`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "daemon not running") {
		t.Errorf("a running daemon's snapshot was labelled stale:\n%s", got)
	}
}

func TestPrintDaemonHealthStoppedDaemonSaysAsOf(t *testing.T) {
	t.Parallel()
	snap, now := healthFixture()
	var out bytes.Buffer
	if err := printDaemonHealth(&out, snap, false, now); err != nil {
		t.Fatal(err)
	}
	if want := "as of 2026-09-01T11:59:00Z, daemon not running"; !strings.Contains(out.String(), want) {
		t.Errorf("output lacks %q:\n%s", want, out.String())
	}
}

func TestPrintDaemonHealthListenerOffAndNoSnapshot(t *testing.T) {
	t.Parallel()
	snap, now := healthFixture()
	snap.Listener = store.ListenerStatus{WrittenAt: now}
	snap.Repos = snap.Repos[1:]
	var out bytes.Buffer
	if err := printDaemonHealth(&out, snap, true, now); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "webhook listener  off") {
		t.Errorf("listener off not printed:\n%s", got)
	}
	if strings.Contains(got, "MISSING") {
		t.Errorf("a secret is reported MISSING with the listener off:\n%s", got)
	}

	out.Reset()
	if err := printDaemonHealth(&out, store.HealthSnapshot{}, true, now); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no health snapshot") {
		t.Errorf("missing snapshot not reported:\n%s", out.String())
	}
}

func TestPrintDaemonHealthChatStatus(t *testing.T) {
	t.Parallel()
	snap, now := healthFixture()
	var out bytes.Buffer
	if err := printDaemonHealth(&out, snap, true, now); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "chat") {
		t.Errorf("a chat line was printed with chat off:\n%s", out.String())
	}
	for _, status := range []string{"unavailable: bot token (chat.discord.token_keychain) could not be resolved; retrying on later heartbeats", "available"} {
		snap.Listener.ChatStatus = status
		out.Reset()
		if err := printDaemonHealth(&out, snap, true, now); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "\nchat ") || !strings.Contains(out.String(), status) {
			t.Errorf("output lacks a chat line with %q:\n%s", status, out.String())
		}
	}
}

func TestDaemonNextPollOwed(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if got := daemonNextPoll(store.RepoDiscoveryStatus{PollOwed: true}, now); !strings.Contains(got, "owed") {
		t.Errorf("owed listing with no schedule = %q", got)
	}
	got := daemonNextPoll(store.RepoDiscoveryStatus{PollOwed: true, NextPollDueAt: now.Add(-10 * time.Minute)}, now)
	if !strings.Contains(got, "10m ago") || !strings.Contains(got, "owed") {
		t.Errorf("late owed listing = %q", got)
	}
}

// AC-009: `daemon status` reads what the daemon published, from a real database,
// and labels it when the daemon is not running.
func TestDaemonStatusPrintsThePublishedHealth(t *testing.T) {
	h := newDaemonHome(t, keychainConfig)
	db := h.migrate()
	repo, err := db.UpsertRepo(h.t.Context(), store.Repo{
		Path: "/tmp/acme-api", Forge: store.ForgeGitHub, Owner: "acme", Name: "api",
		RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf/", WorktreeRoot: "/tmp/acme-api-wt",
	})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2099, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := db.ReplaceHealthSnapshot(h.t.Context(),
		store.ListenerStatus{ListenAddress: "127.0.0.1:8787", Listening: true, DaemonStartedAt: at, WrittenAt: at},
		[]store.RepoDiscoveryStatus{{RepoID: repo.ID, WebhookMode: "webhook, no delivery yet", SecretPresent: true,
			LastPollFinishedAt: at, NextPollDueAt: at.Add(time.Hour), WrittenAt: at}}); err != nil {
		t.Fatal(err)
	}

	h.fake.answer = func(args []string) (string, error) {
		return "", notLoadedError(args, 113, "Bad request.; Could not find service in domain for user gui: 501")
	}
	code, stdout, stderr := h.run("daemon", "status")
	if code != ExitOK {
		t.Fatalf("daemon status: exit %d\nstderr:\n%s", code, stderr)
	}
	for _, want := range []string{
		"127.0.0.1:8787/litefactory/webhook (bound)",
		"as of 2099-01-02T03:04:05Z, daemon not running",
		"acme/api", "webhook, no delivery yet", daemonNoDelivery, "2099-01-02T04:04:05Z (in ",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}

	h.fake.answer = func([]string) (string, error) {
		return "gui/501/" + launchd.DefaultLabel + " = {\n\tstate = running\n\tpid = 4711\n}", nil
	}
	_, stdout, _ = h.run("daemon", "status")
	if strings.Contains(stdout, "daemon not running") || !strings.Contains(stdout, "snapshot written") {
		t.Errorf("a running daemon's snapshot is mislabelled:\n%s", stdout)
	}
}

func heartbeatSnapshot(l store.ListenerStatus) store.HealthSnapshot {
	return store.HealthSnapshot{Found: true, Listener: l}
}

// AC-008: the heartbeat block says what the daemon is doing, each fact with its age.
func TestPrintDaemonHeartbeatRunningAndPaused(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	base := store.ListenerStatus{
		WrittenAt: now.Add(-5 * time.Second), LastHeartbeatStartedAt: now.Add(-12 * time.Second), RunsInFlight: 2,
	}

	var out bytes.Buffer
	if err := printDaemonHeartbeat(&out, heartbeatSnapshot(base), now.Add(-20*time.Second), true, now); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"last heartbeat", "2026-09-01T11:59:48Z (12s ago)",
		"last completed heartbeat", "2026-09-01T11:59:40Z (20s ago)",
		"discovery", "running", "runs in flight", "2 (as of 5s ago)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("running output lacks %q:\n%s", want, &out)
		}
	}
	if strings.Contains(out.String(), "daemon not running") {
		t.Errorf("a running daemon is labelled down:\n%s", &out)
	}

	paused := base
	paused.DiscoveryPaused, paused.PauseReason = true, "spent allowance until reset"
	paused.PausedSince, paused.ExpectedResumeAt = now.Add(-10*time.Minute), now.Add(20*time.Minute)
	out.Reset()
	if err := printDaemonHeartbeat(&out, heartbeatSnapshot(paused), time.Time{}, true, now); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"paused since 2026-09-01T11:50:00Z (10m ago): spent allowance until reset, expected to resume 2026-09-01T12:20:00Z",
		"none yet",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("paused output lacks %q:\n%s", want, &out)
		}
	}

	paused.ExpectedResumeAt = time.Time{}
	paused.PauseReason = "stale rate-limit data"
	out.Reset()
	if err := printDaemonHeartbeat(&out, heartbeatSnapshot(paused), time.Time{}, true, now); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "stale rate-limit data, expected to resume when fresh rate-limit data arrives") {
		t.Errorf("stale pause output:\n%s", &out)
	}
}

func TestPrintDaemonHeartbeatDaemonDownIsLabelled(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	snap := heartbeatSnapshot(store.ListenerStatus{WrittenAt: now.Add(-time.Hour), LastHeartbeatStartedAt: now.Add(-time.Hour)})
	if err := printDaemonHeartbeat(&out, snap, time.Time{}, false, now); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "as of 2026-09-01T11:00:00Z, daemon not running") {
		t.Errorf("down output:\n%s", &out)
	}
	out.Reset()
	if err := printDaemonHeartbeat(&out, store.HealthSnapshot{}, time.Time{}, true, now); err != nil || out.Len() != 0 {
		t.Errorf("no snapshot: err=%v out=%q", err, &out)
	}
}

// AC-008 / AC-009 end to end: `daemon status` against a temporary database
// holding a running, a paused and a daemon-down snapshot.
func TestDaemonStatusPrintsHeartbeatSnapshots(t *testing.T) {
	h := newDaemonHome(t, keychainConfig)
	db := h.migrate()
	repo, err := db.UpsertRepo(h.t.Context(), store.Repo{
		Path: "/tmp/acme-api", Forge: store.ForgeGitHub, Owner: "acme", Name: "api",
		RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf/", WorktreeRoot: "/tmp/acme-api-wt",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	publish := func(l store.ListenerStatus) {
		t.Helper()
		l.DaemonStartedAt, l.WrittenAt = now.Add(-time.Hour), now
		rows := []store.RepoDiscoveryStatus{{RepoID: repo.ID, WebhookMode: "polling only", WrittenAt: now,
			LastPollFinishedAt: now.Add(-2 * time.Minute), NextPollDueAt: now.Add(3 * time.Minute)}}
		if err := db.ReplaceHealthSnapshot(h.t.Context(), l, rows); err != nil {
			t.Fatal(err)
		}
	}
	running := func() {
		h.fake.answer = func([]string) (string, error) {
			return "gui/501/" + launchd.DefaultLabel + " = {\n\tstate = running\n\tpid = 4711\n}", nil
		}
	}

	publish(store.ListenerStatus{LastHeartbeatStartedAt: now.Add(-10 * time.Second), RunsInFlight: 1})
	running()
	_, stdout, _ := h.run("daemon", "status")
	for _, want := range []string{"last heartbeat", "last completed heartbeat", "discovery", "running", "runs in flight", "1 (as of ",
		"last poll", "next poll"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("running status lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "daemon not running") {
		t.Errorf("running status labelled down:\n%s", stdout)
	}

	publish(store.ListenerStatus{LastHeartbeatStartedAt: now.Add(-10 * time.Second), DiscoveryPaused: true,
		PauseReason: "rate-limit backoff", PausedSince: now.Add(-3 * time.Minute), ExpectedResumeAt: now.Add(time.Minute)})
	_, stdout, _ = h.run("daemon", "status")
	wantPaused := "paused since " + now.Add(-3*time.Minute).Format(time.RFC3339) + " (3m ago): rate-limit backoff, expected to resume " + now.Add(time.Minute).Format(time.RFC3339)
	if !strings.Contains(stdout, wantPaused) {
		t.Errorf("paused status:\n%s", stdout)
	}

	h.fake.answer = func(args []string) (string, error) {
		return "", notLoadedError(args, 113, "Bad request.; Could not find service in domain for user gui: 501")
	}
	_, stdout, _ = h.run("daemon", "status")
	if want := "as of " + now.Format(time.RFC3339) + ", daemon not running"; !strings.Contains(stdout, want) {
		t.Errorf("down status lacks %q:\n%s", want, stdout)
	}
	if !strings.Contains(stdout, "last poll") {
		t.Errorf("down status dropped the per-repo poll lines:\n%s", stdout)
	}
}

func TestDaemonStatusDoctorCurrentConfiguration(t *testing.T) {
	h := newDaemonHome(t, keychainConfig)
	db := h.migrate()
	ctx := t.Context()
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "linear", RemoteURL: "git@github.com:acme/linear.git", BaseBranch: "main", FeaturePrefix: "lf/", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	v, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.Linear, Origin: "https://api.linear.app", Destination: "team/project", CredentialSource: "env", CredentialEnv: "UNRESOLVED_LINEAR_TEST", Enabled: true, Provenance: "explicit", Linear: store.LinearConfiguration{WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"plan"}`, LifecycleStatesJSON: `{"completed":"done","retired":"cancelled"}`}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	h.fake.answer = func(args []string) (string, error) {
		return "", notLoadedError(args, 113, "Bad request.; Could not find service in domain for user gui: 501")
	}
	check := func(want bool) {
		t.Helper()
		code, stdout, stderr := h.run("daemon", "status")
		if code != ExitOK {
			t.Fatalf("status: %d %s", code, stderr)
		}
		if strings.Contains(stdout, "background Doctor discovery unsupported/skipped for Linear (current configuration)") != want {
			t.Fatalf("status: %s", stdout)
		}
		if want && !strings.Contains(stdout, "project acme/linear") {
			t.Fatalf("missing project: %s", stdout)
		}
	}
	check(true)
	v, err = db.DisableConnection(ctx, repo.ID, store.TicketConnection, v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	check(false)
	v.Enabled = true
	v, err = db.SetConnection(ctx, v, v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	check(true)
	v.Provider, v.Origin, v.Destination, v.Linear = ticket.GitHub, "https://api.github.com", repo.Slug(), store.LinearConfiguration{}
	if _, err := db.SetConnection(ctx, v, v.Revision); err != nil {
		t.Fatal(err)
	}
	check(false)
}
