package daemon

import (
	"bytes"
	"errors"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// TestStartKeepsLegacyApproverWithOutstandingWork restarts a factory that holds
// a legacy-provenance owner approver, a waiting owner wait, a prelaunch run, an
// uncompleted worker claim and a 'conflict' initialization row. Startup keeps
// the claims, names the blocker in a warning and keeps the same appointment.
func TestStartKeepsLegacyApproverWithOutstandingWork(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "startup.db"))
	researchRecoveryCheck(t, err)
	f := &researchRecoveryFixture{db: db, at: time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)}
	t.Cleanup(func() { researchRecoveryCheck(t, f.db.Close()) })
	researchRecoveryCheck(t, db.Migrate(ctx))
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "research", RemoteURL: "git@github.com:acme/research.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	researchRecoveryCheck(t, err)
	const origin = ticket.Origin("https://api.github.com")
	code, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: origin, Destination: repo.Slug(), RemoteID: "101", CredentialSource: "env", CredentialEnv: "APPROVER_STARTUP", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
	researchRecoveryCheck(t, err)
	tickets := code
	tickets.Role = store.TicketConnection
	tickets, err = db.SetConnection(ctx, tickets, 0)
	researchRecoveryCheck(t, err)
	// The owner who ran everything until now is the approver before any work.
	owner, err := db.SetApprover(ctx, store.ApproverAppointment{RepoID: repo.ID, Actor: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "42"}, Handle: "owner", VerifiedAt: f.at, Provenance: "legacy"}, tickets.Revision, 0)
	researchRecoveryCheck(t, err)
	// Open Research work parked on a waiting owner question.
	f.session, err = db.GetOrCreateSession(ctx, repo, 342)
	researchRecoveryCheck(t, err)
	researchSegment, err := db.GetOrCreateSegment(ctx, f.session.ID, 1, "research", store.AgentClaude)
	researchRecoveryCheck(t, err)
	f.run, err = db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: researchSegment.ID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: 20, LogPath: func(int) string { return "research.log" }})
	researchRecoveryCheck(t, err)
	definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64), CreatedAt: f.at})
	researchRecoveryCheck(t, err)
	f.instance, err = db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: "research", StartedAt: f.at, NextDueAt: f.at})
	researchRecoveryCheck(t, err)
	f.attempt, err = db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: f.instance.ID, Node: "research", RunID: f.run.ID, Limit: 3, StartedAt: f.at})
	researchRecoveryCheck(t, err)
	f.authored(t, true)
	_, err = f.db.ClaimProjectAuthority(ctx, store.AuthorityClaim{RepoID: repo.ID, AppointmentID: owner.ID, Revision: owner.Revision, Actor: owner.Actor, Kind: "worker", Key: "scan"})
	researchRecoveryCheck(t, err)
	inflight, err := f.db.GetOrCreateSession(ctx, repo, 343)
	researchRecoveryCheck(t, err)
	segment, err := f.db.GetOrCreateSegment(ctx, inflight.ID, 1, store.ModeSpike, store.AgentClaude)
	researchRecoveryCheck(t, err)
	prelaunch, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: inflight.ID, SegmentID: segment.ID, Mode: store.ModeSpike, Agent: store.AgentClaude, TriggerCommentID: 40, Prelaunch: true, LogPath: func(int) string { return "/never-launched.jsonl" }})
	researchRecoveryCheck(t, err)
	researchRecoveryCheck(t, f.db.SetApproverImportConflict(ctx, repo.ID, "verify historical identity"))

	var logs bytes.Buffer
	d := &Daemon{db: f.db, connections: projectconnection.NewFactory(projectconnection.Options{}), instanceID: "replacement", now: func() time.Time { return f.at.Add(time.Hour) }, log: slog.New(slog.NewTextHandler(&logs, nil))}
	if _, err = d.Start(ctx); err != nil {
		t.Fatalf("Start refused outstanding work: %v\nlogs:\n%s", err, logs.String())
	}
	if out := logs.String(); !strings.Contains(out, "project worker claims kept") || !strings.Contains(out, "project="+strconv.FormatInt(repo.ID, 10)) || !strings.Contains(out, "session "+inflight.TaskRef) {
		t.Fatalf("startup warning does not name project and blocker:\n%s", out)
	}
	retained, err := f.db.RawCurrentApprover(ctx, repo.ID)
	if err != nil || retained.ID != owner.ID || retained.Revision != owner.Revision || !retained.Actor.Equal(owner.Actor) {
		t.Fatalf("startup changed the approver: %+v %v", retained, err)
	}
	wait, err := f.db.GetGraphOwnerWait(ctx, f.instance.ID)
	if err != nil || wait.State != "waiting" {
		t.Fatalf("owner wait not preserved: %+v %v", wait, err)
	}
	run, err := f.db.GetRun(ctx, prelaunch.ID)
	if err != nil || run.Status != store.RunPrelaunch {
		t.Fatalf("prelaunch run not preserved: %+v %v", run, err)
	}
	next := owner
	next.Actor.RemoteID = "43"
	if _, err = f.db.SetApprover(ctx, next, tickets.Revision, owner.Revision); !errors.Is(err, store.ErrApproverBusy) {
		t.Fatalf("kept worker claim did not block replacement: %v", err)
	}
	if _, err = f.db.SetApprover(ctx, owner, tickets.Revision, owner.Revision); err != nil {
		t.Fatalf("same owner re-confirmation blocked: %v", err)
	}
	if _, err = f.db.GetApproverInitialization(ctx, repo.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("conflict not cleared by re-confirmation: %v", err)
	}
	current, err := f.db.CurrentApprover(ctx, repo.ID)
	if err != nil || current.ID != owner.ID || current.Revision != owner.Revision {
		t.Fatalf("owner admission not restored: %+v %v", current, err)
	}
}

func TestStartRepairsStaleSessionBeforeWorkerRecovery(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "startup.db"))
	researchRecoveryCheck(t, err)
	f := &researchRecoveryFixture{db: db, at: time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)}
	t.Cleanup(func() { researchRecoveryCheck(t, f.db.Close()) })
	researchRecoveryCheck(t, db.Migrate(ctx))
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "research", RemoteURL: "git@github.com:acme/research.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	researchRecoveryCheck(t, err)
	const origin = ticket.Origin("https://api.github.com")
	code, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: origin, Destination: repo.Slug(), RemoteID: "101", CredentialSource: "env", CredentialEnv: "APPROVER_STARTUP", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
	researchRecoveryCheck(t, err)
	tickets := code
	tickets.Role = store.TicketConnection
	tickets, err = db.SetConnection(ctx, tickets, 0)
	researchRecoveryCheck(t, err)
	// The owner who ran everything until now is the approver before any work.
	owner, err := db.SetApprover(ctx, store.ApproverAppointment{RepoID: repo.ID, Actor: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "42"}, Handle: "owner", VerifiedAt: f.at, Provenance: "legacy"}, tickets.Revision, 0)
	researchRecoveryCheck(t, err)

	session, err := db.GetOrCreateSession(ctx, repo, 509)
	researchRecoveryCheck(t, err)
	segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "research", store.AgentClaude)
	researchRecoveryCheck(t, err)
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: 20, LogPath: func(int) string { return "research.log" }})
	researchRecoveryCheck(t, err)
	_, err = db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunSucceeded})
	researchRecoveryCheck(t, err)
	researchRecoveryCheck(t, db.SetSessionState(ctx, session.ID, store.SessionRunning))
	worker := store.AuthorityClaim{RepoID: repo.ID, AppointmentID: owner.ID, Revision: owner.Revision, Actor: owner.Actor, Kind: "worker", Key: "abandoned"}
	_, err = db.ClaimProjectAuthority(ctx, worker)
	researchRecoveryCheck(t, err)
	durable, err := db.ClaimProjectAuthority(ctx, store.AuthorityClaim{RepoID: repo.ID, AppointmentID: owner.ID, Revision: owner.Revision, Actor: owner.Actor, Kind: "instruction", Key: "retained", Source: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "comment:21"}, CreatedAt: owner.ActivatedAt.Add(time.Second)})
	researchRecoveryCheck(t, err)
	var logs bytes.Buffer
	d := &Daemon{db: db, connections: projectconnection.NewFactory(projectconnection.Options{}), instanceID: "replacement", now: func() time.Time { return f.at.Add(time.Hour) }, log: slog.New(slog.NewTextHandler(&logs, nil)), schedule: newDiscoverySchedule(time.Hour), pool: newPool(1)}
	_, err = d.Start(ctx)
	researchRecoveryCheck(t, err)
	got, err := db.GetSession(ctx, session.ID)
	if err != nil || got.State != store.SessionFailed {
		t.Fatalf("stale session not repaired: %+v %v", got, err)
	}
	next := owner
	next.Actor.RemoteID = "43"
	if _, err = db.SetApprover(ctx, next, tickets.Revision, owner.Revision); !errors.Is(err, store.ErrApproverBusy) {
		t.Fatalf("durable work did not block replacement: %v", err)
	}
	researchRecoveryCheck(t, db.CompleteProjectAuthority(ctx, durable.ID))
	if _, err = db.SetApprover(ctx, next, tickets.Revision, owner.Revision); !errors.Is(err, store.ErrApproverBusy) {
		t.Fatalf("unreported result did not block replacement: %v", err)
	}
	_, err = db.CompleteRunReport(ctx, run.ID, 123)
	researchRecoveryCheck(t, err)
	next, err = db.SetApprover(ctx, next, tickets.Revision, owner.Revision)
	if err != nil {
		t.Fatalf("abandoned worker claim still blocks replacement: %v", err)
	}
	// New claims after startup must survive ticks: recovery belongs to Start.
	worker.Key = "current-worker"
	worker.AppointmentID, worker.Revision, worker.Actor = next.ID, next.Revision, next.Actor
	claim, err := db.ClaimProjectAuthority(ctx, worker)
	researchRecoveryCheck(t, err)
	// Avoid forge discovery; no external service is involved in these ticks.
	d.schedule.ListingFinished(repo.ID, d.now())
	for range 2 {
		_, err = d.Tick(ctx)
		researchRecoveryCheck(t, err)
	}
	replacement := next
	replacement.Actor.RemoteID = "44"
	if _, err = db.SetApprover(ctx, replacement, tickets.Revision, next.Revision); !errors.Is(err, store.ErrApproverBusy) {
		t.Fatalf("tick recovered a current worker claim: %v", err)
	}
	if strings.Contains(logs.String(), "project worker claims kept at startup") {
		t.Fatalf("stale session emitted outstanding-work warning: %s", logs.String())
	}
	researchRecoveryCheck(t, db.CompleteProjectAuthority(ctx, claim.ID))
	if _, err = db.SetApprover(ctx, replacement, tickets.Revision, next.Revision); err != nil {
		t.Fatalf("released work still blocks replacement: %v", err)
	}
}
