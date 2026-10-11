package store

import (
	"errors"
	"testing"
	"time"
)

func TestTicketRecoveryAtomicHandoffGenerationAndLegacy(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "recovery")
	ctx := t.Context()
	v := connectionFixture(repo, TicketConnection)
	v.Provenance = "legacy"
	v.RemoteID = "container-1"
	v, err := db.SetConnection(ctx, v, 0)
	if err != nil {
		t.Fatal(err)
	}
	enrolled := repo.CreatedAt
	coverage := enrolled.Add(time.Hour)
	if err := db.SetRepoPollCursor(ctx, repo.ID, coverage); err != nil {
		t.Fatal(err)
	}
	p, err := db.InitTicketRecoveryPosition(ctx, v, enrolled)
	if err != nil {
		t.Fatal(err)
	}
	if !p.CompletedThrough.Equal(coverage) {
		t.Fatalf("legacy coverage lost: %+v", p)
	}
	observation := PendingTicketObservation{ConnectionVersionID: v.ID, RepoID: repo.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: v.RemoteID, TicketID: "ticket-9", TicketKey: "9", ObservedAt: coverage}
	p.ScanThrough = coverage.Add(time.Hour)
	p.TicketCursor = "next"
	p.TicketsComplete = true
	p, err = db.CommitTicketRecoveryPage(ctx, p, p.Revision, []PendingTicketObservation{observation})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := db.ListPendingTicketObservations(ctx, v.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending %+v %v", pending, err)
	}
	generation := pending[0].Generation
	if err := db.HandoffTicketObservation(ctx, observation); err != nil {
		t.Fatal(err)
	}
	if err := db.ClearPendingTicketObservation(ctx, v.ID, observation.TicketID, generation); err != nil {
		t.Fatal(err)
	}
	pending, err = db.ListPendingTicketObservations(ctx, v.ID)
	if err != nil || len(pending) != 1 || pending[0].Generation != generation+1 {
		t.Fatalf("new signal removed %+v %v", pending, err)
	}
	if err := db.ClearPendingTicketObservation(ctx, v.ID, observation.TicketID, pending[0].Generation); err != nil {
		t.Fatal(err)
	}
	if err := db.HandoffTicketObservation(ctx, observation); err != nil {
		t.Fatal(err)
	}
	if err := db.ClearPendingTicketObservation(ctx, v.ID, observation.TicketID, generation); err != nil {
		t.Fatal(err)
	}
	pending, err = db.ListPendingTicketObservations(ctx, v.ID)
	if err != nil || len(pending) != 1 || pending[0].Generation != generation+2 {
		t.Fatalf("generation reused %+v %v", pending, err)
	}
	bad := p
	bad.CompletedThrough = coverage.Add(time.Hour)
	if _, err := db.CommitTicketRecoveryPage(ctx, bad, p.Revision, nil); !errors.Is(err, ErrRecoveryConflict) {
		t.Fatalf("partial advanced %v", err)
	}
	bad = p
	bad.RepliesComplete = true
	bad.CompletedThrough = coverage.Add(time.Hour)
	wrong := observation
	wrong.ContainerID = "other"
	if _, err := db.CommitTicketRecoveryPage(ctx, bad, p.Revision, []PendingTicketObservation{wrong}); !errors.Is(err, ErrRecoveryConflict) {
		t.Fatalf("wrong source accepted %v", err)
	}
	persisted, err := db.GetTicketRecoveryPosition(ctx, v.ID)
	if err != nil || persisted.Revision != p.Revision {
		t.Fatalf("failed page persisted %+v %v", persisted, err)
	}
	bad, err = db.CommitTicketRecoveryPage(ctx, bad, p.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CommitTicketRecoveryPage(ctx, p, p.Revision, nil); !errors.Is(err, ErrRecoveryConflict) {
		t.Fatalf("stale page accepted %v", err)
	}
	v.Provenance = "explicit"
	v.RemoteID = "replacement"
	v, err = db.SetConnection(ctx, v, v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := db.InitTicketRecoveryPosition(ctx, v, coverage.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !replacement.CompletedThrough.Equal(replacement.EnrollmentAt) || replacement.CompletedThrough.Equal(bad.CompletedThrough) {
		t.Fatalf("replacement inherited %+v", replacement)
	}
}

func TestTicketSubscriptionIntentOperationRaces(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "subscriptions")
	ctx := t.Context()
	v := connectionFixture(repo, TicketConnection)
	v.RemoteID = "container"
	v, err := db.SetConnection(ctx, v, 0)
	if err != nil {
		t.Fatal(err)
	}
	s := TicketSubscription{ID: "opaque-selector", RepoID: repo.ID, ConnectionVersionID: v.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: v.RemoteID, CallbackURL: "https://callback.invalid/hooks/opaque-selector", DesiredEnabled: true, SecretService: "service", SecretAccount: "account", SecretVersion: "1"}
	s, err = db.PutTicketSubscription(ctx, s, 0)
	if err != nil {
		t.Fatal(err)
	}
	op, err := db.ClaimSubscriptionOperation(ctx, s.ID, s.Revision, "create")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimSubscriptionOperation(ctx, s.ID, s.Revision, "create"); !errors.Is(err, ErrSubscriptionBusy) {
		t.Fatalf("duplicate dispatch %v", err)
	}
	s.DesiredEnabled = false
	s, err = db.PutTicketSubscription(ctx, s, s.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishSubscriptionOperation(ctx, op, "confirmed", "hook-1", "enabled", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetTicketSubscription(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DesiredEnabled || got.RemoteHookID != "" || got.ObservedState != "pending" {
		t.Fatalf("old response overwrote disable %+v", got)
	}
	if err := db.RecordSubscriptionDelivery(ctx, s.ID, s.Revision, time.Now()); !errors.Is(err, ErrSubscriptionConflict) {
		t.Fatalf("disabled delivery recorded %v", err)
	}
	if _, err := db.PutTicketSubscription(ctx, s, s.Revision-1); !errors.Is(err, ErrSubscriptionConflict) {
		t.Fatalf("stale intent accepted %v", err)
	}
	if err := db.AdoptSubscriptionRemoteHook(ctx, s.ID, s.Revision-1, "", "hook-1"); !errors.Is(err, ErrSubscriptionConflict) {
		t.Fatalf("stale hook adoption %v", err)
	}
	if err := db.AdoptSubscriptionRemoteHook(ctx, s.ID, s.Revision, "", "hook-1"); err != nil {
		t.Fatal(err)
	}
	if err := db.ClearSubscriptionRemoteHook(ctx, s.ID, s.Revision, "other-hook"); !errors.Is(err, ErrSubscriptionConflict) {
		t.Fatalf("unrelated hook clear %v", err)
	}
	current, err := db.ClaimSubscriptionOperation(ctx, s.ID, s.Revision, "disable")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishSubscriptionOperation(ctx, current, "uncertain", "hook-1", "unknown", "remote_unavailable", time.Now()); err != nil {
		t.Fatal(err)
	}
	saved, err := db.GetSubscriptionOperation(ctx, s.ID, s.Revision)
	if err != nil || saved.State != "uncertain" {
		t.Fatalf("uncertainty lost %+v %v", saved, err)
	}
	if _, err := db.ClaimSubscriptionOperation(ctx, s.ID, s.Revision, "disable"); !errors.Is(err, ErrSubscriptionBusy) {
		t.Fatalf("uncertain operation stolen %v", err)
	}
}

func TestTicketRecoveryLegacyEnterpriseAndAmbiguousMapping(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "enterprise")
	ctx := t.Context()
	v := connectionFixture(repo, TicketConnection)
	v.Provenance = "legacy"
	v.Origin = "https://git.example.test/api/v3"
	v, err := db.SetConnection(ctx, v, 0)
	if err != nil {
		t.Fatal(err)
	}
	coverage := repo.CreatedAt.Add(time.Hour)
	if err := db.SetRepoPollCursor(ctx, repo.ID, coverage); err != nil {
		t.Fatal(err)
	}
	p, err := db.InitTicketRecoveryPosition(ctx, v, repo.CreatedAt)
	if err != nil || !p.CompletedThrough.Equal(coverage) {
		t.Fatalf("enterprise legacy coverage %+v %v", p, err)
	}
	bad := v
	bad.Origin = "https://elsewhere.test/api/v3"
	if _, err := db.InitTicketRecoveryPosition(ctx, bad, repo.CreatedAt); !errors.Is(err, ErrRecoveryConflict) {
		t.Fatalf("existing position relabelled %v", err)
	}
	other := testRepo(t, db, "owner", "ambiguous")
	legacy := connectionFixture(other, TicketConnection)
	legacy.Provenance = "legacy"
	legacy, err = db.SetConnection(ctx, legacy, 0)
	if err != nil {
		t.Fatal(err)
	}
	replacement := legacy
	replacement.Provenance = "explicit"
	replacement.Destination = "new/source"
	if _, err := db.SetConnection(ctx, replacement, legacy.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := db.InitTicketRecoveryPosition(ctx, legacy, other.CreatedAt); !errors.Is(err, ErrRecoveryMigrationAmbiguous) {
		t.Fatalf("ambiguous legacy mapped %v", err)
	}
}

func TestLegacySubscriptionImportPreservesOwnedHookAndDisable(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "legacyhook")
	ctx := t.Context()
	v := connectionFixture(repo, TicketConnection)
	v.Provenance = "legacy"
	v, err := db.SetConnection(ctx, v, 0)
	if err != nil {
		t.Fatal(err)
	}
	at := repo.CreatedAt.Add(time.Minute)
	if err := db.SetRepoWebhook(ctx, repo.ID, 42, "https://callback.invalid/webhook", at); err != nil {
		t.Fatal(err)
	}
	sub, err := db.ImportLegacyTicketSubscription(ctx, v, "verified-container")
	if err != nil {
		t.Fatal(err)
	}
	if sub.RemoteHookID != "42" || sub.SecretAccount != repo.Slug() || sub.SecretVersion != "legacy" || sub.ObservedState != "legacy_unverified" || !sub.CheckedAt.Equal(at) {
		t.Fatalf("legacy provenance lost %+v", sub)
	}
	sub.DesiredEnabled = false
	sub, err = db.PutTicketSubscription(ctx, sub, sub.Revision)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := db.ImportLegacyTicketSubscription(ctx, v, "verified-container")
	if err != nil || repeated.ID != sub.ID || repeated.DesiredEnabled {
		t.Fatalf("legacy import enabled disabled subscription %+v %v", repeated, err)
	}
	replacement := v
	replacement.Provenance = "explicit"
	replacement, err = db.SetConnection(ctx, replacement, v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ImportLegacyTicketSubscription(ctx, replacement, "verified-container"); !errors.Is(err, ErrSubscriptionConflict) {
		t.Fatalf("legacy owner moved to replacement %v", err)
	}
}

func TestRecoveryContainerPinRejectsReplacementAndWrongObservation(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "pin")
	ctx := t.Context()
	v, err := db.SetConnection(ctx, connectionFixture(repo, TicketConnection), 0)
	if err != nil {
		t.Fatal(err)
	}
	p, err := db.InitTicketRecoveryPosition(ctx, v, v.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	p, err = db.PinTicketRecoveryContainer(ctx, v.ID, p.Revision, "verified")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.PinTicketRecoveryContainer(ctx, v.ID, p.Revision, "replacement"); !errors.Is(err, ErrRecoveryConflict) {
		t.Fatalf("pinned container replaced %v", err)
	}
	observation := PendingTicketObservation{ConnectionVersionID: v.ID, RepoID: repo.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: "replacement", TicketID: "99", TicketKey: "7"}
	if err := db.HandoffTicketObservation(ctx, observation); !errors.Is(err, ErrRecoveryConflict) {
		t.Fatalf("wrong container signal accepted %v", err)
	}
	observation.ContainerID = "verified"
	if err := db.HandoffTicketObservation(ctx, observation); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionObservationHandoffRejectsInterleavedDisable(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "subscription-reread")
	ctx := t.Context()
	v := connectionFixture(repo, TicketConnection)
	v.RemoteID = "container"
	v, err := db.SetConnection(ctx, v, 0)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := db.PutTicketSubscription(ctx, TicketSubscription{ID: "reread", RepoID: repo.ID, ConnectionVersionID: v.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: v.RemoteID, CallbackURL: "https://callback.invalid/hooks?subscription=reread", DesiredEnabled: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	observedRevision := sub.Revision
	observation := PendingTicketObservation{ConnectionVersionID: v.ID, RepoID: repo.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: v.RemoteID, TicketID: "99", TicketKey: "7"}
	sub.DesiredEnabled = false
	sub, err = db.PutTicketSubscription(ctx, sub, sub.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.HandoffSubscriptionObservation(ctx, sub.ID, observedRevision, observation); !errors.Is(err, ErrSubscriptionConflict) {
		t.Fatalf("stale reread admitted after disable %v", err)
	}
	pending, err := db.ListPendingTicketObservations(ctx, v.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("disabled reread handed off %+v %v", pending, err)
	}
	sub.DesiredEnabled = true
	sub, err = db.PutTicketSubscription(ctx, sub, sub.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.HandoffSubscriptionObservation(ctx, sub.ID, sub.Revision, observation); err != nil {
		t.Fatal(err)
	}
	pending, err = db.ListPendingTicketObservations(ctx, v.ID)
	if err != nil || len(pending) != 1 || pending[0].SubscriptionID != sub.ID || pending[0].SubscriptionRevision != sub.Revision {
		t.Fatalf("webhook provenance missing %+v %v", pending, err)
	}
	v.Enabled = false
	if _, err := db.SetConnection(ctx, v, v.Revision); err != nil {
		t.Fatal(err)
	}
	if err := db.HandoffSubscriptionObservation(ctx, sub.ID, sub.Revision, observation); !errors.Is(err, ErrSubscriptionConflict) {
		t.Fatalf("disabled connection admitted %v", err)
	}
}

func TestRelatedObservationChecksOriginAndIndependentDestination(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	originRepo := testRepo(t, db, "owner", "origin")
	targetRepo := testRepo(t, db, "owner", "related")
	source := connectionFixture(originRepo, TicketConnection)
	source.RemoteID = "origin-container"
	source, err := db.SetConnection(ctx, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	target := connectionFixture(targetRepo, TicketConnection)
	target.RemoteID = "related-container"
	target, err = db.SetConnection(ctx, target, 0)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := db.PutTicketSubscription(ctx, TicketSubscription{ID: "origin-auth", RepoID: source.RepoID, ConnectionVersionID: source.ID, Provider: source.Provider, Origin: source.Origin, Container: source.Destination, ContainerID: source.RemoteID, CallbackURL: "https://callback.invalid/origin", DesiredEnabled: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	observation := PendingTicketObservation{ConnectionVersionID: target.ID, RepoID: target.RepoID, Provider: target.Provider, Origin: target.Origin, Container: target.Destination, ContainerID: target.RemoteID, TicketID: "related-ticket", TicketKey: "7"}
	// A configured target with no webhook subscription can receive the verified
	// relationship hint, using its own source rather than the origin container.
	if err := db.HandoffRelatedTicketObservation(ctx, auth.ID, auth.Revision, "", 0, observation); err != nil {
		t.Fatal(err)
	}
	pending, err := db.ListPendingTicketObservations(ctx, target.ID)
	if err != nil || len(pending) != 1 || pending[0].SubscriptionID != auth.ID || pending[0].RelatedSubscriptionID != "" {
		t.Fatalf("polling-only qualified target %+v %v", pending, err)
	}
	if err := db.ClearPendingTicketObservation(ctx, target.ID, observation.TicketID, pending[0].Generation); err != nil {
		t.Fatal(err)
	}
	targetSub, err := db.PutTicketSubscription(ctx, TicketSubscription{ID: "target-auth", RepoID: target.RepoID, ConnectionVersionID: target.ID, Provider: target.Provider, Origin: target.Origin, Container: target.Destination, ContainerID: target.RemoteID, CallbackURL: "https://callback.invalid/target", DesiredEnabled: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.HandoffRelatedTicketObservation(ctx, auth.ID, auth.Revision, targetSub.ID, targetSub.Revision-1, observation); !errors.Is(err, ErrSubscriptionConflict) {
		t.Fatalf("stale target admitted %v", err)
	}
	if err := db.HandoffRelatedTicketObservation(ctx, auth.ID, auth.Revision, targetSub.ID, targetSub.Revision, observation); err != nil {
		t.Fatal(err)
	}
	pending, err = db.ListPendingTicketObservations(ctx, target.ID)
	if err != nil || len(pending) != 1 || pending[0].RelatedSubscriptionID != targetSub.ID || pending[0].RelatedSubscriptionRevision != targetSub.Revision {
		t.Fatalf("independent target revision missing %+v %v", pending, err)
	}
	if err := db.ClearPendingTicketObservation(ctx, target.ID, observation.TicketID, pending[0].Generation); err != nil {
		t.Fatal(err)
	}
	targetSub.DesiredEnabled = false
	targetSub, err = db.PutTicketSubscription(ctx, targetSub, targetSub.Revision)
	if err != nil {
		t.Fatal(err)
	}
	// Omitting the optional target selector cannot bypass explicit disable.
	if err := db.HandoffRelatedTicketObservation(ctx, auth.ID, auth.Revision, "", 0, observation); !errors.Is(err, ErrSubscriptionConflict) {
		t.Fatalf("disabled target bypassed %v", err)
	}
	targetSub.DesiredEnabled = true
	targetSub, err = db.PutTicketSubscription(ctx, targetSub, targetSub.Revision)
	if err != nil {
		t.Fatal(err)
	}
	auth.DesiredEnabled = false
	auth, err = db.PutTicketSubscription(ctx, auth, auth.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.HandoffRelatedTicketObservation(ctx, auth.ID, auth.Revision-1, targetSub.ID, targetSub.Revision, observation); !errors.Is(err, ErrSubscriptionConflict) {
		t.Fatalf("disabled/stale source admitted %v", err)
	}
	pending, err = db.ListPendingTicketObservations(ctx, target.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("rejected relation handed off %+v %v", pending, err)
	}
}

func TestCredentialEditPreservesProvenEnrollmentWithoutCoverage(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "credential-edit")
	ctx := t.Context()
	v := connectionFixture(repo, TicketConnection)
	v, err := db.SetConnection(ctx, v, 0)
	if err != nil {
		t.Fatal(err)
	}
	enrollment := v.CreatedAt.Add(-time.Hour)
	original, err := db.InitTicketRecoveryPosition(ctx, v, enrollment)
	if err != nil {
		t.Fatal(err)
	}
	original, err = db.PinTicketRecoveryContainer(ctx, v.ID, original.Revision, "immutable-container")
	if err != nil {
		t.Fatal(err)
	}
	original.ScanThrough = v.CreatedAt
	original.CompletedThrough = v.CreatedAt
	original.TicketsComplete = true
	original.RepliesComplete = true
	original.ReconcileCursor = "old-reconciliation-continuation"
	original, err = db.CommitTicketRecoveryPage(ctx, original, original.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	// This intermediate edit never ran discovery but its explicit immutable ID
	// proves that it belongs to the same uninterrupted enrollment.
	v.RemoteID = "immutable-container"
	v.CredentialEnv = "SECOND_TOKEN"
	v, err = db.SetConnection(ctx, v, v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	v.RemoteID = ""
	v.CredentialEnv = "THIRD_TOKEN"
	v, err = db.SetConnection(ctx, v, v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := db.InitTicketRecoveryPosition(ctx, v, v.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err = db.PinTicketRecoveryContainer(ctx, v.ID, fresh.Revision, "immutable-container")
	if err != nil {
		t.Fatal(err)
	}
	if !fresh.EnrollmentAt.Equal(enrollment) || !fresh.CompletedThrough.Equal(enrollment) || !fresh.ScanThrough.IsZero() || fresh.TicketCursor != "" || fresh.ReplyCursor != "" || fresh.ReconcileCursor != "" || fresh.TicketsComplete || fresh.RepliesComplete {
		t.Fatalf("credential edit lost enrollment or inherited coverage %+v", fresh)
	}
	retained, err := db.GetTicketRecoveryPosition(ctx, original.ConnectionVersionID)
	if err != nil || retained != original {
		t.Fatalf("previous position changed %+v %v", retained, err)
	}
}

func TestEnrollmentContinuityStopsAtSourceAndDisableBoundaries(t *testing.T) {
	for _, boundary := range []string{"origin-change", "destination-change", "container-change", "disabled", "unknown-container"} {
		t.Run(boundary, func(t *testing.T) {
			db := newTestDB(t)
			repo := testRepo(t, db, "owner", "continuity")
			ctx := t.Context()
			v := connectionFixture(repo, TicketConnection)
			v.RemoteID = "immutable-container"
			v, err := db.SetConnection(ctx, v, 0)
			if err != nil {
				t.Fatal(err)
			}
			original := v
			oldEnrollment := v.CreatedAt.Add(-time.Hour)
			p, err := db.InitTicketRecoveryPosition(ctx, v, oldEnrollment)
			if err != nil {
				t.Fatal(err)
			}
			switch boundary {
			case "origin-change":
				v.Origin = "https://another.example.test/api/v3"
			case "destination-change":
				v.Destination = "elsewhere/project"
			case "container-change":
				v.RemoteID = "another-container"
			case "disabled":
				v.Enabled = false
			case "unknown-container":
				v.RemoteID = ""
			}
			v, err = db.SetConnection(ctx, v, v.Revision)
			if err != nil {
				t.Fatal(err)
			}
			// Switch-back and re-enable must not look past the intervening boundary.
			original.CredentialEnv = "NEW_TOKEN"
			v, err = db.SetConnection(ctx, original, v.Revision)
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := db.InitTicketRecoveryPosition(ctx, v, v.CreatedAt)
			if err != nil {
				t.Fatal(err)
			}
			fresh, err = db.PinTicketRecoveryContainer(ctx, v.ID, fresh.Revision, "immutable-container")
			if err != nil {
				t.Fatal(err)
			}
			if !fresh.EnrollmentAt.Equal(v.CreatedAt) || fresh.EnrollmentAt.Equal(p.EnrollmentAt) {
				t.Fatalf("enrollment crossed %s boundary %+v", boundary, fresh)
			}
		})
	}
}

func TestEnrollmentCannotChangeAfterDiscoveryStarts(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "started")
	ctx := t.Context()
	v := connectionFixture(repo, TicketConnection)
	v.RemoteID = "immutable-container"
	v, err := db.SetConnection(ctx, v, 0)
	if err != nil {
		t.Fatal(err)
	}
	oldEnrollment := v.CreatedAt.Add(-time.Hour)
	if _, err := db.InitTicketRecoveryPosition(ctx, v, oldEnrollment); err != nil {
		t.Fatal(err)
	}
	v.CredentialEnv = "NEW_TOKEN"
	v, err = db.SetConnection(ctx, v, v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	p, err := db.InitTicketRecoveryPosition(ctx, v, v.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	p.ScanThrough = v.CreatedAt.Add(time.Minute)
	p, err = db.CommitTicketRecoveryPage(ctx, p, p.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := db.PinTicketRecoveryContainer(ctx, v.ID, p.Revision, "immutable-container")
	if err != nil {
		t.Fatal(err)
	}
	if !pinned.EnrollmentAt.Equal(v.CreatedAt) || pinned.Revision != p.Revision {
		t.Fatalf("started recovery enrollment changed %+v", pinned)
	}
}
