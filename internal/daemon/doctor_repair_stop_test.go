package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func doctorStopFixture(t *testing.T) (*Daemon, naming.Ref, store.DoctorRepair, store.Run) {
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
	_ = config.NewPaths(root)
	session, err := d.db.GetOrCreateSession(ctx, repo, 406)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := d.db.GetOrCreateSegment(ctx, session.ID, 1, "implement", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := d.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentClaude, LogPath: func(int) string { return "fixture.log" }})
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
	binding, err := d.db.BindDoctorResultAttempt(ctx, "host-proposal")
	if err != nil {
		t.Fatal(err)
	}
	target := fmt.Sprintf("run:%d", run.ID)
	input := doctor.ResultInput{Diagnosis: doctor.Diagnosis{Findings: []doctor.Finding{{Explanation: "Polling requires the approved correction", Uncertainty: "Controlled local fixture", Evidence: []string{target}}}, CoverageGaps: evidence.Gaps, Proposal: []doctor.RepairStep{{Number: 1, Target: target, Precondition: "poll flag is zero", Change: "set poll flag", Reason: "resume discovery", Reversibility: "row backup", Rollback: "restore zero", Action: &store.DoctorRepairAction{Kind: "stop_run", Before: "running", After: "failed"}}}}}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	// Model accepted legacy history directly in the durable result ledger.
	// Current submission admission correctly refuses this old repair kind.
	snapshot, err := json.Marshal(input.Diagnosis)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(input.Diagnosis.Proposal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.db.RecordResult(ctx, store.ResultRecord{Binding: binding, RequestID: "host-result", Payload: raw, Snapshot: snapshot, Diagnostics: []byte(`{"valid":true,"errors":[]}`), Retired: []byte(`[]`), ExpectedRevision: input.ExpectedRevision, Accepted: true}); err != nil {
		t.Fatal(err)
	}
	if err = d.db.AdoptDoctorResult(ctx, binding, "host-result", manifest); err != nil {
		t.Fatal(err)
	}
	proposal, _, err := d.db.DoctorProposalReference(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.db.PresentDoctorProposal(ctx, ref.String(), "host-presentation", proposal); err != nil {
		t.Fatal(err)
	}
	operation, err := d.db.ReserveDoctorRepair(ctx, store.DoctorApproval{ID: "host-repair", SourceID: "host-approval", Principal: "local-owner", TaskRef: ref.String(), PresentationID: "host-presentation"})
	if err != nil {
		t.Fatal(err)
	}
	return d, ref, operation, run
}

func TestDoctorStopRunCancelsAndJoinsBeforeSettlement(t *testing.T) {
	d, ref, op, run := doctorStopFixture(t)
	worker, cancel := context.WithCancel(t.Context())
	defer cancel()
	d.inflight.enter(run.SessionID)
	d.inflight.bindCancel(run.SessionID, cancel)
	done := make(chan error, 1)
	go func() { done <- d.StopDoctorRun(t.Context(), ref, op.ID, 1) }()
	<-worker.Done()
	select {
	case err := <-done:
		t.Fatal("returned before join", err)
	default:
	}
	got, err := d.db.GetRun(t.Context(), run.ID)
	if err != nil || got.Status != store.RunRunning {
		t.Fatal(got, err)
	}
	d.inflight.leave(run.SessionID)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := d.DrainDoctorTarget(t.Context(), ref, op.ID); err != nil {
		t.Fatal(err)
	}
	got, err = d.db.GetRun(t.Context(), run.ID)
	if err != nil || got.Status != store.RunFailed {
		t.Fatal(got, err)
	}
}
func TestDoctorStopRunTimeoutLeavesBackupAndNoFalseCompletion(t *testing.T) {
	d, ref, op, run := doctorStopFixture(t)
	worker, cancel := context.WithCancel(t.Context())
	defer cancel()
	d.inflight.enter(run.SessionID)
	defer d.inflight.leave(run.SessionID)
	d.inflight.bindCancel(run.SessionID, cancel)
	ctx, end := context.WithTimeout(t.Context(), time.Second)
	defer end()
	if err := d.StopDoctorRun(ctx, ref, op.ID, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if worker.Err() == nil {
		t.Fatal("worker not cancelled")
	}
	steps, err := d.db.DoctorRepairSteps(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].State != "backed_up" || steps[0].BackupReference == "" {
		t.Fatal(steps)
	}
	if _, err = d.DrainDoctorTarget(t.Context(), ref, op.ID); err == nil {
		t.Fatal("timed out invocation drained")
	}
}
