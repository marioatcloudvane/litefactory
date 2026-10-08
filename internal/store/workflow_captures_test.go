package store

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func captureFixture(t *testing.T) (*DB, WorkflowLaunch) {
	t.Helper()
	db := newTestDB(t)
	session := testSession(t, db, 9516)
	if _, err := db.AdmitResearchPath(t.Context(), session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	pin, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 2, Path: "research/research.dot", Digest: strings.Repeat("a", 64), CreatedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	config := []byte("name: standard\nversion: 1.0.0\n")
	return db, WorkflowLaunch{SessionID: session.ID, TriggerID: 1, DefinitionID: pin.ID, Mode: "research", Start: "work", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte(`{"research.dot":"eA=="}`), Provenance: WorkflowProvenance{Kind: "fetched", Source: "https://example.com/teams/index.json", Slug: "standard", Version: "1.0.0", ReleaseDigest: strings.Repeat("d", 64), GraphPath: "research/research.dot", GraphFile: "research.dot", Team: "builtin", TeamConfig: config, ConfigDigest: captureBytesDigest(config)}}
}

func TestWorkflowCaptureExactReferencesAndImmutableProvenance(t *testing.T) {
	db, launch := captureFixture(t)
	first, err := db.AdmitWorkflowLaunch(t.Context(), launch)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: first.Instance.ID, LaunchID: first.LaunchID, Node: "work", Limit: 8, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: graphTestTime(), InstanceStatus: GraphRunning}); err != nil {
		t.Fatal(err)
	}
	dispatch, err := db.ParkGraphInstance(t.Context(), first.Instance.ID, "work", graphTestTime(), graphTestTime())
	if err != nil {
		t.Fatal(err)
	}
	if dispatch.LaunchID != first.LaunchID {
		t.Fatal("dispatch did not bind exact admitted capture")
	}
	changed := launch
	changed.Provenance.Version = "2.0.0"
	if _, err := db.AdmitWorkflowLaunch(t.Context(), changed); err == nil {
		t.Fatal("replay replaced exact package provenance")
	}
	changed.TriggerID++
	changed.ContentDigest = strings.Repeat("e", 64)
	changed.Files = []byte(`{"research.dot":"eQ=="}`)
	second, err := db.AdmitWorkflowLaunch(t.Context(), changed)
	if err != nil {
		t.Fatal(err)
	}
	if second.Instance.ID != first.Instance.ID || second.LaunchID == first.LaunchID {
		t.Fatal("expected distinct launches sharing topology")
	}
	original, err := db.WorkflowCaptureForAttempt(t.Context(), attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if original.ID != first.LaunchID || original.Provenance.Version != "1.0.0" || !bytes.Equal(original.Files, launch.Files) {
		t.Fatalf("attempt substituted latest capture: %+v", original)
	}
	continued, err := db.WorkflowCaptureForAdmission(t.Context(), launch.SessionID, dispatch.TriggerID(), "research")
	if err != nil || continued.ID != first.LaunchID {
		t.Fatalf("dispatch substituted latest capture: %+v %v", continued, err)
	}
	if _, ok, err := db.ReserveGraphDispatch(t.Context(), dispatch.ID, graphTestTime()); err != nil || !ok {
		t.Fatalf("reserve original dispatch: %v", err)
	}
	next, err := db.ParkCapturedGraphInstanceAfter(t.Context(), first.Instance.ID, "work", graphTestTime(), graphTestTime(), dispatch.ID, first.LaunchID)
	if err != nil || next.ID == dispatch.ID || next.LaunchID != first.LaunchID {
		t.Fatalf("next stage substituted newer capture: %+v %v", next, err)
	}
	active, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: first.Instance.ID, LaunchID: first.LaunchID, Node: "work", Limit: 8, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := db.WorkflowCaptureForAdmission(t.Context(), launch.SessionID, launch.TriggerID+100, "research")
	if err != nil || recovery.ID != first.LaunchID {
		t.Fatalf("active recovery substituted newest launch: %+v %v", recovery, err)
	}
	if active.LaunchID != first.LaunchID {
		t.Fatal("active run lost exact reference")
	}
	replay, err := db.WorkflowCaptureForAdmission(t.Context(), launch.SessionID, launch.TriggerID, "research")
	if err != nil || replay.ID != first.LaunchID {
		t.Fatalf("replay lost exact capture: %+v %v", replay, err)
	}
}

func TestWorkflowCaptureBoundsCorruptionAndHistoricalReadability(t *testing.T) {
	db, launch := captureFixture(t)
	invalid := launch
	invalid.Provenance.ConfigDigest = strings.Repeat("0", 64)
	if _, err := db.AdmitWorkflowLaunch(t.Context(), invalid); err == nil {
		t.Fatal("invalid config digest retained")
	}
	invalid = launch
	invalid.Files = make([]byte, (16<<20)+1)
	if _, err := db.AdmitWorkflowLaunch(t.Context(), invalid); err == nil {
		t.Fatal("oversized serialized files retained")
	}
	if _, err := db.LatestResearchInstance(t.Context(), launch.SessionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed retention admitted work: %v", err)
	}
	first, err := db.AdmitWorkflowLaunch(t.Context(), launch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE research_launch_definitions SET files=? WHERE id=?`, []byte(`{"research.dot":"eQ=="}`), first.LaunchID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.GetWorkflowCapture(t.Context(), first.LaunchID); err == nil {
		t.Fatal("changed retained files accepted")
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE research_launch_definitions SET files=?,provenance=NULL,files_digest=NULL WHERE id=?`, launch.Files, first.LaunchID); err != nil {
		t.Fatal(err)
	}
	historical, err := db.GetWorkflowCapture(t.Context(), first.LaunchID)
	if err != nil {
		t.Fatal(err)
	}
	if historical.Provenance.Kind != "" {
		t.Fatal("historical row mislabeled installed release")
	}
}
