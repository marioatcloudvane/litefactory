package daemon

import (
	"context"
	"os"

	"github.com/cloudvane/litefactory/internal/store"
)

// testModeRole reads the base team's mode classifier, as a linked team would supply it.
func testModeRole(context.Context, store.Repo) ([]byte, error) {
	return os.ReadFile("../../teams/source/base-team/mode/mode-classifier.md")
}
