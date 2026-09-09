package codex

import (
	"os"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

// machineLocation reads the same OS timezone the child uses, independently of
// a daemon TZ override (the existing child allowlist intentionally omits TZ).
func machineLocation() *time.Location {
	data, err := os.ReadFile("/etc/localtime")
	if err != nil {
		return nil
	}
	loc, err := time.LoadLocationFromTZData("machine-local", data)
	if err != nil {
		return nil
	}
	return loc
}

func quotaInterruption(message string, started, observed time.Time, startLoc, endLoc *time.Location) *agent.Interruption {
	// This prefix is a provider-owned error diagnostic, never agent prose.
	if !strings.HasPrefix(message, "You've hit your usage limit.") {
		return nil
	}
	out := &agent.Interruption{Precision: agent.ResetMinute, Reason: "quota reset time is missing or ambiguous"}
	_, retry, ok := strings.Cut(message, "try again at ")
	if !ok || startLoc == nil || endLoc == nil || started.IsZero() || observed.Before(started) {
		return out
	}
	retry = strings.TrimSuffix(retry, ".")
	var clock time.Time
	dated := false
	for _, layout := range []string{"3:04 PM", "Jan 2, 2006 3:04 PM", "January 2, 2006 3:04 PM", "2006-01-02 3:04 PM", "3:04 PM on Jan 2, 2006", "3:04 PM on January 2, 2006"} {
		parsed, err := time.Parse(layout, retry)
		if err == nil {
			clock = parsed
			dated = layout != "3:04 PM"
			break
		}
	}
	if clock.IsZero() {
		return out
	}
	// Both observations must describe the same timezone rules. Check the whole
	// candidate neighborhood too, so a changed zone cannot silently move due time.
	for _, instant := range []time.Time{started, observed, observed.Add(-48 * time.Hour), observed.Add(48 * time.Hour)} {
		_, a := instant.In(startLoc).Zone()
		_, b := instant.In(endLoc).Zone()
		if a != b {
			out.Reason = "machine-local timezone changed during invocation"
			return out
		}
	}
	y, m, d := clock.Date()
	if !dated {
		y, m, d = observed.In(endLoc).Date()
		sy, sm, sd := started.In(startLoc).Date()
		if y != sy || m != sm || d != sd {
			out.Reason = "time-only reset spans a local date boundary"
			return out
		}
	}
	// Enumerate candidate offsets around the date, then round-trip each instant.
	// time.Date's normalization alone would silently pick a DST fold or gap.
	nominal := time.Date(y, m, d, clock.Hour(), clock.Minute(), 0, 0, time.UTC)
	offsets := make(map[int]bool)
	for h := -48; h <= 48; h++ {
		_, offset := nominal.Add(time.Duration(h) * time.Hour).In(endLoc).Zone()
		offsets[offset] = true
	}
	var candidate time.Time
	count := 0
	for offset := range offsets {
		instant := nominal.Add(-time.Duration(offset) * time.Second)
		local := instant.In(endLoc)
		ly, lm, ld := local.Date()
		if ly == y && lm == m && ld == d && local.Hour() == clock.Hour() && local.Minute() == clock.Minute() {
			candidate = instant
			count++
		}
	}
	if count != 1 || !candidate.After(observed) {
		return out
	}
	out.ResetBound = candidate.Add(time.Minute).UTC()
	out.DueAt = out.ResetBound.Add(time.Minute)
	out.Reason = ""
	return out
}
