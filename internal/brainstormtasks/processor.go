// Package brainstormtasks drives confirmed brainstorm task batches: it creates
// one issue per frozen draft, reconciles uncertain creates through bounded
// delayed-visibility checks, settles continuations and sends one summary per
// continuation. Starting Plan belongs to the launch step, not to this package.
package brainstormtasks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/featuredraft"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

// Issue is the forge observation the processor compares against an item.
type Issue struct {
	ID        int64
	Number    int
	Title     string
	Body      string
	Link      string
	AuthorID  int64
	CreatedAt time.Time
}

// Forge is the narrow issue port: one create and one bounded listing of the
// bot's own issues, open and closed, newest first, stopping before since.
type Forge interface {
	CreateIssue(ctx context.Context, repositoryRef, title, body string) (Issue, error)
	ListBotIssues(ctx context.Context, repositoryRef string, since time.Time) ([]Issue, error)
}

// Summary delivery states, as stored on a continuation.
const (
	SummaryPending   = "pending"
	SummarySent      = "sent"
	SummaryNoChannel = "no_channel"
)

// Announcer queues one owner summary under an idempotent event key. It returns
// SummarySent once queued, SummaryNoChannel when the project has no workflow
// notice channel, and SummaryPending while Discord is unavailable.
type Announcer interface {
	Announce(ctx context.Context, repositoryRef, key, text string, at time.Time) (string, error)
}

// Authority rechecks the continuation's current owner-action authority and
// consent immediately before a remote write. allowed=false means revoked,
// withdrawn or cancelled; err is a storage failure.
type Authority interface {
	Allowed(ctx context.Context, executionID string) (bool, error)
}

// Store is the persistence the processor uses; *store.DB satisfies it.
type Store interface {
	ActiveBrainstormTaskItems(ctx context.Context, limit int) ([]store.BrainstormTaskItem, error)
	ReadBrainstormTaskBatch(ctx context.Context, id string) (store.BrainstormTaskBatch, error)
	BrainstormTaskContinuations(ctx context.Context, batchID string) ([]store.BrainstormTaskContinuation, error)
	BrainstormTaskItems(ctx context.Context, batchID string) ([]store.BrainstormTaskItem, error)
	SaveBrainstormTaskItem(ctx context.Context, it store.BrainstormTaskItem, fromState string, at time.Time) (bool, error)
	SettleBrainstormTaskContinuation(ctx context.Context, id string, at time.Time) error
	SetBrainstormTaskContinuationSummary(ctx context.Context, id, summaryState string) error
	SettleBrainstormTaskBatch(ctx context.Context, id string, at time.Time) error
}

// runningBatches is optional: a store that lists running batches lets settlement
// and pending summaries resume after a restart even when no item is active.
type runningBatches interface {
	RunningBrainstormTaskBatches(ctx context.Context, limit int) ([]store.BrainstormTaskBatch, error)
}

// CheckSchedule is the delay after the attempt for each delayed-visibility check.
var CheckSchedule = []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute}

const (
	// MaxRateLimitRetries bounds create retries after a rate-limit refusal.
	MaxRateLimitRetries = 3
	// CreateSpacing is the minimum gap between two creates.
	CreateSpacing = time.Second
	// ListingLookback extends the listing before the batch start.
	ListingLookback = 10 * time.Minute

	reasonRateLimited     = "rate limited; retrying after the provider's wait"
	reasonRateLimitedDone = "rate limited; retries used"
	reasonStopped         = "not attempted: authority, consent or the action was withdrawn"
	reasonRestart         = "restart found the create begun; checking"
	reasonNotFound        = "not found in search"
)

// Processor is the daemon-owned batch driver. Step is safe to call from one loop.
type Processor struct {
	Store     Store
	Forge     Forge
	Announcer Announcer
	Authority Authority
	BotUserID int64
	Now       func() time.Time
	// CreateTimeout bounds one create call; zero means 30 seconds.
	CreateTimeout time.Duration

	mu         sync.Mutex
	lastCreate time.Time
	inFlight   map[string]bool
	known      map[string]bool
}

func (p *Processor) now() time.Time {
	if p.Now == nil {
		return time.Now().UTC()
	}
	return p.Now().UTC()
}

// Step advances every running batch once: at most one create, due checks,
// continuation settlement, summaries and batch settlement.
func (p *Processor) Step(ctx context.Context) error {
	if p.Store == nil || p.Forge == nil {
		return errors.New("brainstorm task processor unavailable")
	}
	p.mu.Lock()
	if p.inFlight == nil {
		p.inFlight, p.known = map[string]bool{}, map[string]bool{}
	}
	p.mu.Unlock()
	active, err := p.Store.ActiveBrainstormTaskItems(ctx, 500)
	if err != nil {
		return err
	}
	batches := []string{}
	seen := map[string]bool{}
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			batches = append(batches, id)
		}
	}
	for _, it := range active {
		add(it.BatchID)
	}
	if lister, ok := p.Store.(runningBatches); ok {
		running, e := lister.RunningBrainstormTaskBatches(ctx, 500)
		if e != nil {
			return e
		}
		for _, b := range running {
			add(b.ID)
		}
	}
	p.mu.Lock()
	for id := range p.known {
		add(id)
	}
	p.mu.Unlock()
	slices.Sort(batches)
	created := false
	var errs []error
	for _, id := range batches {
		did, e := p.stepBatch(ctx, id, !created)
		created = created || did
		if e != nil {
			errs = append(errs, e)
		}
	}
	return errors.Join(errs...)
}

// stepBatch returns whether it issued a create, so pacing stays one per step.
func (p *Processor) stepBatch(ctx context.Context, batchID string, mayCreate bool) (bool, error) {
	batch, err := p.Store.ReadBrainstormTaskBatch(ctx, batchID)
	if errors.Is(err, store.ErrNotFound) {
		p.forget(batchID)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if batch.State != "running" {
		p.forget(batchID)
		return false, nil
	}
	p.mu.Lock()
	p.known[batchID] = true
	p.mu.Unlock()
	conts, err := p.Store.BrainstormTaskContinuations(ctx, batchID)
	if err != nil {
		return false, err
	}
	items, err := p.Store.BrainstormTaskItems(ctx, batchID)
	if err != nil {
		return false, err
	}
	allowed := map[string]bool{}
	authority := func(executionID string) (bool, error) {
		if v, ok := allowed[executionID]; ok {
			return v, nil
		}
		if p.Authority == nil {
			allowed[executionID] = false
			return false, nil
		}
		v, e := p.Authority.Allowed(ctx, executionID)
		if e != nil {
			return false, e
		}
		allowed[executionID] = v
		return v, nil
	}
	created := false
	for i := range items {
		it := &items[i]
		driver, ok := drivingContinuation(conts, *it)
		switch it.State {
		case store.BrainstormItemCreating:
			if p.isInFlight(it.ID) {
				continue
			}
			if err = p.toUncertain(ctx, it, store.BrainstormItemCreating, reasonRestart); err != nil {
				return created, err
			}
		case store.BrainstormItemUncertain:
			if err = p.check(ctx, batch, it); err != nil {
				return created, err
			}
		case store.BrainstormItemPending:
			if !ok || created || !mayCreate {
				continue
			}
			if !it.NextCheckAt.IsZero() && p.now().Before(it.NextCheckAt) {
				continue
			}
			did, e := p.create(ctx, batch, driver, it, authority)
			created = created || did
			if e != nil {
				return created, e
			}
		}
	}
	if err = p.settle(ctx, batch, conts, items, authority); err != nil {
		return created, err
	}
	return created, nil
}

func (p *Processor) forget(batchID string) {
	p.mu.Lock()
	delete(p.known, batchID)
	p.mu.Unlock()
}

func (p *Processor) isInFlight(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inFlight[id]
}

// drivingContinuation is the newest running continuation that froze this item
// at its current generation.
func drivingContinuation(conts []store.BrainstormTaskContinuation, it store.BrainstormTaskItem) (store.BrainstormTaskContinuation, bool) {
	var out store.BrainstormTaskContinuation
	found := false
	for _, c := range conts {
		if c.State != "running" {
			continue
		}
		for _, f := range c.Items {
			if f.ItemID == it.ID && f.Generation == it.Generation && (!found || !c.CreatedAt.Before(out.CreatedAt)) {
				out, found = c, true
			}
		}
	}
	return out, found
}

func (p *Processor) create(ctx context.Context, batch store.BrainstormTaskBatch, driver store.BrainstormTaskContinuation, it *store.BrainstormTaskItem, authority func(string) (bool, error)) (bool, error) {
	ok, err := authority(driver.ExecutionID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, p.markStopped(ctx, it)
	}
	p.mu.Lock()
	wait := !p.lastCreate.IsZero() && p.now().Before(p.lastCreate.Add(CreateSpacing))
	p.mu.Unlock()
	if wait {
		return false, nil
	}
	at := p.now()
	begun := *it
	begun.State = store.BrainstormItemCreating
	begun.AttemptAt = at
	begun.ChecksDone = 0
	begun.NextCheckAt = time.Time{}
	begun.Reason = ""
	saved, err := p.Store.SaveBrainstormTaskItem(ctx, begun, it.State, at)
	if err != nil || !saved {
		return false, err
	}
	*it = begun
	p.mu.Lock()
	p.inFlight[it.ID] = true
	p.lastCreate = at
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.inFlight, it.ID); p.mu.Unlock() }()
	// The effect check runs after the durable begin: a refusal here proves no
	// write was sent, so the item returns to pending.
	ok, err = false, nil
	if p.Authority != nil {
		ok, err = p.Authority.Allowed(ctx, driver.ExecutionID)
	}
	if err != nil || !ok {
		allowedErr := err
		back := *it
		back.State = store.BrainstormItemPending
		back.AttemptAt = time.Time{}
		back.Reason = reasonStopped
		if _, e := p.Store.SaveBrainstormTaskItem(context.WithoutCancel(ctx), back, store.BrainstormItemCreating, p.now()); e != nil {
			return true, errors.Join(allowedErr, e)
		}
		*it = back
		return true, allowedErr
	}
	timeout := p.CreateTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	call, cancel := context.WithTimeout(ctx, timeout)
	issue, createErr := p.Forge.CreateIssue(call, batch.RepositoryRef, it.Title, it.Body)
	cancel()
	save := context.WithoutCancel(ctx)
	next := *it
	switch {
	case createErr == nil && p.matches(issue, *it):
		next.State = store.BrainstormItemCreated
		next.IssueID, next.IssueNumber, next.IssueLink = issue.ID, issue.Number, issue.Link
	case createErr == nil:
		next.State = store.BrainstormItemUncertain
		next.Reason = "the reply did not match the draft; checking"
		next.NextCheckAt = it.AttemptAt.Add(CheckSchedule[0])
	case errors.Is(createErr, github.ErrRateLimited):
		retryAfter := github.DefaultRetryAfter
		var limited *github.RateLimitError
		if errors.As(createErr, &limited) && limited.RetryAfter > 0 {
			retryAfter = limited.RetryAfter
		}
		if it.CreateRetries < MaxRateLimitRetries {
			next.State = store.BrainstormItemPending
			next.CreateRetries++
			next.NextCheckAt = p.now().Add(retryAfter)
			next.Reason = reasonRateLimited
		} else {
			next.State = store.BrainstormItemNotCreated
			next.Reason = reasonRateLimitedDone
		}
	case featuredraft.KnownCreateFailure(createErr):
		next.State = store.BrainstormItemNotCreated
		next.Reason = "refused before any write: " + refusal(createErr)
	default:
		next.State = store.BrainstormItemUncertain
		next.Reason = "no clear answer from the create; checking"
		next.NextCheckAt = it.AttemptAt.Add(CheckSchedule[0])
	}
	if _, err = p.Store.SaveBrainstormTaskItem(save, next, store.BrainstormItemCreating, p.now()); err != nil {
		return true, err
	}
	*it = next
	return true, nil
}

func refusal(err error) string {
	for _, c := range []struct {
		err  error
		text string
	}{{github.ErrInvalidArgument, "invalid argument"}, {github.ErrUnauthorized, "unauthorized"}, {github.ErrForbidden, "forbidden"}, {github.ErrNotFound, "not found"}, {github.ErrUnprocessable, "unprocessable"}} {
		if errors.Is(err, c.err) {
			return c.text
		}
	}
	return "refused"
}

func (p *Processor) matches(issue Issue, it store.BrainstormTaskItem) bool {
	return p.BotUserID > 0 && issue.AuthorID == p.BotUserID && issue.Title == it.Title && issue.Body == it.Body && issue.Number > 0 && issue.ID > 0
}

func (p *Processor) markStopped(ctx context.Context, it *store.BrainstormTaskItem) error {
	if it.Reason == reasonStopped {
		return nil
	}
	next := *it
	next.Reason = reasonStopped
	if _, err := p.Store.SaveBrainstormTaskItem(ctx, next, it.State, p.now()); err != nil {
		return err
	}
	*it = next
	return nil
}

func (p *Processor) toUncertain(ctx context.Context, it *store.BrainstormTaskItem, from, reason string) error {
	next := *it
	next.State = store.BrainstormItemUncertain
	if next.AttemptAt.IsZero() {
		next.AttemptAt = p.now()
	}
	next.ChecksDone = 0
	next.NextCheckAt = next.AttemptAt.Add(CheckSchedule[0])
	next.Reason = reason
	saved, err := p.Store.SaveBrainstormTaskItem(ctx, next, from, p.now())
	if err == nil && saved {
		*it = next
	}
	return err
}

// check runs one due delayed-visibility check. It never creates.
func (p *Processor) check(ctx context.Context, batch store.BrainstormTaskBatch, it *store.BrainstormTaskItem) error {
	now := p.now()
	due := it.NextCheckAt
	if due.IsZero() {
		due = it.AttemptAt.Add(CheckSchedule[min(it.ChecksDone, len(CheckSchedule)-1)])
	}
	if now.Before(due) {
		return nil
	}
	next := *it
	next.ChecksDone++
	since := batch.CreatedAt.Add(-ListingLookback)
	call, cancel := context.WithTimeout(ctx, time.Minute)
	issues, err := p.Forge.ListBotIssues(call, batch.RepositoryRef, since)
	cancel()
	if err != nil {
		next.Reason = "the check could not list issues; nothing was created again"
	} else {
		var matches []Issue
		for _, issue := range issues {
			if issue.CreatedAt.IsZero() || !issue.CreatedAt.Before(since) {
				if hasLine(issue.Body, it.Marker) {
					matches = append(matches, issue)
				}
			}
		}
		switch {
		case len(matches) == 1 && p.matches(matches[0], *it):
			next.State = store.BrainstormItemCreated
			next.IssueID, next.IssueNumber, next.IssueLink = matches[0].ID, matches[0].Number, matches[0].Link
			next.Reason = fmt.Sprintf("adopted on check %d", next.ChecksDone)
			next.NextCheckAt = time.Time{}
		case len(matches) == 1:
			next.Reason = fmt.Sprintf("an issue carries this draft's marker but differs: #%d", matches[0].Number)
		case len(matches) > 1:
			numbers := make([]string, len(matches))
			for i, m := range matches {
				numbers[i] = fmt.Sprintf("#%d", m.Number)
			}
			next.Reason = "several issues carry this draft's marker: " + strings.Join(numbers, ", ")
		default:
			next.Reason = reasonNotFound
		}
	}
	if next.State == store.BrainstormItemUncertain {
		if next.ChecksDone >= len(CheckSchedule) {
			next.State = store.BrainstormItemUncertainExhausted
			next.NextCheckAt = time.Time{}
		} else {
			next.NextCheckAt = it.AttemptAt.Add(CheckSchedule[next.ChecksDone])
		}
	}
	saved, err := p.Store.SaveBrainstormTaskItem(context.WithoutCancel(ctx), next, store.BrainstormItemUncertain, p.now())
	if err == nil && saved {
		*it = next
	}
	return err
}

func hasLine(body, marker string) bool {
	if marker == "" {
		return false
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimRight(line, "\r") == marker {
			return true
		}
	}
	return false
}

// outcome is one item's settled reading for one continuation; ok=false while
// the item still needs work.
func outcome(it store.BrainstormTaskItem, frozen store.BrainstormFrozenItem, stopped bool) (string, bool) {
	if it.Generation > frozen.Generation {
		return outcomeReplaced, true
	}
	switch it.State {
	case store.BrainstormItemPlanStarted:
		return outcomeStarted, true
	case store.BrainstormItemPlanStartFailed:
		return outcomeNotStarted, true
	case store.BrainstormItemCreated, store.BrainstormItemPlanStarting:
		if it.FailedLaunchAttempts >= store.BrainstormLaunchFailureLimit {
			return outcomeNotStarted, true
		}
		return "", false
	case store.BrainstormItemUncertainExhausted:
		return outcomeUncertain, true
	case store.BrainstormItemNotCreated:
		return outcomeNotCreated, true
	case store.BrainstormItemPending:
		if stopped {
			return outcomeNotAttempted, true
		}
	}
	return "", false
}

func (p *Processor) settle(ctx context.Context, batch store.BrainstormTaskBatch, conts []store.BrainstormTaskContinuation, items []store.BrainstormTaskItem, authority func(string) (bool, error)) error {
	byID := map[string]store.BrainstormTaskItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	at := p.now()
	for i := range conts {
		c := &conts[i]
		if c.State == "running" {
			ok, err := authority(c.ExecutionID)
			if err != nil {
				return err
			}
			done := true
			for _, f := range c.Items {
				it, found := byID[f.ItemID]
				if !found {
					continue
				}
				if _, settled := outcome(it, f, !ok); !settled {
					done = false
					break
				}
			}
			if !done {
				continue
			}
			if err = p.Store.SettleBrainstormTaskContinuation(ctx, c.ID, at); err != nil {
				return err
			}
			c.State = "settled"
		}
		if c.State == "settled" && (c.SummaryState == "" || c.SummaryState == SummaryPending) {
			state := SummaryPending
			if p.Announcer != nil {
				text, err := renderSummary(*c, byID)
				if err != nil {
					return err
				}
				state, err = p.Announcer.Announce(ctx, batch.RepositoryRef, SummaryKey(batch.ID, c.ID), text, at)
				if err != nil || state == "" {
					state = SummaryPending
				}
			}
			if state != SummaryPending {
				if err := p.Store.SetBrainstormTaskContinuationSummary(ctx, c.ID, state); err != nil {
					return err
				}
			}
			c.SummaryState = state
		}
	}
	for _, c := range conts {
		if c.State == "running" || c.SummaryState == "" || c.SummaryState == SummaryPending {
			return nil
		}
	}
	for _, it := range items {
		switch it.State {
		case store.BrainstormItemCreating, store.BrainstormItemUncertain, store.BrainstormItemCreated, store.BrainstormItemPlanStarting:
			if it.FailedLaunchAttempts < store.BrainstormLaunchFailureLimit || it.State == store.BrainstormItemCreating || it.State == store.BrainstormItemUncertain {
				return nil
			}
		}
	}
	if err := p.Store.SettleBrainstormTaskBatch(ctx, batch.ID, at); err != nil {
		return err
	}
	p.forget(batch.ID)
	return nil
}

// SummaryKey is the idempotent announcement key of one continuation.
func SummaryKey(batchID, continuationID string) string {
	return "brainstorm-tasks:" + batchID + ":" + continuationID
}
