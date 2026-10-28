package store

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestArchiveRegistryRetainsValidationEvidenceAndSuiteSources(t *testing.T) {
	db := newTestDB(t)
	session := archiveTestSession(t, db, "acme", 1241)
	archiveTestComments(t, db, session.ID, 1)
	request, err := db.EnsureValidationRequest(t.Context(), ValidationRequest{Lineage: session.TaskRef, SourceKey: "original-validation", Policy: "full", Commit: suiteSHA})
	if err != nil {
		t.Fatal(err)
	}
	run := suiteBegin(t, db, 99)
	exit := 0
	command := SuiteCommand{RunID: run.ID, Position: 0, Argv: []string{"go", "test", "./..."}, State: SuitePassed, Exit: &exit, StartedAt: graphTestTime(), FinishedAt: graphTestTime(), CleanupConfirmed: true, OutputTail: "retained output", OutputDigest: "retained digest"}
	if err := db.FinishSuiteCommand(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishSuiteRun(t.Context(), run.ID, SuitePassed, "t", "", graphTestTime()); err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"events":[{"case":"TestOriginal","status":"pass"}],"diagnostic":"retained raw test evidence"}`)
	original, err := db.AppendValidationEvidence(t.Context(), ValidationEvidence{RequestID: request.ID, RunID: run.ID, Kind: "observation", Version: 1, Complete: true, Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	next, err := db.EnsureValidationRequest(t.Context(), ValidationRequest{Lineage: request.Lineage, SourceKey: "inherited-validation", Policy: "auto", Commit: suiteSHA})
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := db.FinalizeValidationAssessment(t.Context(), ValidationEvidence{RequestID: next.ID, Version: 1, Complete: true, Payload: json.RawMessage(`{"state":"covered"}`), References: []string{original.ID}})
	if err != nil {
		t.Fatal(err)
	}
	family := ArchiveFamily{SessionID: session.ID, RepoID: session.RepoID, TaskRef: session.TaskRef}
	deleted, err := db.DeleteArchivedFamily(t.Context(), family, archiveTestTrailer(t, db, family), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if deleted["comments"] != 1 {
		t.Fatalf("family was not archived: %v", deleted)
	}
	for _, req := range []ValidationRequest{request, next} {
		if got, err := db.GetValidationRequest(t.Context(), req.ID); err != nil || got.SourceKey != req.SourceKey {
			t.Fatalf("validation request not retained: %+v %v", got, err)
		}
	}
	got, err := db.ValidationEvidence(t.Context(), original.ID)
	if err != nil || !bytes.Equal(got.Payload, raw) {
		t.Fatalf("original evidence not retained: %+v %v", got, err)
	}
	inherited, err := db.ValidationEvidence(t.Context(), assessment.ID)
	if err != nil || len(inherited.References) != 1 || inherited.References[0] != original.ID {
		t.Fatalf("cross-generation source link not retained: %+v %v", inherited, err)
	}
	if got, err := db.GetSuiteRun(t.Context(), run.ID); err != nil || got.State != SuitePassed {
		t.Fatalf("source suite not retained: %+v %v", got, err)
	}
	commands, err := db.SuiteCommands(t.Context(), run.ID)
	if err != nil || len(commands) != 1 || commands[0].OutputDigest != command.OutputDigest {
		t.Fatalf("source command not retained: %+v %v", commands, err)
	}
}
