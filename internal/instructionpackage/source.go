package instructionpackage

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/config"
)

// CaptureSource is the invocation boundary for selecting owned role bytes.
type CaptureSource interface {
	Capture(context.Context, string) (*RoleCapture, []Diagnostic, error)
}

// CaptureRole resolves the shared package when no fixture source is supplied.
// It reports only structured fallback metadata, never instruction content.
// Stateless callers retain the returned capture and composed prompt in memory
// until the invocation finishes; they do not need durable replay storage.
func CaptureRole(ctx context.Context, source CaptureSource, paths config.Paths, logger *slog.Logger, role string) (*RoleCapture, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if source == nil {
		p, err := New(ctx, paths)
		if err != nil {
			return nil, fmt.Errorf("load role instructions: %w", err)
		}
		source = p
	}
	capture, diagnostics, err := source.Capture(ctx, role)
	if err != nil {
		return nil, fmt.Errorf("capture %s instructions: %w", role, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if capture == nil || len(capture.Instructions()) == 0 {
		return nil, fmt.Errorf("capture %s instructions: empty capture", role)
	}
	for _, diagnostic := range diagnostics {
		if logger != nil {
			logger.WarnContext(ctx, "instruction package fallback", "role", diagnostic.Role, "file", diagnostic.File, "category", diagnostic.Category, "selected_source", diagnostic.SelectedSource)
		}
	}
	return capture, nil
}
