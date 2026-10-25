package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/retainedevidence"
	"github.com/cloudvane/litefactory/internal/store"
)

// Recovery uses the real database and approved repair path, with a controlled
// provider. Removing the first sandbox and reopening storage rules out scratch
// files and provider memory as the source of the recovered evidence.
func TestRetainedRepairHandoffRestartReuse(t *testing.T) {
	s, approval, schedule := typedApplyFixture(t, false)
	ctx := t.Context()
	capture := testDoctorCapture(t, "doctor-diagnosis")
	rawCapture, err := capture.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.BeginDoctorAttempt(ctx, approval.Target.String(), store.DoctorAttempt{ID: "retained-guidance", SourceID: "retained-guidance-source", OwnerText: "Captured investigation", PromptVersion: PromptVersion, GuideVersion: GuideVersion}); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.RetainDoctorInstructionCapture(ctx, "retained-guidance", rawCapture); err != nil {
		t.Fatal(err)
	}
	commandBinding, err := s.DB.BindDoctorResultAttempt(ctx, "retained-guidance")
	if err != nil {
		t.Fatal(err)
	}
	commandOutput := `{"stdout":"retained command output","stderr":"retained command diagnostic","observed":true}`
	if _, err = s.DB.RecordResult(ctx, store.ResultRecord{Binding: commandBinding, RequestID: "retained-command", Payload: []byte(`{"step":1}`), Diagnostics: []byte(commandOutput), Accepted: true, DoctorToolReceipt: true}); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.FailDoctorAttempt(ctx, "retained-guidance", "failed", "Investigation retained for review"); err != nil {
		t.Fatal(err)
	}
	runner := &typedApplyRunner{}
	s.RepairRunner, s.RepairEnvironment = runner, repairFixtureEnvironment{}
	operation, err := s.Apply(ctx, approval)
	if err != nil || operation.State != "completed" {
		t.Fatalf("approved repair: %+v %v", operation, err)
	}
	_, err = s.DB.BeginDoctorAttempt(ctx, approval.Target.String(), store.DoctorAttempt{ID: "handoff-reader", SourceID: "handoff-reader-source", OwnerText: "Review retained evidence", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	beforeSteps, err := s.DB.DoctorRepairSteps(ctx, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	prepare := func(base string) (any, map[string]string) {
		t.Helper()
		doctorDir := filepath.Join(s.Paths.DoctorScratchDir(), filepath.Base(base))
		if err := os.MkdirAll(doctorDir, 0700); err != nil {
			t.Fatal(err)
		}
		dm, err := s.PrepareRetainedEvidence(ctx, approval.Target, "handoff-reader", doctorDir)
		if err != nil {
			t.Fatal(err)
		}
		indexBytes, err := os.ReadFile(dm.Index)
		if err != nil {
			t.Fatal(err)
		}
		var index struct {
			Views []struct {
				Entry retainedevidence.Entry `json:"entry"`
				File  string                 `json:"file"`
			} `json:"views"`
		}
		if err = json.Unmarshal(indexBytes, &index); err != nil {
			t.Fatal(err)
		}
		observed := map[string]bool{}
		for _, view := range index.Views {
			if view.File == "" {
				continue
			}
			var source store.DoctorRetainedSource
			if err = json.Unmarshal([]byte(view.Entry.OwnerID), &source); err != nil {
				t.Fatal(err)
			}
			retained, err := s.DB.ReadDoctorRetainedSource(ctx, approval.Target.String(), "handoff-reader", source)
			if err != nil {
				t.Fatal(err)
			}
			viewBytes := retained
			if source.Kind == "doctor_guidance" {
				viewBytes, err = renderRetainedGuidance(retained)
				if err != nil {
					t.Fatal(err)
				}
			}
			safe, err := s.Redactor.Apply(ctx, string(viewBytes), 131072)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(filepath.Join(doctorDir, view.File))
			if err != nil {
				t.Fatal(err)
			}
			if string(actual) != safe.String() {
				t.Fatalf("view differs from retained %s", source.Kind)
			}
			if source.Kind == "doctor_guidance" && source.ID == "retained-guidance" && string(retained) != string(rawCapture) {
				t.Fatal("guidance changed")
			}
			observed[source.Kind] = true
		}
		for _, kind := range []string{"doctor_guidance", "doctor_findings", "doctor_evidence", "doctor_command_receipt", "repair_operation", "repair_step"} {
			if !observed[kind] {
				t.Fatalf("retained %s was not recovered", kind)
			}
		}
		contents := map[string]string{}
		for name, body := range retainedTree(t, doctorDir) {
			contents["doctor/"+name] = body
		}
		return dm.Manifest, contents
	}
	firstDir := t.TempDir()
	dm, contents := prepare(firstDir)
	joined := ""
	for _, body := range contents {
		joined += body
	}
	for _, want := range []string{"repair_operation", "repair_step", "ObservedAfter", "doctor_guidance", "doctor_findings"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("handoff missing %q", want)
		}
	}
	if err = os.RemoveAll(firstDir); err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(s.Paths.DoctorScratchDir()); err != nil {
		t.Fatal(err)
	}
	path := s.DB.Path()
	if err = s.DB.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	s.DB = reopened
	for i := 0; i < 2; i++ {
		dm2, got := prepare(t.TempDir())
		if !reflect.DeepEqual(dm, dm2) || !reflect.DeepEqual(contents, got) {
			t.Fatal("restart/reuse changed retained identities or authorized content")
		}
	}
	afterSteps, err := s.DB.DoctorRepairSteps(ctx, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.DB.GetSchedule(ctx, schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if runner.runs != 1 || current.Status != "failed" || !reflect.DeepEqual(beforeSteps, afterSteps) {
		t.Fatal("recovery replayed or changed repair effects")
	}
}

func retainedTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[rel] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
