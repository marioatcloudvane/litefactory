package harness

import (
	"context"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/providerprocess"
)

// killSubtree terminates pid and every descendant it has, whatever process
// group each of them managed to escape into (SD-1, AC-3.19). The mechanism is
// shared with the codex runner and lives in [providerprocess.KillSubtree].
//
// ctx must not be the run's own context — by the time this is called that
// context is cancelled, and the enumeration needs to run anyway. See the call
// site in [Runner.Run].
func killSubtree(ctx context.Context, pid int, logger *slog.Logger) error {
	return providerprocess.KillSubtree(ctx, pid, logger)
}
