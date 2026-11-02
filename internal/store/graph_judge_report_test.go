package store

import (
	"testing"
)

func TestGraphJudgeEvidencePinsResultRevisionAndObservedModel(t *testing.T) {
	db, _, instance, session, diagnosis, _ := graphDiagnosisFixture(t)
	ctx := t.Context()
	source, err := db.ResolveJudgeSource(ctx, session.TaskRef, "")
	if err != nil {
		t.Fatal(err)
	}
	if source.ID != diagnosis.ResultID || source.Revision != diagnosis.ResultRevision {
		t.Fatalf("wrong source %+v", source)
	}
	request, err := db.AdmitJudgeRequest(ctx, JudgeRequest{SessionID: session.ID, TriggerCommentID: 99, Task: session.TaskRef, TicketKey: "graph-report", CommentID: "99", Source: source, Configuration: []byte(`{}`), Evidence: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := db.StartJudgeAttempt(ctx, request.ID, "codex", "requested-model", "high")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.RecordJudgeInvocation(ctx, attempt.ID, "observed-model", "test-client"); err != nil {
		t.Fatal(err)
	}
	got, err := db.ListGraphJudgeAttempts(ctx, instance.ID)
	if err != nil || len(got) != 1 || got[0].ObservedModel != "observed-model" || got[0].Model != "requested-model" {
		t.Fatalf("evidence=%+v err=%v", got, err)
	}
	meter, err := db.StartJudgeMeterAttempt(ctx, attempt.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	runs, err := db.ListRuns(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.StartMeterAttempt(ctx, runs[0].ID, "claude"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := db.NewMeterSnapshot(ctx, MeterFilter{JudgeAttemptID: attempt.ID}, MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := snapshot.Next(ctx, 10)
	if err != nil || len(rows) != 1 || rows[0].AttemptID != meter.ID || rows[0].ExecutionPath != "judge" {
		t.Fatalf("judge filter leaked attempts: %+v %v", rows, err)
	}
	if err = snapshot.Close(ctx); err != nil {
		t.Fatal(err)
	}
	other := graphTestInstance(t, db)
	got, err = db.ListGraphJudgeAttempts(ctx, other.ID)
	if err != nil || len(got) != 0 {
		t.Fatalf("unrelated graph inherited judge: %+v %v", got, err)
	}
	if _, err = db.ListGraphJudgeAttempts(ctx, 0); err == nil {
		t.Fatal("invalid instance accepted")
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ListGraphJudgeAttempts(ctx, instance.ID); err == nil {
		t.Fatal("storage error hidden")
	}
}
