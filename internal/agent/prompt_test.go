package agent

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
)

func shippedRoleText(t *testing.T, role string) string {
	t.Helper()
	pkg, err := instructionpackage.New(t.Context(), config.NewPaths(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	capture, _, err := pkg.Capture(t.Context(), role)
	if err != nil {
		t.Fatal(err)
	}
	return string(capture.Instructions())
}
