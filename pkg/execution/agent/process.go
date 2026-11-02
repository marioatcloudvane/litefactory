package agent

import (
	"context"
	"log/slog"
	"os/exec"
	"time"

	"github.com/cloudvane/litefactory/pkg/execution/internal/process"
)

// FailureObserver receives native failure evidence. The caller owns redaction
// and persistence; the shared lifecycle does not grant permissions.
type FailureObserver = process.FailureObserver

// OversizedObserver reports dropped judge records after bounded transport completes.
type OversizedObserver = process.OversizedObserver

// Lines assembles bounded newline records and preserves the judge discard policy.
type Lines = process.Lines

// ErrProviderExit marks an observed nonzero provider exit.
var ErrProviderExit = process.ErrProviderExit

// ErrProbeTransient marks a locally retryable capability probe failure.
var ErrProbeTransient = process.ErrProbeTransient

// NewLines builds the bounded line writer used by owned conversation transport.
func NewLines(observe func([]byte), cancel context.CancelFunc) *Lines {
	return process.NewLines(observe, cancel)
}

// RunProcess executes prepared bounded line transport without granting permissions.
func RunProcess(ctx context.Context, binary string, args, env []string, prompt, directory string, timeout time.Duration, observe func([]byte), failure FailureObserver, cancelProcess func(int) error, judge bool, oversized OversizedObserver) error {
	return process.Run(ctx, binary, args, env, prompt, directory, timeout, observe, failure, cancelProcess, judge, oversized)
}

// Start retries only a demonstrably absent executable; each attempt must authorize its own launch.
func Start(ctx context.Context, binary, directory string, attempt func() error) error {
	return process.Start(ctx, binary, directory, attempt)
}

// KillSubtree cancels only the supplied owned PID and its descendants.
func KillSubtree(ctx context.Context, pid int, logger *slog.Logger) error {
	return process.KillSubtree(ctx, pid, logger)
}

// CancelConversationTree verifies and cancels descendants of an owned conversation PID.
func CancelConversationTree(ctx context.Context, pid int) error {
	return process.CancelConversationTree(ctx, pid)
}

// IsolatedEnvironment retains only the existing private-home environment allowlist.
func IsolatedEnvironment(parent []string, home string) []string {
	return process.DoctorEnvironment(parent, home)
}

// Probe performs a bounded local native capability probe without a model invocation.
func Probe(ctx context.Context, binary string, args, env []string, directory string) (string, error) {
	return process.ConversationProbe(ctx, binary, args, env, directory)
}

// Command creates a process with explicit caller-owned argv. Provider adapters
// finish configuring pipes, ownership barriers and cancellation before Start.
func Command(ctx context.Context, binary string, args ...string) *exec.Cmd {
	return process.Command(ctx, binary, args...)
}

// OwnedOptions exposes only the process coordination seam; the caller owns the
// lock, durable PID acknowledgment and cleanup uncertainty record.
type OwnedOptions = process.OwnedOptions

// RunOwnedProcess runs behind an inherited lock and PID acknowledgment barrier.
func RunOwnedProcess(ctx context.Context, binary string, args, env []string, options OwnedOptions, observe func([]byte)) (string, bool, error) {
	return process.RunOwned(ctx, binary, args, env, options, observe)
}

// OutputPolicy selects line and total byte limits without changing provider policy.
type OutputPolicy = process.OutputPolicy

// NewLinesWithPolicy creates bounded output with caller-selected limits.
func NewLinesWithPolicy(observe func([]byte), cancel context.CancelFunc, policy OutputPolicy) *Lines {
	return process.NewLinesWithPolicy(observe, cancel, policy)
}
