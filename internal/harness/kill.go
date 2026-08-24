package harness

import (
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Killing the process subtree (SD-1, AC-3.19).
//
// The obvious implementation — SysProcAttr{Setpgid: true} plus a single
// kill(-pgid) — was measured and does not work. claude's Bash tool setsids its
// shell into a new session and a new process group, so every grandchild escapes
// the group litefactory created: 2 of 2 survivors, reparented to PID 1, still
// holding the worktree open.
//
// What works is to treat the process *tree* as the unit: walk it, collect every
// distinct process group inside it, and signal each group. The walk and the
// kill are not atomic — a tool can spawn between them — so the sweep runs
// twice, and the second sweep escalates to SIGKILL.

const (
	// killScanTimeout bounds one pgrep invocation. This runs while a run is
	// already being torn down, so it must not itself become the thing that
	// hangs.
	killScanTimeout = 2 * time.Second

	// killGrace is how long the subtree is given to act on SIGTERM before the
	// second sweep escalates. It is short on purpose: by the time this runs the
	// run has already exceeded its wall clock or the daemon is shutting down.
	killGrace = 300 * time.Millisecond

	// maxProcessNodes bounds the tree walk. A fork bomb inside the worktree
	// must not turn the kill path into an unbounded one.
	maxProcessNodes = 4096

	// maxProcessDepth bounds how deep the walk descends, for the same reason.
	maxProcessDepth = 32
)

// pgrepPath is the process-tree enumerator. It is an absolute path because the
// child's PATH is not this process's PATH, and macOS always has it here.
const pgrepPath = "/usr/bin/pgrep"

// killSubtree terminates pid and every descendant it has, whatever process
// group each of them managed to escape into.
//
// It is best effort by design and always reports nil: it runs as cmd.Cancel,
// where a returned error would displace the exit status the caller needs, and
// there is no useful recovery from "the process we were killing had already
// gone". Everything that failed is logged.
//
// ctx must not be the run's own context — by the time this is called that
// context is cancelled, and the enumeration needs to run anyway. See the call
// site in [Runner.Run].
func killSubtree(ctx context.Context, pid int, logger *slog.Logger) error {
	if pid <= 1 {
		return nil
	}

	// Never signal our own process group. The child is Setpgid'd into its own
	// group so this cannot happen by construction, but a kill(-pgid) aimed at
	// the daemon's group would take down the daemon — and, under `go test`, the
	// test binary — so the guarantee is checked rather than assumed.
	self, err := syscall.Getpgid(0)
	if err != nil {
		self = 0
		logger.Warn("cannot determine own process group; subtree kill will be conservative",
			slog.String("error", err.Error()))
	}

	groups := processGroups(ctx, pid, logger)
	signalGroups(groups, syscall.SIGTERM, self, logger)

	// Re-scan once: a tool may have spawned between the walk and the signal,
	// and a process that ignored SIGTERM is still there to be found.
	time.Sleep(killGrace)
	for _, group := range processGroups(ctx, pid, logger) {
		if !slices.Contains(groups, group) {
			groups = append(groups, group)
		}
	}
	signalGroups(groups, syscall.SIGKILL, self, logger)

	// The direct child last, in case it managed to leave every group we saw.
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		logger.Warn("kill claude process failed",
			slog.Int("pid", pid), slog.String("error", err.Error()))
	}

	logger.Info("killed claude process subtree",
		slog.Int("pid", pid), slog.Int("process_groups", len(groups)))
	return nil
}

// signalGroups sends sig to each process group, skipping the ones it must never
// touch: the init group, the unspecified group, and our own.
func signalGroups(groups []int, sig syscall.Signal, self int, logger *slog.Logger) {
	for _, group := range groups {
		if group <= 1 || (self != 0 && group == self) {
			continue
		}
		// A negative pid signals the whole process group.
		if err := syscall.Kill(-group, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
			logger.Warn("signalling process group failed",
				slog.Int("pgid", group), slog.String("signal", sig.String()),
				slog.String("error", err.Error()))
		}
	}
}

// processGroups returns every distinct process group id found in the subtree
// rooted at pid, including pid's own.
func processGroups(ctx context.Context, pid int, logger *slog.Logger) []int {
	var groups []int
	add := func(p int) {
		group, err := syscall.Getpgid(p)
		if err != nil {
			// The process exited between being listed and being inspected,
			// which is the common case during a teardown.
			return
		}
		if !slices.Contains(groups, group) {
			groups = append(groups, group)
		}
	}

	add(pid)
	for _, descendant := range descendants(ctx, pid, logger) {
		add(descendant)
	}
	return groups
}

// descendants walks the process tree below pid, breadth first.
func descendants(ctx context.Context, pid int, logger *slog.Logger) []int {
	var found []int
	frontier := []int{pid}

	for depth := 0; depth < maxProcessDepth && len(frontier) > 0 && len(found) < maxProcessNodes; depth++ {
		var next []int
		for _, parent := range frontier {
			for _, child := range children(ctx, parent, logger) {
				if child <= 1 || child == pid || slices.Contains(found, child) {
					continue
				}
				found = append(found, child)
				next = append(next, child)
				if len(found) >= maxProcessNodes {
					logger.Warn("process subtree walk truncated",
						slog.Int("pid", pid), slog.Int("limit", maxProcessNodes))
					return found
				}
			}
		}
		frontier = next
	}
	return found
}

// children lists the direct children of parent.
//
// pgrep exits 1 when nothing matched, which is the ordinary answer for a leaf
// process and not a failure worth logging.
func children(ctx context.Context, parent int, logger *slog.Logger) []int {
	scanCtx, cancel := context.WithTimeout(ctx, killScanTimeout)
	defer cancel()

	// #nosec G204 -- constant binary path; the only argument is a formatted int.
	cmd := exec.CommandContext(scanCtx, pgrepPath, "-P", strconv.Itoa(parent))
	// An explicit empty environment: nil would mean os.Environ(), and this
	// package hands no process the daemon's environment.
	cmd.Env = []string{}
	cmd.Stdin = nil

	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		if !isExitCode(err, 1) {
			logger.Warn("listing child processes failed",
				slog.Int("ppid", parent), slog.String("error", err.Error()))
		}
		return nil
	}

	var pids []int
	for _, field := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(field); err == nil && pid > 1 {
			pids = append(pids, pid)
		}
	}
	return pids
}

// isExitCode reports whether err is a process exit with the given status.
func isExitCode(err error, code int) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == code
}
