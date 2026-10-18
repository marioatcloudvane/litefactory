package doctor

import (
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"testing"
)

func testDoctorCapture(t *testing.T, role string) *instructionpackage.RoleCapture {
	t.Helper()
	pkg, err := instructionpackage.New(t.Context(), config.NewPaths(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	captured, _, err := pkg.Capture(t.Context(), role)
	if err != nil {
		t.Fatal(err)
	}
	return captured
}
func testDoctorGuide(t *testing.T) string {
	t.Helper()
	return string(testDoctorCapture(t, "doctor-diagnosis").SkillInstructions("doctor-field-guide"))
}
