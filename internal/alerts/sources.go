package alerts

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// Disk uses the same physical probe and threshold as dispatch, while equality
// remains unknown for alert recovery. Probe errors never establish health.
func (e *Engine) Disk(ctx context.Context, at time.Time, free, threshold uint64, probeErr error) error {
	condition := "unknown"
	evidence := ""
	if threshold > 0 && probeErr == nil {
		condition = "neutral"
		if free < threshold {
			condition = "failing"
		} else if free > threshold {
			condition = "clean"
			evidence = fmt.Sprintf("disk:%d:%d", at.UnixNano(), free)
		}
	}
	if threshold == 0 {
		if err := e.Readiness(ctx, "disk_low", at, false, "disk_guard_disabled"); err != nil {
			return err
		}
		return nil
	}
	return e.Observe(ctx, Observation{Rule: "disk_low", Kind: "factory disk", Key: "daemon-volume", Condition: condition, Severity: "critical", At: at, ID: fmt.Sprintf("disk:%d", at.UnixNano()), RecoveryEvidence: evidence, EvidenceAt: at, Summary: "Free disk space is below the factory run threshold.", Action: "Inspect disk usage and free space before new runs can start."})
}

// Progress compares source-owned stage/run/result evidence; elapsed time and
// an empty incomplete read never count as recovery.
func (e *Engine) Progress(ctx context.Context, t store.AlertTaskSource, at time.Time) error {
	at = at.UTC()
	// Application result revisions have no event timestamp. Retain the first
	// successful observation of a changed source version as progress evidence.
	if err := e.DB.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
		key := "progress-source:" + t.Task
		previous, seen := l.Evidence[key]
		progress := t.ProgressAt
		if seen && previous.Source != t.Version {
			progress = at
		} else if seen && previous.At.After(progress) {
			progress = previous.At
		}
		l.Evidence[key] = store.AlertEvidence{ID: key, Source: t.Version, At: progress, Approximate: seen && previous.Source != t.Version}
		t.ProgressAt = progress
		return nil
	}); err != nil {
		return err
	}
	condition := "failing"
	if t.Waiting || at.Sub(t.ProgressAt) <= time.Duration(e.Settings.NoProgressMinutes)*time.Minute {
		condition = "clean"
	}
	evidenceAt := t.ProgressAt
	if t.Waiting {
		evidenceAt = at
	}
	return e.Observe(ctx, Observation{Rule: "no_progress", Kind: "task", Key: t.Task, TaskRef: t.Task, At: at, Condition: condition, Severity: "critical", ID: fmt.Sprintf("progress:%s:%d", t.Task, at.UnixNano()), RecoveryEvidence: fmt.Sprintf("%s:%s:%s:%t", t.Task, t.Version, t.ProgressAt.UTC().Format(time.RFC3339Nano), t.Waiting), EvidenceAt: evidenceAt, Summary: "A factory task has made no progress beyond the configured delay.", Action: "Inspect the task report and doctor evidence before deciding how work should continue."})
}

// Pool records only authoritative eligible saturated waits; no slot is taken.
func (e *Engine) Pool(ctx context.Context, at time.Time, waiting, complete bool) error {
	at = at.UTC()
	var since time.Time
	err := e.DB.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
		key := "pool-wait"
		v := l.Evidence[key]
		if waiting && complete {
			if v.At.IsZero() {
				v = store.AlertEvidence{ID: key, At: at}
				l.Evidence[key] = v
			}
			since = v.At
		} else if complete {
			delete(l.Evidence, key)
		}
		return nil
	})
	if err != nil {
		return err
	}
	condition := "unknown"
	if complete {
		condition = "neutral"
		if !waiting {
			condition = "clean"
		} else if at.Sub(since) > time.Duration(e.Settings.PoolWaitMinutes)*time.Minute {
			condition = "failing"
		}
	}
	return e.Observe(ctx, Observation{Rule: "pool_blocked", Kind: "shared pool", Key: "shared", At: at, Condition: condition, Severity: "critical", ID: fmt.Sprintf("pool:%d", at.UnixNano()), RecoveryEvidence: fmt.Sprintf("pool-progress:%d", at.UnixNano()), EvidenceAt: at, Summary: "Eligible work has waited while the shared worker pool is full beyond its configured delay.", Action: "Inspect running work and capacity in factory overview."})
}

// ProviderOutcome is normalized at a provider boundary, never inferred from
// model prose. Account is a nonsecret provider-owned identity, not a token.
type ProviderOutcome struct {
	ID, Provider, Account, Source, Class string
	At                                   time.Time
}

// Provider atomically normalizes one deduplicated terminal operation into policy.
func (e *Engine) Provider(ctx context.Context, o ProviderOutcome) error {
	o.At = o.At.UTC()
	var observations []Observation
	err := e.DB.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
		if _, ok := l.Evidence["provider-event:"+o.ID]; ok {
			return nil
		}
		l.Evidence["provider-event:"+o.ID] = store.AlertEvidence{ID: o.ID, Source: o.Source, Subject: o.Provider + ":" + o.Account, Class: o.Class, At: o.At}
		for _, class := range []string{"authentication", "quota", "refusal", "5xx"} {
			key := o.Provider + ":" + o.Account + ":" + class
			if class == "refusal" {
				key += ":" + o.Source
			}
			id := SubjectID("provider_failure", "provider", key)
			latestKey := "provider-latest:" + id
			if latest := l.Evidence[latestKey]; o.At.Before(latest.At) {
				continue
			}
			l.Evidence[latestKey] = store.AlertEvidence{At: o.At, ID: o.ID, Class: o.Class}
			s := l.Subjects[id]
			if s == nil {
				s = &store.AlertSubject{Rule: "provider_failure", Kind: "provider", Key: key, ProviderClass: class, Episodes: map[int64]*store.AlertEpisode{}}
				l.Subjects[id] = s
				if class == "refusal" {
					s.TaskRef = o.Source
				}
			}
			if o.Class == "success" {
				s.Consecutive = 0
				observations = append(observations, Observation{Rule: s.Rule, Kind: s.Kind, Key: s.Key, At: o.At, Condition: "clean", Severity: "critical", RecoveryEvidence: o.ID, EvidenceAt: o.At})
				continue
			}
			if o.Class != "success" {
				s.CleanCount = 0
				s.RecoveryEvidence = ""
			}
			if o.Class != class {
				s.Consecutive = 0
				continue
			}
			s.Consecutive++
			if class == "5xx" {
				var events []store.AlertEvidence
				for k, v := range l.Evidence {
					if strings.HasPrefix(k, "provider-event:") && v.Subject == o.Provider+":"+o.Account && !v.At.After(o.At) {
						events = append(events, v)
					}
				}
				sort.Slice(events, func(i, j int) bool { return events[i].At.After(events[j].At) })
				s.Consecutive = 0
				for _, ev := range events {
					if ev.Class != "5xx" || o.At.Sub(ev.At) > 15*time.Minute {
						break
					}
					s.Consecutive++
					if s.Consecutive == 3 {
						break
					}
				}
			}
			threshold := 1
			if class == "refusal" {
				threshold = 2
			}
			if class == "5xx" {
				threshold = 3
			}
			if s.Consecutive >= threshold {
				observations = append(observations, Observation{Rule: s.Rule, Kind: s.Kind, Key: s.Key, At: o.At, ID: "provider-failure:" + o.ID + ":" + class, Condition: "failing", Severity: "critical", Summary: "The " + o.Provider + " provider reported " + class + " failures.", Action: "Check the provider sign-in, account quota or service status; alerts do not retry work."})
			}
		}
		for _, v := range observations {
			if err := e.observe(l, v); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// ConfirmProviderSuccess re-observes a later same-source success. The source
// outcome itself remains deduplicated, while two clean scans can recover it.
func (e *Engine) ConfirmProviderSuccess(ctx context.Context, at time.Time) error {
	at = at.UTC()
	return e.DB.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
		for _, s := range l.Subjects {
			if s.Rule != "provider_failure" || !s.Active || s.RecoveryEvidence == "" {
				continue
			}
			latest := l.Evidence["provider-latest:"+SubjectID(s.Rule, s.Kind, s.Key)]
			if latest.Class != "success" {
				continue
			}
			ev, ok := l.Evidence["provider-event:"+s.RecoveryEvidence]
			if !ok {
				continue
			}
			if err := e.observe(l, Observation{Rule: s.Rule, Kind: s.Kind, Key: s.Key, At: at, Condition: "clean", Severity: s.Severity, RecoveryEvidence: s.RecoveryEvidence, EvidenceAt: ev.At}); err != nil {
				return err
			}
		}
		return nil
	})
}
