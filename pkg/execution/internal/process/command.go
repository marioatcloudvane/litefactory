package process

import (
	"context"
	"os/exec"
)

// Command creates a native command for caller-owned pipe and cancellation configuration.
func Command(ctx context.Context, binary string, args ...string) *exec.Cmd {
	//nolint:gosec // Explicit caller-selected executable and native arguments; prompts use stdin.
	return exec.CommandContext(ctx, binary, args...)
}
