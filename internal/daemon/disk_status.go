package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/diskstatus"
)

// diskObservation is independent of the run hold message and is never read by
// admission. Its mutex covers small value copies, never filesystem operations.
type diskObservation struct {
	mu    sync.Mutex
	value *diskstatus.Observation
}

func (d *Daemon) observeDisk(ctx context.Context, at time.Time) bool {
	if d.diskPath == "" {
		return false
	}
	read := d.freeDiskBytes
	if read == nil {
		read = bugchecks.FreeDiskBytes
	}
	o := &diskstatus.Observation{Version: 1, ObservedAt: at, GuardedPath: d.diskPath, GuardEnabled: d.minFreeDiskBytes > 0, StopBytes: d.minFreeDiskBytes, ProbeState: "available", AutomaticCleanup: "unavailable"}
	free, err := read(d.diskPath)
	if err != nil {
		o.ProbeState = "unknown"
		o.ProbeError = err.Error()
		d.log.WarnContext(ctx, "free disk space unreadable; dispatch continues", slog.String("path", d.diskPath), slog.Any("error", err))
	} else {
		o.FreeBytes = &free
		o.Held = o.GuardEnabled && free < o.StopBytes
	}
	d.diskObservation.mu.Lock()
	d.diskObservation.value = o
	d.diskObservation.mu.Unlock()
	var loggedFree any
	if o.FreeBytes != nil {
		loggedFree = *o.FreeBytes
	}
	d.log.DebugContext(ctx, "disk observation", slog.String("path", o.GuardedPath), slog.Any("free_bytes", loggedFree), slog.Uint64("stop_bytes", o.StopBytes), slog.Bool("guard_enabled", o.GuardEnabled), slog.Bool("actual_hold", o.Held), slog.String("automatic_cleanup", "unavailable"))
	return o.Held
}

func (d *Daemon) diskStatus() *diskstatus.Observation {
	d.diskObservation.mu.Lock()
	var o *diskstatus.Observation
	if d.diskObservation.value != nil {
		value := *d.diskObservation.value
		o = &value
	}
	d.diskObservation.mu.Unlock()
	if o == nil {
		if d.diskPath == "" && d.cacheSampler == nil {
			return nil
		}
		o = &diskstatus.Observation{Version: 1, GuardedPath: d.diskPath, GuardEnabled: d.minFreeDiskBytes > 0, StopBytes: d.minFreeDiskBytes, ProbeState: "unavailable", AutomaticCleanup: "unavailable"}
	}
	if d.cacheSampler != nil {
		snapshots := d.cacheSampler.Snapshot()
		o.Main = cacheStatus(snapshots["main"], d.diskPath)
		o.Reviewer = cacheStatus(snapshots["reviewer"], d.diskPath)
	}
	return o
}

func cacheStatus(c cacheSizeObservation, guarded string) diskstatus.Cache {
	return diskstatus.Cache{Path: c.Path, Filesystem: cacheFilesystemRelation(guarded, c.Path), State: c.State, PartialBytes: c.PartialBytes, EntriesExamined: c.EntriesExamined, PassStartedAt: c.PassStartedAt, ProgressAt: c.ProgressAt, CompleteStale: c.CompleteStale, CompleteBytes: c.CompleteBytes, CompleteStartedAt: c.CompleteStartedAt, CompleteAt: c.CompleteAt, Error: c.Error}
}
func cacheFilesystemRelation(guarded, path string) string {
	a, e := os.Stat(guarded)
	if e != nil {
		return "unknown"
	}
	b, e := os.Stat(path)
	if e != nil {
		return "unknown"
	}
	ad, ok := cacheDevice(a)
	bd, bok := cacheDevice(b)
	if !ok || !bok {
		return "unknown"
	}
	if ad == bd {
		return "same"
	}
	return "other"
}
func diskSignature(o *diskstatus.Observation) string { b, _ := json.Marshal(o); return string(b) }

// restoreCacheObservations preserves original sample times across restart. Only
// observations are restored; a new sampler always begins with a new cursor.
func (d *Daemon) restoreCacheObservations(ctx context.Context) {
	if d.cacheSampler == nil {
		return
	}
	previous, err := d.db.ReadHealthSnapshot(ctx)
	if err != nil {
		d.log.WarnContext(ctx, "cache observations unavailable after restart", slog.Any("error", err))
		return
	}
	if previous.Listener.Disk == nil {
		return
	}
	saved := previous.Listener.Disk
	values := make(map[string]cacheSizeObservation, 2)
	for _, v := range []struct {
		role  string
		cache diskstatus.Cache
	}{{"main", saved.Main}, {"reviewer", saved.Reviewer}} {
		c := v.cache
		values[v.role] = cacheSizeObservation{Path: c.Path, State: c.State, PartialBytes: c.PartialBytes, EntriesExamined: c.EntriesExamined, PassStartedAt: c.PassStartedAt, ProgressAt: c.ProgressAt, CompleteBytes: c.CompleteBytes, CompleteStartedAt: c.CompleteStartedAt, CompleteAt: c.CompleteAt, Error: c.Error}
	}
	d.cacheSampler.Restore(values)
}

func (d *Daemon) clearDiskHold() {
	d.diskObservation.mu.Lock()
	defer d.diskObservation.mu.Unlock()
	if d.diskObservation.value != nil {
		value := *d.diskObservation.value
		value.Held = false
		d.diskObservation.value = &value
	}
}
