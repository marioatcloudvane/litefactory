package brainstormtasks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

const bot = 99

type fakeStore struct {
	mu    sync.Mutex
	batch store.BrainstormTaskBatch
	conts []store.BrainstormTaskContinuation
	items []store.BrainstormTaskItem
	// failSaveFrom makes the next save from this state fail, simulating a crash
	// between the create call and the durable save of its outcome.
	failSaveFrom string
}

func (s *fakeStore) ActiveBrainstormTaskItems(context.Context, int) ([]store.BrainstormTaskItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.batch.State != "running" {
		return nil, nil
	}
	var out []store.BrainstormTaskItem
	for _, it := range s.items {
		switch it.State {
		case "pending", "creating", "uncertain", "created", "plan_starting":
			out = append(out, it)
		}
	}
	return out, nil
}
func (s *fakeStore) ReadBrainstormTaskBatch(_ context.Context, id string) (store.BrainstormTaskBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.batch.ID {
		return store.BrainstormTaskBatch{}, store.ErrNotFound
	}
	return s.batch, nil
}
func (s *fakeStore) BrainstormTaskContinuations(context.Context, string) ([]store.BrainstormTaskContinuation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]store.BrainstormTaskContinuation(nil), s.conts...), nil
}
func (s *fakeStore) BrainstormTaskItems(context.Context, string) ([]store.BrainstormTaskItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]store.BrainstormTaskItem(nil), s.items...), nil
}
func (s *fakeStore) SaveBrainstormTaskItem(_ context.Context, it store.BrainstormTaskItem, from string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failSaveFrom != "" && s.failSaveFrom == from {
		s.failSaveFrom = ""
		return false, errors.New("crash")
	}
	for i := range s.items {
		if s.items[i].ID == it.ID && s.items[i].State == from && s.items[i].Generation == it.Generation {
			s.items[i] = it
			return true, nil
		}
	}
	return false, nil
}
func (s *fakeStore) SettleBrainstormTaskContinuation(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.conts {
		if s.conts[i].ID == id && s.conts[i].State == "running" {
			s.conts[i].State, s.conts[i].SettledAt = "settled", at
		}
	}
	return nil
}
func (s *fakeStore) SetBrainstormTaskContinuationSummary(_ context.Context, id, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.conts {
		if s.conts[i].ID == id {
			s.conts[i].SummaryState = state
		}
	}
	return nil
}
func (s *fakeStore) SettleBrainstormTaskBatch(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conts {
		if c.State == "running" {
			return nil
		}
	}
	s.batch.State, s.batch.SettledAt = "settled", at
	return nil
}
func (s *fakeStore) item(id string) store.BrainstormTaskItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range s.items {
		if it.ID == id {
			return it
		}
	}
	return store.BrainstormTaskItem{}
}
func (s *fakeStore) setState(id, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == id {
			s.items[i].State = state
		}
	}
}

// continueWith admits a fresh continuation over the named items, as the action
// does: frozen not_created items return to pending.
func (s *fakeStore) continueWith(id string, at time.Time, ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := store.BrainstormTaskContinuation{ID: id, BatchID: s.batch.ID, ExecutionID: "owner-action:" + id, Kind: "continue", State: "running", SummaryState: "pending", CreatedAt: at}
	for _, want := range ids {
		for i := range s.items {
			if s.items[i].ID == want {
				if s.items[i].State == store.BrainstormItemNotCreated {
					s.items[i].State = store.BrainstormItemPending
				}
				c.Items = append(c.Items, store.BrainstormFrozenItem{ItemID: want, State: s.items[i].State, Generation: s.items[i].Generation})
			}
		}
	}
	s.conts = append(s.conts, c)
	s.batch.State = "running"
}

type createResult struct {
	issue  *Issue // nil: return the real created issue
	err    error
	create bool // whether the issue really exists remotely after the call
}

type fakeForge struct {
	mu       sync.Mutex
	clock    *fakeClock
	issues   []Issue // remote issues
	visible  map[int]int
	lists    int
	creates  []time.Time
	script   []createResult
	nextNum  int
	listings []time.Time
}

func (f *fakeForge) CreateIssue(_ context.Context, _ string, title, body string) (Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates = append(f.creates, f.clock.now())
	r := createResult{create: true}
	if len(f.script) > 0 {
		r, f.script = f.script[0], f.script[1:]
	}
	var issue Issue
	if r.create {
		f.nextNum++
		issue = Issue{ID: int64(1000 + f.nextNum), Number: f.nextNum, Title: title, Body: body, Link: fmt.Sprintf("https://example.test/%d", f.nextNum), AuthorID: bot, CreatedAt: f.clock.now()}
		f.issues = append(f.issues, issue)
	}
	if r.err != nil {
		return Issue{}, r.err
	}
	if r.issue != nil {
		return *r.issue, nil
	}
	return issue, nil
}

func (f *fakeForge) ListBotIssues(_ context.Context, _ string, since time.Time) ([]Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	f.listings = append(f.listings, since)
	var out []Issue
	for i := len(f.issues) - 1; i >= 0; i-- {
		issue := f.issues[i]
		if after, ok := f.visible[issue.Number]; ok && f.lists < after {
			continue
		}
		out = append(out, issue)
	}
	return out, nil
}

type fakeAnnouncer struct {
	mu    sync.Mutex
	sent  map[string]string
	order []string
	down  bool
}

func (a *fakeAnnouncer) Announce(_ context.Context, _ string, key, text string, _ time.Time) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.down {
		return SummaryPending, errors.New("discord down")
	}
	if a.sent == nil {
		a.sent = map[string]string{}
	}
	if _, ok := a.sent[key]; !ok {
		a.order = append(a.order, key)
	}
	a.sent[key] = text
	return SummarySent, nil
}

type fakeAuthority struct{ revoked map[string]bool }

func (a fakeAuthority) Allowed(_ context.Context, id string) (bool, error) {
	return !a.revoked[id], nil
}

type fixture struct {
	t     *testing.T
	clock *fakeClock
	db    *fakeStore
	forge *fakeForge
	ann   *fakeAnnouncer
	auth  fakeAuthority
	p     *Processor
}

func newFixture(t *testing.T, n int) *fixture {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
	db := &fakeStore{batch: store.BrainstormTaskBatch{ID: "batch-1", ExecutionID: "owner-action:c1", RepositoryRef: "acme/app", State: "running", CreatedAt: clock.t}}
	c := store.BrainstormTaskContinuation{ID: "c1", BatchID: "batch-1", ExecutionID: "owner-action:c1", Kind: "create", State: "running", SummaryState: "pending", CreatedAt: clock.t}
	for i := range n {
		id := fmt.Sprintf("item-%d", i+1)
		marker := fmt.Sprintf("<!-- lf:brainstorm-draft brainstorm=b1 draft=d%d gen=1 -->", i+1)
		db.items = append(db.items, store.BrainstormTaskItem{ID: id, BatchID: "batch-1", DraftID: fmt.Sprintf("d%d", i+1), Ordinal: i, Generation: 1, Title: fmt.Sprintf("Draft %d", i+1), Body: "Goal\n\n" + marker + "\n", Marker: marker, State: store.BrainstormItemPending})
		c.Items = append(c.Items, store.BrainstormFrozenItem{ItemID: id, State: "pending", Generation: 1})
	}
	db.conts = []store.BrainstormTaskContinuation{c}
	forge := &fakeForge{clock: clock, visible: map[int]int{}}
	ann := &fakeAnnouncer{}
	auth := fakeAuthority{revoked: map[string]bool{}}
	p := &Processor{Store: db, Forge: forge, Announcer: ann, Authority: auth, BotUserID: bot, Now: clock.now}
	return &fixture{t: t, clock: clock, db: db, forge: forge, ann: ann, auth: auth, p: p}
}

func (f *fixture) step() {
	f.t.Helper()
	if err := f.p.Step(context.Background()); err != nil {
		f.t.Fatalf("step: %v", err)
	}
}

// run steps with a fake second between steps for the given fake duration.
func (f *fixture) run(d time.Duration) {
	f.t.Helper()
	for end := f.clock.t.Add(d); f.clock.t.Before(end); f.clock.advance(time.Second) {
		f.step()
	}
}

func (f *fixture) state(id string) string { return f.db.item(id).State }

// startPlan stands in for the launch step, which owns created items.
func (f *fixture) startPlan() {
	for _, it := range f.db.items {
		if it.State == store.BrainstormItemCreated {
			f.db.setState(it.ID, store.BrainstormItemPlanStarted)
		}
	}
}

func TestLostReplyAfterRealCreateIsAdoptedOnLaterCheck(t *testing.T) {
	f := newFixture(t, 1)
	f.forge.script = []createResult{{create: true, err: context.DeadlineExceeded}}
	f.step()
	if got := f.state("item-1"); got != store.BrainstormItemUncertain {
		t.Fatalf("state after lost reply = %q", got)
	}
	f.run(31 * time.Second)
	it := f.db.item("item-1")
	if it.State != store.BrainstormItemCreated || it.IssueNumber != 1 || it.IssueID != 1001 || it.IssueLink == "" {
		t.Fatalf("adopted item = %+v", it)
	}
	if len(f.forge.creates) != 1 {
		t.Fatalf("creates = %d, want 1", len(f.forge.creates))
	}
	if want := f.db.batch.CreatedAt.Add(-ListingLookback); f.forge.listings[0] != want {
		t.Fatalf("listing since %v, want %v", f.forge.listings[0], want)
	}
}

func TestIssueVisibleOnlyAfterThirdCheckIsAdopted(t *testing.T) {
	f := newFixture(t, 1)
	f.forge.script = []createResult{{create: true, err: errors.New("connection reset")}}
	f.forge.visible[1] = 3
	f.step()
	attempt := f.db.item("item-1").AttemptAt
	for i, d := range CheckSchedule[:2] {
		f.clock.t = attempt.Add(d)
		f.step()
		it := f.db.item("item-1")
		if it.State != store.BrainstormItemUncertain || it.ChecksDone != i+1 || it.Reason != reasonNotFound {
			t.Fatalf("check %d: %+v", i+1, it)
		}
		if it.NextCheckAt != attempt.Add(CheckSchedule[i+1]) {
			t.Fatalf("check %d next at %v", i+1, it.NextCheckAt)
		}
	}
	// Nothing happens before the next scheduled time.
	f.clock.t = attempt.Add(CheckSchedule[2] - time.Second)
	f.step()
	if f.forge.lists != 2 {
		t.Fatalf("listed early: %d", f.forge.lists)
	}
	f.clock.t = attempt.Add(CheckSchedule[2])
	f.step()
	if it := f.db.item("item-1"); it.State != store.BrainstormItemCreated || it.ChecksDone != 3 {
		t.Fatalf("third check: %+v", it)
	}
	if len(f.forge.creates) != 1 {
		t.Fatalf("creates = %d", len(f.forge.creates))
	}
}

func TestNeverVisibleEndsExhaustedWithoutSecondCreate(t *testing.T) {
	f := newFixture(t, 1)
	f.forge.script = []createResult{{create: false, err: errors.New("timeout")}}
	f.step()
	// A fresh processor stands in for a restart; checks resume from saved times.
	f.p = &Processor{Store: f.db, Forge: f.forge, Announcer: f.ann, Authority: f.auth, BotUserID: bot, Now: f.clock.now}
	f.run(16 * time.Minute)
	it := f.db.item("item-1")
	if it.State != store.BrainstormItemUncertainExhausted || it.ChecksDone != len(CheckSchedule) {
		t.Fatalf("item = %+v", it)
	}
	if f.forge.lists != len(CheckSchedule) || len(f.forge.creates) != 1 {
		t.Fatalf("lists=%d creates=%d", f.forge.lists, len(f.forge.creates))
	}
	text := f.ann.sent[SummaryKey("batch-1", "c1")]
	if !strings.Contains(text, "not identified yet") || !strings.Contains(text, "nothing was duplicated") || !strings.Contains(text, "Not found in search is not proof") {
		t.Fatalf("summary = %q", text)
	}
	if f.db.batch.State != "settled" {
		t.Fatalf("batch = %q", f.db.batch.State)
	}
}

func TestTwoMarkerMatchesStayUncertain(t *testing.T) {
	f := newFixture(t, 1)
	f.forge.script = []createResult{{create: true, err: errors.New("bad gateway")}}
	f.step()
	first := f.forge.issues[0]
	dup := first
	dup.ID, dup.Number = 2002, 2
	f.forge.issues = append(f.forge.issues, dup)
	f.clock.t = f.db.item("item-1").AttemptAt.Add(CheckSchedule[0])
	f.step()
	it := f.db.item("item-1")
	if it.State != store.BrainstormItemUncertain || !strings.Contains(it.Reason, "several") || !strings.Contains(it.Reason, "#2") {
		t.Fatalf("item = %+v", it)
	}
	f.run(16 * time.Minute)
	if got := f.state("item-1"); got != store.BrainstormItemUncertainExhausted || len(f.forge.creates) != 1 {
		t.Fatalf("state=%q creates=%d", got, len(f.forge.creates))
	}
}

func TestDifferingMarkerMatchStaysUncertain(t *testing.T) {
	f := newFixture(t, 1)
	f.forge.script = []createResult{{create: true, err: errors.New("bad gateway")}}
	f.step()
	f.forge.issues[0].Title = "Edited"
	f.clock.t = f.db.item("item-1").AttemptAt.Add(CheckSchedule[0])
	f.step()
	if it := f.db.item("item-1"); it.State != store.BrainstormItemUncertain || !strings.Contains(it.Reason, "differs") {
		t.Fatalf("item = %+v", it)
	}
}

func TestMismatchingReplyIsUncertain(t *testing.T) {
	f := newFixture(t, 1)
	f.forge.script = []createResult{{create: true, issue: &Issue{ID: 5, Number: 5, Title: "other", AuthorID: bot}}}
	f.step()
	if got := f.state("item-1"); got != store.BrainstormItemUncertain {
		t.Fatalf("state = %q", got)
	}
}

func TestKnownRefusalIsNotCreated(t *testing.T) {
	f := newFixture(t, 1)
	f.forge.script = []createResult{{create: false, err: fmt.Errorf("github: create issue: %w", github.ErrForbidden)}}
	f.step()
	it := f.db.item("item-1")
	if it.State != store.BrainstormItemNotCreated || !strings.Contains(it.Reason, "forbidden") {
		t.Fatalf("item = %+v", it)
	}
	f.step()
	if len(f.forge.creates) != 1 {
		t.Fatalf("creates = %d", len(f.forge.creates))
	}
	if text := f.ann.sent[SummaryKey("batch-1", "c1")]; !strings.Contains(text, "proven not created") {
		t.Fatalf("summary = %q", text)
	}
}

func TestRateLimitWaitsRetryTimeAndRetriesAtMostThreeTimes(t *testing.T) {
	f := newFixture(t, 1)
	limited := &github.RateLimitError{APIError: github.APIError{StatusCode: 429, Err: github.ErrRateLimited}, RetryAfter: 20 * time.Second}
	f.forge.script = []createResult{{err: limited}, {err: limited}}
	f.step()
	if it := f.db.item("item-1"); it.State != store.BrainstormItemPending || it.CreateRetries != 1 || it.NextCheckAt != f.clock.t.Add(20*time.Second) {
		t.Fatalf("after first refusal: %+v", it)
	}
	f.run(19 * time.Second)
	if len(f.forge.creates) != 1 {
		t.Fatalf("retried before the wait: %d", len(f.forge.creates))
	}
	f.run(2 * time.Second)
	if len(f.forge.creates) != 2 {
		t.Fatalf("second attempt missing: %d", len(f.forge.creates))
	}
	f.run(25 * time.Second)
	if got := f.state("item-1"); got != store.BrainstormItemCreated || len(f.forge.creates) != 3 {
		t.Fatalf("state=%q creates=%d", got, len(f.forge.creates))
	}
	if gap := f.forge.creates[1].Sub(f.forge.creates[0]); gap < 20*time.Second {
		t.Fatalf("retry gap %v", gap)
	}

	g := newFixture(t, 1)
	g.forge.script = []createResult{{err: limited}, {err: limited}, {err: limited}, {err: limited}, {err: limited}}
	g.run(5 * time.Minute)
	if it := g.db.item("item-1"); it.State != store.BrainstormItemNotCreated || len(g.forge.creates) != MaxRateLimitRetries+1 {
		t.Fatalf("item=%+v creates=%d", it, len(g.forge.creates))
	}
}

func TestCrashBetweenBeginAndSaveIsUncertainNeverSecondCreate(t *testing.T) {
	f := newFixture(t, 1)
	f.db.failSaveFrom = store.BrainstormItemCreating
	if err := f.p.Step(context.Background()); err == nil {
		t.Fatal("expected save failure")
	}
	if got := f.state("item-1"); got != store.BrainstormItemCreating {
		t.Fatalf("state = %q", got)
	}
	f.p = &Processor{Store: f.db, Forge: f.forge, Announcer: f.ann, Authority: f.auth, BotUserID: bot, Now: f.clock.now}
	f.step()
	if got := f.state("item-1"); got != store.BrainstormItemUncertain {
		t.Fatalf("restart state = %q", got)
	}
	f.run(time.Minute)
	if it := f.db.item("item-1"); it.State != store.BrainstormItemCreated || len(f.forge.creates) != 1 {
		t.Fatalf("item=%+v creates=%d", it, len(f.forge.creates))
	}
}

func TestPacingOneCreateAtATimeAtLeastOneSecondApart(t *testing.T) {
	f := newFixture(t, 3)
	f.step()
	f.step()
	if len(f.forge.creates) != 1 {
		t.Fatalf("creates in one second = %d", len(f.forge.creates))
	}
	f.run(5 * time.Second)
	if len(f.forge.creates) != 3 {
		t.Fatalf("creates = %d", len(f.forge.creates))
	}
	for i := 1; i < len(f.forge.creates); i++ {
		if f.forge.creates[i].Sub(f.forge.creates[i-1]) < CreateSpacing {
			t.Fatalf("creates %d and %d too close", i-1, i)
		}
	}
}

func TestPartialBatchKeepsCreatedIssuesAndContinuesOnlyTheRest(t *testing.T) {
	f := newFixture(t, 3)
	f.forge.script = []createResult{{create: true}, {err: github.ErrUnprocessable}, {create: true}}
	f.run(5 * time.Second)
	f.startPlan()
	f.run(2 * time.Second)
	if f.state("item-2") != store.BrainstormItemNotCreated || f.db.conts[0].State != "settled" {
		t.Fatalf("first pass: %q %q", f.state("item-2"), f.db.conts[0].State)
	}
	before := []store.BrainstormTaskItem{f.db.item("item-1"), f.db.item("item-3")}
	f.db.continueWith("c2", f.clock.t, "item-2")
	f.run(3 * time.Second)
	if len(f.forge.creates) != 4 {
		t.Fatalf("creates = %d, want 4", len(f.forge.creates))
	}
	if f.state("item-2") != store.BrainstormItemCreated {
		t.Fatalf("item-2 = %q", f.state("item-2"))
	}
	for _, b := range before {
		if a := f.db.item(b.ID); a.IssueNumber != b.IssueNumber || a.IssueID != b.IssueID {
			t.Fatalf("issue identity changed for %s", b.ID)
		}
	}
}

func TestSummaryReplaySendsNothingAndFreshContinuationSendsItsOwn(t *testing.T) {
	f := newFixture(t, 2)
	f.forge.script = []createResult{{create: true}, {err: github.ErrInvalidArgument}}
	f.run(3 * time.Second)
	f.startPlan()
	f.run(2 * time.Second)
	key1 := SummaryKey("batch-1", "c1")
	if len(f.ann.order) != 1 || f.ann.order[0] != key1 {
		t.Fatalf("summaries = %v", f.ann.order)
	}
	if text := f.ann.sent[key1]; !strings.Contains(text, "created and started in Plan (#1)") || !strings.Contains(text, "proven not created") {
		t.Fatalf("summary = %q", text)
	}
	// A replay of the same confirmation maps to the same, already-sent continuation.
	f.db.mu.Lock()
	f.db.batch.State = "running"
	f.db.mu.Unlock()
	f.run(5 * time.Second)
	if len(f.ann.order) != 1 {
		t.Fatalf("replay sent again: %v", f.ann.order)
	}
	f.db.continueWith("c2", f.clock.t, "item-2")
	f.run(3 * time.Second)
	f.startPlan()
	f.run(2 * time.Second)
	key2 := SummaryKey("batch-1", "c2")
	if len(f.ann.order) != 2 || f.ann.order[1] != key2 {
		t.Fatalf("summaries = %v", f.ann.order)
	}
	if text := f.ann.sent[key2]; !strings.Contains(text, "#2") || strings.Contains(text, "Draft 1") {
		t.Fatalf("fresh summary = %q", text)
	}
	if f.db.batch.State != "settled" {
		t.Fatalf("batch = %q", f.db.batch.State)
	}
}

func TestSummaryStaysPendingWhileDiscordIsDown(t *testing.T) {
	f := newFixture(t, 1)
	f.ann.down = true
	f.forge.script = []createResult{{err: github.ErrNotFound}}
	f.run(3 * time.Second)
	if c := f.db.conts[0]; c.State != "settled" || c.SummaryState != SummaryPending || f.db.batch.State != "running" {
		t.Fatalf("cont=%+v batch=%q", c, f.db.batch.State)
	}
	f.ann.down = false
	f.run(2 * time.Second)
	if c := f.db.conts[0]; c.SummaryState != SummarySent || f.db.batch.State != "settled" {
		t.Fatalf("cont=%+v batch=%q", c, f.db.batch.State)
	}
}

func TestRevokedAuthorityStartsNoWriteAndReportsNotAttempted(t *testing.T) {
	f := newFixture(t, 2)
	f.step()
	f.auth.revoked["owner-action:c1"] = true
	f.run(3 * time.Second)
	if len(f.forge.creates) != 1 || f.state("item-2") != store.BrainstormItemPending {
		t.Fatalf("creates=%d item-2=%q", len(f.forge.creates), f.state("item-2"))
	}
	f.startPlan()
	f.run(2 * time.Second)
	if text := f.ann.sent[SummaryKey("batch-1", "c1")]; !strings.Contains(text, "not yet attempted") {
		t.Fatalf("summary = %q", text)
	}
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

type secondCallRevoked struct{ calls *int }

func (a secondCallRevoked) Allowed(context.Context, string) (bool, error) {
	*a.calls++
	return *a.calls < 2, nil
}

func TestEffectCheckAfterBeginRefusesWriteAndKeepsItemPending(t *testing.T) {
	f := newFixture(t, 1)
	calls := 0
	f.p.Authority = secondCallRevoked{calls: &calls}
	f.step()
	if it := f.db.item("item-1"); it.State != store.BrainstormItemPending || it.Reason != reasonStopped || len(f.forge.creates) != 0 {
		t.Fatalf("item=%+v creates=%d", it, len(f.forge.creates))
	}
}

func TestAdmittedLaunchThatNeverStartedSettlesAsNotStarted(t *testing.T) {
	f := newFixture(t, 1)
	f.run(3 * time.Second)
	f.db.setState("item-1", store.BrainstormItemPlanStarting)
	f.run(2 * time.Second)
	if c := f.db.conts[0]; c.State != "running" || len(f.ann.order) != 0 {
		t.Fatalf("waiting launch settled: %+v %v", c, f.ann.order)
	}
	// The launch step ends the bounded wait without counting a failed attempt.
	f.db.setState("item-1", store.BrainstormItemPlanStartFailed)
	f.run(2 * time.Second)
	c := f.db.conts[0]
	if c.State != "settled" || c.SummaryState != SummarySent || f.db.batch.State != "settled" || len(f.ann.order) != 1 {
		t.Fatalf("cont=%+v batch=%q summaries=%v", c, f.db.batch.State, f.ann.order)
	}
	if o, ok := outcome(f.db.item("item-1"), c.Items[0], false); !ok || o != outcomeNotStarted {
		t.Fatalf("outcome = %q %v", o, ok)
	}
}
