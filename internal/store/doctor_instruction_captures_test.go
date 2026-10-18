package store

import (
	"bytes"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/instructionpackage"
)

func doctorCaptureFixture(t *testing.T) []byte {
	t.Helper()
	pkg, err := instructionpackage.Load(t.Context(), fstest.MapFS{
		"metadata.yaml":          {Data: []byte("schema: 1\nversion: test\n")},
		"catalogue.yaml":         {Data: []byte("- fixture-role\n")},
		"agents/fixture-role.md": {Data: []byte("Retained instruction body.\n")},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	capture, _, err := pkg.Capture(t.Context(), "fixture-role")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := capture.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDoctorInstructionCaptureImmutableFencedAndLegacy(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	const ref = "github:acme/api#406"
	if _, err := db.BeginDoctorAttempt(ctx, ref, doctorIOAttempt("captured")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DoctorInstructionCapture(ctx, "captured"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("legacy capture: %v", err)
	}
	raw := doctorCaptureFixture(t)
	if err := db.RetainDoctorInstructionCapture(ctx, "captured", raw); err != nil {
		t.Fatal(err)
	}
	if err := db.RetainDoctorInstructionCapture(ctx, "captured", raw); !errors.Is(err, ErrDoctorFenced) {
		t.Fatalf("second write: %v", err)
	}
	if err := db.StopDoctorConversation(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if err := db.RetainDoctorInstructionCapture(ctx, "captured", raw); !errors.Is(err, ErrDoctorFenced) {
		t.Fatalf("stopped write: %v", err)
	}
	retained, err := db.DoctorInstructionCapture(ctx, "captured")
	if err != nil || !bytes.Equal(raw, retained) {
		t.Fatalf("restore after stop: %v", err)
	}
	if _, err := db.BeginDoctorAttempt(ctx, ref, doctorIOAttempt("superseded")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.BeginDoctorAttempt(ctx, ref, doctorIOAttempt("active")); err != nil {
		t.Fatal(err)
	}
	if err := db.RetainDoctorInstructionCapture(ctx, "superseded", raw); !errors.Is(err, ErrDoctorFenced) {
		t.Fatalf("superseded write: %v", err)
	}
	if err := db.RetainDoctorInstructionCapture(ctx, "missing", raw); !errors.Is(err, ErrDoctorFenced) {
		t.Fatalf("missing attempt write: %v", err)
	}
}

func TestDoctorInstructionCaptureRejectsCorruption(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	if _, err := db.BeginDoctorAttempt(ctx, "github:acme/api#406", doctorIOAttempt("captured")); err != nil {
		t.Fatal(err)
	}
	raw := doctorCaptureFixture(t)
	broken := bytes.Replace(raw, []byte(`"schema":1`), []byte(`"schema":9`), 1)
	if err := db.RetainDoctorInstructionCapture(ctx, "captured", broken); err == nil {
		t.Fatal("accepted invalid capture")
	}
	if err := db.RetainDoctorInstructionCapture(ctx, "captured", raw); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE doctor_instruction_captures SET capture=? WHERE attempt_id='captured'`, broken); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DoctorInstructionCapture(ctx, "captured"); err == nil {
		t.Fatal("restored corrupt capture")
	}
}
