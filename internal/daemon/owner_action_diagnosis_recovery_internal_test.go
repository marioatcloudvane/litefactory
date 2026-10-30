package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type recoveryOnlyDiagnoser struct{ calls int }

func (d *recoveryOnlyDiagnoser) Diagnose(context.Context, doctor.DiagnoseRequest) (doctor.Conversation, error) {
	d.calls++
	return doctor.Conversation{}, nil
}

func retainSourceDiagnosis(t *testing.T, db *store.DB, id, source, text string) {
	t.Helper()
	ctx := t.Context()
	if _, err := db.BeginDoctorAttempt(ctx, "github:acme/app#42", store.DoctorAttempt{ID: id, SourceID: source, OwnerText: "Inspect the task", PromptVersion: "fixture", GuideVersion: "fixture"}); err != nil {
		t.Fatal(err)
	}
	binding, err := db.BindDoctorResultAttempt(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := json.Marshal(doctor.Diagnosis{Findings: []doctor.Finding{{Explanation: text}}, CoverageGaps: []string{}, Questions: []string{}, Proposal: []doctor.RepairStep{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.RecordResult(ctx, store.ResultRecord{Binding: binding, RequestID: "accepted", Payload: []byte(`{}`), Diagnostics: []byte(`{}`), Snapshot: snapshot, Retired: []byte(`{}`), Accepted: true}); err != nil {
		t.Fatal(err)
	}
	if err = db.AdoptDoctorResult(ctx, binding, "accepted", nil); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerActionDiagnosisRecoveryReadsOriginalSourceWithoutProvider(t *testing.T) {
	db := authorDB(t)
	ctx := t.Context()
	const target = "github:acme/app#42"
	retainSourceDiagnosis(t, db, "original", "owner-action:original", "Original diagnosis diagnosis-owner-secret")
	if err := db.StopDoctorConversation(ctx, target); err != nil {
		t.Fatal(err)
	}
	retainSourceDiagnosis(t, db, "later", "owner-action:later", "Unrelated newer diagnosis")
	provider := &recoveryOnlyDiagnoser{}
	chat := messaging.New(db, "discord", "7", []string{"3"}, nil, messaging.FactoryStatus{DB: db})
	d := &Daemon{db: db, chat: &chatRuntime{service: chat, diagnoser: provider, redactor: redact.New(redact.Options{Environ: []string{"API_SECRET=diagnosis-owner-secret"}})}}
	for range 2 {
		result, found, err := (chatResearch{daemon: d}).RecoverOwnerActionDiagnosis(ctx, target, "owner-action:original")
		if err != nil || !found || result.State != "succeeded" || result.Reference != "original" || !strings.Contains(result.Summary, "Original diagnosis") || strings.Contains(result.Summary, "diagnosis-owner-secret") || strings.Contains(result.Summary, "Unrelated") {
			t.Fatalf("wrong recovered diagnosis: %+v %v %v", result, found, err)
		}
	}
	if provider.calls != 0 {
		t.Fatalf("recovery launched provider %d times", provider.calls)
	}
	if _, found, err := (chatResearch{daemon: d}).RecoverOwnerActionDiagnosis(ctx, "github:acme/app#43", "owner-action:original"); err != nil || found {
		t.Fatalf("foreign task adopted source: %v %v", found, err)
	}
}

func TestOwnerActionDiagnosisRecoveryKeepsUnacceptedAttemptUncertain(t *testing.T) {
	db := authorDB(t)
	ctx := t.Context()
	const target = "github:acme/app#42"
	if _, err := db.BeginDoctorAttempt(ctx, target, store.DoctorAttempt{ID: "running", SourceID: "owner-action:running", OwnerText: "Inspect the task", PromptVersion: "fixture", GuideVersion: "fixture"}); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{db: db, chat: &chatRuntime{service: messaging.New(db, "discord", "7", []string{"3"}, nil, messaging.FactoryStatus{DB: db}), redactor: redact.New(redact.Options{})}}
	result, found, err := (chatResearch{daemon: d}).RecoverOwnerActionDiagnosis(ctx, target, "owner-action:running")
	if err != nil || !found || result.State != "uncertain" {
		t.Fatalf("unaccepted attempt claimed success: %+v %v %v", result, found, err)
	}
}
