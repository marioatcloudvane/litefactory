package alerts

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// CommitCI atomically fences a complete source scan with its incident and
// notification handoff. No network operations occur inside this transaction.
func (e *Engine) CommitCI(ctx context.Context, generation int64, state store.CIObservation, observations []Observation) error {
	return e.DB.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
		if l.CI.Generation != generation {
			return fmt.Errorf("stale CI observation")
		}
		if l.CITargets == nil {
			l.CITargets = map[string]store.CIObservation{}
		}
		if l.CI.Target != "" {
			l.CITargets[l.CI.Target] = l.CI
		}
		if l.CI.Configuration != state.Configuration {
			for _, subject := range l.Subjects {
				if subject.Rule == "main_red" || subject.Rule == "ci_monitoring_unavailable" {
					subject.CleanCount = 0
					subject.RecoveryEvidence = ""
				}
			}
		}
		for _, o := range observations {
			id := SubjectID(o.Rule, o.Kind, o.Key)
			if s := l.Subjects[id]; s != nil && o.Condition == "clean" && s.RecoveryEvidence != "" && s.RecoveryEvidence != o.RecoveryEvidence {
				s.CleanCount = 0
			}
			if err := e.observe(l, o); err != nil {
				return err
			}
		}
		if s := l.Subjects[SubjectID("main_red", "ci", state.Target)]; s != nil && !s.Active {
			state.Failure = nil
		}
		state.Generation = generation + 1
		l.CI = state
		if state.Target != "" {
			l.CITargets[state.Target] = state
		}
		for _, rule := range []string{"main_red", "ci_monitoring_unavailable"} {
			o := l.Observers[rule]
			o.Ready = state.Ready
			o.Reason = state.Reason
			o.LastAttempt = state.LastAttempt
			o.LastSuccess = state.LastSuccess
			l.Observers[rule] = o
		}
		return nil
	})
}

// ResetCI invalidates in-flight observations and recovery streaks on restart;
// incidents, notification debt and accepted source identity remain retained.
func (e *Engine) ResetCI(ctx context.Context, at time.Time) error {
	return e.DB.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
		l.CI.Generation++
		l.CI.Ready = false
		l.CI.Reason = "startup_pending"
		l.CI.NextDue = at
		for _, s := range l.Subjects {
			if s.Rule == "main_red" || s.Rule == "ci_monitoring_unavailable" {
				s.CleanCount = 0
			}
		}
		for _, rule := range []string{"main_red", "ci_monitoring_unavailable"} {
			o := l.Observers[rule]
			o.Ready = false
			o.Reason = "startup_pending"
			l.Observers[rule] = o
		}
		return nil
	})
}
