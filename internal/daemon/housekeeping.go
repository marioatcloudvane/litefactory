package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// HousekeepingInterval is how often the housekeeping steps run. The due time is
// held in memory only, so the first tick after a start always runs them.
const HousekeepingInterval = 24 * time.Hour

// HousekeepingStep is one local maintenance task. It touches neither GitHub nor
// the database. A step reports its own outcome in its own log line; a returned
// error is logged by the runner and never fails the tick or blocks later steps.
type HousekeepingStep struct {
	Name string
	Run  func(ctx context.Context, now time.Time) error
}

// Housekeeping runs registered steps in registration order, once per
// HousekeepingInterval.
type Housekeeping struct {
	log *slog.Logger

	mu      sync.Mutex
	steps   []HousekeepingStep
	nextDue time.Time
}

// NewHousekeeping returns an empty runner whose next run is due immediately.
func NewHousekeeping(log *slog.Logger) *Housekeeping {
	return &Housekeeping{log: log}
}

// Register appends a step; steps run in the order they were registered.
func (h *Housekeeping) Register(step HousekeepingStep) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.steps = append(h.steps, step)
}

// Run executes every step when due and then schedules the next run a day after
// now. Steps are skipped when ctx is already done. Failures are logged; they
// are retried at the next daily run, never earlier.
func (h *Housekeeping) Run(ctx context.Context, now time.Time) {
	h.mu.Lock()
	if len(h.steps) == 0 || now.Before(h.nextDue) || ctx.Err() != nil {
		h.mu.Unlock()
		return
	}
	steps := append([]HousekeepingStep(nil), h.steps...)
	h.nextDue = now.Add(HousekeepingInterval)
	h.mu.Unlock()

	for _, step := range steps {
		if ctx.Err() != nil {
			return
		}
		if err := step.Run(ctx, now); err != nil {
			h.log.WarnContext(ctx, "housekeeping step failed", slog.String("step", step.Name), slog.String("error", fmt.Sprint(err)))
		}
	}
}

// HousekeepingOptions configures the default housekeeping steps. A step whose
// inputs are unset is not registered.
type HousekeepingOptions struct {
	// DBPath locates the database; its directory holds the deploy backups.
	DBPath string
	// DeployBackupKeep is how many newest deploy backups to keep (settings.deploy_backup_keep).
	DeployBackupKeep int
	// LogsDir is the global logs directory (config.Paths.LogsDir).
	LogsDir string
	// LogRetainDays is settings.log_retain_days.
	LogRetainDays int
	// LogSink rotates daemon.log; nil skips daemon.log rotation.
	LogSink LogRotator
}

// LogRotator is the daemon.log sink's rotation seam (logging.FileSink).
type LogRotator interface {
	Rotate(name string) error
}

// newHousekeeping builds the runner with the default steps in their fixed
// order: prune backups, rotate global logs, delete old rotated logs.
func newHousekeeping(opts HousekeepingOptions, log *slog.Logger) *Housekeeping {
	h := NewHousekeeping(log)
	if opts.DBPath != "" && opts.DeployBackupKeep > 0 {
		h.Register(HousekeepingStep{Name: "prune deploy backups", Run: func(ctx context.Context, now time.Time) error {
			return pruneDeployBackups(ctx, log, opts.DBPath, opts.DeployBackupKeep)
		}})
	}
	if opts.LogsDir != "" && opts.LogRetainDays > 0 {
		report := &globalLogReport{}
		h.Register(HousekeepingStep{Name: "rotate global logs", Run: func(ctx context.Context, now time.Time) error {
			return rotateGlobalLogs(opts.LogsDir, opts.LogSink, now, report)
		}})
		h.Register(HousekeepingStep{Name: "delete old rotated logs", Run: func(ctx context.Context, now time.Time) error {
			return deleteOldRotatedLogs(ctx, log, opts.LogsDir, opts.LogRetainDays, now, report)
		}})
	}
	return h
}

// RunHousekeeping runs the daily housekeeping steps when due. It has no error
// result: a step can never fail a tick.
func (d *Daemon) RunHousekeeping(ctx context.Context, now time.Time) {
	if d.housekeeping == nil {
		return
	}
	d.housekeeping.Run(ctx, now)
}
