package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/logging"
)

// Live global log file names whose rotated siblings are managed here.
const (
	globalDaemonLog = "daemon.log"
	globalLaunchOut = "launchd.out.log"
	globalLaunchErr = "launchd.err.log"
	logRotateAge    = 24 * time.Hour
)

var globalLogLiveNames = []string{globalDaemonLog, globalLaunchOut, globalLaunchErr}

// RotatedLogStamp reports whether name is exactly live + "." + YYYYMMDD-HHMMSS
// and returns the stamp text.
func RotatedLogStamp(live, name string) (string, bool) {
	stamp, ok := strings.CutPrefix(name, live+".")
	if !ok || len(stamp) != len(logging.RotationStampLayout) || stamp[8] != '-' {
		return "", false
	}
	for i := 0; i < len(stamp); i++ {
		if i != 8 && (stamp[i] < '0' || stamp[i] > '9') {
			return "", false
		}
	}
	return stamp, true
}

// ShouldRotateLog reports whether a live log file is due for rotation: it
// exists and is non-empty, and the newest rotated sibling named in siblings
// (stamp read from the name, UTC) is at least 24 hours old, or absent. Siblings
// whose stamp is not a real time are ignored.
func ShouldRotateLog(live string, liveSize int64, siblings []string, now time.Time) bool {
	if liveSize <= 0 {
		return false
	}
	var newest time.Time
	for _, name := range siblings {
		stamp, ok := RotatedLogStamp(live, name)
		if !ok {
			continue
		}
		t, err := time.ParseInLocation(logging.RotationStampLayout, stamp, time.UTC)
		if err != nil {
			continue
		}
		if t.After(newest) {
			newest = t
		}
	}
	return newest.IsZero() || !now.Before(newest.Add(logRotateAge))
}

// LogFile is one regular file at the top of the logs directory.
type LogFile struct {
	Name    string
	ModTime time.Time
}

// SelectRotatedLogsToDelete returns the rotated global logs to delete: names
// exactly daemon.log., launchd.out.log. or launchd.err.log. plus a stamp, whose
// mtime is at or before now minus days*24h. Live files never match.
func SelectRotatedLogsToDelete(files []LogFile, now time.Time, days int) []string {
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	var out []string
	for _, f := range files {
		for _, live := range globalLogLiveNames {
			if _, ok := RotatedLogStamp(live, f.Name); ok && !f.ModTime.After(cutoff) {
				out = append(out, f.Name)
				break
			}
		}
	}
	return out
}

// RotatedLog is one file produced by a rotation.
type RotatedLog struct {
	Name string
	Size int64
}

// globalLogReport carries the rotate step's outcome to the delete step, which
// writes the single per-run log line.
type globalLogReport struct {
	rotated  []RotatedLog
	failures int
}

// rotateGlobalLogs rotates daemon.log (when a sink is present) and the launchd
// capture files, each only when due. A failure on one file never stops the
// others.
func rotateGlobalLogs(logsDir string, sink LogRotator, now time.Time, report *globalLogReport) error {
	*report = globalLogReport{}
	root, err := os.OpenRoot(logsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open %s: %w", logsDir, err)
	}
	defer func() { _ = root.Close() }()
	files, _, err := listRegularFiles(root)
	if err != nil {
		return fmt.Errorf("read %s: %w", logsDir, err)
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Name
	}
	stamp := now.UTC().Format(logging.RotationStampLayout)
	var errs []error
	for _, live := range globalLogLiveNames {
		if live == globalDaemonLog && sink == nil {
			continue
		}
		info, err := root.Lstat(live)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if !ShouldRotateLog(live, info.Size(), names, now) {
			continue
		}
		dest := live + "." + stamp
		var rotateErr error
		size := info.Size()
		if live == globalDaemonLog {
			rotateErr = sink.Rotate(dest)
		} else {
			size, rotateErr = logging.CopyTruncate(root, live, dest)
		}
		if rotateErr != nil {
			report.failures++
			errs = append(errs, fmt.Errorf("rotate %s: %w", live, rotateErr))
			continue
		}
		if size > 0 {
			report.rotated = append(report.rotated, RotatedLog{Name: dest, Size: size})
		}
	}
	return errors.Join(errs...)
}

// deleteOldRotatedLogs deletes rotated global logs older than days, then writes
// the one log line for the whole run, including what rotateGlobalLogs did.
// Deletion failures are counted and retried at the next daily run.
func deleteOldRotatedLogs(ctx context.Context, log *slog.Logger, logsDir string, days int, now time.Time, report *globalLogReport) (err error) {
	var (
		deleted  []string
		freed    int64
		failures = report.failures
	)
	defer func() {
		rotated := make([]string, len(report.rotated))
		for i, r := range report.rotated {
			rotated[i] = fmt.Sprintf("%s (%d bytes)", r.Name, r.Size)
		}
		log.InfoContext(ctx, "global logs housekeeping",
			slog.Int("rotated", len(rotated)),
			slog.Any("rotated_files", rotated),
			slog.Int("deleted", len(deleted)),
			slog.Any("deleted_files", deleted),
			slog.Int64("bytes_freed", freed),
			slog.Int("failures", failures))
		*report = globalLogReport{}
	}()
	root, err := os.OpenRoot(logsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open %s: %w", logsDir, err)
	}
	defer func() { _ = root.Close() }()
	files, sizes, err := listRegularFiles(root)
	if err != nil {
		return fmt.Errorf("read %s: %w", logsDir, err)
	}
	entries := make([]LogFile, len(files))
	for i, f := range files {
		entries[i] = LogFile{Name: f.Name, ModTime: f.ModTime}
	}
	for _, name := range SelectRotatedLogsToDelete(entries, now, days) {
		if ctx.Err() != nil {
			break
		}
		if err := root.Remove(name); err != nil {
			failures++
			log.WarnContext(ctx, "rotated log delete failed", slog.String("file", name), slog.String("error", err.Error()))
			continue
		}
		deleted = append(deleted, name)
		freed += sizes[name]
	}
	return nil
}
