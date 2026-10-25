package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

var errManagedPreparationAssessment = errors.New("managed request requires a preparation assessment")

const pendingPreparationRefinement = "Pending planning refinements remain pending and are excluded from implementation scope."
const assessmentPreparation = "Managed scope is not approved and frozen; this request is limited to preparation and reading."

func retainPreparationCaveats(ctx context.Context, opts Options, req Request, run *store.Run, in *harness.PromptInput) error {
	in.PreparationAssessment = req.preparationAssessment
	var caveats []string
	pending, err := opts.DB.ListPendingStoryRefinements(ctx, req.SessionID)
	if err != nil {
		return err
	}
	if req.mode() == "plan" {
		pending = slices.DeleteFunc(pending, func(r store.StoryRefinement) bool { return r.CommentID == req.Trigger.CommentID })
	}
	if len(pending) == 0 {
		if worker, lookupErr := opts.DB.ImplementStoryByWorkerSession(ctx, req.SessionID); lookupErr == nil {
			parent, parentErr := opts.DB.GetSessionByTaskRef(ctx, worker.Parent)
			if parentErr == nil {
				pending, err = opts.DB.ListPendingStoryRefinements(ctx, parent.ID)
				if err != nil {
					return err
				}
			} else if !errors.Is(parentErr, store.ErrNotFound) {
				return parentErr
			}
		} else if !errors.Is(lookupErr, store.ErrNotFound) {
			return lookupErr
		}
	}
	if len(pending) > 0 {
		caveats = append(caveats, pendingPreparationRefinement)
	}
	if req.preparationAssessment {
		caveats = append(caveats, assessmentPreparation)
	}
	if in.PlanContextUnavailable != "" {
		caveats = append(caveats, in.PlanContextUnavailable)
	}
	if len(caveats) > 0 {
		*run, err = opts.DB.RecordPreparationCaveats(ctx, run.ID, caveats)
		if err != nil {
			return fmt.Errorf("retain preparation conditions: %w", err)
		}
	}
	current, err := opts.DB.GetRun(ctx, run.ID)
	if err != nil {
		return err
	}
	*run = current
	in.PreparationCaveats = append([]string(nil), run.PreparationCaveats...)
	for _, s := range run.SyncObservations {
		if s.Outcome == "blocked" || s.Outcome == "deferred_dirty" || (s.Outcome == "skipped" && s.Behind > 0) {
			in.PreparationCaveats = append(in.PreparationCaveats, fmt.Sprintf("%s synchronization deferred (%d behind): %s", s.Phase, s.Behind, s.Reason))
		}
	}
	if req.preparationAssessment {
		in.History = append(in.History, harness.HistoryEntry{Role: harness.RoleOldThirdParty, Body: assessmentPreparation + " Continue the original request with a reading assessment only. Do not implement proposed refinements, publish scope or save changes."})
	}
	return nil
}

func (p *preparation) reqAssessment() bool {
	for _, c := range p.run.PreparationCaveats {
		if c == assessmentPreparation {
			return true
		}
	}
	return false
}

// graphPreparationCaveat projects retained evidence, including on failed agent
// attempts. A later observation of the same branch pair replaces older freshness.
func graphPreparationCaveat(ctx context.Context, db *store.DB, instanceID int64) (string, error) {
	attempts, err := db.ListGraphNodeAttempts(ctx, instanceID)
	if err != nil {
		return "", err
	}
	var caveats []string
	instance, err := db.GetGraphInstance(ctx, instanceID)
	if err != nil {
		return "", err
	}
	session, err := db.GetSession(ctx, instance.SessionID)
	if err != nil {
		return "", err
	}
	definition, err := db.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return "", err
	}
	if definition.Mode == "implement" {
		pending, err := db.ListPendingStoryRefinements(ctx, instance.SessionID)
		if err != nil {
			return "", err
		}
		if len(pending) > 0 {
			caveats = append(caveats, pendingPreparationRefinement)
		}
	}
	var syncs []*report.Sync
	var keys []string
	var seenRuns []int64
	for _, a := range attempts {
		if a.RunID != 0 && !slices.Contains(seenRuns, a.RunID) {
			seenRuns = append(seenRuns, a.RunID)
			run, err := db.GetRun(ctx, a.RunID)
			if err != nil {
				return "", err
			}
			for _, c := range run.PreparationCaveats {
				if !slices.Contains(caveats, c) && len(caveats) < 16 {
					caveats = append(caveats, c)
				}
			}
			for _, s := range run.SyncObservations {
				key := s.Phase + ":" + s.Ref
				if s.Phase == "base" {
					ref := s.Ref
					if _, suffix, ok := strings.Cut(ref, "/"); ok {
						ref = suffix
					}
					key = "base:" + session.Branch + ":" + ref
				}
				value := &report.Sync{Phase: s.Phase, Base: s.Ref, Outcome: report.SyncOutcome(s.Outcome), Behind: s.Behind, Ahead: s.Ahead, Reason: s.Reason}
				i := slices.Index(keys, key)
				if i < 0 && len(keys) < 16 {
					keys = append(keys, key)
					syncs = append(syncs, value)
				} else if i >= 0 {
					syncs[i] = value
				}
			}
		}
		if a.Outcome != "deferred" && a.Outcome != "synced" && a.Outcome != "current" && a.Outcome != "conflict" && a.Outcome != "retry" {
			continue
		}
		outputs, err := db.ListGraphResultOutputsBounded(ctx, a.ID, 1<<20, 16)
		if err != nil {
			return "", err
		}
		for _, output := range outputs {
			if output.Kind != store.GraphOutputJSON {
				continue
			}
			var out syncBaseOutput
			if json.Unmarshal(output.Payload, &out) != nil || out.Working == "" || out.Base == "" || out.Result == "" {
				continue
			}
			if out.Result != "deferred" && out.Result != "synced" && out.Result != "current" {
				continue
			}
			key := "base:" + out.Working + ":" + out.Base
			value := &report.Sync{Phase: "base", Base: out.Base, Outcome: report.SyncUpToDate, CountsUnavailable: true}
			if out.Result == "deferred" {
				value.Outcome = report.SyncBlocked
				value.Reason = out.Reason
			}
			i := slices.Index(keys, key)
			if i < 0 && len(keys) < 16 {
				keys = append(keys, key)
				syncs = append(syncs, value)
			} else if i >= 0 {
				syncs[i] = value
			}
		}
	}
	return report.PreparationCaveat(caveats, syncs...), nil
}

// preparationObservationError preserves known observations across an unrelated
// preparation failure without changing the error's retry or safety boundary.
type preparationObservationError struct {
	cause error
	sync  syncResult
}

func (e *preparationObservationError) Error() string { return e.cause.Error() }
func (e *preparationObservationError) Unwrap() error { return e.cause }
