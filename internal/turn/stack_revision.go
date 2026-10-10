package turn

import (
	"bytes"
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/git"
)

// A fresh graph admission cannot launch with configuration changed by the
// subsequent fetch or base synchronization. Retained attempts keep their inputs.
func validateFreshStackRevision(ctx context.Context, clone *git.Clone, req Request, revision string) error {
	d := req.researchDefinition
	if d == nil || d.StackProfile == nil || !d.StackFresh {
		return nil
	}
	raw, err := clone.ReadStackOverrides(ctx, revision)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, d.StackProfile.OverrideBytes) {
		return errors.New("stack.overrides: repository configuration changed after admission; fresh admission is required")
	}
	return nil
}
