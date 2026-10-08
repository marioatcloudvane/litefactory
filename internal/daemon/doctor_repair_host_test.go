package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

type doctorHostFakeReplacement struct {
	calls   atomic.Int32
	failure error
}

func (f *doctorHostFakeReplacement) StartDoctorReplacement(context.Context, store.DoctorRestart) error {
	f.calls.Add(1)
	return f.failure
}

func doctorHostFixture(t *testing.T) (*Daemon, config.Paths, naming.Ref, store.DoctorRepair) {
	t.Helper()
	return doctorHostFixtureBeforeApproval(t, nil)
}

func doctorHostFixtureBeforeApproval(t *testing.T, seed func(*store.DB, store.Session)) (*Daemon, config.Paths, naming.Ref, store.DoctorRepair) {
	t.Helper()
	d, _, repo := doctorDaemonFixture(t)
	ctx := t.Context()
	root, err := os.MkdirTemp("/tmp", "lf-host-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	paths := config.NewPaths(root)
	session, err := d.db.GetOrCreateSession(ctx, repo, 406)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := d.db.ReadDoctorEvidence(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := d.db.BeginDoctorAttempt(ctx, ref.String(), store.DoctorAttempt{ID: "host-proposal", SourceID: "host-source", OwnerText: "fixture", PromptVersion: doctor.PromptVersion, GuideVersion: doctor.GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.db.PrepareDoctorAttempt(ctx, store.DoctorAttempt{ID: "host-proposal", Provider: "claude", ConfiguredModel: "fixture", Evidence: string(encoded)}); err != nil {
		t.Fatal(err)
	}
	result, err := doctor.BindResults(ctx, d.db, "host-proposal", evidence, d.doctor.service.Redactor)
	if err != nil {
		t.Fatal(err)
	}
	target := fmt.Sprintf("session:%d", session.ID)
	input := doctor.ResultInput{Diagnosis: doctor.Diagnosis{Findings: []doctor.Finding{{Explanation: "Polling requires the approved correction", Uncertainty: "Controlled local fixture", Evidence: []string{target}}}, CoverageGaps: evidence.Gaps, Proposal: []doctor.RepairStep{{Number: 1, Target: target, Precondition: "poll flag is zero", Change: "set poll flag", Reason: "resume discovery", Reversibility: "row backup", Rollback: "restore zero", Action: &store.DoctorRepairAction{Kind: "poll_flag", Before: "0", After: "1"}}}}}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = result.Submit(ctx, "host-result", raw); err != nil {
		t.Fatal(err)
	}
	if err = result.Adopt(ctx, "host-result"); err != nil {
		t.Fatal(err)
	}
	proposal, _, err := d.db.DoctorProposalReference(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.db.PresentDoctorProposal(ctx, ref.String(), "host-presentation", proposal); err != nil {
		t.Fatal(err)
	}
	if seed != nil {
		seed(d.db, session)
	}
	operation, err := d.db.ReserveDoctorRepair(ctx, store.DoctorApproval{ID: "host-repair", SourceID: "host-approval", Principal: "local-owner", TaskRef: ref.String(), PresentationID: "host-presentation"})
	if err != nil {
		t.Fatal(err)
	}
	return d, paths, ref, operation
}

func doctorHostVerifiedIntent(t *testing.T, d *Daemon, operation store.DoctorRepair, instance string) store.DoctorRestart {
	t.Helper()
	ctx := t.Context()
	if err := d.db.AcknowledgeDoctorExclusion(ctx, operation.ID, instance); err != nil {
		t.Fatal(err)
	}
	if err := d.db.BackupDoctorPoll(ctx, operation.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := d.db.ApplyDoctorPoll(ctx, operation.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := d.db.CommitDoctorRestart(ctx, operation.ID, "host-token", "verified fixture repair"); err != nil {
		t.Fatal(err)
	}
	intent, err := d.db.ClaimDoctorRestart(ctx, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

func TestDoctorHostProtectedDrainRestartAndNewInstanceObservation(t *testing.T) {
	d, paths, ref, operation := doctorHostFixture(t)
	ctx := t.Context()
	lock, err := AcquireLock(paths.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	d.lock = lock
	replacement := &doctorHostFakeReplacement{}
	var stopped atomic.Bool
	host, err := d.startDoctorRepairHost(ctx, DoctorHostOptions{Paths: paths, Replacement: replacement, Stop: func() { stopped.Store(true) }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = host.Close(); _ = d.lock.Close() }()
	client := &DoctorLocalControl{Paths: paths}
	if _, err = client.DrainDoctorTarget(ctx, ref, operation.ID); err == nil {
		t.Fatal("unready startup granted drain")
	}
	if err = host.prepare(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = client.DrainDoctorTarget(ctx, ref, "foreign-operation"); err == nil {
		t.Fatal("foreign operation drained")
	}
	instance, err := client.DrainDoctorTarget(ctx, ref, operation.ID)
	if err != nil || instance != d.instanceID {
		t.Fatalf("bound drain: %q %v", instance, err)
	}
	intent := doctorHostVerifiedIntent(t, d, operation, instance)
	route := &DoctorHostRestart{DB: d.db, Paths: paths, Live: client}
	if err = route.Dispatch(ctx, intent); err != nil {
		t.Fatal(err)
	}
	if replacement.calls.Load() != 1 || !stopped.Load() {
		t.Fatal("handoff did not use trusted lifecycle callback")
	}
	if err = route.Dispatch(ctx, intent); err == nil || replacement.calls.Load() != 1 {
		t.Fatal("restart repeated")
	}
	if _, confirmed, err := route.Observe(ctx, intent); err != nil || confirmed {
		t.Fatalf("old instance proved recovery: %t %v", confirmed, err)
	}
	if err = host.Close(); err != nil {
		t.Fatal(err)
	}
	if err = d.lock.Close(); err != nil {
		t.Fatal(err)
	}
	replacementLock, err := WaitDoctorRecoveryLock(ctx, d.db, paths, intent)
	if err != nil {
		t.Fatal(err)
	}
	d.lock = replacementLock
	d.instanceID = "replacement-instance"
	replacementHost, err := d.startDoctorRepairHost(ctx, DoctorHostOptions{Paths: paths, Replacement: replacement, Stop: func() {}, Recovery: &intent})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := replacementHost.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err = replacementHost.prepare(ctx); err != nil {
		t.Fatal(err)
	}
	got, confirmed, err := route.Observe(ctx, intent)
	if err != nil || !confirmed || got != "replacement-instance" {
		t.Fatalf("new-instance proof: %q %t %v", got, confirmed, err)
	}
}

func TestDoctorHostDownExclusionAndUncertainDispatchNeverReplay(t *testing.T) {
	d, paths, ref, operation := doctorHostFixture(t)
	ctx := t.Context()
	replacement := &doctorHostFakeReplacement{failure: errors.New("simulated lost start reply")}
	environment, route := NewDoctorHostCapabilities(d.db, paths, replacement)
	defer func() {
		if err := environment.Close(); err != nil {
			t.Error(err)
		}
	}()
	instance, err := environment.Exclude(ctx, ref, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	intent := doctorHostVerifiedIntent(t, d, operation, instance)
	if err = route.Dispatch(ctx, intent); err == nil {
		t.Fatal("uncertain start claimed success")
	}
	if err = route.Dispatch(ctx, intent); err == nil || replacement.calls.Load() != 1 {
		t.Fatal("uncertain start repeated")
	}
	if lock, err := AcquireLock(paths.LockPath()); err == nil {
		_ = lock.Close()
		t.Fatal("uncertain start released singleton exclusion")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = WaitDoctorRecoveryLock(cancelled, d.db, paths, intent); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled recovery wait: %v", err)
	}
}

func TestDoctorHostRefusesMissingSingletonAndChangedExecutable(t *testing.T) {
	d, paths, _, _ := doctorHostFixture(t)
	if _, err := d.startDoctorRepairHost(t.Context(), DoctorHostOptions{Paths: paths, Replacement: &doctorHostFakeReplacement{}, Stop: func() {}}); err == nil {
		t.Fatal("host without singleton ownership")
	}
	binary := filepath.Join(paths.Home(), "fixture")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	replacement, err := NewSameBinaryDoctorReplacement(binary, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	// This refuses before process creation. No lifecycle executable is run.
	if err := replacement.StartDoctorReplacement(t.Context(), store.DoctorRestart{RepairID: "fixture", Token: "token"}); err == nil {
		t.Fatal("changed factory executable accepted")
	}
}

type unavailableDoctorControl struct{}

func (unavailableDoctorControl) DrainDoctorTarget(context.Context, naming.Ref, string) (string, error) {
	return "", errors.New("stale private socket")
}

func TestDoctorHostStaleControlRequiresActualSingletonProof(t *testing.T) {
	d, paths, ref, operation := doctorHostFixture(t)
	environment := &DoctorRepairEnvironment{DB: d.db, Paths: paths, Live: unavailableDoctorControl{}}
	defer func() {
		if err := environment.Close(); err != nil {
			t.Error(err)
		}
	}()
	live, err := AcquireLock(paths.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = environment.Exclude(t.Context(), ref, operation.ID); err == nil {
		t.Fatal("socket failure treated as proof of down daemon")
	}
	if err = live.Close(); err != nil {
		t.Fatal(err)
	}
	instance, err := environment.Exclude(t.Context(), ref, operation.ID)
	if err != nil || instance != "down:"+operation.ID {
		t.Fatalf("actual singleton did not recover stale control: %q %v", instance, err)
	}
}
