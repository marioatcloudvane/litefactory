package analystschedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/drift"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/prevention"
	"github.com/cloudvane/litefactory/internal/store"
)

const leaseDuration = 30 * time.Minute

// Reserve recovers old receipts first and reserves at most one due occurrence.
func (s *Service) Reserve(ctx context.Context, now time.Time) (store.AnalystOccurrence, bool, error) {
	return s.ReserveEligible(ctx, now, nil)
}

// ReserveEligible skips held repositories within a bounded oldest-first page.
// The host predicate can only narrow admission; it cannot grant analyst access.
func (s *Service) ReserveEligible(ctx context.Context, now time.Time, eligible func(int64) bool) (store.AnalystOccurrence, bool, error) {
	if e := s.Recover(ctx, now); e != nil {
		return store.AnalystOccurrence{}, false, e
	}
	allowed := func(id int64) bool { return eligible == nil || eligible(id) }
	active, e := s.DB.ListActiveAnalystOccurrences(ctx, 100)
	if e != nil {
		return store.AnalystOccurrence{}, false, e
	}
	for _, old := range active {
		if old.State == "reserved" && !old.LeaseUntil.After(now) && allowed(old.RepositoryID) {
			o, e := s.DB.RecoverAnalystOccurrence(ctx, old.ID, now, leaseDuration)
			return o, e == nil, e
		}
	}
	due, e := s.DB.DueAnalystSchedules(ctx, now, 100)
	if e != nil {
		return store.AnalystOccurrence{}, false, e
	}
	for _, a := range due {
		if !allowed(a.RepositoryID) {
			continue
		}
		o, e := s.DB.ReserveAnalystOccurrence(ctx, a.ID, a.Version, now, leaseDuration)
		if errors.Is(e, store.ErrAnalystScheduleConflict) {
			continue
		}
		return o, e == nil, e
	}
	return store.AnalystOccurrence{}, false, nil
}

// Run prepares one saved occurrence and invokes only its chosen upstream analyst.
func (s *Service) Run(ctx context.Context, o store.AnalystOccurrence, now time.Time) error {
	var c Config
	if e := json.Unmarshal(o.Config, &c); e != nil {
		return e
	}
	repo, e := s.DB.GetRepo(ctx, o.RepositoryID)
	if e != nil {
		return s.finish(ctx, o, "blocked", "repository is no longer registered", now)
	}
	if s.Authorize == nil {
		return s.finish(ctx, o, "blocked", "launch authorization is unavailable", now)
	}
	if e = s.Authorize(ctx, repo, c.Kind); e != nil {
		return s.finish(ctx, o, "blocked", "current access or provider admission refused; inspect connection and provider settings", now)
	}
	reserved := o
	if c.Kind == "drift" {
		o, e = s.driftSources(ctx, o, c, repo, now)
	} else {
		o, e = s.DB.SelectAnalystSources(ctx, o.ID, o.Fence, store.AnalystSourceQuery{After: c.After, TaskRefs: c.TaskRefs, Modes: c.Modes, Limit: c.RunLimit}, now)
	}
	if e != nil {
		if errors.Is(e, store.ErrAnalystScheduleConflict) || ctx.Err() != nil {
			return e
		}
		return s.finish(ctx, reserved, "blocked", "source preparation failed or an unresolved pinned drift window requires explicit reanalysis", now)
	}
	if len(o.Works) == 0 {
		return s.finish(ctx, o, "no_new_evidence", "no new eligible evidence in the saved scope", now)
	}
	// Access is checked again after source preparation; the allocation transaction
	// checks enabled state and the fence immediately before any upstream effects.
	if e = s.Authorize(ctx, repo, c.Kind); e != nil {
		return s.finish(ctx, o, "blocked", "launch access or provider admission changed", now)
	}
	launch := store.WithAnalystOccurrence(ctx, o.ID, o.Fence, now)
	switch c.Kind {
	case "logs":
		if s.Logs == nil {
			return s.finish(ctx, o, "blocked", "log analyst runtime unavailable", now)
		}
		ids := []int64{}
		for _, w := range o.Works {
			ids = append(ids, w.RunID)
		}
		_, e = s.Logs.Start(launch, loganalysis.Request{RepositoryIDs: []int64{repo.ID}, RunIDs: ids, TaskRefs: c.TaskRefs, StartKey: o.StartKey, Limits: c.LogLimits})
	case "bugs":
		if s.Bugs == nil {
			return s.finish(ctx, o, "blocked", "bug analyst runtime unavailable", now)
		}
		issues := []int{}
		for i := range o.Works {
			w := &o.Works[i]
			mapped, mapErr := naming.ParseRef(w.TaskRef)
			if w.IssueNumber < 1 || mapErr != nil || mapped.Forge != repo.Forge || !strings.EqualFold(mapped.Owner+"/"+mapped.Repo, repo.Slug()) || mapped.Issue != w.IssueNumber {
				w.State = "unresolved"
				w.Reason = "terminal Bug run has no authorized issue mapping"
				continue
			}
			issues = append(issues, w.IssueNumber)
		}
		slices.Sort(issues)
		issues = slices.Compact(issues)
		if len(issues) == 0 {
			return s.finish(ctx, o, "incomplete", "no selected Bug run maps to an issue", now)
		}
		_, e = s.Bugs.Start(launch, prevention.Scope{RequestKey: o.StartKey, Repository: repo.Slug(), Issues: issues, Limits: c.BugLimits})
	case "drift":
		if s.Drift == nil {
			return s.finish(ctx, o, "blocked", "drift analyst runtime unavailable", now)
		}
		var svc *drift.Service
		svc, e = s.Drift(ctx, repo)
		if e != nil {
			return s.finish(ctx, o, "blocked", "drift runtime unavailable", now)
		}
		var window struct{ Base, Head string }
		if e = json.Unmarshal(o.Works[0].Metadata, &window); e != nil {
			return e
		}
		_, e = svc.Start(launch, drift.Request{Repository: repo.Slug(), Base: window.Base, Head: window.Head, Paths: c.Paths, Issues: c.Issues, Key: o.StartKey, Limits: c.DriftLimits})
	}
	current, readErr := s.DB.GetAnalystOccurrence(context.WithoutCancel(ctx), o.ID)
	if readErr != nil {
		return errors.Join(e, readErr)
	}
	if current.Fence != o.Fence {
		return store.ErrAnalystScheduleConflict
	}
	if current.State != "reserved" && current.State != "launched" {
		return nil
	}
	if current.State == "reserved" {
		return errors.Join(e, s.finish(context.WithoutCancel(ctx), current, "failed", "upstream allocation refused before launch", now))
	}
	// Upstream finalization is authoritative even when the provider lost its reply.
	done, reconcileErr := s.reconcile(context.WithoutCancel(ctx), current, now, false)
	if done {
		return reconcileErr
	}
	return errors.Join(e, reconcileErr)
}

func (s *Service) finish(ctx context.Context, o store.AnalystOccurrence, state, reason string, now time.Time) error {
	o.State = state
	o.Disposition = "not_evaluated"
	o.Reason = reason
	for i := range o.Works {
		if !o.Works[i].Covered {
			o.Works[i].State = "unresolved"
			if o.Works[i].Reason == "" {
				o.Works[i].Reason = reason
			}
		}
	}
	return s.DB.FinalizeAnalystOccurrence(ctx, o, nil, now)
}

func (s *Service) driftSources(ctx context.Context, o store.AnalystOccurrence, c Config, repo store.Repo, now time.Time) (store.AnalystOccurrence, error) {
	if len(o.Works) > 0 {
		return o, nil
	}
	if o.ReanalysisOf > 0 {
		prior, e := s.DB.GetAnalystOccurrence(ctx, o.ReanalysisOf)
		if e != nil {
			return o, e
		}
		works := append([]store.AnalystSourceWork(nil), prior.Works...)
		for i := range works {
			works[i].State = "selected"
			works[i].Offered = false
			works[i].Cited = false
			works[i].Covered = false
			works[i].Reason = ""
		}
		return s.DB.SetAnalystSources(ctx, o.ID, o.Fence, works, now)
	}
	if _, e := s.DB.UnresolvedAnalystDrift(ctx, o.ScheduleID); e == nil {
		return o, errors.New("unresolved pinned drift window requires explicit reanalysis")
	} else if !errors.Is(e, store.ErrNotFound) {
		return o, e
	}
	binary, e := git.Resolve(ctx)
	if e != nil {
		return o, e
	}
	clone, e := binary.Open(ctx, repo.Path)
	if e != nil {
		return o, e
	}
	base := c.Base
	prior, e := s.DB.LatestCoveredAnalystDrift(ctx, o.ScheduleID, o.Generation)
	if e != nil && !errors.Is(e, store.ErrNotFound) {
		return o, e
	}
	if e == nil {
		var p struct{ Base, Head string }
		if e = json.Unmarshal(prior.Metadata, &p); e != nil {
			return o, e
		}
		base = p.Head
	}
	base, e = clone.RevParse(ctx, base+"^{commit}", git.RevParseOptions{})
	if e != nil {
		return o, e
	}
	head, e := clone.RevParse(ctx, c.Ref+"^{commit}", git.RevParseOptions{})
	if e != nil {
		return o, e
	}
	if base == head {
		return o, nil
	}
	key := "drift:" + base + ":" + head
	exists, e := s.DB.HasAnalystSource(ctx, o.ScheduleID, o.Generation, key)
	if e != nil || exists {
		return o, e
	}
	return s.DB.SetAnalystSources(ctx, o.ID, o.Fence, []store.AnalystSourceWork{{Kind: "drift", SourceKey: key, State: "selected", Metadata: raw(struct{ Base, Head string }{base, head})}}, now)
}

// Recover reconciles committed receipts before fencing expired orphans. It never
// relaunches work whose upstream identity was allocated.
func (s *Service) Recover(ctx context.Context, now time.Time) error {
	rows, e := s.DB.ListActiveAnalystOccurrences(ctx, 100)
	if e != nil {
		return e
	}
	for _, o := range rows {
		if o.AnalysisID == "" {
			continue
		}
		orphan := !o.LeaseUntil.After(now)
		if orphan {
			o, e = s.DB.RecoverAnalystOccurrence(ctx, o.ID, now, leaseDuration)
			if e != nil {
				return e
			}
		}
		_, e = s.reconcile(ctx, o, now, orphan)
		if e != nil {
			return fmt.Errorf("recover schedule occurrence %d: %w", o.ID, e)
		}
	}
	return nil
}
