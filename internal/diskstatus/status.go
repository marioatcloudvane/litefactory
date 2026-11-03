// Package diskstatus carries display-only disk and Go cache observations.
// Observations never authorize admission or deletion.
package diskstatus

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Observation is a versioned, optional health projection. A nil free or complete
// byte count means unavailable, while a pointer to zero is a measured empty value.
type Observation struct {
	Version          int       `json:"version"`
	ObservedAt       time.Time `json:"observed_at"`
	GuardedPath      string    `json:"guarded_path"`
	GuardEnabled     bool      `json:"guard_enabled"`
	StopBytes        uint64    `json:"stop_bytes"`
	FreeBytes        *uint64   `json:"free_bytes"`
	ProbeState       string    `json:"probe_state"`
	ProbeError       string    `json:"probe_error,omitempty"`
	Held             bool      `json:"held"`
	Main             Cache     `json:"main"`
	Reviewer         Cache     `json:"reviewer"`
	AutomaticCleanup string    `json:"automatic_cleanup"`
}

// Cache preserves current pass progress separately from the last complete pass.
type Cache struct {
	Path              string    `json:"path"`
	Filesystem        string    `json:"filesystem"`
	State             string    `json:"state"`
	PartialBytes      int64     `json:"partial_bytes"`
	EntriesExamined   int64     `json:"entries_examined"`
	PassStartedAt     time.Time `json:"pass_started_at"`
	ProgressAt        time.Time `json:"progress_at"`
	CompleteStale     bool      `json:"complete_stale"`
	CompleteBytes     *int64    `json:"complete_bytes"`
	CompleteStartedAt time.Time `json:"complete_started_at"`
	CompleteAt        time.Time `json:"complete_at"`
	Error             string    `json:"error,omitempty"`
}

// Decode accepts only known versions and bounded observations. An absent or
// unsupported older snapshot is unavailable rather than a fabricated zero.
func Decode(raw []byte) (*Observation, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > 65536 {
		return nil, fmt.Errorf("disk observation exceeds 64 KiB")
	}
	var o Observation
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, err
	}
	if o.Version != 1 {
		return nil, nil
	}
	return &o, nil
}

// Text is shared by existing status consumers so partial observations retain
// their timestamps and labels everywhere. now controls freshness, not sampling.
func Text(o *Observation, now time.Time) string {
	if o == nil {
		return "Disk/cache observations unavailable; automatic_cleanup=unavailable.\n"
	}
	var b strings.Builder
	guard := "enabled"
	if !o.GuardEnabled {
		guard = "disabled (display only)"
	}
	free := "unavailable"
	if o.FreeBytes != nil {
		free = fmt.Sprintf("%.2f GiB", float64(*o.FreeBytes)/(1<<30))
	}
	fmt.Fprintf(&b, "Disk: %s; free=%s; stop=%.2f GiB; guard=%s; actual_hold=%t; probe=%s; observed=%s%s", o.GuardedPath, free, float64(o.StopBytes)/(1<<30), guard, o.Held, o.ProbeState, stamp(o.ObservedAt), freshness(o.ObservedAt, now))
	if o.ProbeError != "" {
		fmt.Fprintf(&b, "; error=%s", o.ProbeError)
	}
	b.WriteByte('\n')
	for _, v := range []struct {
		role  string
		cache Cache
	}{{"main", o.Main}, {"reviewer", o.Reviewer}} {
		c := v.cache
		fmt.Fprintf(&b, "Go cache %s: %s; filesystem=%s; size_state=%s", v.role, c.Path, c.Filesystem, c.State)
		if !c.PassStartedAt.IsZero() {
			fmt.Fprintf(&b, "; observed partial logical bytes=%d; entries=%d; pass_started=%s; progress=%s%s", c.PartialBytes, c.EntriesExamined, stamp(c.PassStartedAt), stamp(c.ProgressAt), freshness(c.ProgressAt, now))
		}
		if c.CompleteBytes != nil {
			fmt.Fprintf(&b, "; last complete approximate logical bytes=%d; interval=%s..%s%s", *c.CompleteBytes, stamp(c.CompleteStartedAt), stamp(c.CompleteAt), freshness(c.CompleteAt, now))
			if c.CompleteStale {
				b.WriteString(" (stale/interrupted)")
			}
		} else {
			b.WriteString("; complete size unavailable")
		}
		if c.Error != "" {
			fmt.Fprintf(&b, "; error=%s", c.Error)
		}
		b.WriteByte('\n')
	}
	b.WriteString("automatic_cleanup=unavailable\n")
	return b.String()
}
func stamp(at time.Time) string {
	if at.IsZero() {
		return "unavailable"
	}
	return at.UTC().Format(time.RFC3339Nano)
}
func freshness(at, now time.Time) string {
	if at.IsZero() {
		return " (unavailable)"
	}
	if now.Sub(at) > 10*time.Minute {
		return " (stale)"
	}
	return ""
}
