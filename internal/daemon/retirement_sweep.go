package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

const retirementBatchTimeout = 30 * time.Second
const retirementOwnerRequests = 7

// retirementBudgetIssues checks worker-armed backoff even between final reads.
type retirementBudgetIssues struct {
	d      *Daemon
	issues RetirementIssues
	now    time.Time
}

func (b retirementBudgetIssues) GetIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	if _, held := b.d.connectionHold(ctx, b.now); held {
		return github.Issue{}, ErrDiscoveryDeferred
	}
	return b.issues.GetIssue(ctx, repo, number)
}

// RunRetirementBatch visits at most one owner after normal discovery/dispatch.
// Finishing an exhausted pass on a subsequent tick ensures next_due is measured
// from completion using supplied daemon time, never a second clock or goroutine.
func (d *Daemon) RunRetirementBatch(ctx context.Context, now time.Time) (err error) {
	if d.retirement.CloneGate == nil || d.retirement.LockDir == "" {
		return nil
	}
	if _, held := d.connectionHold(ctx, now); held {
		return nil
	}
	parentCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, retirementBatchTimeout)
	defer cancel()
	s, err := d.db.GetRetirementSweep(ctx)
	if err != nil {
		return err
	}
	if s.Complete && now.Before(s.NextDue) {
		return nil
	}
	if s.ID == "" || s.Complete {
		boundary, err := d.db.RetirementBoundary(ctx)
		if err != nil {
			return err
		}
		s = store.RetirementSweep{ID: fmt.Sprintf("retirement-%d", now.UnixNano()), StartedAt: now, Boundary: boundary, Counts: make(map[string]int)}
		if err := d.db.SaveRetirementSweep(ctx, s); err != nil {
			return err
		}
	}
	defer func() {
		if err != nil {
			d.log.ErrorContext(ctx, "retirement sweep incomplete", slog.String("sweep", s.ID), slog.Int64("cursor", s.Cursor), slog.Duration("duration", now.Sub(s.StartedAt)), slog.Any("outcomes", s.Counts), slog.Int("deferred", s.Deferred), slog.Bool("completed", false))
		}
	}()
	owners, err := d.db.ListRetirementOwners(ctx, s.Cursor, s.Boundary, 1)
	if err != nil {
		return err
	}
	if len(owners) == 0 {
		s.Complete = true
		s.NextDue = now.Add(24 * time.Hour)
		if err := d.db.SaveRetirementSweep(ctx, s); err != nil {
			return err
		}
		d.log.InfoContext(ctx, "retirement sweep complete", slog.String("sweep", s.ID), slog.Duration("duration", now.Sub(s.StartedAt)), slog.Int("owners", s.Owners), slog.Any("outcomes", s.Counts), slog.Int("deferred", s.Deferred), slog.Bool("completed", true), slog.Time("next_due", s.NextDue))
		return nil
	}
	owner := owners[0]
	// A reservation covers the evaluator plus two fresh reads for each resource.
	// All unused capacity is returned. Normal discovery has already had its turn.
	ctx = sessionContext(ctx, owner.Session.ID)
	budget, err := d.quotaBudget(ctx)
	if err != nil {
		return err
	}
	reservation, err := budget.acquire(fmt.Sprintf("retirement:%d", owner.Session.ID), retirementOwnerRequests, false)
	if errors.Is(err, ErrDiscoveryDeferred) {
		d.log.InfoContext(ctx, "retirement sweep deferred", slog.String("sweep", s.ID), slog.String("reason", "request_budget"), slog.Bool("completed", false))
		return nil
	}
	if err != nil {
		return err
	}
	defer reservation.finish()
	if d.connections != nil {
		ctx = context.WithValue(ctx, quotaReservationKey{}, reservation)
	} else {
		ctx = github.WithRequestObserver(ctx, reservation.observe)
	}
	opts := d.retirement
	issues := opts.Issues
	if issues == nil {
		issues = d.forge
	}
	opts.Issues = retirementBudgetIssues{d: d, issues: issues, now: now}
	var evaluation RetirementEvaluation
	var executionErr error
	if owner.ObservationError != nil {
		executionErr = owner.ObservationError
		evaluation = RetirementEvaluation{Owner: owner, ObservedAt: now, Error: executionErr, Decisions: []RetirementDecision{{Kind: "owner", Resource: fmt.Sprint(owner.Session.ID), Action: "unknown", Reason: "owner_observation_failed"}}}
		LogRetirement(ctx, d.log, s.ID, "owner", false, evaluation)
	} else {
		evaluation, executionErr = d.executeRetirement(ctx, s.ID, owner.Session.ID, now, opts)
	}
	if errors.Is(executionErr, ErrRetirementStorage) {
		return executionErr
	}
	// Shutdown retains this owner. A bounded owner timeout is a deferral, so
	// contention cannot indefinitely starve later owners.
	if parentCtx.Err() != nil {
		return parentCtx.Err()
	}
	var rate *github.RateLimitError
	if errors.As(executionErr, &rate) {
		return d.observe(ctx, now, executionErr)
	}
	if errors.Is(executionErr, ErrDiscoveryDeferred) {
		return nil
	}
	if s.Counts == nil {
		s.Counts = make(map[string]int)
	}
	deferred := executionErr != nil
	for _, decision := range evaluation.Decisions {
		s.Counts[decision.Action]++
		if decision.Action == "kept" || decision.Action == "unknown" || decision.Action == "failed" {
			deferred = true
		}
	}
	if deferred {
		s.Deferred++
	}
	s.Owners++
	s.Cursor = owner.Session.ID
	return d.db.SaveRetirementSweep(parentCtx, s)
}
